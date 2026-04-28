// Package identity_admin封装 Zitadel 的身份模型，让 Lurus 平台的运维人员
// 永远不需要进 Zitadel console。
//
// 核心抽象：
//
//	APIKey         — 平台层的"应用密钥"概念，对应 Zitadel 的 Service User + PAT
//	Service.Create — 幂等创建（同名二次调用返回已有 row，不重复消耗 Zitadel 资源）
//	Service.Rotate — 原子换 token（撤销旧 PAT → 生成新 PAT → 更新 token_hash）
//	Service.Revoke — 删除 Zitadel 端用户（cascade PAT），DB 行保留作审计
//
// 设计原则：
//
//	幂等性 — 相同输入二次调用得到相同最终状态；网络重试安全
//	可靠性 — Zitadel 任一调用失败时，DB 行翻成 'failed' + 清理孤儿 user
//	准确性 — 不缓存 token；token 仅 Create/Rotate 返回一次
//	性能   — 创建低频不优化；List 走 DB（管理面板小规模，几十~几百行）
package identity_admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// 错误哨兵 — handler 据此映射 HTTP 状态码。
var (
	// ErrAPIKeyExists 表示 name 已被一个 active 状态的 key 占用。
	// 调用方处理：返回 409 + 现有 key 的元信息（不返回 token，token 只能 Create 返回一次）。
	ErrAPIKeyExists = errors.New("identity_admin: api key with this name already exists and is active")

	// ErrAPIKeyCreating 表示同名 key 正在创建中（status='creating'）。
	// 通常意味着上一次调用尚未完成或卡死。Service 层不会自动恢复 — handler 返回 409
	// 让运维者人工 Revoke 后再试，避免双重创建。
	ErrAPIKeyCreating = errors.New("identity_admin: api key creation in progress; revoke and retry if stuck")

	// ErrAPIKeyNotFound = repo.ErrAPIKeyNotFound 的别名，简化 handler 引用。
	ErrAPIKeyNotFound = repo.ErrAPIKeyNotFound

	// ErrInvalidName / ErrInvalidPurpose 是输入校验错误 — handler 返回 400。
	ErrInvalidName    = errors.New("identity_admin: invalid name (3-64 chars, lowercase alnum/dash/underscore)")
	ErrInvalidPurpose = errors.New("identity_admin: invalid purpose (must be one of login_ui|mcp|external|admin)")
)

// ZitadelClient 描述 Service 需要 Zitadel 提供的最小能力。
// 接口只有四个方法，让 service_test 用 mock 完全替换。
type ZitadelClient interface {
	CreateMachineUser(ctx context.Context, username, displayName, description string) (string, error)
	CreatePAT(ctx context.Context, userID string, expiresAt time.Time) (tokenID, token string, err error)
	DeletePAT(ctx context.Context, userID, tokenID string) error
	DeleteUser(ctx context.Context, userID string) error
}

// Store 是 service 对 repo 的依赖抽象 — 测试时可换 in-mem 实现。
// 与 repo.APIKeyRepo 同构，但只暴露 service 实际用到的子集，避免泄漏 GORM。
type Store interface {
	FindByName(ctx context.Context, name string) (*entity.APIKey, error)
	FindByID(ctx context.Context, id int64) (*entity.APIKey, error)
	Create(ctx context.Context, k *entity.APIKey) error
	MarkActive(ctx context.Context, id int64, zitadelUserID, zitadelTokenID, tokenHash string) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
	MarkRevoked(ctx context.Context, id int64) error
	UpdateToken(ctx context.Context, id int64, zitadelTokenID, tokenHash string) error
	Reincarnate(ctx context.Context, id int64, displayName, purpose string, expiresAt *time.Time, createdBy *int64) error
	List(ctx context.Context, purpose, status string, limit, offset int) ([]entity.APIKey, int64, error)
}

// CreateRequest 是 Create 的入参 DTO。
type CreateRequest struct {
	Name           string // 幂等键 — 3-64 chars, 小写字母数字 + dash/underscore
	DisplayName    string
	Purpose        string // login_ui | mcp | external | admin
	ExpirationDays int    // 0 = never expires
	CreatedBy      *int64
}

// CreatedAPIKey 是 Create / Rotate 返回值。Token 字段只在该 API 返回时被填充，
// DB 永远不存明文 — token_hash 仅作 "曾创建过该 token" 的审计指针。
type CreatedAPIKey struct {
	APIKey entity.APIKey `json:"api_key"`
	Token  string        `json:"token"`
}

// Service 是模块对外的入口。所有方法都是幂等且可重试的。
type Service struct {
	store   Store
	zitadel ZitadelClient
	logger  *slog.Logger
}

// NewService 构造一个 Service。logger 可为 nil（默认 slog.Default()）。
func NewService(store Store, zitadel ZitadelClient, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, zitadel: zitadel, logger: logger}
}

// nameRegex 强制 name 限定为 [a-z0-9_-]{3,64} —— Zitadel username 也按此规则校验，
// 避免在 Zitadel 端报 400 时再回滚 DB row。
var nameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)

// validPurposes 维护允许的 purpose 列表。新增 purpose 不需 DB 迁移，只改这里。
var validPurposes = map[string]bool{
	entity.APIKeyPurposeLoginUI:  true,
	entity.APIKeyPurposeMCP:      true,
	entity.APIKeyPurposeExternal: true,
	entity.APIKeyPurposeAdmin:    true,
}

// Create 是核心幂等操作。流程：
//
//  1. 校验输入。
//  2. 按 name 查 DB。
//     2a. 不存在 → 跳到 4。
//     2b. status=active → 返回 ErrAPIKeyExists（不返回 token；要换 token 请用 Rotate）。
//     2c. status=creating → 返回 ErrAPIKeyCreating（防双发）。
//     2d. status=failed/revoked → 调 Reincarnate 复活该 row（保 id 与 audit history）→ 跳到 5。
//  3.（不存在）创建新 row（status=creating）。
//  4. 调 Zitadel CreateMachineUser → 拿 userID。
//  5. 调 Zitadel CreatePAT → 拿 tokenID + token。
//  6. MarkActive 写入 userID + tokenID + tokenHash。
//  7. 返回 token（仅这一次）。
//
// 任一 Zitadel 调用失败 → MarkFailed 写错误 → 调用 cleanup → 返回 error。
// 下次以同 name 调用 Create 会触发 2d 路径自动重试。
func (s *Service) Create(ctx context.Context, req CreateRequest) (*CreatedAPIKey, error) {
	if !nameRegex.MatchString(req.Name) {
		return nil, ErrInvalidName
	}
	if req.DisplayName == "" || len(req.DisplayName) > 128 {
		return nil, fmt.Errorf("identity_admin: display_name must be 1-128 chars")
	}
	if !validPurposes[req.Purpose] {
		return nil, ErrInvalidPurpose
	}
	if req.ExpirationDays < 0 || req.ExpirationDays > 3650 {
		return nil, fmt.Errorf("identity_admin: expiration_days must be 0-3650")
	}

	expiresAt := computeExpiresAt(req.ExpirationDays)

	existing, err := s.store.FindByName(ctx, req.Name)
	if err != nil && !errors.Is(err, ErrAPIKeyNotFound) {
		return nil, fmt.Errorf("identity_admin: lookup existing: %w", err)
	}

	var rowID int64
	switch {
	case existing == nil:
		row := &entity.APIKey{
			Name:        req.Name,
			DisplayName: req.DisplayName,
			Purpose:     req.Purpose,
			Status:      entity.APIKeyStatusCreating,
			ExpiresAt:   expiresAt,
			CreatedBy:   req.CreatedBy,
		}
		if err := s.store.Create(ctx, row); err != nil {
			return nil, fmt.Errorf("identity_admin: insert row: %w", err)
		}
		rowID = row.ID

	case existing.Status == entity.APIKeyStatusActive:
		return &CreatedAPIKey{APIKey: *existing}, ErrAPIKeyExists

	case existing.Status == entity.APIKeyStatusCreating:
		return nil, ErrAPIKeyCreating

	default: // failed | revoked → reincarnate
		if err := s.store.Reincarnate(ctx, existing.ID, req.DisplayName, req.Purpose, expiresAt, req.CreatedBy); err != nil {
			return nil, fmt.Errorf("identity_admin: reincarnate row: %w", err)
		}
		// 复活前如果 Zitadel side 还有孤儿 user (从 failed 状态来)，删除它。
		// revoked 状态的 row 通常 Zitadel user 已被删，DeleteUser 对 404 返回成功，幂等。
		if existing.ZitadelUserID != "" {
			if delErr := s.zitadel.DeleteUser(ctx, existing.ZitadelUserID); delErr != nil {
				// 删孤儿用户失败是 non-fatal — 记一行 warn 继续，
				// 因为它可能确实已经不存在或权限不足，不该阻塞复活流程。
				s.logger.WarnContext(ctx, "identity_admin: orphan zitadel user cleanup failed",
					"name", req.Name, "zitadel_user_id", existing.ZitadelUserID, "err", delErr)
			}
		}
		rowID = existing.ID
	}

	// Zitadel 端创建。失败时立刻 MarkFailed + 清理。
	zitadelUserID, err := s.zitadel.CreateMachineUser(ctx, req.Name, req.DisplayName,
		fmt.Sprintf("Lurus API key (purpose=%s)", req.Purpose))
	if err != nil {
		_ = s.store.MarkFailed(ctx, rowID, fmt.Sprintf("create machine user: %v", err))
		return nil, fmt.Errorf("identity_admin: create zitadel machine user: %w", err)
	}

	tokenExpires := time.Time{}
	if expiresAt != nil {
		tokenExpires = *expiresAt
	}
	tokenID, token, err := s.zitadel.CreatePAT(ctx, zitadelUserID, tokenExpires)
	if err != nil {
		_ = s.store.MarkFailed(ctx, rowID, fmt.Sprintf("create PAT: %v", err))
		// PAT 失败 → 删除孤儿 machine user 防累积。
		if delErr := s.zitadel.DeleteUser(ctx, zitadelUserID); delErr != nil {
			s.logger.WarnContext(ctx, "identity_admin: cleanup machine user after PAT failure",
				"name", req.Name, "zitadel_user_id", zitadelUserID, "err", delErr)
		}
		return nil, fmt.Errorf("identity_admin: create PAT: %w", err)
	}

	if err := s.store.MarkActive(ctx, rowID, zitadelUserID, tokenID, hashToken(token)); err != nil {
		// DB 写失败但 Zitadel 已建好 — 这是最尴尬的情况。返回 token 给 caller，
		// 但 row 状态会停在 'creating'。下次同 name Create 会走 2c 报 ErrAPIKeyCreating，
		// 需运维 Revoke + 重建。极罕见（DB 故障），日志告警就够。
		s.logger.ErrorContext(ctx, "identity_admin: mark active after zitadel ok — row stuck in creating",
			"name", req.Name, "row_id", rowID, "zitadel_user_id", zitadelUserID, "err", err)
		return nil, fmt.Errorf("identity_admin: mark active: %w", err)
	}

	row, err := s.store.FindByID(ctx, rowID)
	if err != nil {
		// 极罕见：刚 MarkActive 立刻消失。仍返回 token 让 caller 不丢凭据。
		row = &entity.APIKey{ID: rowID, Name: req.Name, DisplayName: req.DisplayName, Purpose: req.Purpose, Status: entity.APIKeyStatusActive}
	}
	s.logger.InfoContext(ctx, "identity_admin: api key created",
		"name", req.Name, "purpose", req.Purpose, "row_id", rowID, "zitadel_user_id", zitadelUserID)
	return &CreatedAPIKey{APIKey: *row, Token: token}, nil
}

// Rotate 原子换 token：撤销旧 PAT → 生成新 PAT → 更新 token_hash。
// 注意：旧 PAT 调 DeletePAT 失败（404 已被外部撤销）也走 happy path，是幂等的。
func (s *Service) Rotate(ctx context.Context, name string) (*CreatedAPIKey, error) {
	row, err := s.store.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if row.Status != entity.APIKeyStatusActive {
		return nil, fmt.Errorf("identity_admin: cannot rotate key in %s state", row.Status)
	}

	// 撤销旧 PAT。404 也算成功（已被外部撤销）。
	if row.ZitadelTokenID != "" {
		if err := s.zitadel.DeletePAT(ctx, row.ZitadelUserID, row.ZitadelTokenID); err != nil {
			s.logger.WarnContext(ctx, "identity_admin: rotate — delete old PAT failed (continuing)",
				"name", name, "err", err)
		}
	}

	expires := time.Time{}
	if row.ExpiresAt != nil {
		expires = *row.ExpiresAt
	}
	newTokenID, newToken, err := s.zitadel.CreatePAT(ctx, row.ZitadelUserID, expires)
	if err != nil {
		return nil, fmt.Errorf("identity_admin: rotate — create new PAT: %w", err)
	}

	if err := s.store.UpdateToken(ctx, row.ID, newTokenID, hashToken(newToken)); err != nil {
		// DB 写失败但新 PAT 已生成 — 返给 caller，row 端 token_id 不一致是次要问题
		// （rotate 是手动操作，运维能看到日志）。
		s.logger.ErrorContext(ctx, "identity_admin: rotate — update token id failed",
			"name", name, "err", err)
	}

	updated, err := s.store.FindByID(ctx, row.ID)
	if err != nil {
		updated = row
	}
	s.logger.InfoContext(ctx, "identity_admin: api key rotated",
		"name", name, "row_id", row.ID)
	return &CreatedAPIKey{APIKey: *updated, Token: newToken}, nil
}

// Revoke 撤销 key。删 Zitadel User（cascade PAT）→ MarkRevoked。
// Zitadel 删除返 404 也算成功（已不存在），整体幂等。
func (s *Service) Revoke(ctx context.Context, name string) error {
	row, err := s.store.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if row.Status == entity.APIKeyStatusRevoked {
		return nil // 幂等
	}
	if row.ZitadelUserID != "" {
		if err := s.zitadel.DeleteUser(ctx, row.ZitadelUserID); err != nil {
			return fmt.Errorf("identity_admin: revoke — delete zitadel user: %w", err)
		}
	}
	if err := s.store.MarkRevoked(ctx, row.ID); err != nil {
		return fmt.Errorf("identity_admin: revoke — mark row: %w", err)
	}
	s.logger.InfoContext(ctx, "identity_admin: api key revoked",
		"name", name, "row_id", row.ID)
	return nil
}

// List 列出 API keys。purpose 为空 = 全部 purpose；status 为空 = 排除 revoked。
func (s *Service) List(ctx context.Context, purpose, status string, limit, offset int) ([]entity.APIKey, int64, error) {
	return s.store.List(ctx, purpose, status, limit, offset)
}

// hashToken 是 SHA256 hex — 用于审计指针，不是密码学防御（PAT 本身已是
// 长随机字符串）。存 hash 而非明文是为了万一 DB 泄漏时不直接给攻击者
// 一把可用的 token。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// computeExpiresAt 把 ExpirationDays 转成绝对时间戳。0 = 永不过期 (nil)。
func computeExpiresAt(days int) *time.Time {
	if days <= 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return &t
}
