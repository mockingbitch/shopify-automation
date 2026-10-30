# Implementation Roadmap — Shopee Shop Automation

Status: Phase 0 draft · Last updated: 2026-10-30

Estimates assume one senior engineer working with an AI agent. They are for ordering and scoping, not commitments.

## 1. Phases at a glance

| Phase | Scope | Depends on | External blockers | Estimate |
|---|---|---|---|---|
| 0 Discovery | These docs | — | — | done |
| 1 Foundation | Repo, config, DB, migrations, logging, OTel, auth, orgs, members, RBAC, tenant context + RLS, audit, CI | 0 | none | 2 weeks |
| 2 Shopee integration | Apps, signer, client, OAuth, tokens + encryption, connect/reconnect/revoke, sync, webhooks, capability registry | 1 | **S-1** sandbox app for E2E (unit/integration work proceeds without it) | 2–3 weeks |
| 3 Template engine | CRUD, versions, publish, clone, section schemas, validation, overrides, diff, preview | 1 (2 for live preview) | — | 2 weeks |
| 4 Workflow engine | Engine, sweeper, retries, idempotency, approvals, resume, cancel, events, SHOP_SETUP steps 1–8 + 12–13 | 2, 3 | — | 3 weeks |
| 5 Products | Import (CSV/XLSX streaming), row validation, category/attribute mapping, image pipeline (scan, store, upload), add_item protocol, variations, verification, report | 2, 4 | S-6 | 3 weeks |
| 6 Browser extension | MV3 extension (WXT + Vue popup), pairing + device tokens, signed recipes, recipe runner, shop-ID check, extension API, screenshots, Playwright tests on fixture pages, first recipes (banner, decoration, warehouse, address) | 4 | E-1…E-4; a test seller account per market for recipe verification | 3 weeks |
| 7 Dashboard | All pages and states; can start in parallel from Phase 2 onwards, page by page | API of each phase | — | 3 weeks (spread) |
| 8 Hardening | Load tests, security tests, backups + restore drill, deploy pipeline, MFA enforcement, runbooks | all | — | 2 weeks |

The dashboard is built incrementally: each backend phase ships with its pages, so Phase 7 is mostly the overview, polish and accessibility audit.

## 2. Phase 1 task breakdown

**Status (2026-10-30): done.** All tasks below are implemented and verified locally: `go test -race` with integration tests against Postgres 17 and Redis, `golangci-lint` (0 issues), `sqlc diff`, migrations up/down/up, `nuxt typecheck`, a Playwright smoke test in Chromium, and the Docker image build. CI is written but has not run yet (no GitHub remote).

Deferred from Phase 1, tracked for later phases:

| Item | Moves to |
|---|---|
| Emails through a retried background job | Phase 4 |
| Breached-password check (k-anonymity range API) | Phase 8 |
| TOTP / MFA (Q-5) | Phase 8 |
| Platform admin endpoints (`/admin/*`) | Done in Phase 2 (`/admin/shopee-apps`) |
| Job that creates future `audit_logs` partitions and purges deleted organizations | Phase 4 (scheduler) |
| Resend verification email endpoint | Done in Phase 2 |



Goal: a running stack where a user can sign up, create an organization, invite members, change roles, and every action is audited, with tenant isolation enforced in both code and database.

| # | Task | Output | Done when |
|---|---|---|---|
| 1.1 | Repo bootstrap | `git init`, `go.mod`, layout from architecture.md §3, `Makefile`, `.editorconfig`, `.golangci.yml`, Conventional Commits | `make lint test` passes on empty skeleton |
| 1.2 | Docker Compose (dev) | `deploy/docker-compose.yml`: postgres:17, redis:7, minio, mailpit, otel-collector; `.env.example` | `make up` starts all, health checks green |
| 1.3 | Config | `platform/config`: env parsing, validation, secrets from files (`*_FILE`), fail fast on missing | Unit tests for required/invalid values |
| 1.4 | Logging + tracing | `platform/logx` (slog JSON + redaction), `platform/otel` (OTLP exporter), request ID | Test: secret-looking keys are redacted |
| 1.5 | DB + migrations | `platform/db` (pgxpool, `InTenantTx`, `InUserTx`), goose, DB roles, first migrations: users, sessions, orgs, members, roles, permissions, role_permissions, invitations, audit_logs (partitioned) | Up/down/up in CI; RLS enabled + forced on tenant tables |
| 1.6 | sqlc setup | `sqlc.yaml`, queries per module, generated code committed | `sqlc diff` clean in CI |
| 1.7 | HTTP platform | `platform/httpx`: router (chi), middleware chain, problem+json errors, JSON decode with `DisallowUnknownFields`, validator | Tests for error mapping and limits |
| 1.8 | Identity | Register, verify email, login, logout, session store, password reset, Argon2id, login rate limit, CSRF | Unit + integration tests; session rotation on login |
| 1.9 | Tenancy | Create org (creator = owner), list, get, update, soft delete; TenantContext middleware (`404` for non-members) | Integration tests |
| 1.10 | Members + invitations | Invite, accept, list, change role, remove, transfer ownership; last-owner and rank rules | Table tests for every role pair |
| 1.11 | RBAC | Permission seeds, `rbac.Require("perm")` route option, policy checks in services | Every route declares a permission (test walks the router) |
| 1.12 | Audit | `audit.Record` inside the same tx; `GET /audit-logs` | Every mutating endpoint writes exactly one audit row (test) |
| 1.13 | Tenant isolation suite | Test that creates org A and B and calls every org-scoped route as A with B's IDs; plus "no tenant set → zero rows" test for every query | Part of CI; must stay green forever |
| 1.14 | OpenAPI | `api/openapi.yaml` for Phase 1 endpoints; contract test validates responses | CI fails on drift |
| 1.15 | CI | GitHub Actions: lint, vet, test (race), migrations, sqlc diff, govulncheck, gitleaks, Docker build | Green on PR |
| 1.16 | Dashboard shell | Nuxt 3 SPA, Tailwind, shadcn-vue, generated API types, login/register, org switcher, members page | Lint + type-check + Playwright smoke test |
| 1.17 | Docs sync | Update architecture/database/api docs with anything that changed | Reviewed |

Suggested order: 1.1 → 1.2 → 1.3/1.4 → 1.5/1.6 → 1.7 → 1.8 → 1.9 → 1.11 → 1.10 → 1.12 → 1.13 → 1.14 → 1.15 (incrementally from 1.1) → 1.16 → 1.17.

Key libraries (proposed, confirmed in 1.1): `go-chi/chi/v5`, `jackc/pgx/v5`, `sqlc`, `pressly/goose/v3`, `hibiken/asynq`, `google/uuid` (v7), `alexedwards/argon2id` or `x/crypto/argon2`, `go-playground/validator/v10`, `shopspring/decimal`, `go.opentelemetry.io/otel`, `testcontainers-go`, `xuri/excelize/v2` (Phase 5).

## 3. Phase 2 task breakdown

**Status (2026-10-30): done against the documented API.** Tasks 2.1–2.14 are implemented and verified locally: unit tests (signer, client, envelope encryption, capability resolver), integration tests against Postgres 17 with an in-process fake of the documented Shopee endpoints (connect, replayed/foreign/expired state, shop owned by another org, main-account selection, 50 concurrent refreshes → one upstream call, refresh rejection → reconnect, scheduled refresh, webhooks codes 1/2/12 with documented sample bodies, permissions, tenant isolation over the new routes and tables, system-role column grants), mutation checks (removing the refresh row lock, the state-user binding or the push signature check each makes a test fail), `golangci-lint` 0 issues, `sqlc diff`, migrations up/down/up, `nuxt typecheck` and build, Playwright smoke test, Docker image build, and a manual run of the full UI flow (connect, sync, main-account selection) against a local fake. **Nothing is `SANDBOX-VERIFIED`:** that needs S-1.

Deferred from Phase 2:

| Item | Moves to |
|---|---|
| Sandbox E2E run and `SANDBOX-VERIFIED` statuses | When S-1 is done |
| Lost-token recovery via `get_token_by_resend_code` (S-3) | After the FAQ is read and tested in sandbox |
| Dashboard picker when several Shopee apps are enabled (the API already accepts `shopee_app_id`) | Phase 7 |
| Admin UI for capability overrides (table and resolver exist) | Phase 7 |
| Notifications for `RECONNECT_REQUIRED` / expiring authorizations (shown in the dashboard today) | Phase 4 (notifications) |
| Daily `public.get_shops_by_partner` reconciliation and `push.get_lost_push_message` recovery | Phase 4 (scheduler) |
| KEK rotation job (`envelope.Keyring.Rewrap` exists) | Phase 8 |
| Trace propagation into task headers | Phase 4 |

Goal: an admin registers a Shopee partner app, an org admin connects a shop through OAuth, tokens are stored encrypted and refreshed safely, the shop profile is synced, webhooks for authorization changes are processed, and every shop shows its resolved capabilities. Everything is tested against an in-process fake of the documented Shopee responses; nothing is `SANDBOX-VERIFIED` until S-1 is done.

| # | Task | Output | Done when |
|---|---|---|---|
| 2.1 | Envelope encryption | `platform/crypto`: AES-256-GCM, per-row DEK wrapped by a versioned KEK, AAD binding, keyring from `ENCRYPTION_KEYS` | Unit tests: round trip, wrong AAD fails, KEK rotation, tamper detection |
| 2.2 | Signer | `shopee/signer` | Official base-string vector; every field changes the sign |
| 2.3 | Client | `shopee` package: signing per attempt, retries only when safe, error normalization, query-string redaction, per-app and per-shop limits | `httptest` tests for each rule |
| 2.4 | Endpoints | `token/get`, `access_token/get`, `shop/get_shop_info`, `shop/get_profile`, auth link builder, push signature check | Field names match the official API reference |
| 2.5 | Schema | Migration `00002`: `shopee_apps`, `shop_connections`, `shop_credentials`, `shop_authorizations`, `shop_profiles`, `oauth_states`, `capability_definitions`, `capability_overrides`, `webhook_events` | RLS on tenant tables; up/down/up |
| 2.6 | Platform admin | `/admin/shopee-apps` (partner key write-only), `cmd/admin promote` | Integration tests; non-admins get 404 |
| 2.7 | Connect flow | connect, callback (state bound to org + user, single use, 10 min), main-account shop selection, reconnect, disconnect | Integration tests incl. replayed / foreign / expired state |
| 2.8 | Tokens | Just-in-time access with row-locked single-flight refresh; scheduler refresh; `RECONNECT_REQUIRED` on rejection | Concurrency test: one upstream refresh for 50 callers |
| 2.9 | Sync | `shop.sync` job: shop info + profile → `shop_profiles`, connection status and expiry | Integration test |
| 2.10 | Webhooks | Receiver (signature, dedupe, empty 204), processor for codes 1, 2, 12 | Tests with documented sample bodies |
| 2.11 | Capability registry | Seeded definitions from shopee-integration.md §7, overrides, pure resolver | Table tests; API shows reasons |
| 2.12 | Worker | `cmd/worker` (Asynq) with scheduler for due refreshes | Tasks carry IDs only |
| 2.13 | Dashboard | Shops list, connect, callback result, shop detail (profile, capabilities, sync, reconnect, disconnect), main-account selection, admin apps page | Type-check + Playwright |
| 2.14 | Docs sync | Update docs with what was built and what is still unverified | Reviewed |

## 4. Phase 3 task breakdown

**Status (2026-10-30): done.** Verified locally:
- Unit tests: every section schema, structural and semantic validation, override merge, canonical hash, diff, per-field sensitivity.
- Integration tests against Postgres 17: version lifecycle with ETags, the database trigger refusing edits to published sections, clone, soft delete, overrides, preview against shops connected through the Shopee fake (sensitive changes not preselected, unknown current values flagged, region mismatch, disconnected shop fully blocked), permissions, and tenant isolation extended to the new routes and tables.
- `golangci-lint` 0 issues, `sqlc diff`, migrations up/down/up, `nuxt typecheck` and build, a Playwright smoke test (create, edit with a validation error, validate, publish), the Docker build, and a manual UI run of the editor and the preview.

Deferred from Phase 3:

| Item | Moves to |
|---|---|
| Apply endpoint (`…/apply`), snapshots `BEFORE_APPLY`/`AFTER_APPLY`, blocking delete while runs are active | Phase 4 |
| Live current values for logistics, addresses and warehouses in the preview (read with `get_channel_list`, `get_address_list`, `get_warehouse_detail` in the workflow's `fetch_current_config` step) | Phase 4 |
| Market-specific limits (`get_item_limit`, days-to-ship ranges, channel weight limits) | Phase 5 |
| Form-based overrides editor (JSON editor today) and a picker for channel/category IDs | Phase 7 |

Goal: an operator builds a versioned shop template, admins publish it, shop-specific overrides are stored, and a preview shows per shop which fields would change, through which capability and mode, and which changes are sensitive. Applying a template is a workflow and comes in Phase 4.

| # | Task | Output | Done when |
|---|---|---|---|
| 3.1 | Section schemas | One JSON Schema (draft 2020-12) per section in `internal/templates/schema/`, embedded, served by `GET /template-sections` | Every field is grounded in a documented Shopee field or marked as an internal limit |
| 3.2 | Schema | Migration `00003`: `shop_templates`, `shop_template_versions`, `shop_template_sections`, `shop_template_mappings`; RLS; one open draft per template; published sections immutable (trigger) | Up/down/up; isolation tests |
| 3.3 | Templates and versions | Create, list, get, rename, soft delete, clone, new draft from a version, section update with `If-Match`, publish (archives the previous published version, stores a content hash) | Integration tests |
| 3.4 | Validation | Structural (JSON Schema) on every write; semantic checks (cross-field, uniqueness) and warnings on validate/publish | Table tests per section |
| 3.5 | Overrides | Per (template, shop) partial sections, deep-merged on top of the version | Merge unit tests; validated after merge |
| 3.6 | Diff and preview | Pure field-level diff; preview for up to 50 shops against the last synced profile; changes labelled with capability, mode, enabled, sensitive, default selection; fields whose current value is unknown are marked so | Unit + integration tests |
| 3.7 | Dashboard | Template list, editor generated from the schemas, versions, publish, clone, overrides, preview | Type-check + Playwright |
| 3.8 | Docs sync | | Reviewed |

## 5. Definition of done (every phase)

1. Code + tests merged; `go test -race ./...`, lint and type-check green.
2. Integration tests run against real Postgres/Redis containers.
3. Tenant isolation suite extended for new routes and tables.
4. OpenAPI updated; contract tests green.
5. Docs in `docs/` updated in the same PR.
6. No feature is reported as working against Shopee unless it was exercised in sandbox; otherwise it is labelled `DOC-VERIFIED` only.

## 6. Decisions needed from the product owner

| ID | Decision | Needed by |
|---|---|---|
| ~~Q-1~~ | Answered: browser extension in the seller's browser | — |
| Q-2 | Launch markets | Phase 3 (template validation per market) |
| Q-3 | Self-approval by Operators | Phase 4 |
| Q-4 | Retention periods | Phase 1 (audit partitions) — default used if not answered |
| Q-5 | MFA requirement | Phase 1 (optional TOTP) / Phase 8 (enforced) |
| S-1 | Register the Shopee developer account + sandbox app | Sandbox verification of Phase 2 |
