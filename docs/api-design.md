# API Design — Shopee Shop Automation

Status: Phase 0 draft · Last updated: 2026-10-30

This document fixes the conventions and the endpoint catalog. Full request and response schemas live in [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.0.3, the newest version `kin-openapi` validates fully), written in Phase 1 and extended in each phase. Every response in the integration tests is validated against that file, and a test fails if the route table and the document list different endpoints, so the two cannot drift.

## 1. Conventions

| Topic | Rule |
|---|---|
| Base path | `/api/v1` |
| Format | JSON, UTF-8, `snake_case` fields |
| IDs | UUID strings. Shopee IDs are exposed as **strings** (`"shopee_item_id": "123456789012"`) to avoid JS number precision loss |
| Time | RFC 3339 UTC (`2026-10-30T04:05:06Z`) |
| Money | Decimal string + currency: `{"amount": "199000.00", "currency": "VND"}` |
| Tenant | `/api/v1/orgs/{org_id}/...`. Body or query `org_id` is ignored and rejected by `DisallowUnknownFields` |
| Auth | Session cookie `sid`. Unsafe methods need `X-CSRF-Token` |
| Idempotency | `POST` endpoints that start work accept `Idempotency-Key` (UUID). Same key + same body → same response; same key + different body → `409` |
| Pagination | Cursor-based: `?limit=50&cursor=<opaque>`; response `{ "data": [...], "next_cursor": "..." \| null }`. Max limit 200 |
| Filtering/sorting | Explicit allowlisted params per endpoint (`?status=FAILED&sort=-created_at`) |
| Concurrency | Mutable resources return `ETag`; `PATCH`/`PUT` require `If-Match`; mismatch → `412` |
| Rate limits | `429` with `Retry-After`; per IP (auth endpoints) and per user |
| Request ID | `X-Request-ID` accepted or generated; echoed in every response and in errors |

### Success envelope

Single resource: the object itself, `{ "data": {...} }`. Lists: `{ "data": [...], "next_cursor": ... }`. Async starts: `202` with `{ "data": { "workflow_run_id": "..." } }` and a `Location` header.

### Error format

[RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details, `Content-Type: application/problem+json`:

```json
{
  "type": "https://docs.example.com/errors/validation",
  "title": "Validation failed",
  "status": 422,
  "code": "validation_failed",
  "detail": "2 fields are invalid.",
  "request_id": "01JBX...",
  "errors": [
    { "field": "sections.contact.phone", "code": "invalid_format", "message": "Phone must be in E.164 format." }
  ]
}
```

| HTTP | `code` | When |
|---|---|---|
| 400 | `bad_request` | Malformed JSON, unknown fields |
| 401 | `unauthenticated` | No or expired session |
| 403 | `forbidden` / `csrf_failed` | Member lacks permission; CSRF token missing or wrong |
| 404 | `not_found` | Resource missing **or** in another org (never 403 for cross-tenant) |
| 409 | `conflict` | State conflict (publishing a published version, shop already connected elsewhere, idempotency mismatch) |
| 412 | `precondition_failed` | `If-Match` mismatch |
| 413 | `payload_too_large` | Upload over limit |
| 422 | `validation_failed` | Field errors |
| 424 | `shop_reconnect_required` | Operation needs a Shopee connection that is not active |
| 429 | `rate_limited` | Too many requests |
| 500 | `internal` | Unexpected; detail is generic, request_id for support |
| 502/503 | `upstream_unavailable` | Shopee unavailable for a synchronous call |

Shopee errors are never passed through raw. Normalized `code` + safe `message` + Shopee `request_id` are included under `"upstream"` only for members with `shops.manage`.

## 2. Permissions

Permission keys, granted to roles in `role_permissions` (seeded by migration):

| Permission | Owner | Admin | Operator | Viewer |
|---|:-:|:-:|:-:|:-:|
| `org.read` | ✓ | ✓ | ✓ | ✓ |
| `org.update` | ✓ | ✓ | | |
| `org.delete`, `org.transfer` | ✓ | | | |
| `members.read` | ✓ | ✓ | ✓ | ✓ |
| `members.manage` | ✓ | ✓ | | |
| `shops.read` | ✓ | ✓ | ✓ | ✓ |
| `shops.connect`, `shops.disconnect` | ✓ | ✓ | | |
| `shops.sync` | ✓ | ✓ | ✓ | |
| `templates.read` | ✓ | ✓ | ✓ | ✓ |
| `templates.write` (drafts) | ✓ | ✓ | ✓ | |
| `templates.publish` | ✓ | ✓ | | |
| `products.read` | ✓ | ✓ | ✓ | ✓ |
| `products.write`, `imports.write` | ✓ | ✓ | ✓ | |
| `workflows.read` | ✓ | ✓ | ✓ | ✓ |
| `workflows.run`, `workflows.cancel`, `workflows.retry` | ✓ | ✓ | ✓ | |
| `approvals.decide` | ✓ | ✓ | ✓* | |
| `audit.read` | ✓ | ✓ | | |

\* Operators can approve only if the org setting `allow_self_approval` is on **and** they did not start the run (Q-3).

Platform admin endpoints (`/api/v1/admin/*`) need `users.is_platform_admin` (set with `go run ./cmd/admin promote -email …`); everyone else gets `404`.

## 3. Endpoint catalog

`Auth` column: **S** = session required, **P** = public, **A** = platform admin, **W** = Shopee webhook signature, **I** = internal service credential. Permission applies to org-scoped routes.

### 3.1 Authentication

| Method | Path | Auth | Request | Response | Notes / errors |
|---|---|---|---|---|---|
| POST | `/auth/register` | P | `{email, password, display_name}` | `202` | Password ≥ 12 chars, checked against a breached-password list (k-anonymity). Always returns `202` and sends an email ("verify" or "you already have an account"), so the response never reveals whether an email is registered |
| POST | `/auth/login` | P | `{email, password, totp_code?}` | `200 {user}` + cookie | `401` generic; rate-limited 5/min/IP+email |
| POST | `/auth/logout` | S | — | `204` | |
| GET | `/auth/session` | S | — | `{user, memberships[], csrf_token}` | Dashboard bootstrap |
| POST | `/auth/verify-email` | P | `{token}` | `204` | |
| POST | `/auth/verify-email/resend` | S | — | `202` (same answer when already verified; 3 per hour) | |
| POST | `/auth/password/forgot` | P | `{email}` | `202` always | |
| POST | `/auth/password/reset` | P | `{token, new_password}` | `204` | Revokes all sessions |
| GET | `/me` | S | — | `{user}` | |
| PATCH | `/me` | S | `{display_name?}` | `{user}` | |

### 3.2 Organizations and members

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| POST | `/orgs` | S | `{name, slug}` | `201 {org}`; caller becomes owner |
| GET | `/orgs` | S | — | Orgs the caller belongs to |
| GET | `/orgs/{org_id}` | `org.read` | — | `{org}` |
| PATCH | `/orgs/{org_id}` | `org.update` | `{name?, settings?}` + `If-Match` | `{org}` |
| DELETE | `/orgs/{org_id}` | `org.delete` | `{confirm_slug}` | `202` (soft delete + purge job) |
| POST | `/orgs/{org_id}/transfer-ownership` | `org.transfer` | `{new_owner_user_id}` | `204` |
| GET | `/orgs/{org_id}/members` | `members.read` | — | list of `{member}` |
| PATCH | `/orgs/{org_id}/members/{member_id}` | `members.manage` | `{role}` | `{member}`; `403` above own role, `409` last owner |
| DELETE | `/orgs/{org_id}/members/{member_id}` | `members.manage` | — | `204` |
| POST | `/orgs/{org_id}/invitations` | `members.manage` | `{email, role}` | `201 {invitation}` |
| GET | `/orgs/{org_id}/invitations` | `members.manage` | — | list |
| DELETE | `/orgs/{org_id}/invitations/{id}` | `members.manage` | — | `204` |
| POST | `/invitations/accept` | S | `{token}` | `{membership}`; `410` expired/used |

### 3.3 Shops and Shopee connection

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| POST | `/orgs/{org_id}/shops/connect` | `shops.connect` | `{shopee_app_id?}` | `{authorization_url, expires_at}`; `424 no_shopee_app` when none is enabled |
| GET | `/oauth/shopee/callback` | Callback (session if present) | query `code, shop_id \| main_account_id, state` | `302` to the shop page; on error `302` to `/orgs/{org}/shops?connect_error=<code>`; main account → `/orgs/{org}/shops/select?state=<id>`; no session → `/login?next=…` |
| GET | `/orgs/{org_id}/shops/connect/{state_id}` | `shops.connect` (same user) | — | `{id, shops[], expires_at}` (main-account flow, 30 min) |
| POST | `/orgs/{org_id}/shops/connect/{state_id}/select` | `shops.connect` (same user) | `{shopee_shop_ids[]}` | `201 {data: shops[]}` |
| GET | `/orgs/{org_id}/shops` | `shops.read` | `?status=&region=&q=` | list of `{shop}` |
| GET | `/orgs/{org_id}/shops/{shop_id}` | `shops.read` | — | `{shop, profile, capabilities[], history[]}` |
| POST | `/orgs/{org_id}/shops/{shop_id}/sync` | `shops.sync` | — | `202 {job_id}`; `424` if not active |
| POST | `/orgs/{org_id}/shops/{shop_id}/reconnect` | `shops.connect` | — | `{authorization_url, expires_at}` |
| DELETE | `/orgs/{org_id}/shops/{shop_id}` | `shops.disconnect` | `{confirm: true}` | `204` (idempotent) |
| GET | `/orgs/{org_id}/shops/{shop_id}/capabilities` | `shops.read` | — | `[{key, title, mode, enabled, reason, requires_approval, sensitive, verification}]` |
| POST | `/webhooks/shopee/{app_id}` | W | Shopee push body | `204` empty body; `401` bad signature |

Planned for later phases: `GET …/snapshots` (Phase 4), `GET …/categories` and `…/categories/{id}/attributes` (Phase 5).

Callback error codes: `invalid_state`, `state_expired`, `state_used`, `authorization_denied`, `token_exchange_failed`, `shop_connected_elsewhere`, `shop_mismatch`, `shopee_app_disabled`, `permission_denied`, `internal_error`.

`{shop}` never contains tokens. It includes `connection_status`, `authorization_expires_at`, `last_synced_at`, `last_sync_error`, and `environment` (sandbox or production).

### 3.4 Templates

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| GET | `/template-sections` | S | — | JSON Schema (draft 2020-12) per section, in display order |
| GET | `/orgs/{org_id}/templates` | `templates.read` | — | list (`draft_version_id`, `current_published_version_id`, `latest_version_no`) |
| POST | `/orgs/{org_id}/templates` | `templates.write` | `{name, description?, sections?}` | `201 {template, draft_version}` |
| GET | `/orgs/{org_id}/templates/{id}` | `templates.read` | — | `{template, versions[]}` |
| PATCH | `/orgs/{org_id}/templates/{id}` | `templates.write` | `{name?, description?}` | `{template}` |
| DELETE | `/orgs/{org_id}/templates/{id}` | `templates.publish` | — | `204` (soft). Blocking while workflows run comes in Phase 4 |
| POST | `/orgs/{org_id}/templates/{id}/clone` | `templates.write` | `{name, from_version_id?}` | `201 {template, draft_version}` (default source: published, else latest) |
| GET | `/orgs/{org_id}/templates/{id}/versions/{vid}` | `templates.read` | — | `{version, sections[{key, content, etag, updated_at}]}` |
| POST | `/orgs/{org_id}/templates/{id}/versions` | `templates.write` | `{from_version_id}` | `201 {draft_version}`; `409 draft_exists` |
| PUT | `/orgs/{org_id}/templates/{id}/versions/{vid}/sections/{section_key}` | `templates.write` | section JSON + `If-Match: <etag>` | `{section}` + `ETag`; `409 version_not_draft`; `412`; `422` with JSON Schema errors |
| POST | `/orgs/{org_id}/templates/{id}/versions/{vid}/validate` | `templates.read` | — | `{valid, errors[], warnings[]}` |
| POST | `/orgs/{org_id}/templates/{id}/versions/{vid}/publish` | `templates.publish` | — | `{version}`; `422` if invalid; previous published version → `ARCHIVED` |
| GET/PUT | `/orgs/{org_id}/templates/{id}/shops/{shop_id}/overrides` | read / `templates.write` | `{overrides: {section: partial}}` | `{shop_id, overrides, updated_at}`; validated after merging on the template |
| POST | `/orgs/{org_id}/templates/{id}/versions/{vid}/preview` | `templates.read` | `{shop_ids[]}` (1–50) | `[{shop_id, shop_name, connection_status, has_overrides, errors[], warnings[], changes[], blocked[]}]` |
| POST | `/orgs/{org_id}/templates/{id}/versions/{vid}/apply` | `workflows.run` | `{shop_ids[], import_batch_id?}` + `Idempotency-Key` | Phase 4 |

A change item: `{path, section, current, current_known, target, capability, mode, enabled, reason, sensitive, default_selected, note?}`. `current_known` is false when the value was not read from Shopee (only the profile is synced today). Sensitive fields (`x-sensitive` in the schema: shop name, return address, logistics) are never selected by default. Changes whose capability is disabled for the shop are listed under `blocked`.

Merge rule for overrides: objects merge key by key, arrays and scalars replace.

### 3.5 Products and imports

As built in Phase 5 (`api/openapi.yaml` is the contract).

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| GET | `/orgs/{org_id}/products` | `products.read` | `?q=&batch_id=&limit=&cursor=` | list with images and per-shop mappings |
| POST | `/orgs/{org_id}/products` | `products.write` | product JSON (tiers ≤ 2, variations ≤ 50) | `201 {product}` + `ETag` |
| GET | `/orgs/{org_id}/products/{id}` | `products.read` | — | `{product}` with variations, images (presigned URLs), mappings |
| PUT | `/orgs/{org_id}/products/{id}` | `products.write` | full product + `If-Match` | `{product}`; `412` on a stale ETag |
| DELETE | `/orgs/{org_id}/products/{id}` | `products.write` | — | `204` (soft; does **not** delete on Shopee) |
| POST | `/orgs/{org_id}/products/{id}/images` | `products.write` | multipart `file` (JPEG/PNG/WebP, ≤ 10 MB, ≤ 40 MP) | `201 {image}`; `413`, `422` |
| DELETE | `/orgs/{org_id}/products/{id}/images/{image_id}` | `products.write` | — | `204` |
| POST | `/orgs/{org_id}/products/publish` | `workflows.run` | `{product_ids[] \| import_batch_id, shop_ids[] (≤ 50), item_status?}` + optional `Idempotency-Key` | `202 {batch_id, workflow_run_ids[], replayed}` |
| POST | `/orgs/{org_id}/imports` | `imports.write` | multipart `file` (CSV/XLSX ≤ 20 MB), `template_id?`, `template_version_id?` | `202 {batch}` |
| GET | `/orgs/{org_id}/imports` | `products.read` | — | recent batches |
| GET | `/orgs/{org_id}/imports/sample.csv` | `products.read` | — | sample file |
| GET | `/orgs/{org_id}/imports/{batch_id}` | `products.read` | — | `{batch}` with counts |
| GET | `/orgs/{org_id}/imports/{batch_id}/rows` | `products.read` | `?status=&limit=&cursor=` | rows with `errors[]` |
| PATCH | `/orgs/{org_id}/imports/{batch_id}/rows/{row_id}` | `imports.write` | `{values}` (empty value removes a cell) | `{row}` revalidated |
| POST | `/orgs/{org_id}/imports/{batch_id}/retry` | `imports.write` | — | `202 {batch}`; imports fixed and failed rows, or reads the file again after a `FAILED` batch |
| GET | `/orgs/{org_id}/imports/{batch_id}/report` | `products.read` | — | `{url, expires_at}` (presigned CSV) |

Run views include `item_key` on fan-out item steps (the product ID for PRODUCT_PUBLISH).

### 3.6 Workflows and approvals

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| POST | `/orgs/{org_id}/templates/{id}/versions/{vid}/apply` | `workflows.run` | `{shop_ids[]}` (1–50) + optional `Idempotency-Key` (UUID) | `202 {batch_id, workflow_run_ids[], replayed}`; `409 version_not_published`, `shop_busy`, `idempotency_mismatch`; `424` if a shop is not connected |
| GET | `/orgs/{org_id}/workflows` | `workflows.read` | `?status=&shop_id=&batch_id=&limit=&cursor=` | list of `{run}` |
| GET | `/orgs/{org_id}/workflows/{run_id}` | `workflows.read` | — | `{run, steps[], progress, pending_approval}` |
| GET | `/orgs/{org_id}/workflows/{run_id}/events` | `workflows.read` | `?after=<cursor>` | events in order; poll with the returned cursor |
| POST | `/orgs/{org_id}/workflows/{run_id}/cancel` | `workflows.cancel` | `{reason?}` | `202 {run view}`; `409 not_cancellable` |
| POST | `/orgs/{org_id}/workflows/{run_id}/retry` | `workflows.retry` | — | `202`; `409 not_retryable` unless FAILED/PARTIALLY_COMPLETED |
| POST | `/orgs/{org_id}/workflows/{run_id}/resume` | `workflows.run` | — | `202`; `409` if not waiting or only manual tasks are open |
| POST | `/orgs/{org_id}/workflows/{run_id}/steps/{step_id}/complete` | `workflows.run` | `{note?}` | `202` (manual tasks done) |
| GET | `/orgs/{org_id}/workflow-events` | `workflows.read` | `?level=&cursor=` | execution log across runs, newest first |
| GET | `/orgs/{org_id}/approvals` | `workflows.read` | `?status=` | list |
| GET | `/orgs/{org_id}/approvals/{id}` | `workflows.read` | — | `{approval}` with `diff: {changes[], blocked[]}` |
| POST | `/orgs/{org_id}/approvals/{id}/decision` | `approvals.decide` | `{decision: APPROVE\|REJECT, selected_change_paths[], comment?}` | `{approval}`; `409 already_decided`; `422` unknown path; `403 self_approval_not_allowed` |

Step retries with explicit `step_ids` were not needed: retry resets every failed step and never reruns succeeded ones.

### 3.7 Audit and notifications

| Method | Path | Permission | Request | Response |
|---|---|---|---|---|
| GET | `/orgs/{org_id}/audit-logs` | `audit.read` | `?actor_id=&action=&target_type=&from=&to=` | list |
| GET | `/orgs/{org_id}/overview` | `shops.read` | — | counters for the overview page (as built; replaces `/dashboard`) |
| GET | `/orgs/{org_id}/shops/{id}/activity` | `shops.read` | — | last setup run, published products, run history |
| GET | `/orgs/{org_id}/notifications` | member | `?unread=true&limit=` | the caller's notifications in this org + `unread` count (as built: per org, so RLS applies) |
| POST | `/orgs/{org_id}/notifications/{id}/read` | member | — | `204` |
| POST | `/orgs/{org_id}/notifications/read-all` | member | — | `204` |
| GET | `/orgs/{org_id}/shopee-apps` | `shops.connect` | — | enabled apps to connect with (no secrets) |
| GET | `/orgs/{org_id}/shops/{id}/catalog/categories` | `products.read` | `?refresh=true` | Shopee categories of the shop's market (cached 24 h) |
| GET | `/orgs/{org_id}/shops/{id}/catalog/categories/{cat}/attributes` | `products.read` | — | attribute tree (cached 24 h) |
| GET | `/orgs/{org_id}/shops/{id}/catalog/categories/{cat}/brands` | `products.read` | — | brands, first 1,000 (cached 24 h) |

Platform admin (Phase 7): `GET /admin/capabilities`, `PUT /admin/capability-overrides` (upsert per capability + app + market; `enabled: false` is a kill switch), `DELETE /admin/capability-overrides/{id}`. Extension: `POST /ext/tasks/{id}/release` gives a held task back to the queue (Stop).

### 3.8 Platform admin

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET/POST | `/admin/shopee-apps` | A | List and register partner apps (partner key write-only; never returned). Hosts default to the documented hosts of the environment |
| PATCH | `/admin/shopee-apps/{app_id}` | A | Rename, replace key, change hosts/URLs, enable/disable |
| GET/PATCH | `/admin/capabilities` | A | Registry and overrides (planned) |
| GET | `/admin/organizations` | A | Org list (no business data) |

### 3.9 Browser extension

As built in Phase 6. Auth **D** = extension device token (`Authorization: Bearer`). No cookies, no CSRF; CORS allows only `EXTENSION_ORIGINS`. See [browser-extension.md](browser-extension.md) §11.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/orgs/{org_id}/extension/pairing-codes` | S + `workflows.run` | One-time code (`XXXXX-XXXXX`, 6 minutes) |
| GET | `/orgs/{org_id}/extension/devices` | S + `workflows.read` | Paired browsers |
| DELETE | `/orgs/{org_id}/extension/devices/{id}` | S + `workflows.run` (own device) or `members.manage` | Unpair |
| GET | `/orgs/{org_id}/workflows/{run_id}/browser-tasks` | S + `workflows.read` | Tasks of a run with the extension's step log and screenshot IDs |
| POST | `/orgs/{org_id}/browser-tasks/{id}/requeue` | S + `workflows.run` | Queue a failed task again |
| GET | `/orgs/{org_id}/browser-tasks/{id}/screenshots/{sid}` | S + `workflows.read` | Presigned link (5 minutes, audited) |
| POST | `/ext/pair` | none (rate limited) | Code → device token (returned once) |
| GET | `/ext/me` | D | Device, org, user |
| POST | `/ext/token/rotate` | D | New token; the old one stops working |
| DELETE | `/ext/device` | D | Unpair this browser |
| GET | `/ext/tasks` | D | Queued tasks of the org and tasks this device holds |
| POST | `/ext/tasks/{id}/claim` | D | Lease (10 min) + signed recipe + presigned inputs; `409 task_unavailable \| recipe_disabled \| capability_disabled` |
| POST | `/ext/tasks/{id}/heartbeat` | D | Extend lease; `state: RUNNING \| WAITING_USER` |
| POST | `/ext/tasks/{id}/screenshots` | D | Multipart PNG/JPEG ≤ 5 MB, 10 per task |
| POST | `/ext/tasks/{id}/result` | D | `{lease_token, status, error_code?, message?, steps[]}`; stale lease `409 lease_lost` |
| GET/POST | `/admin/browser-recipes` | platform admin | List; add a version (validated, disabled) |
| PATCH | `/admin/browser-recipes/{id}` | platform admin | Verify, enable (one per capability and market), disable (kill switch) |

## 4. Validation rules (examples)

| Field | Rule |
|---|---|
| email | RFC 5322 subset, ≤ 254 chars, lowercased |
| org slug | `^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$`, reserved words blocked |
| role | one of `owner, admin, operator, viewer`; `owner` only via transfer |
| shop_ids in apply/preview | 1–50 UUIDs, all in the org, all `ACTIVE` |
| template section JSON | JSON Schema per section (draft 2020-12), stored in `internal/templates/schema/` and published in OpenAPI |
| product name / description lengths | Checked against `get_item_limit` for the shop's category at validate time; static max as first pass |
| price | > 0, ≤ 2 decimals, within `price_limit` |
| images | JPEG/PNG/WebP by magic bytes, ≤ 10 MB, decoded size ≤ 40 MP |
| uploads | CSV/XLSX ≤ 50 MB; ZIP ≤ 500 MB, ≤ 5,000 entries, no path traversal |

## 5. Versioning

Additive changes stay in `v1`. Breaking changes go to `/api/v2` with at least 6 months of overlap. Deprecated fields are marked in OpenAPI and announced in the changelog.
