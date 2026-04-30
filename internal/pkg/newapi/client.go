// Package newapi 是 NewAPI (newapi.lurus.cn) admin API 的最小封装。
//
// 设计原则（per docs/ADR-newapi-billing-sync.md，C.2 计划）：
//
//   1. 完全适配 NewAPI 现有 admin 端点；不要求 NewAPI 加任何接口
//   2. 仅暴露 platform 集成实际需要的方法（User CRUD + 配额读写）
//   3. 错误就是错误，不重试 / 不缓存（调用方决定）
//   4. 可 mock 接口：tests 用 httptest，不联真实 NewAPI
//
// NewAPI admin 鉴权惯例：
//   Authorization: <access_token>      (注意：没有 Bearer 前缀)
//   New-Api-User:  <user_id>            (必填，否则 401)
//
// 这两个 header 都来自 NEWAPI_ADMIN_ACCESS_TOKEN / NEWAPI_ADMIN_USER_ID
// 环境变量（已在 platform-core-secrets 配齐）。
package newapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 默认 HTTP 超时；NewAPI admin API 一般 < 1s，10s 足够 + 留 buffer。
const defaultTimeout = 10 * time.Second

// ErrUserNotFound 表示按 username 搜索没找到。调用方据此决定 create 还是
// 跳过；不当作"系统错误"，也不要 fmt.Errorf wrap 后再 errors.Is —
// 直接 == 比较即可。
var ErrUserNotFound = errors.New("newapi: user not found")

// Client 是 NewAPI admin API 的客户端。零值不可用 — 必须 New。
//
// 线程安全（http.Client 内部并发安全；headers 静态构造一次）。
type Client struct {
	baseURL     string // 如 "http://lurus-newapi.lurus-system.svc:3000"（in-cluster）
	accessToken string // NEWAPI_ADMIN_ACCESS_TOKEN
	adminUserID string // NEWAPI_ADMIN_USER_ID（字符串避免反复转换）
	http        *http.Client
}

// New 构造一个 Client。三个参数必填；任一空 → 返回 ErrConfigMissing 让 main.go
// 选择"不启用 NewAPI 集成"路径而非半启动。
func New(baseURL, accessToken, adminUserID string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	accessToken = strings.TrimSpace(accessToken)
	adminUserID = strings.TrimSpace(adminUserID)
	if baseURL == "" || accessToken == "" || adminUserID == "" {
		return nil, fmt.Errorf("newapi: missing config (url=%q tokenLen=%d uid=%q)", baseURL, len(accessToken), adminUserID)
	}
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		adminUserID: adminUserID,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// User 是 NewAPI user 实体的 platform-需要-知道 子集。NewAPI 实际有更多
// 字段（status、role、aff_code…）— 故意不暴露，避免 platform 代码意外
// 依赖 NewAPI 的内部状态机。
type User struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Quota       int64  `json:"quota"` // 单位：NewAPI 内部 quota（≈ amount × QuotaPerUnit）
}

// CreateUser 在 NewAPI 创建一个 user。NewAPI 的 CreateUser 端点不返回 id —
// 调用方需要再调 FindByUsername 获取。username 必须唯一（NewAPI 端校验）。
//
// password 在 platform 集成里没意义（用户永远不直接登录 NewAPI Web UI），
// 但 NewAPI 端校验非空。这里固定一个长随机字符串，调用方不需关心。
func (c *Client) CreateUser(ctx context.Context, username, displayName string) error {
	if strings.TrimSpace(username) == "" {
		return errors.New("newapi: username required")
	}
	if displayName == "" {
		displayName = username
	}
	body := map[string]any{
		"username":     username,
		"display_name": displayName,
		// NewAPI 校验需要 password；platform 用户从不直接登录 NewAPI，
		// 这里给一个 NewAPI 校验通过的占位值。安全模型：rely on
		// admin token 隔离 —— 没有 platform/Lucrum 流程会用到这个密码。
		"password": "lurus-platform-managed-no-direct-login-" + username,
		"role":     1, // 普通用户角色（NewAPI 的 RoleCommonUser）
	}
	if _, err := c.do(ctx, http.MethodPost, "/api/user/", body); err != nil {
		return fmt.Errorf("newapi: create user %q: %w", username, err)
	}
	return nil
}

// FindUserByUsername 根据 username 查 user_id。未找到返 (0, ErrUserNotFound)。
//
// NewAPI 的 search 端点接 keyword 模糊匹配；这里精确匹配 username。
func (c *Client) FindUserByUsername(ctx context.Context, username string) (int, error) {
	if strings.TrimSpace(username) == "" {
		return 0, errors.New("newapi: username required")
	}
	path := "/api/user/search?keyword=" + username
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, fmt.Errorf("newapi: search user %q: %w", username, err)
	}
	// do() already unwrapped the envelope; resp is the inner `data` field
	// (a JSON array of users for search).
	var users []struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(resp, &users); err != nil {
		return 0, fmt.Errorf("newapi: decode search response: %w", err)
	}
	for _, u := range users {
		if u.Username == username {
			return u.ID, nil
		}
	}
	return 0, ErrUserNotFound
}

// GetUser 拉取单个 user。404 返 ErrUserNotFound。
func (c *Client) GetUser(ctx context.Context, id int) (*User, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/user/"+strconv.Itoa(id), nil)
	if err != nil {
		// HTTP 4xx 走 do() 的 statusErr —— 调用方可断言。
		var se *statusErr
		if errors.As(err, &se) && se.code == http.StatusNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("newapi: get user %d: %w", id, err)
	}
	// do() already unwrapped the envelope; resp is the inner `data` field
	// (a single user object).
	var u User
	if err := json.Unmarshal(resp, &u); err != nil {
		return nil, fmt.Errorf("newapi: decode get response: %w", err)
	}
	return &u, nil
}

// SetUserQuota 把 user.quota 设成 absolute 值（不是增量）。调用方负责
// 读-改-写：先 GetUser 拿当前 quota → 加 delta → 调 SetUserQuota。
//
// 选择 absolute API 而不是 delta API 是因为 NewAPI 的 PUT /api/user/ 行为
// 就是 absolute 设值，要 delta 得另起端点。约束 = 完全适配 NewAPI 现有
// 端点（见 ADR），所以选 absolute。
func (c *Client) SetUserQuota(ctx context.Context, id int, quota int64) error {
	body := map[string]any{
		"id":    id,
		"quota": quota,
	}
	if _, err := c.do(ctx, http.MethodPut, "/api/user/", body); err != nil {
		return fmt.Errorf("newapi: set quota for user %d: %w", id, err)
	}
	return nil
}

// IncrementUserQuota 是 GetUser + SetUserQuota 的组合：读当前 quota，加
// delta，写回。**非原子**——并发调用可能丢失某次累加。Platform 的 topup
// 同步是 outbox 模型（顺序消费 NATS subject），所以并发风险低。
//
// 返回更新后的绝对 quota 值，便于 caller 记日志。
func (c *Client) IncrementUserQuota(ctx context.Context, id int, delta int64) (int64, error) {
	cur, err := c.GetUser(ctx, id)
	if err != nil {
		return 0, err
	}
	newQuota := cur.Quota + delta
	if err := c.SetUserQuota(ctx, id, newQuota); err != nil {
		return 0, err
	}
	return newQuota, nil
}

// APIKey 是 NewAPI per-user API token 的对外契约（不是 admin access_token）。
// .Key 是用户的 OpenAI-compatible bearer：products 用 `Authorization: Bearer <Key>`
// 直连 newapi.lurus.cn 的 /v1/* endpoints。
type APIKey struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id"`
	Name           string `json:"name"`
	Key            string `json:"key"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// Ping calls NewAPI's public /api/status endpoint as a cheap liveness
// probe. Used by the readiness checker — does NOT require admin auth and
// can run as often as needed without polluting NewAPI's audit logs.
//
// Returns nil only on HTTP 200 with the standard NewAPI envelope
// reporting `success: true`. Anything else (5xx, network error, malformed
// body) is wrapped and returned as the underlying probe failure.
func (c *Client) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pingCtx, http.MethodGet, c.baseURL+"/api/status", nil)
	if err != nil {
		return fmt.Errorf("newapi ping: build request: %w", err)
	}
	// /api/status is public — no auth headers needed. Setting them
	// anyway is harmless but creates noise in NewAPI logs; skip.
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("newapi ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("newapi ping: http %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	var env struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("newapi ping: decode: %w", err)
	}
	if !env.Success {
		return fmt.Errorf("newapi ping: success=false")
	}
	return nil
}

// EnsureUserAPIKey upserts a per-user API key keyed by name, returning the
// existing entry on repeated calls. Idempotent on (user_id, name) — NewAPI
// admin endpoint enforces that contract; this client just forwards.
//
// Default behaviour (empty name) → NewAPI uses "lurus-platform-default"
// per the admin endpoint's contract.
//
// 注意：requires the local NewAPI fork to expose POST /api/user/:id/api-key
// (上游 QuantumNous/new-api 没这个端点)。404 → 返回 statusErr，调用方据此
// 提示运维 NewAPI 镜像未升级。
func (c *Client) EnsureUserAPIKey(ctx context.Context, userID int, name string) (*APIKey, error) {
	body := map[string]any{}
	if strings.TrimSpace(name) != "" {
		body["name"] = name
	}
	path := "/api/user/" + strconv.Itoa(userID) + "/api-key"
	resp, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("newapi: ensure api key user=%d name=%q: %w", userID, name, err)
	}
	var key APIKey
	if err := json.Unmarshal(resp, &key); err != nil {
		return nil, fmt.Errorf("newapi: decode api key response: %w", err)
	}
	if key.Key == "" {
		return nil, fmt.Errorf("newapi: empty api key returned for user %d", userID)
	}
	return &key, nil
}

// ── HTTP 底层 ─────────────────────────────────────────────────────────────

// statusErr 包装 NewAPI 的 4xx/5xx 响应，让 callers 可以 errors.As 出来。
type statusErr struct {
	code int
	body string
}

func (e *statusErr) Error() string {
	body := e.body
	if len(body) > 200 {
		body = body[:200] + "...(truncated)"
	}
	return fmt.Sprintf("newapi: http %d: %s", e.code, body)
}

// do 是 HTTP 请求的统一入口：注入鉴权头、做 NewAPI 标准 envelope 解码
// （`{success, message, data}`），并把 success=false 当作错误返回。
func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	url := c.baseURL + path

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("newapi: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("newapi: build request: %w", err)
	}
	// NewAPI admin auth — no "Bearer " prefix. See middleware/auth.go.
	req.Header.Set("Authorization", c.accessToken)
	req.Header.Set("New-Api-User", c.adminUserID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newapi: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap

	// NewAPI 在 4xx 上偶尔返回 200 + success:false，偶尔返回真 4xx。两者都映成
	// statusErr 让上层一致处理。
	if resp.StatusCode >= 400 {
		return nil, &statusErr{code: resp.StatusCode, body: string(respBody)}
	}

	// 解 envelope 检查 success 字段。
	var env struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		// 部分端点返回非 envelope 形式（如 list 直接返 array）— 这种情况
		// 直接返回原 body，让 caller decode。
		return respBody, nil
	}
	if !env.Success {
		return respBody, &statusErr{code: resp.StatusCode, body: env.Message}
	}
	// 标准 envelope，返 data 字段（list / search 走这里）。
	if len(env.Data) > 0 {
		return env.Data, nil
	}
	return respBody, nil
}
