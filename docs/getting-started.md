# Getting Started / 快速开始

Deploy Lurus Platform from zero to a running instance in minutes.

## Prerequisites / 前置条件

| Component | Version | Notes |
|-----------|---------|-------|
| Docker + Docker Compose | v24+ | Required for all deployment modes |
| PostgreSQL | 15+ | External or bundled |
| Redis | 7+ | External or bundled |
| Zitadel | v4.12+ | External or bundled |

## Deployment Modes / 部署模式

| Mode | Components | Use Case |
|------|-----------|----------|
| **Core Only** | Auth + Billing + Wallet | Minimum viable identity platform |
| **Core + Mail** | + Stalwart + Webmail | Enterprise email integration |
| **Core + Notification** | + Notification + NATS | Multi-channel push |
| **Full** | All above + Admin + Login UI | Complete enterprise platform |

## Quick Start: Core Only / 最小部署

```bash
# 1. Clone the repository
git clone https://github.com/hanmahong5-arch/lurus-platform.git
cd lurus-platform

# 2. Copy and edit environment config
cp deploy/.env.example deploy/.env
# Edit deploy/.env with your database, Redis, and Zitadel credentials

# 3. Start core services
docker-compose -f deploy/docker-compose.core.yml up -d

# 4. Run database migrations
docker exec -i lurus-platform-core \
  /lurus-platform-core migrate

# 5. Verify
curl http://localhost:18104/health
# Expected: {"status":"ok"}
```

## Quick Start: Full Stack / 全量部署

```bash
docker-compose -f deploy/docker-compose.yml up -d
```

This starts: Core + Notification + NATS + Stalwart + Webmail + Admin + Login UI.

## Kubernetes Deployment / K8s 部署

```bash
# Core only
kubectl apply -k deploy/k8s/base/

# Full (all modules)
kubectl apply -k deploy/k8s/overlays/full/

# Selective modules
kubectl apply -k deploy/k8s/overlays/with-mail/
kubectl apply -k deploy/k8s/overlays/with-notification/
```

## Verify Installation / 验证安装

```bash
# Health check
curl http://<host>:18104/health

# gRPC (requires grpcurl)
grpcurl -plaintext <host>:18105 identity.v1.IdentityService/GetAccountByZitadelSub

# Metrics
curl http://<host>:18104/metrics
```

## Next Steps / 下一步

- [Architecture](./architecture.md) — System design and component overview
- [Configuration](./configuration.md) — Full environment variable reference
- [Modules](./modules/) — Per-module documentation
