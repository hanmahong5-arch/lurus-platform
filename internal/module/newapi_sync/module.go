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
// 故意不引整个 accountStore 接口 — 模块只需要"写映射 ID"的能力。这让单元
// 测试可以用一个超薄的 fake 替代，不需要 mock 整套 Account CRUD。
type AccountStore interface {
	SetNewAPIUserID(ctx context.Context, accountID int64, newapiUserID int) error
}

// NewAPIClient 是 NewAPI admin 客户端契约 — 形状和 *newapi.Client 一致，但
// 用接口接收让测试可以 mock。
type NewAPIClient interface {
	CreateUser(ctx context.Context, username, displayName string) error
	FindUserByUsername(ctx context.Context, username string) (int, error)
}

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
