## 2026-04-06: QR Code 全场景集成 (platform + lutu)

Backend: `internal/adapter/handler/qr_login_handler.go` (新建, 270行) — QR 登录状态机 (pending→confirmed→consumed)，Lua CAS 原子转换，长轮询, Redis TTL 5min。  
Router + main.go 注入 QRLoginHandler，注册 3 个端点。  
新增 `alipay.go` / `wechat_pay.go` 支付 provider 骨架 (供 TopupScreen QR 展示)。  
23 个后端测试全通过；handler package 覆盖率 79.1%，router 88.8%。  
`go build ./...` 干净通过。

Flutter (`2c-app-lutu`): 7 个新文件 (scanner screens, confirm screen, qr_payment_display widget, qr_login_provider), 6 个修改 (topup/redeem/home screen, main.dart, constants, l10n)。  
5 组新测试 (59 个测例)，全用 Fake pattern，无需 build_runner。

状态: 🔧 Code Complete — 待部署；flutter analyze 待 CI 验证。
