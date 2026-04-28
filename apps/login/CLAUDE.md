# lurus-login

Zitadel 开源 Login UI（Next.js）的 Lurus 定制版本。
Namespace: `lurus-login` | Domain: `login.lurus.cn` | Port: `3000`

## 架构

本服务是 [zitadel/zitadel](https://github.com/zitadel/zitadel) 主库中 `apps/login` 的独立部署版本。
Zitadel 配置 `ZITADEL_LOGIN_URL=https://login.lurus.cn` 后，所有 OIDC 授权流程均由本服务处理。

**品牌化**：通过 Zitadel Console → Instance Settings → Branding 配置 Logo/颜色，login-ui 自动读取。
**微信登录**：lurus-platform 暴露 `/oauth/wechat/*` OAuth2 端点，在 Zitadel 注册为 Generic OAuth IDP，login-ui 自动显示微信按钮。

## Tech Stack

| 层 | 选型 |
|----|------|
| 框架 | Next.js 15 + React 19 + TypeScript |
| 样式 | Tailwind CSS |
| 容器 | node:22-alpine |
| 镜像源 | ghcr.io/hanmahong5-arch/lurus-login:main |

## 定制化说明

Lurus 在上游 Zitadel Login UI 基础上进行覆盖式定制（overlay pattern）：
- `overrides/` 目录结构镜像 `upstream/apps/login/`
- Dockerfile 在 pnpm install 前执行 `COPY overrides/ /src/apps/login/`
- 每次上游升级后，检查 `overrides/` 中的文件是否需要同步

**已覆盖的文件**：

| 覆盖文件 | 上游文件 | 变更原因 |
|---------|---------|---------|
| `overrides/src/app/(login)/loginname/page.tsx` | `src/app/(login)/loginname/page.tsx` | 手机号优先登录入口（中文环境） |
| `overrides/src/components/phone-login-form.tsx` | (新增) | 手机号码表单组件 |
| `overrides/src/lib/server/phone-loginname.ts` | (新增) | 手机+OTP Server Action，含自动注册 |
| `overrides/locales/zh.json` | `locales/zh.json` | 中文文案（标题改为 Lurus Tally，补充手机号相关字段） |

**语言切换逻辑**：Zitadel Instance Settings → defaultLanguage 控制，手机号优先界面
仅在 Accept-Language 为 `zh` 时启用（可通过 `?mode=classic` 强制降级）。

## 初始化（首次部署）

```bash
# 1. 克隆上游登录 UI 源码（sparse checkout）
bash setup.sh

# 2. 在 Zitadel 创建 Service Account，获取 Service User Token
#    Zitadel Console → Service Users → Generate PAT
#    把 user_id 和 token 填入 K8s secrets

# 3. 部署到 K8s
kubectl apply -k deploy/k8s/

# 4. 在 Zitadel Console 配置：
#    Instance Settings → Login → Custom Login URL: https://login.lurus.cn
#    Instance Settings → Branding：上传 Lurus Logo，设置主色 #1677ff
```

## Commands

```bash
# 首次初始化
bash setup.sh

# 本地开发（同步 overrides 后启动）
# overrides 已在 Docker 构建时覆盖；本地开发需手动同步：
cp -r overrides/* upstream/apps/login/
cd upstream && pnpm install && pnpm --filter @zitadel/login dev
# 访问 http://localhost:3000/ui/v2/login 验证手机号码优先界面

# 构建镜像（需要 Docker 环境）
docker build -t ghcr.io/hanmahong5-arch/lurus-login:dev .

# 推送到 GHCR（CI 自动执行，手动时）
SHA7=$(git -C ../.. rev-parse --short=7 HEAD)
docker tag ghcr.io/hanmahong5-arch/lurus-login:dev ghcr.io/hanmahong5-arch/lurus-login:main-${SHA7}
docker push ghcr.io/hanmahong5-arch/lurus-login:main-${SHA7}

# 部署（Task #37 执行）
kubectl rollout restart deployment/lurus-login -n lurus-platform
kubectl logs -f deployment/lurus-login -n lurus-platform
```

## Environment Variables

| 变量 | 必填 | 说明 |
|------|------|------|
| `ZITADEL_API_URL` | ✓ | Zitadel 实例地址 (`https://auth.lurus.cn`) |
| `ZITADEL_SERVICE_USER_ID` | ✓ | Service Account 用户 ID |
| `ZITADEL_SERVICE_USER_TOKEN` | ✓ | Service Account PAT |
| `ZITADEL_TLS_ENABLED` | — | `true` in production |
| `NEXT_PUBLIC_THEME_ROUNDNESS` | — | `mid` |
| `NEXT_PUBLIC_THEME_LAYOUT` | — | `top-to-bottom` |
| `NEXT_PUBLIC_THEME_APPEARANCE` | — | `material` |

## BMAD

| Resource | Path |
|----------|------|
| PRD | `./_bmad-output/planning-artifacts/prd.md` |
| Epics | `./_bmad-output/planning-artifacts/epics.md` |
| Architecture | `./_bmad-output/planning-artifacts/architecture.md` |
| Sprint Status | `./_bmad-output/implementation-artifacts/sprint-status.yaml` |
| Dev Stories | `./_bmad-output/implementation-artifacts/<story-id>.md` |
