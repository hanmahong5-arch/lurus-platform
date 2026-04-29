# ADR: NewAPI 计费同步集成（C.2）

**Status**: Accepted
**Date**: 2026-04-29
**Decision-maker**: 用户（明确选定 C.2，"完全适配 NewAPI"）

---

## 背景

简化路线图阶段 1 #4 是"Lucrum 切到 drop-in"。计划中 Lucrum 的 LLM 调用走 `newapi.lurus.cn`。验证发现 **NewAPI 与 platform 钱包没有任何集成** —— 两套并行计费。

详细排查结果：
- NewAPI 有自己的 `users` 表 + `quota` 字段 + `WalletFunding` 资金源
- NewAPI 内部 `WalletFunding.PreConsume` 调 `model.DecreaseUserQuota`，与 platform 钱包零关系
- Platform 仅有 `/proxy/newapi/*` admin 反向代理，不做计费
- Platform 钱包 + NewAPI 配额是两套独立账本

不修复这个 gap，C 计划在产品上没意义（用户充 platform 钱包，但 LLM 用不了）。

## 候选方案对比

| 方案 | 概述 | 修改 NewAPI | 修改 platform | 优 | 劣 |
|-----|------|-----------|-------------|----|----|
| C.1 | NewAPI 调 platform wallet | 大（新增 PlatformWalletFunding） | 中 | 单一真相 | NewAPI fork 偏离上游、每次 LLM 多一跳 |
| **C.2** | **Platform→NewAPI 单向同步充值** | **零** | **小** | **NewAPI 不动；产品体验已达标** | **两套余额需对账** |
| C.3 | platform 自建 LLM 代理跳过 NewAPI | 零 | 大（重写 NewAPI 已有功能） | 单一真相 | 违反 "删>加"，重复造轮 |

**用户选定 C.2**。原话："完全适配他"。

## 决策

采用 **C.2 — Platform → NewAPI 单向同步充值**。

### 心智模型

- **Platform 钱包是用户唯一可见的余额**（所有产品扣的就是它）
- **NewAPI 配额是 platform 钱包的"LLM 子账户"**，自动同步，对终端用户透明
- 用户充值 / 平台直接扣（debit）只动 platform 钱包；platform 在合适时机 push 同步到 NewAPI

### 同步规则（一句话）

**platform 钱包 +X CNY → NewAPI 用户 +X×QuotaPerUnit 配额**。
**不做反向同步**（NewAPI 内部消耗不会回写 platform）。

## 实施契约

### 数据模型（platform 端）

`identity.accounts` 表加列：
```sql
ALTER TABLE identity.accounts ADD COLUMN newapi_user_id INT;
CREATE INDEX idx_accounts_newapi_user_id ON identity.accounts(newapi_user_id);
```

每个 platform account 对应**至多一个**NewAPI user（1:1 映射）。NULL = 未同步。

### NewAPI 调用面（platform→NewAPI）

完全用 NewAPI 现有 admin API（不要求 NewAPI 加任何端点）：

| Platform 行为 | 调 NewAPI | 备注 |
|--------------|-----------|------|
| 新 account 创建 | `POST /api/user/` 建 NewAPI user，再 `GET /api/user/search?keyword=<username>` 取 id | NewAPI CreateUser 不返回 id 的妥协 |
| Platform 钱包充值成功 | `GET /api/user/:id` 读当前 quota → `PUT /api/user/` 写 `quota = old + delta` | 容许微秒级 race（admin 写 vs. LLM 消费），尾数对账时纠正 |
| 用户登录平台想用 LLM | platform 暴露 `GET /api/v1/account/me/llm-token`，从 NewAPI 拉 user 的 token 返回前端 | 无需新 NewAPI 端点 — 用现有 token 列表 API |

NewAPI admin 鉴权：env `NEWAPI_ADMIN_ACCESS_TOKEN` + `NEWAPI_ADMIN_USER_ID`（platform-core 已配，见 `secrets.yaml`）。

### NewAPI 端**零修改**

不新增端点、不改 schema、不改资金源逻辑。Platform 完全走 NewAPI 现有 admin API。

NewAPI 升级上游时不影响（除非 NewAPI 把 admin API 路径改了，那是另一回事，标准 fork 维护成本）。

### 用户名约定（NewAPI 上）

NewAPI user.username = `lurus_<account_id>`（如 `lurus_42`）。
- 唯一、不冲突（NewAPI 用户名 unique）
- 不泄露用户隐私（不用 email/phone）
- 反查容易（platform.account_id ↔ NewAPI.username）

### 同步失败兜底

- 创建 NewAPI user 失败 → platform account 创建仍成功，`newapi_user_id` 留 NULL，后台 cron 重试（每小时一次，TODO）
- 充值同步失败 → 平台账户 credit 仍记 success（钱已收），newapi 同步进 outbox 表重试，alarm
- 不让 NewAPI 故障阻塞用户在 platform 的核心流程

## 实施序列（独立可发布）

每一步独立部署，互不阻塞。

### Step A — NewAPI 客户端包（platform 内部）
**新增** `internal/pkg/newapi/client.go`，封装：
- `CreateUser(username, displayName) error`
- `FindUserByUsername(username) (UserID, error)`
- `GetUserQuota(userID) (int64, error)`
- `SetUserQuota(userID, quota int64) error`
- `ListUserTokens(userID) ([]Token, error)`

完全 HTTP 封装，无 GORM、无业务逻辑。

**测试**：mock NewAPI HTTP 端点。
**风险**：极低，仅引入新文件不动现有代码。

### Step B — 数据库迁移 + Account entity 扩展
- 加 column `newapi_user_id`
- Account entity 加字段
- Repo 加 `SetNewAPIUserID(accountID, newapiUserID)`

### Step C — 注册 hook：账号创建即建 NewAPI user
- `RegistrationService.Register` 成功后异步触发 NewAPI user 创建
- 失败不阻塞（写 audit + 后台重试）

### Step D — 充值 hook：钱包 credit 即同步 NewAPI 配额
- 在 `WalletService.Credit` / topup webhook 完成后异步推送
- 用 outbox 模式（NATS subject: `identity.wallet.topup_synced`）

### Step E — 用户拉 token 的端点
- `GET /api/v1/account/me/llm-token` → 返回该用户的 NewAPI token（取列表第一条；无则建一条）
- 用于 Lucrum 直连 NewAPI（避免每次 chat 都过 platform）

### Step F — Lucrum 改造
- 删 NextAuth + LLM_API_KEY env
- 登录走 /whoami；LLM 调用 `https://newapi.lurus.cn/v1/...` + 用户 token
- **此 step 是 dogfood 终点，验证整套链路**

### Step G — 对账兜底
- 后台 cron 巡检 `accounts.newapi_user_id IS NULL` → 重试创建
- 巡检最近 24h topup → 抽样核对 NewAPI 配额是否同步成功
- 失败的进 alert

## 不做（明确）

- ❌ 反向同步（NewAPI 配额变化推回 platform）— 不需要
- ❌ NewAPI 端 fork 添加端点 — "完全适配"原则
- ❌ 强一致（NewAPI 配额 == platform 钱包余额）— 接受微秒级窗口
- ❌ 多 NewAPI 实例支持 — 当前只有一个
- ❌ NewAPI 用户名换 email/phone — 隐私 + 唯一性考虑

## 风险登记

| 风险 | 概率 | 缓解 |
|------|------|------|
| NewAPI CreateUser 不返回 id → search-by-username 慢 | 中 | username 上有 NewAPI 索引；可接受 |
| Platform 充值成功但 NewAPI 同步失败 → 用户充了钱用不了 LLM | 低 | outbox 重试；alarm；用户能在 24h 内自愈 |
| NewAPI 配额被用户耗尽但 platform 钱包还有钱 → 用户困惑 | 中 | 长期看是 C.1 才能根治；短期用文档解释 |
| Race: PUT quota 与 LLM PreConsume 并发 → 写入丢失 | 低 | 选合理 PUT 时机（topup 完成后立刻），LLM 高峰期不会和 admin 写撞 |
| NewAPI fork 上游升级，admin API 路径变了 | 低 | 这是 fork 必须的常态成本，与 ADR 无关 |

## 验证标准

ADR 实施完成的判定（产品端）：
1. 路途/Tally/Lucrum 任一新用户注册 → NewAPI 自动出现对应 user
2. 用户充值 100 CNY → NewAPI 配额准确增加 50,000,000（=100×QuotaPerUnit）
3. 用户调 LLM → NewAPI 内部扣，余额下降，platform 钱包不变
4. **平台 admin UI 能看到"LLM 已用 / 已充值"对应关系**（用 NewAPI 内部数据展示）
5. **从用户视角，没有"两个余额"的认知负担**（UI 只显示 platform 钱包）

## 演进路径（C.2 → C.1）

C.2 不是终点。当用户量增长 / Lucrum 上线后实践证明"两套余额对账"成本 > "NewAPI fork 维护"成本时，启动 C.1：
- 在 NewAPI 加 `PlatformWalletFunding` 资金源（替代 NewAPIWalletFunding）
- 把 `model.DecreaseUserQuota` 改成 `platformClient.Debit(account_id, amount)`
- 删 `accounts.newapi_user_id` 列（或保留作纯映射）

时机判断：见 `docs/产品成熟度审计.md`，至少 2 个产品稳定运行后再考虑。

## 相关引用

- 简化路线图：`docs/简化路线图.md` 阶段 1 #4
- 多租户简化：`docs/多租户简化.md`
- 产品审计：`docs/产品成熟度审计.md`
