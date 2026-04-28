package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/identity_admin"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// APIKeysAdminHandler 暴露 /admin/v1/api-keys/*。
//
// 把 Zitadel 的 Service User + IAM_OWNER + PAT 三步流程封装成
// "建一个 API 密钥" 单步操作，让运维者再也不需要进 Zitadel console。
//
// Service 层负责幂等、可靠、错误恢复；handler 只做：(a) 解析请求，
// (b) 错误码映射，(c) 不把 token 漏到日志。
type APIKeysAdminHandler struct {
	svc *identity_admin.Service
}

// NewAPIKeysAdminHandler 装配。svc 不可为 nil — main.go 会保证 Zitadel
// 已配置时才注入这个 handler，否则路由直接不挂载。
func NewAPIKeysAdminHandler(svc *identity_admin.Service) *APIKeysAdminHandler {
	return &APIKeysAdminHandler{svc: svc}
}

// Compile-time assertion: handler 也是 Op 元数据提供者，让 ops catalog
// 把 create_api_key / rotate_api_key / revoke_api_key 三个 op 暴露给 UI。
var _ ops.Op = (*APIKeysAdminHandler)(nil)

// 把 handler 当成"create_api_key" 这一个 op 的元数据持有者；rotate /
// revoke 两个 op 用 ops.Info 注册即可（见 main.go 装配处）。

// Type 是 ops registry 的主键 — 这里取最高频的 create。
func (h *APIKeysAdminHandler) Type() string { return "create_api_key" }

// Description 显示在管理面板特权操作清单上。
func (h *APIKeysAdminHandler) Description() string {
	return "Create a Lurus API key (provisions a Zitadel Service User + PAT under the hood)"
}

// RiskLevel — 创建是 warn 而非 destructive，因为：
//   - 失败可逆（Reincarnate 路径自动恢复）
//   - 不会破坏现有数据
//   - 但生成的 token 是高权限凭据 → 不能像 info 一样无视
func (h *APIKeysAdminHandler) RiskLevel() ops.RiskLevel { return ops.RiskWarn }

// IsDestructive — false 因为创建操作不删除任何东西。
func (h *APIKeysAdminHandler) IsDestructive() bool { return false }

// ── 请求/响应 DTO（外部 API 形状，与 entity.APIKey 解耦） ───────────────

// createAPIKeyRequest 是 POST /admin/v1/api-keys 的请求体。
//
// 故意只暴露运维真正关心的字段；purpose 用枚举字符串而不是 int，让 curl
// 调试时看上去自然。
type createAPIKeyRequest struct {
	Name           string `json:"name"            binding:"required"`
	DisplayName    string `json:"display_name"    binding:"required"`
	Purpose        string `json:"purpose"         binding:"required"` // login_ui|mcp|external|admin
	ExpirationDays int    `json:"expiration_days"`                    // 0 = 永不过期
}

// createAPIKeyResponse 是创建 / rotate 的响应。token 字段只在这里返回一次。
type createAPIKeyResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Purpose     string `json:"purpose"`
	Status      string `json:"status"`
	Token       string `json:"token,omitempty"`     // ⚠️ 只在 Create / Rotate 返回，且只这一次
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// listAPIKeysResponse 是 GET 的响应。
type listAPIKeysResponse struct {
	APIKeys []apiKeySummary `json:"api_keys"`
	Total   int64           `json:"total"`
}

// apiKeySummary 不含 token / token_hash / zitadel ids — 这些都是内部细节。
type apiKeySummary struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Purpose     string `json:"purpose"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// ── HTTP handlers ────────────────────────────────────────────────────────

// Create — POST /admin/v1/api-keys
//
// 幂等：相同 name 二次调用返回 409 + 现有 row（不返回 token）。
// 想换 token 请用 Rotate endpoint。
func (h *APIKeysAdminHandler) Create(c *gin.Context) {
	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	createdBy := callerAccountID(c)
	out, err := h.svc.Create(c.Request.Context(), identity_admin.CreateRequest{
		Name:           req.Name,
		DisplayName:    req.DisplayName,
		Purpose:        req.Purpose,
		ExpirationDays: req.ExpirationDays,
		CreatedBy:      createdBy,
	})

	switch {
	case errors.Is(err, identity_admin.ErrAPIKeyExists):
		// 幂等：返回现有元信息，但不带 token。前端据此提示运维"已存在，是否 Rotate？"
		resp := mkResponse(out, "")
		c.JSON(http.StatusConflict, resp)
		return
	case errors.Is(err, identity_admin.ErrAPIKeyCreating):
		c.JSON(http.StatusConflict, gin.H{
			"error":   err.Error(),
			"hint":    "another create call is in flight or stuck; revoke and retry if it stays this way >5min",
		})
		return
	case errors.Is(err, identity_admin.ErrInvalidName):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case errors.Is(err, identity_admin.ErrInvalidPurpose):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, mkResponse(out, out.Token))
}

// Rotate — POST /admin/v1/api-keys/:name/rotate
//
// 撤销旧 PAT，签发新 PAT。返回新 token（只这一次）。
func (h *APIKeysAdminHandler) Rotate(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name path param required"})
		return
	}
	out, err := h.svc.Rotate(c.Request.Context(), name)
	switch {
	case errors.Is(err, identity_admin.ErrAPIKeyNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mkResponse(out, out.Token))
}

// Revoke — DELETE /admin/v1/api-keys/:name
//
// 删除 Zitadel User（cascade 撤销 PAT），DB 行翻成 revoked（保审计）。
// 幂等：二次调用直接返回 204。
func (h *APIKeysAdminHandler) Revoke(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name path param required"})
		return
	}
	err := h.svc.Revoke(c.Request.Context(), name)
	switch {
	case errors.Is(err, identity_admin.ErrAPIKeyNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// List — GET /admin/v1/api-keys?purpose=&status=&limit=&offset=
//
// 默认排除 revoked 行；想看历史传 ?status=revoked。
func (h *APIKeysAdminHandler) List(c *gin.Context) {
	purpose := c.Query("purpose")
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	rows, total, err := h.svc.List(c.Request.Context(), purpose, status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	out := make([]apiKeySummary, 0, len(rows))
	for _, r := range rows {
		s := apiKeySummary{
			ID:          r.ID,
			Name:        r.Name,
			DisplayName: r.DisplayName,
			Purpose:     r.Purpose,
			Status:      r.Status,
			Error:       r.Error,
			CreatedAt:   r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if r.ExpiresAt != nil {
			s.ExpiresAt = r.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, s)
	}
	c.JSON(http.StatusOK, listAPIKeysResponse{APIKeys: out, Total: total})
}

// ── 辅助函数 ──────────────────────────────────────────────────────────────

func mkResponse(c *identity_admin.CreatedAPIKey, token string) createAPIKeyResponse {
	if c == nil {
		return createAPIKeyResponse{}
	}
	resp := createAPIKeyResponse{
		ID:          c.APIKey.ID,
		Name:        c.APIKey.Name,
		DisplayName: c.APIKey.DisplayName,
		Purpose:     c.APIKey.Purpose,
		Status:      c.APIKey.Status,
		Token:       token,
	}
	if c.APIKey.ExpiresAt != nil {
		resp.ExpiresAt = c.APIKey.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return resp
}

// callerAccountID 从 Gin context 取出当前管理员账号 id，用于审计 created_by。
// 复用 jwt 中间件挂载的 "account_id" 键（与 platform 其他 admin handler 一致）。
// 失败时返回 nil（表示"未知"，仍允许创建，避免阻塞）。
func callerAccountID(c *gin.Context) *int64 {
	if v, ok := c.Get("account_id"); ok {
		if id, ok := v.(int64); ok {
			return &id
		}
	}
	return nil
}

// EnsureContext lets a smoke test exercise the handler with a real
// context.Context that's not from gin (e.g. lifecycle init). Currently
// unused but documents intent — handlers should never need to fish a
// context out of gin.
func (h *APIKeysAdminHandler) EnsureContext(_ context.Context) {}
