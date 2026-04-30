// Package newapi_sync 把 platform account 与 NewAPI user 的 1:1 映射建起来
// 并保持。订阅 module Registry 的 OnAccountCreated 钩子，新 account 落地时
// 在 NewAPI 端建对应 user，把返回的 user_id 写回 accounts.newapi_user_id。
//
// 这是 C.2 计费同步集成的"建模"环节（见 docs/ADR-newapi-billing-sync.md）。
// 充值同步是另一个钩子（见 4d）。
//
// 设计原则：
//
//   1. 失败不阻塞主流程 — module Registry 的 hook 失败仅记日志（FireAccountCreated
//      包了 try/catch），所以 NewAPI 抖动不会让用户登录失败
//   2. 幂等 — 重复触发同一 account 是 no-op；FindByUsername-then-Create
//      避免重复建用户
//   3. nil-safe — client 或 store 为 nil 时整个模块停用，**不**报错；让 ops
//      可以零配置部署 platform 而不需要 NewAPI（dev、单元测试场景）
package newapi_sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/module"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/newapi"
)

// AccountStore 是模块对 platform account 仓储的最小依赖。
//
// 故意不引整个 accountStore 接口 — 模块只需要"写映射 ID"和"按 ID 取 account"
// 两个能力。让单元测试可以用一个超薄的 fake 替代，不需要 mock 整套 CRUD。
type AccountStore interface {
	SetNewAPIUserID(ctx context.Context, accountID int64, newapiUserID int) error
	GetByID(ctx context.Context, id int64) (*entity.Account, error)
}

// NewAPIClient 是 NewAPI admin 客户端契约 — 形状和 *newapi.Client 一致，但
// 用接口接收让测试可以 mock。
type NewAPIClient interface {
	CreateUser(ctx context.Context, username, displayName string) error
	FindUserByUsername(ctx context.Context, username string) (int, error)
	IncrementUserQuota(ctx context.Context, id int, delta int64) (int64, error)
	EnsureUserAPIKey(ctx context.Context, userID int, name string) (*newapi.APIKey, error)
}

// LLMToken is the per-product LLM credential the platform hands out via
// /api/v1/account/me/llm-token. Stable wire shape — products consume `Key`
// as `Authorization: Bearer <key>` against newapi.lurus.cn.
type LLMToken struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// DefaultTokenName is the per-account default key name. Each platform account
// gets one such key; products can request scoped keys later via name=...
// (extension point — current callers only use the default).
const DefaultTokenName = "lurus-platform-default"

// QuotaPerUnit 把"1 CNY"换算为 NewAPI quota 单位。NewAPI 上游硬编码
// `common.QuotaPerUnit = 500000` — 即 ¥1 = 500_000 quota。我们在 platform 侧
// 复制一份这个常量而不是 import NewAPI 包，因为 NewAPI 是一个独立服务的 fork
// 不该被 platform 直接依赖；如果上游改了，对账 cron (4g) 会暴露差异。
const QuotaPerUnit = 500_000

// Module 是 newapi_sync 模块。零值不可用 — 必须通过 New 构造。
type Module struct {
	client   NewAPIClient
	accounts AccountStore
}

// New 构造 module。client 或 accounts 为 nil 时返 nil — main.go 据此决定是否
// Register 进 module.Registry。返回 nil 而非"半启用 module"是为了避免运行时
// 静默 swallow 配置缺失。
func New(client NewAPIClient, accounts AccountStore) *Module {
	if client == nil || accounts == nil {
		return nil
	}
	return &Module{client: client, accounts: accounts}
}

// usernameFor 返回 NewAPI 端使用的 username。约定见 ADR：
//
//	platform.account_id = 42  →  newapi.username = "lurus_42"
//
// 唯一、稳定、不暴露用户隐私（不用 email/phone）。
func usernameFor(accountID int64) string {
	return fmt.Sprintf("lurus_%d", accountID)
}

// OnAccountCreated 是 module.AccountHook 实现：新 account 创建时建对应 NewAPI
// user 并把返回 id 写回 accounts.newapi_user_id。
//
// 幂等流程：
//
//  1. 已经同步过 (account.NewAPIUserID != nil) → 立即返回 nil
//  2. 按 username 搜 NewAPI — 找到说明上次创建成功但回写失败 → 跳到 4
//  3. 否则调 Create + 再 search 取 id
//  4. SetNewAPIUserID 持久化映射
//
// 任一 NewAPI 调用失败 → 立即返错（registry 会记 warn，不阻塞调用方）。
func (m *Module) OnAccountCreated(ctx context.Context, account *entity.Account) error {
	if account == nil {
		return errors.New("newapi_sync: nil account")
	}
	if account.NewAPIUserID != nil {
		// Already synced — repeat-trigger is a no-op (idempotent).
		return nil
	}
	username := usernameFor(account.ID)

	// 先 search：上次 Create 成功但回写 SetNewAPIUserID 失败时这条路径
	// 会让我们幂等地拿到 id 而不重复建用户。
	id, err := m.client.FindUserByUsername(ctx, username)
	if errors.Is(err, newapi.ErrUserNotFound) {
		displayName := account.DisplayName
		if displayName == "" {
			displayName = username // NewAPI 校验非空
		}
		if cerr := m.client.CreateUser(ctx, username, displayName); cerr != nil {
			return fmt.Errorf("newapi_sync: create user %s: %w", username, cerr)
		}
		// NewAPI CreateUser 不返 id — 再 search 一次。
		id, err = m.client.FindUserByUsername(ctx, username)
		if err != nil {
			return fmt.Errorf("newapi_sync: post-create lookup %s: %w", username, err)
		}
	} else if err != nil {
		return fmt.Errorf("newapi_sync: find user %s: %w", username, err)
	}

	if err := m.accounts.SetNewAPIUserID(ctx, account.ID, id); err != nil {
		// NewAPI user 已经存在但 platform 没记下来 — 下次 hook 触发会走"先 search
		// 再 Set"的幂等分支。这里返错让 registry 记 warn 引起注意。
		return fmt.Errorf("newapi_sync: set newapi_user_id account=%d newapi=%d: %w", account.ID, id, err)
	}

	slog.InfoContext(ctx, "newapi_sync: account synced",
		"account_id", account.ID, "newapi_user_id", id, "username", username)
	return nil
}

// Register 把 OnAccountCreated 注册到 module.Registry 上。空 module（New 返
// nil）时不调用，所以 main.go 写法保持简洁：`if mod != nil { mod.Register(r) }`。
func (m *Module) Register(r *module.Registry) {
	if r == nil {
		return
	}
	r.OnAccountCreated(m.OnAccountCreated)
	slog.Info("module registered", "module", "newapi_sync")
}

// EnsureUserLLMToken returns a usable per-account LLM token (idempotent on
// repeated calls — same key comes back every time). Implements C.2 step 4e.
//
// Decoupling: handler layer calls this; module orchestrates account lookup
// + NewAPI admin call. No DB persistence on platform side — NewAPI is the
// source of truth for keys, and its admin endpoint is itself idempotent.
//
// Error shape:
//   - account not found / not yet synced → ErrAccountNotProvisioned
//     (caller maps to 503 + "try again later" so user knows it's transient)
//   - NewAPI down or 5xx → wrapped error (caller maps to 502)
//
// Extensibility: `name` argument lets future callers request product-scoped
// keys ("lucrum", "tally"). DefaultTokenName covers the common case.
func (m *Module) EnsureUserLLMToken(ctx context.Context, accountID int64, name string) (*LLMToken, error) {
	if name == "" {
		name = DefaultTokenName
	}
	account, err := m.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("newapi_sync: lookup account %d: %w", accountID, err)
	}
	if account == nil || account.NewAPIUserID == nil {
		return nil, ErrAccountNotProvisioned
	}
	key, err := m.client.EnsureUserAPIKey(ctx, *account.NewAPIUserID, name)
	if err != nil {
		return nil, fmt.Errorf("newapi_sync: ensure api key for account=%d: %w", accountID, err)
	}
	return &LLMToken{
		Key:            key.Key,
		Name:           key.Name,
		UnlimitedQuota: key.UnlimitedQuota,
	}, nil
}

// ErrAccountNotProvisioned signals "account exists in platform but its
// NewAPI mirror hasn't been created yet" — distinct from NewAPI-side
// failures so callers can show a transient-please-retry state instead
// of a hard error.
var ErrAccountNotProvisioned = fmt.Errorf("newapi_sync: account not yet provisioned in NewAPI")

// OnTopupCompleted 是充值同步钩子（C.2 step 4d）：用户在 platform 钱包成功
// 充值 amountCNY 后调用，把等额 quota 增加到对应的 NewAPI user 上。
//
// 由 NATS consumer 订阅 `identity.topup.completed` subject 后调用（详见
// internal/adapter/nats/consumer.go）。NATS-free 设计让单元测试不需要起 NATS。
//
// 无 NewAPIUserID 映射 → 跳过（账户尚未同步，4g 对账 cron 后会补上）。
// IncrementUserQuota 失败 → 返错给 Consumer 决定 ack/nak，让 JetStream 重试。
//
// **注意**：方法非幂等。NATS JetStream 至少一次投递，理论上重发会双扣（增）。
// 当前规模下罕见、可接受；4g 对账 cron 会暴露差异。生产规模上线前应加 Redis
// SETNX 基于 event_id 去重。
func (m *Module) OnTopupCompleted(ctx context.Context, accountID int64, amountCNY float64) error {
	if amountCNY <= 0 {
		return nil
	}
	account, err := m.accounts.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("newapi_sync: lookup account %d: %w", accountID, err)
	}
	if account == nil || account.NewAPIUserID == nil {
		// 账户不存在或尚未同步 NewAPI — 跳过；对账 cron 会处理。
		slog.InfoContext(ctx, "newapi_sync: skip topup (no newapi mapping)",
			"account_id", accountID, "amount_cny", amountCNY)
		return nil
	}
	delta := int64(amountCNY * QuotaPerUnit)
	newQuota, err := m.client.IncrementUserQuota(ctx, *account.NewAPIUserID, delta)
	if err != nil {
		return fmt.Errorf("newapi_sync: increment quota account=%d delta=%d: %w", accountID, delta, err)
	}
	slog.InfoContext(ctx, "newapi_sync: topup synced",
		"account_id", accountID,
		"newapi_user_id", *account.NewAPIUserID,
		"amount_cny", amountCNY,
		"delta_quota", delta,
		"new_quota", newQuota)
	return nil
}
