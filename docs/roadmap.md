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
| Emails through a retried background job | Done in Phase 7 (`notify.Outbox`) |
| Breached-password check (k-anonymity range API) | Done in Phase 8 |
| TOTP / MFA (Q-5) | Done in Phase 8 |
| Platform admin endpoints (`/admin/*`) | Done in Phase 2 (`/admin/shopee-apps`) |
| Job that creates future `audit_logs` partitions | Done in Phase 4; purge of deleted organizations moved to Phase 8 |
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
| Dashboard picker when several Shopee apps are enabled (the API already accepts `shopee_app_id`) | Done in Phase 7 |
| Admin UI for capability overrides (table and resolver exist) | Done in Phase 7 |
| Notifications for `RECONNECT_REQUIRED` / expiring authorizations (shown in the dashboard today) | Phase 4 (notifications) |
| Daily `public.get_shops_by_partner` reconciliation and `push.get_lost_push_message` recovery | Phase 4 (scheduler) |
| KEK rotation job (`envelope.Keyring.Rewrap` exists) | Done in Phase 8 |
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
| Form-based overrides editor (JSON editor today) and a picker for channel/category IDs | Done in Phase 7 (overrides form; category picker in the product form) |

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

## 5. Phase 4 task breakdown

**Status (2026-10-30): done against the documented API.** Verified locally:
- Exhaustive state-machine table test.
- Engine integration tests on Postgres 17: duplicate delivery runs a step once, expired lease is taken over and the zombie's late result is dropped, retry with backoff, user retry reruns only failed steps, ContinueOnFailure, mutating steps serialize per shop, approve/reject, manual tasks, resume, cancel, sweeper recovers lost tasks.
- SHOP_SETUP end to end through the API against the Shopee fake: idempotent apply, one active setup per shop, selection validation, self-approval rule, profile/channel/address writes, manual tasks, verification and report, partial failure then retry, lost authorization then reconnect and resume.
- Tenant isolation extended to every workflow route and table; system role cannot read step data; partition job.
- Mutation checks: removing the lease fence or the selection check each fails a test.
- `golangci-lint` 0 issues, `sqlc diff`, migrations up/down/up, `nuxt typecheck` and build, Playwright smoke test, Docker build, and a manual UI run (apply → approve → manual task → completed).

Deferred from Phase 4:

| Item | Moves to |
|---|---|
| Steps 9–11 (products) and fan-out per item | Phase 5 |
| Shop logo upload through the API (S-4); today a manual task | When S-4 is verified |
| Emails through a retried job; in-app notifications (approval waiting, reconnect required) | Done in Phase 7 |
| Per-org concurrency limit and per-class queues (critical/default/bulk/low) | Phase 8 (load tests decide the numbers) |
| Compensation (restore from `BEFORE_APPLY`) | Later; needs product decision |
| Purge of deleted organizations; daily `get_shops_by_partner` reconciliation; `get_lost_push_message` recovery; trace propagation into tasks | Phase 8 |

Goal: applying a published template to shops starts one SHOP_SETUP run per shop. Runs read the current shop configuration, show a diff for approval, apply only approved changes through the Open API, turn operations without an API into manual tasks, verify the result and report. Runs survive worker crashes, duplicate deliveries and retries without applying anything twice.

| # | Task | Output | Done when |
|---|---|---|---|
| 4.1 | State machines | Pure `Transition` for runs and steps | Exhaustive table test (every state × every event) |
| 4.2 | Schema | Migration `00004`: `workflow_runs`, `workflow_steps`, `workflow_step_attempts`, `workflow_approvals`, `workflow_events` (monthly partitions), `shop_config_snapshots` | RLS, one active SHOP_SETUP per shop, idempotency key per org |
| 4.3 | Engine | Definitions in code, start, claim with lease (conditional update), fenced finish, dependencies, retry with full-jitter backoff, approvals, manual tasks, cancel, resume, user retry, events | Integration tests: duplicate delivery runs once, expired lease is taken over, zombie cannot overwrite, retry only failed steps |
| 4.4 | Sweeper | Scheduled task that re-enqueues runnable steps and steps with expired leases | Test |
| 4.5 | Shopee endpoints | `logistics/get_channel_list`, `logistics/get_address_list`, `shop/update_profile`, `logistics/update_channel`, `logistics/update_address`, `logistics/set_address_config` | Field names from the reference; fake extended |
| 4.6 | SHOP_SETUP v1 | Steps 1–8 and 12–13 from workflow-design.md §6 (products 9–11 come in Phase 5). Branding, warehouse and address creation become manual tasks until the extension exists | End-to-end test against the fake |
| 4.7 | API | Apply (with `Idempotency-Key`), runs list/detail/events, cancel, retry, resume, complete manual task, approvals list/detail/decision with the self-approval rule (Q-3) | Contract + isolation tests |
| 4.8 | Housekeeping jobs | Create future `audit_logs` / `workflow_events` partitions | Test |
| 4.9 | Dashboard | Apply from the preview, workflow list and detail (steps, approval with change selection, manual tasks), execution logs | Type-check + Playwright |
| 4.10 | Docs sync | | Reviewed |

## 6. Phase 5 task breakdown

**Status (2026-10-30): done against the documented API (`DOC-VERIFIED`; not yet exercised in a Shopee sandbox).** Verified locally:
- Storage against SeaweedFS; image pipeline unit tests (HTTPS only, private and redirected addresses blocked, magic-byte types, size and pixel caps, re-encode).
- Products CRUD with `If-Match`, variations and images; imports from CSV/XLSX with row errors, fix-and-revalidate, retry, report, update by SKU; an import whose file cannot be read ends `FAILED` and can be retried.
- Engine fan-out (per-item steps, continue on item failure, retry reruns only failed items and the report).
- PRODUCT_PUBLISH end to end against the Shopee fake: blocked products with reasons (currency vs market, missing mandatory attribute, non-standard variation, no images), approval with selection validation, image upload once per shop, `add_item` with a lost response reconciled by `item_sku` (the item is adopted, never created twice), standardised variations, verification, report, idempotent start and a second publish that calls nothing.
- Tenant isolation: every product/import route with org A's IDs, cross-org publish, every table with `org_id` must have forced RLS and be in the suite (this found `shop_template_mappings` missing from the suite; now covered).
- `go test -race`, `golangci-lint` 0 issues, `sqlc diff`, migrations up/down/up, `nuxt typecheck` and build, Playwright smoke test (product with image, import with a fixed row), Docker build.

Deferred from Phase 5:

| Item | Moves to |
|---|---|
| Sandbox verification of every product endpoint and of the add_item → init_tier_variation delay | When S-1 is available |
| Category, attribute and brand pickers in the dashboard (today: IDs typed by hand; the publish check lists what is missing) | Done in Phase 7 |
| Channels with `fee_type = SIZE_SELECTION` (need a `size_id` per item) | Later; needs a product decision |
| Updating items that already exist on Shopee (price, stock, description); deleting items | Later phase (sync) |
| Pre-order (`pre_order.days_to_ship`), wholesale, video, size chart, GTIN | Later |
| Malware scanning of uploaded images beyond decode/re-encode | Done in Phase 8 |
| Item-level rate limiting across runs (`add_item` quotas per shop) | Phase 8 (load tests) |

Goal: products can be created by hand or imported from CSV/XLSX, validated row by row, and published to shops by a PRODUCT_PUBLISH workflow that never creates a duplicate on Shopee, with a per-shop mapping to Shopee item and model IDs and a downloadable report.

| # | Task | Output | Done when |
|---|---|---|---|
| 5.1 | Object storage | `platform/storage` (S3 API; SeaweedFS in development because MinIO no longer publishes public images), presigned GET URLs | Integration test against the dev S3 |
| 5.2 | Image pipeline | SSRF-safe HTTPS fetcher; type by magic bytes (JPEG/PNG/WebP), ≤ 10 MB, ≤ 40 MP; decode and re-encode to JPEG/PNG (drops metadata and polyglot payloads; Shopee accepts JPG/PNG) | Unit tests incl. private-IP and redirect cases |
| 5.3 | Schema | Migration `00005`: products, variations, images, product mappings, image uploads, import batches and rows | RLS; one SKU per org |
| 5.4 | Products | CRUD with `If-Match`, up to 2 tiers of variations, image upload | Integration tests |
| 5.5 | Imports | Streaming CSV/XLSX parser with row/size caps, per-row validation, fix-and-revalidate, materialize valid rows into products, CSV report (formula-safe) in storage | Integration tests |
| 5.6 | Shopee product APIs | `get_category`, `get_attribute_tree`, `get_brand_list`, `get_item_limit`, `get_variation_tree`, `media_space.upload_image`, `add_item`, `init_tier_variation`, `search_item`, `get_item_base_info`, `get_model_list` | Fields from the reference; fake extended |
| 5.7 | Engine fan-out | A step may expand into per-item steps that run in parallel | Engine tests |
| 5.8 | PRODUCT_PUBLISH | Prepare (category, attributes, brand, limits, channels, standardised variations per shop), approval, image upload, `add_item` protocol with reconciliation by `item_sku`, variations, verification, report | End-to-end test incl. ambiguous `add_item` |
| 5.9 | Dashboard | Products list/detail/form, imports (upload, rows, fix, report), publish dialog | Type-check + Playwright |
| 5.10 | Docs sync | | Reviewed |

## 7. Phase 6 task breakdown

**Status (2026-10-30): built and tested against local fixtures. No real Seller Centre recipe exists yet**, so every capability without an Open API is still a manual task in practice. Enabling one needs a recipe written and verified on a real seller account per market (E-1, E-2).

Goal: operations without an Open API (banner, decoration, warehouse, address creation) run in the seller's own browser through an extension, with the seller present, after the workflow approval, and with the same guarantees as API steps (no double run, kill switch, audit).

| # | Task | Output | Done when |
|---|---|---|---|
| 6.1 | Contract | browser-extension.md §11, `internal/browser/recipe.schema.json`, OpenAPI tag `extension` | Both sides build against it |
| 6.2 | Schema | Migration `00006`: `extension_pairing_codes`, `extension_devices`, `browser_recipes`, `browser_tasks`, `browser_task_screenshots` | RLS (recipes are platform-wide), isolation suite |
| 6.3 | Pairing and devices | One-time code, hashed device token, sliding 30-day expiry, rotation, revoke, membership/role re-checked on every call | Integration tests |
| 6.4 | Recipes | Schema + semantic validation (shop check before any write, templates only in value fields, origins), Ed25519 signing, admin versions with verify/enable/disable, auto-disable after 3 recipe faults in a row | Unit + integration tests |
| 6.5 | Task queue | Create from SHOP_SETUP manual step, list, claim with lease and kill switches, heartbeat/pause, screenshots, fenced result; step completes when all its tasks were done by the extension | Integration tests |
| 6.6 | Capability resolution | `BROWSER` only with a paired device and an enabled recipe for the market; otherwise `MANUAL` with a reason | Resolver table test |
| 6.7 | Extension | WXT MV3 + Vue popup: pairing, task list, runner (signature, validation, targeting, stop_on pause, shop-ID check, uploads, multi-page), token rotation | Vitest + Playwright with fixture pages |
| 6.8 | Dashboard | Browser Extension page (pair, devices), task status in runs (retry, screenshots), admin recipes page | Type-check + Playwright |
| 6.9 | Docs sync | | Reviewed |

Verified locally: Go unit and integration tests (pairing, wrong/used/expired codes, rotation, revoke, role change, sliding expiry, CORS, full banner run with signature check, mixed manual/browser step, auto-disable and kill switch, cancellation when the step is finished by hand, cross-org isolation); extension 101 unit tests and 7 Playwright tests against a fake API and fixture pages; `go test -race`, `golangci-lint`, `sqlc diff`, migrations up/down/up, dashboard type-check, build and Playwright smoke, Docker build.

Not verified:
- Real Seller Centre pages, selectors and recipes (none exist; E-1, E-2).
- Screenshots: `captureVisibleTab` needs the `activeTab` grant from a real toolbar click, which Playwright cannot produce (E-5). A refused screenshot is skipped.
- The extension against the real Go API end to end (both sides are tested against the same contract: the API with OpenAPI contract tests, the extension with a fake API).
- A service worker killed mid-run (the run is marked interrupted and not resumed blindly); Edge, Cốc Cốc and Brave.

Deferred from Phase 6:

| Item | Moves to |
|---|---|
| First real recipes (banner, address creation) per market, verified on a seller account | When E-1/E-2 are answered and a test seller account exists |
| Extension store listing and privacy text (E-4); signed CRX/update channel | Before production |
| "Release" a claimed task back to the queue (today Stop finishes it as FAILED `stopped_by_user`, which does not count against the recipe) | Done in Phase 7 |
| Notifications when a task waits for the extension | Done in Phase 7 (waiting-for-you notification; the overview counts queued extension tasks) |
| Recipe failure-rate alerting beyond auto-disable | Phase 8 |

## 8. Phase 7 task breakdown

**Status (2026-10-30): done.** Verified locally: Go integration tests (overview counts; notifications to the right roles and users, dedupe, read/read-all, another user's notification unreachable; email outbox survives a mail outage, stores no plaintext, sends after recovery and deletes the row; catalog cache hit, refresh and expiry, cross-org 404; capability override kill switch and removal, admin-only; app options without secrets; extension release), extension unit + Playwright tests, dashboard type-check, build, Playwright smoke and an axe-core audit of every main page (0 WCAG A/AA violations), `go test -race`, `golangci-lint`, `sqlc diff`, migrations up/down/up, Docker build. The isolation suites now include the new tables and routes.

| # | Task | Output |
|---|---|---|
| 7.1 | Overview | `GET /orgs/{org}/overview`; stat cards (linked to filtered lists) and a "Needs attention" list |
| 7.2 | Shop detail | Configuration status (last setup), products published / needing attention, last sync, setup history |
| 7.3 | Notifications | In-app notifications written with the event (approval, waiting for you, failed, reconnect required); bell with unread count |
| 7.4 | Email outbox | Encrypted outbox + retried `mail:send` task + sweeper (Phase 1/4 deferral) |
| 7.5 | Pickers | Category search, mandatory attributes and brands from a connected shop, cached |
| 7.6 | Extension Stop | `POST /ext/tasks/{id}/release`; Stop no longer finishes the task as FAILED |
| 7.7 | Admin | Capability overrides page; Shopee app picker on connect; form editor for template overrides (JSON kept as an option) |
| 7.8 | Accessibility | axe-core audit in Playwright; contrast fix |
| 7.9 | Docs sync | |

Found and fixed during Phase 7: the worker had no SMTP mailer (it only logged emails), which would have silently dropped every verification email once email moved to the worker; the HTTP isolation test could fail at random when a request ID contained the shop ID it searches for.

Deferred from Phase 7:

| Item | Moves to |
|---|---|
| Email notifications and per-user notification preferences | Later; needs a product decision on what is worth an email |
| Live updates (server-sent events) instead of polling | Phase 8 if load tests show polling is too costly |
| Manual screen-reader pass (NVDA/VoiceOver) beyond the automated audit | Before production |

## 9. Phase 8 task breakdown

**Status (2026-10-31): done locally.** Not deployed: no production environment exists yet (the managed-platform choice is open).

| # | Task | Result |
|---|---|---|
| 8.1 | Two-step verification (Q-5) | TOTP + recovery codes, two-step sign-in, per-org requirement for owners/admins, mandatory for platform admins in production; dashboard account page and settings |
| 8.2 | Breached passwords | k-anonymity range check on sign-up and reset (`PWNED_PASSWORDS_URL`) |
| 8.3 | KEK rotation | `admin rewrap` |
| 8.4 | Purge of deleted organizations | Hourly job + `admin purge-orgs`; files, events, rows; audit kept |
| 8.5 | Per-org concurrency | `WORKFLOW_ORG_CONCURRENCY` |
| 8.6 | Malware scanning | Optional clamd scan of uploads (fails closed) |
| 8.7 | Security checks | `govulncheck`: 0 reachable after Go 1.27.3 and the excelize fix; `npm audit`: extension 0; dashboard findings are build-tool only (see architecture.md §15) |
| 8.8 | Load tests (`cmd/loadtest`) | 500 orgs / 5,000 shops / 25,000 products / 10,000 runs: 50 users, ~5,000 requests/s, p95 15 ms, p99 19 ms, 0 errors (NFR-1, NFR-5). 10,000-row import parsed and validated in 6.1 s (NFR-6; image downloads not included). Single dev machine, Postgres and Redis in Docker. |
| 8.9 | Deploy, backups, runbooks | `deploy/production` (compose reference, nginx, secrets as files), `deploy/backup` (encrypted backup, guarded restore, restore drill: 80 tables match row for row), `.github/workflows/release.yml`, docs/deployment.md, docs/runbooks.md |

Verified locally: Go unit and integration tests (TOTP vectors, MFA sign-in incl. replay, attempts, expiry, recovery codes, org and admin requirements; breached-password client; rewrap; purge across every tenant table; concurrency cap; clamd scanning incl. fail-closed), `go test -race`, `golangci-lint`, `sqlc diff`, migrations up/down/up, Docker build, `make prod-config-check` (compose config, `nginx -t`), restore drill, dashboard type-check and Playwright (smoke incl. MFA sign-in, accessibility audit incl. the account page).

Deferred from Phase 8:

| Item | Moves to |
|---|---|
| Choose the production platform; run the stack, PITR and alerting there | Before go-live |
| Load test on production-like hardware, with Shopee calls through the sandbox | After S-1 |
| Shopee reconciliation jobs (`get_shops_by_partner`, `get_lost_push_message`) | After S-1 (sandbox) |
| Revoke-all-sessions / revoke-all-devices from the dashboard (runbooks use SQL) | Later |
| Per-class queues (critical/default/bulk/low) | When production metrics show contention |
| Personal access tokens (A-5) | Later |
| Trace propagation into queued tasks | Later |

## 10. Definition of done (every phase)

1. Code + tests merged; `go test -race ./...`, lint and type-check green.
2. Integration tests run against real Postgres/Redis containers.
3. Tenant isolation suite extended for new routes and tables.
4. OpenAPI updated; contract tests green.
5. Docs in `docs/` updated in the same PR.
6. No feature is reported as working against Shopee unless it was exercised in sandbox; otherwise it is labelled `DOC-VERIFIED` only.

## 11. Decisions needed from the product owner

| ID | Decision | Needed by |
|---|---|---|
| ~~Q-1~~ | Answered: browser extension in the seller's browser | — |
| Q-2 | Launch markets | Phase 3 (template validation per market) |
| Q-3 | Self-approval by Operators | Phase 4 |
| Q-4 | Retention periods | Phase 1 (audit partitions) — default used if not answered |
| Q-5 | MFA requirement: provisional answer built in Phase 8 (per-org requirement; mandatory for platform admins). Confirm whether every owner must use it | Before go-live |
| S-1 | Register the Shopee developer account + sandbox app | Sandbox verification of Phase 2 |
