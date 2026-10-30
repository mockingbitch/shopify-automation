# Requirements — Shopee Shop Automation

Status: Phase 0 draft · Last updated: 2026-10-30

This document restates the product requirements in a testable form, records what was learned during discovery, and lists the assumptions and open questions that later phases depend on.

## 1. Repository assessment

| Item | Finding |
|---|---|
| Repository | Empty directory, not yet a git repository. No existing code, conventions or CI. |
| Local toolchain | Go 1.27.1, Node.js 24.21, Docker Engine (with Compose). No local PostgreSQL or Redis; both will run in Docker Compose. |
| Node version | Spec asks for Node.js 26. The browser extension's build and Playwright tests pin Node 26 in CI. Local development on Node 24 works. |
| Folder name | The folder is named `shopify-automation`, but the product targets **Shopee**. Nothing in the code will reference Shopify. |

Because the repository is greenfield, all conventions are defined in [architecture.md](architecture.md).

## 2. Actors

| Actor | Description |
|---|---|
| Platform Admin | Operator of the SaaS. Manages Shopee partner apps, capability registry, global settings. Does not see tenant business data by default. |
| Organization Owner | Created the organization. Full control including deleting the organization and transferring ownership. Exactly one per organization. |
| Organization Admin | Manages members, shops, templates, approvals. Cannot delete the organization or change the owner. |
| Operator | Runs workflows, imports products, edits templates (draft only). Cannot approve their own high-risk workflow steps when approval separation is enabled. |
| Viewer | Read-only access to the organization. |
| Seller (external) | The person who logs in to Shopee to authorize the app. Often the same person as an Owner/Admin, but not necessarily. |
| Shopee Open Platform | External system. Sends push notifications (webhooks) and answers API calls. |

## 3. Functional requirements

IDs are referenced by tests and the roadmap.

### 3.1 Identity and tenancy (Phase 1)

- **FR-ID-1** Users sign up and sign in with email and password. Passwords are hashed with Argon2id.
- **FR-ID-2** A user can belong to many organizations, with one role per organization.
- **FR-ID-3** Owners and Admins invite members by email. Invitations expire after 7 days and are single-use.
- **FR-ID-4** Owners and Admins change member roles. Nobody can grant a role higher than their own. The last Owner cannot be removed or demoted.
- **FR-ID-5** Every business request resolves an organization from the URL path, then verifies the caller's membership. A client-supplied `organization_id` in a body or query is never trusted.
- **FR-ID-6** Every state-changing action writes an audit log entry (actor, organization, action, target, outcome, request ID, IP, user agent). Audit logs are append-only.

### 3.2 Shopee connection (Phase 2)

- **FR-SH-1** A Platform Admin registers one or more Shopee partner apps (partner ID, partner key, environment, region host, redirect domain).
- **FR-SH-2** An Org Admin starts "Connect shop", is redirected to Shopee's authorization page, and returns to a callback that exchanges the `code` for tokens.
- **FR-SH-3** The callback is bound to the initiating organization and user through a signed, single-use `state` value (CSRF protection).
- **FR-SH-4** One Shopee shop can be connected to at most one organization at a time per partner app (DB unique constraint). Re-connecting from another organization is rejected with a clear message.
- **FR-SH-5** Access tokens (4 h) are refreshed before expiry. Refresh tokens (30 days, single-use) are rotated atomically.
- **FR-SH-6** Authorization expiry (up to 365 days, chosen by the seller) is tracked. Users are warned 7 days before expiry (Shopee push code 12, plus our own scheduled check).
- **FR-SH-7** Revocation (push code 2) or refresh failure moves the connection to `RECONNECT_REQUIRED`. Workflows that need the shop pause in `WAITING_USER`.
- **FR-SH-8** Shop information (`get_shop_info`, `get_profile`, `get_warehouse_detail`, `logistics.get_channel_list`, `logistics.get_address_list`) is synchronized on connect, on demand and on a schedule.

### 3.3 Capability registry (Phase 2)

- **FR-CAP-1** Every operation the platform can perform against a shop is a capability with a key, execution mode (`API`, `BROWSER`, `MANUAL`, `UNSUPPORTED`), required platform permission, supported markets, approval requirement, enabled flag, documentation reference and last verification date.
- **FR-CAP-2** The workflow engine resolves the execution mode at run time from the registry plus the shop's market and app permissions. Business logic never hardcodes "call API X".
- **FR-CAP-3** Platform Admins can disable a capability globally, per market or per partner app, without a deploy.

### 3.4 Templates (Phase 3)

- **FR-TPL-1** Templates contain the sections listed in the master spec (general, branding, description, contact, warehouse, return address, logistics, product defaults, category mappings, SKU rules, default descriptions, default attributes).
- **FR-TPL-2** A template has versions. A version is `DRAFT` until published. Published versions are immutable; editing creates a new draft version.
- **FR-TPL-3** Clone creates a new template whose first draft copies the source version.
- **FR-TPL-4** Applying a template to N shops creates N workflow runs (one per shop), sharing a batch ID.
- **FR-TPL-5** Shop-specific overrides are stored per (template, shop) and merged on top of the version at apply time.
- **FR-TPL-6** Preview shows a field-level diff between the shop's current snapshot and the resolved target configuration, and labels each change with its capability and execution mode.
- **FR-TPL-7** Fields marked *sensitive* or *user-managed* (shop name, return address, logistics on/off, COD) are never changed unless the user explicitly ticks them in the approval step.

### 3.5 Products (Phase 5)

- **FR-PRD-1** Import from CSV and XLSX, or create manually. Files up to 50 MB / 100,000 rows (assumption A-7).
- **FR-PRD-2** Each row is validated independently. Errors carry row number, column and message.
- **FR-PRD-3** Partial success is allowed. Failed rows can be fixed and retried without touching succeeded rows.
- **FR-PRD-4** Category mapping uses template mappings first, then `category_recommend` as a suggestion that a user confirms.
- **FR-PRD-5** Required attributes come from `get_attribute_tree` (`mandatory: true`). Brand rules come from `get_brand_list` (`is_mandatory`). Limits come from `get_item_limit`.
- **FR-PRD-6** Images are validated (type, max 10 MB, dimensions), stored in object storage, then uploaded through `media_space.upload_image` (scene `normal` for product images, `desc` for description images).
- **FR-PRD-7** Creating a product never duplicates it on retry (see [workflow-design.md](workflow-design.md#idempotency-against-shopee)).
- **FR-PRD-8** Internal product ID ↔ Shopee `item_id` / `model_id` mapping is stored per shop.
- **FR-PRD-9** An import report (counts, per-row outcome, Shopee IDs) is downloadable as CSV.

### 3.6 Workflow engine (Phase 4)

- **FR-WF-1** Workflow and step state lives in PostgreSQL. Asynq only delivers work.
- **FR-WF-2** Statuses and transitions follow [workflow-design.md](workflow-design.md).
- **FR-WF-3** Steps support retry policy (max attempts, exponential backoff with full jitter), timeout, idempotency key and optional compensation.
- **FR-WF-4** Users can cancel, resume and retry a run, and approve or reject an approval step.
- **FR-WF-5** A worker restart or duplicate task delivery never re-executes a `SUCCEEDED` step.
- **FR-WF-6** Concurrency limits: per organization, per shop (1 mutating workflow at a time) and per Shopee partner app.
- **FR-WF-7** Every transition emits a workflow event, visible in the UI as the execution log.

### 3.7 Browser automation (Phase 6) — browser extension

Decision Q-1: operations without an Open API run through a **browser extension in the seller's own browser**. Full design in [browser-extension.md](browser-extension.md).

- **FR-BR-1** Capabilities with mode `BROWSER` are executed by the extension, only when enabled and only after the seller clicks "Run".
- **FR-BR-2** Before any write, the extension confirms the logged-in Seller Centre shop matches the task's shop; otherwise it stops.
- **FR-BR-3** The extension never solves CAPTCHA, OTP, MFA or verification. It pauses and asks the seller to finish it.
- **FR-BR-4** The extension has no database access and never sends Shopee cookies or passwords. It talks to `/api/v1/ext/*` with a revocable device token.
- **FR-BR-5** UI actions only; it never calls Seller Centre internal endpoints and does not collect shop data beyond verifying its own changes.
- **FR-BR-6** Screenshots are taken only on failure or final verification, stored encrypted and deleted after 14 days.
- **FR-BR-7** If no extension runs a task, the step stays `WAITING_USER` and the seller can complete it manually.

### 3.8 Dashboard (Phase 7)

Pages, widgets and UX standards are as listed in master spec §13. WCAG 2.2 AA is the target.

## 4. Non-functional requirements

| ID | Requirement | Target |
|---|---|---|
| NFR-1 | API latency (p95, excluding Shopee calls) | < 200 ms |
| NFR-2 | Availability (initial release) | 99.5 % monthly |
| NFR-3 | Recovery point objective | ≤ 15 min (WAL archiving) |
| NFR-4 | Recovery time objective | ≤ 4 h |
| NFR-5 | Concurrent connected shops (initial) | 5,000 shops, 500 orgs |
| NFR-6 | Import throughput | 10,000 rows parsed + validated in < 60 s |
| NFR-7 | Security | OWASP ASVS L2 as the review baseline |
| NFR-8 | Observability | Every request and job has a trace ID that appears in logs and workflow events |
| NFR-9 | Data residency | Single region at launch (assumption A-3) |

## 5. Discovery findings that change the design

These came out of reading the official Shopee Open Platform documentation on 2026-10-30 (details and sources in [shopee-integration.md](shopee-integration.md)).

1. **Shop banner, shop decoration, warehouse creation/edit and pickup address creation have no Open API.** They run through the browser extension (mode `BROWSER`, manual fallback).
2. **Shopee's Platform Partner Rules list "using web crawlers for collection of any Shopee-related data" as prohibited behavior**, and the Terms of Service forbid circumventing access controls. Browser automation of Seller Centre is therefore a compliance risk, not only a technical question. Q-1 resolved this with an extension that acts only through the UI, with the seller present (see [browser-extension.md §2](browser-extension.md#2-compliance-guardrails)).
3. **`add_item` has no idempotency key.** Duplicate prevention needs our own outbox record plus reconciliation through `product.search_item` by `item_sku`.
4. **Variations need at least 5 seconds after `add_item`** before `init_tier_variation` (stated in the API description). The workflow models this as a delayed step.
5. **Refresh tokens are single-use.** Losing the rotated token forces the seller to re-authorize. Token refresh must be serialized and persisted before anything else happens.
6. **Webhooks must return 2xx with an empty body**, or Shopee retries and may eventually disable push for the app. The webhook handler stores and acknowledges first, processes later.
7. **Official rate limits are not published per endpoint** (`rate_limit` is `[0,0,0]` in the doc metadata). We use conservative client-side limits and back off on throttling errors.

## 6. Assumptions

| ID | Assumption | Impact if wrong |
|---|---|---|
| A-1 | We will register one Shopee app of type **ERP System** (multi-seller SaaS). Its permission set includes Shop, Product, MediaSpace and Logistics. | Some capabilities unavailable; registry must reflect it. |
| A-2 | Launch markets: VN, then SG/MY/TH/PH/ID/TW. BR and CNSC (cross-border, merchant APIs) are out of scope for v1. | Merchant API, global products and BR host would be needed. |
| A-3 | Single deployment region (Singapore), so the API host is `partner.shopeemobile.com`. | Host is configurable per partner app anyway. |
| A-4 | Organizations sign up self-service. Billing is out of scope, but the schema reserves a `plan` field. | — |
| A-5 | Sessions use cookies (same-site dashboard). No third-party API clients at launch. | Personal access tokens would be added in Phase 8. |
| A-6 | Seller Centre passwords are never stored by the platform. | — |
| A-7 | Import limits: 50 MB file, 100,000 rows, 20 images per product row. | Adjust config. |
| A-8 | Images referenced in imports are either uploaded files (ZIP) or HTTPS URLs we download once, scan and store. | — |
| A-9 | Email delivery via an SMTP provider (configurable). | — |

## 7. Open questions (need a business decision)

| ID | Question | Why it matters | Proposed default |
|---|---|---|---|
| ~~Q-1~~ | **Answered 2026-10-30:** use a browser extension / tool in the seller's browser to perform operations without an API, no manual work. | — | See [browser-extension.md](browser-extension.md). |
| Q-2 | Which markets launch first? | Category trees, logistics channels and required attributes differ per market. | VN first. |
| Q-3 | Should Operators be able to approve their own workflows? | Separation of duties. | No by default; per-org setting. |
| Q-4 | Retention period for audit logs and browser artifacts? | Storage cost and privacy law (VN Decree 13/2023, PDPA). | Audit 2 years, artifacts 14 days, import files 90 days. |
| Q-5 | Is MFA required for Owners/Admins at launch? | Account takeover risk on accounts that hold seller tokens. | TOTP optional in Phase 1, enforced for Owners in Phase 8. |
