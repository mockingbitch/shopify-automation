# Shopee Shop Automation

Multi-tenant SaaS for configuring many Shopee shops from reusable templates, using the Shopee Open Platform API, and a browser extension for operations that have no API.

Status:
- **Phase 1 (foundation) complete**: accounts, organizations, members, invitations, roles, audit log, tenant isolation, dashboard shell.
- **Phase 2 (Shopee integration) complete against the documented API**: partner apps, OAuth connect and reconnect (including main accounts), encrypted tokens with safe refresh, shop sync, authorization webhooks, capability registry, shop pages. Not yet exercised against the real Shopee sandbox (needs S-1, see [docs/roadmap.md](docs/roadmap.md)).
- **Phase 3 (templates) complete**: versioned shop templates with 12 schema-defined sections, publish/clone, shop overrides and a per-shop change preview.
- **Phase 4 (workflows) complete**: applying a template starts a durable SHOP_SETUP run per shop that reads the shop, asks for approval of each change, applies approved changes through the Open API, turns operations without an API into manual tasks, verifies and reports. Product steps come in Phase 5.

Quick start: see [docs/development.md](docs/development.md).

| Document | Contents |
|---|---|
| [docs/requirements.md](docs/requirements.md) | Requirements, discovery findings, assumptions, open questions |
| [docs/architecture.md](docs/architecture.md) | Architecture, decisions, module map, security risk assessment |
| [docs/database.md](docs/database.md) | ERD, constraints, row-level security, migrations, backups |
| [docs/api-design.md](docs/api-design.md) | REST conventions, error format, permissions, endpoint catalog |
| [docs/workflow-design.md](docs/workflow-design.md) | Workflow engine, state machines, idempotency, SHOP_SETUP |
| [docs/shopee-integration.md](docs/shopee-integration.md) | Verified Shopee API facts, signing, tokens, webhooks, capability matrix |
| [docs/browser-extension.md](docs/browser-extension.md) | Browser extension for Seller Centre operations without an API |
| [docs/roadmap.md](docs/roadmap.md) | Phases, task breakdowns, pending decisions |
| [docs/development.md](docs/development.md) | Local setup, commands, conventions |
| [api/openapi.yaml](api/openapi.yaml) | HTTP API contract |

## Hướng dẫn sử dụng

Hướng dẫn cho người bán (tiếng Việt, có hình minh họa): [docs/huong-dan-su-dung.md](docs/huong-dan-su-dung.md). Trong hệ thống: menu **Hướng dẫn sử dụng** (`/help`). Chạy thử trên máy: `make local-up` (xem `docs/development.md`).
