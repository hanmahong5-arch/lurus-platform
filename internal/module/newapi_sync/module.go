// Package newapi_sync 把 platform account 与 NewAPI user 的 1:1 映射建起来
// 并保持。订阅 module Registry 的 OnAccountCreated 钩子，新 account 落地时
// 在 NewAPI 端建对应 user，把返回的 user_id 写回 accounts.newapi_user_id。
//
// 这是 C.2 计费同步集成的"建模"环节（见 docs/ADR-newapi-billing-sync.md）。
// 充值同步是另一个钩子（见 4d）。
//
// 设计原则：
//
//  1. 失败不阻塞主流程 — module Registry 的 hook 失败仅记日志（FireAccountCreated
//     包了 try/catch），所以 NewAPI 抖动不会让用户登录失败
//  2. 幂等 — 重复触发同一 account 是 no-op；FindByUsername-then-Create
//     避免重复建用户
//  3. nil-safe — client 或 store 为 nil 时整个模块停用，**不**报错；让 ops
//     可以零配置部署 platform 而不需要 NewAPI（dev、单元测试场景）
package newapi_sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/module"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/newapi"
)

// Metric op + result label vocabularies (also documented on
// metrics.RecordNewAPISyncOp). Centralised here so every call site uses
// the same strings — typos break dashboards silently.
const (
	opAccountProvisioned = "account_provisioned"
	opTopupSynced        = "topup_synced"
	opLLMTokenIssued     = "llm_token_issued"

	resultSuccess   = "success"
	resultSkipped   = "skipped"
	resultDuplicate = "duplicate"
	resultError     = "error"
)

// AccountStore 是模块对 platform account 仓储的最小依赖。
//
// 故意不引整个 accountStore 接口 — 模块只需要"写映射 ID"和"按 ID 取 account"
// 两个能力。让单元测试可以用一个超薄的 fake 替代，不需要 mock 整套 CRUD。
type AccountStore interface {
	SetNewAPIUserID(ctx context.Context, accountID int64, newapiUserID int) error
	GetByID(ctx context.Context, id int64) (*entity.Account, error)
	// ListWithoutNewAPIUser returns up to `limit` accounts whose NewAPI
	// mapping is NULL — used by the reconciliation cron (P1-4) to backfill
	// orphans from earlier failed OnAccountCreated hooks.
	ListWithoutNewAPIUser(ctx context.Context, limit int) ([]*entity.Account, error)
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

// Deduper is the contract the module uses for at-least-once → at-most-once
// upgrade on event handlers. Money flows MUST run on a fail-closed
// implementation (e.g. idempotency.WebhookDeduper.WithFailClosed()) so
// Redis outage NAKs the message instead of risking double-credit.
//
// nil-safe: when no Deduper is wired (m.deduper == nil) topup events are
// processed without dedup. The wiring path in main.go ALWAYS sets this in
// production; this nil-safe contract only protects unit tests + dev.
type Deduper interface {
	TryProcess(ctx context.Context, eventID string) error
}

// Module 是 newapi_sync 模块。零值不可用 — 必须通过 New 构造。
type Module struct {
	client   NewAPIClient
	accounts AccountStore
	deduper  Deduper // nil = no dedup (dev/test only); prod must wire fail-closed
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

// WithDeduper wires an idempotency deduper for OnTopupCompleted (money
// path). For correctness on JetStream's at-least-once delivery, the
// passed deduper SHOULD be fail-closed (idempotency.WebhookDeduper.
// WithFailClosed()) so Redis outages NAK instead of silently duplicating.
//
// Safe to call with nil — disables dedup (returns receiver unchanged).
// Chainable.
func (m *Module) WithDeduper(d Deduper) *Module {
	if m == nil {
		return nil
	}
	m.deduper = d
	return m
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
		metrics.RecordNewAPISyncOp(opAccountProvisioned, resultError)
		return errors.New("newapi_sync: nil account")
	}
	if account.NewAPIUserID != nil {
		// Already synced — repeat-trigger is a no-op (idempotent).
		metrics.RecordNewAPISyncOp(opAccountProvisioned, resultSkipped)
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
			metrics.RecordNewAPISyncOp(opAccountProvisioned, resultError)
			return fmt.Errorf("newapi_sync: create user %s: %w", username, cerr)
		}
		// NewAPI CreateUser 不返 id — 再 search 一次。
		id, err = m.client.FindUserByUsername(ctx, username)
		if err != nil {
			metrics.RecordNewAPISyncOp(opAccountProvisioned, resultError)
			return fmt.Errorf("newapi_sync: post-create lookup %s: %w", username, err)
		}
	} else if err != nil {
		metrics.RecordNewAPISyncOp(opAccountProvisioned, resultError)
		return fmt.Errorf("newapi_sync: find user %s: %w", username, err)
	}

	if err := m.accounts.SetNewAPIUserID(ctx, account.ID, id); err != nil {
		metrics.RecordNewAPISyncOp(opAccountProvisioned, resultError)
		// NewAPI user 已经存在但 platform 没记下来 — 下次 hook 触发会走"先 search
		// 再 Set"的幂等分支。这里返错让 registry 记 warn 引起注意。
		return fmt.Errorf("newapi_sync: set newapi_user_id account=%d newapi=%d: %w", account.ID, id, err)
	}

	metrics.RecordNewAPISyncOp(opAccountProvisioned, resultSuccess)
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
	// "newapi_sync" is the DLQ hook_name — stable across deploys so DLQ
	// rows persist through code reorganization.
	r.OnAccountCreated("newapi_sync", m.OnAccountCreated)
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
		metrics.RecordNewAPISyncOp(opLLMTokenIssued, resultError)
		return nil, fmt.Errorf("newapi_sync: lookup account %d: %w", accountID, err)
	}
	if account == nil || account.NewAPIUserID == nil {
		// "skipped" — caller will retry; this is not an error in our metric
		// view, but if sustained it signals 4c is stuck.
		metrics.RecordNewAPISyncOp(opLLMTokenIssued, resultSkipped)
		return nil, ErrAccountNotProvisioned
	}
	key, err := m.client.EnsureUserAPIKey(ctx, *account.NewAPIUserID, name)
	if err != nil {
		metrics.RecordNewAPISyncOp(opLLMTokenIssued, resultError)
		return nil, fmt.Errorf("newapi_sync: ensure api key for account=%d: %w", accountID, err)
	}
	metrics.RecordNewAPISyncOp(opLLMTokenIssued, resultSuccess)
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
// **idempotency** (P0-1, 平台硬化清单)：
// JetStream 至少一次投递，重发会双扣。eventID（由上游 envelope.event_id 透传）
// 走 fail-closed Redis SETNX：
//   - 第一次见 → 处理 + 在 Redis 留 24h marker
//   - 重发      → ErrAlreadyProcessed → 视作成功（ack 消息但跳过 NewAPI 调用）
//   - Redis 挂 → ErrRedisUnavailable → 返错，consumer NAK，JetStream 重投
//   - eventID 空 → 警告但仍处理（envelope bug 不阻断流量）
//
// 处理失败的边缘案例：
//   - 账户不存在 / 未同步 → 跳过 + 记日志 + 返 nil（ack）；对账 cron 会回填
//   - amountCNY ≤ 0 → 早返 nil（ack）；不污染 dedup key
//   - NewAPI 失败 → 返错，consumer NAK，**dedup key 保留** — 重试时会被识别
//     为重复，需要业务层判断"上次到底有没有真扣到"。当前简化处理：dedup TTL 24h
//     内重复都视为成功，**接受单次 NewAPI 失败 = 那次 topup 没同步**（4g 对账
//     cron 兜底）。如果要更严格，需要 2-phase commit (mark-in-flight → mark-done)
//     这是 P1 的事。
func (m *Module) OnTopupCompleted(ctx context.Context, eventID string, accountID int64, amountCNY float64) error {
	if amountCNY <= 0 {
		metrics.RecordNewAPISyncOp(opTopupSynced, resultSkipped)
		return nil
	}

	// Idempotency check first — short-circuits both happy-path and
	// error-path so a missing eventID can't bypass the gate silently.
	if m.deduper != nil {
		switch err := m.deduper.TryProcess(ctx, eventID); {
		case err == nil:
			// First time we've seen this event — proceed.
		case errors.Is(err, idempotency.ErrAlreadyProcessed):
			metrics.RecordNewAPISyncOp(opTopupSynced, resultDuplicate)
			slog.InfoContext(ctx, "newapi_sync: skip topup (duplicate event)",
				"event_id", eventID, "account_id", accountID, "amount_cny", amountCNY)
			return nil
		case errors.Is(err, idempotency.ErrEmptyEventID):
			// Empty eventID — process anyway but loud warning so the
			// upstream (NATS publisher / Temporal workflow) can be fixed.
			slog.WarnContext(ctx, "newapi_sync: topup event missing event_id, dedup bypassed",
				"account_id", accountID, "amount_cny", amountCNY)
		case errors.Is(err, idempotency.ErrRedisUnavailable):
			metrics.RecordNewAPISyncOp(opTopupSynced, resultError)
			// Fail-closed: caller MUST NAK. Bubbling the error up does
			// exactly that. JetStream will retry once Redis recovers.
			return fmt.Errorf("newapi_sync: dedup unavailable, will retry: %w", err)
		default:
			metrics.RecordNewAPISyncOp(opTopupSynced, resultError)
			// Unknown deduper error — also fail-closed for money safety.
			return fmt.Errorf("newapi_sync: dedup error: %w", err)
		}
	}

	account, err := m.accounts.GetByID(ctx, accountID)
	if err != nil {
		metrics.RecordNewAPISyncOp(opTopupSynced, resultError)
		return fmt.Errorf("newapi_sync: lookup account %d: %w", accountID, err)
	}
	if account == nil || account.NewAPIUserID == nil {
		// 账户不存在或尚未同步 NewAPI — 跳过；对账 cron 会处理。
		metrics.RecordNewAPISyncOp(opTopupSynced, resultSkipped)
		slog.InfoContext(ctx, "newapi_sync: skip topup (no newapi mapping)",
			"account_id", accountID, "amount_cny", amountCNY)
		return nil
	}
	delta := int64(amountCNY * QuotaPerUnit)
	newQuota, err := m.client.IncrementUserQuota(ctx, *account.NewAPIUserID, delta)
	if err != nil {
		metrics.RecordNewAPISyncOp(opTopupSynced, resultError)
		return fmt.Errorf("newapi_sync: increment quota account=%d delta=%d: %w", accountID, delta, err)
	}
	metrics.RecordNewAPISyncOp(opTopupSynced, resultSuccess)
	slog.InfoContext(ctx, "newapi_sync: topup synced",
		"event_id", eventID,
		"account_id", accountID,
		"newapi_user_id", *account.NewAPIUserID,
		"amount_cny", amountCNY,
		"delta_quota", delta,
		"new_quota", newQuota)
	return nil
}
