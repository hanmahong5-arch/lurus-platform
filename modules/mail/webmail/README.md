# Lurus Mail
The self-hosted unified communication platform for **email, calendars, contacts and storage**.


## What's New

### Lurus Mail Drive — integrated **WebDAV/S3 storage**
Store and manage files directly within Lurus Mail using **WebDAV** locally or S3-compatible storage.
Support for providers like AWS S3, Backblaze B2, MinIO, and DigitalOcean Spaces is coming soon.

### Sync your calendars across all your devices
Lurus Mail supports Calendars and syncs your calendars through **CalDAV**, compatible with iOS, macOS, Android (DAVx5), Thunderbird, and all CalDAV-supporting apps.

### Full **CardDAV support** — sync your contacts across all your devices
Lurus Mail supports **complete CardDAV sync**, meaning your address book updates instantly across iOS, macOS, Android (DAVx5), Thunderbird, and any CardDAV-compatible app.

### Lurus Mail supports **Labels**
Organize your inbox your way with flexible, multi-color labels.

---

## What is Lurus Mail?

Lurus Mail is a **self-hosted, unified communication platform** that brings together:

- Email (JMAP / IMAP / SMTP)
- Calendars (CalDAV)
- Contacts (CardDAV)

All wrapped into a clean, fast, modern web UI — powered by **Stalwart** mail server and hosted on **your** infrastructure.

Lurus Mail lets you:

- Use **Stalwart** as the single source of truth for mail, calendar, contacts
- Sync calendars across devices
- Sync contacts across devices
- Manage multiple identities and domains
- Route China domestic mail via SendCloud SMTP relay
- Keep all data private and under your control

Whether you're running a personal server, a small-business mail setup, or a multi-domain environment, Lurus Mail gives you a **beautiful unified interface** without losing control of your data.

---

## Why Lurus Mail?

Lurus Mail is designed as a next-generation alternative to traditional webmail and PIM suites:

- **JMAP-first architecture**
  Modern, efficient protocol (RFC 8620/8621) replacing IMAP sync overhead.

- **Stalwart-native**
  Mail, calendar, contacts all stored in Stalwart — no redundant data sync.

- **Unified web interface**
  Email, calendars, and contacts — consistent, fast.

- **Self-hosted first**
  Your data stays on *your* server. No third-party analytics. No vendor lock-in.

- **Open standards**
  JMAP/IMAP/SMTP for mail, CalDAV for calendars, CardDAV for contacts.

- **Modern app stack**
  Next.js 16 + React 19 + Mantine 8 + Nitro + PostgreSQL — fast and extensible.

- **Kubernetes-ready**
  GitOps deployment with ArgoCD, Kustomize manifests included.

Lurus Mail aims to combine the simplicity of a webmail client with the flexibility of a complete, modern communication backend — all under your control.

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Frontend | Next.js 16 + React 19, Mantine 8, Tailwind CSS 4, TipTap 3 |
| API / Worker | Nitro (H3), BullMQ, Drizzle ORM |
| Mail Server | Stalwart (JMAP / IMAP / SMTP / CalDAV / CardDAV), RocksDB |
| Auth | Zitadel OIDC PKCE |
| Database | PostgreSQL (app metadata only) |
| Cache / Queue | Redis (BullMQ + session cache) |
| Runtime | Bun (dev/build), Node 20 Alpine (Docker runtime) |

---

## Getting Started

```bash
# Install dependencies
bun install

# Start development
bun run dev          # web:3000 + worker:3001 + docs:3002

# Build
bun run build        # Next.js standalone
bun run build:worker # Nitro

# Test
bun run test         # vitest watch
```

---

## Contributing

Contributions are welcome. PRs, issues, ideas, and feedback are all appreciated.

---
