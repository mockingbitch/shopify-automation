# Graph Report - shopify-automation  (2026-10-30)

## Corpus Check
- 260 files · ~120,293 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 9 file(s) not represented in the graph (top: (none) 6, .example 1, .conf 1)

## Summary
- 2234 nodes · 5763 edges · 142 communities (104 shown, 38 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 164 edges (avg confidence: 0.86)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `4bb08c84`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- go_pkg_context
- net/http.Request
- github.com/google/uuid.UUID
- time.Time
- apperr.go
- context.Context
- Service
- package.json
- fake.go
- 2. Architecture decisions
- reka-ui
- wrap
- cn
- environment
- Internal
- Keyring
- members.vue
- newEnv
- linters
- shops_test.go
- tenancydb/db.go
- shopsdb/models.go
- AlertDialogContent.vue
- settings.vue
- client
- Input.vue
- Principal
- tenancydb/models.go
- ShopConnection
- go
- shopee-apps.vue
- Client
- components.json
- time.Duration
- rbacdb/db.go
- shops/index.vue
- [shopId].vue
- run
- Shopee Integration — Shopee Shop Automation
- .Get
- Service
- default.vue
- encoding/json.RawMessage
- DB
- Database Design — Shopee Shop Automation
- steps
- select.vue
- sections.go
- Browser Extension — Shopee Shop Automation
- Workflow Engine Design — Shopee Shop Automation
- ci.yml
- OrganizationMember
- Badge.vue
- api-error.ts
- [templateId]/index.vue
- Load
- with
- AlertDialogAction.vue
- README.md
- 3. Endpoint catalog
- 3. Functional requirements
- InsertAuditLogParams
- preview.vue
- Development guide
- jobs
- env
- postgres
- auditdb/models.go
- Organization
- ref_types_api
- new.vue
- reset-password.vue
- scripts
- Requirements — Shopee Shop Automation
- backend
- e2e
- login.vue
- audit.vue
- register.vue
- api.d.ts
- SchemaForm.vue
- env
- Hash
- CreateInvitationParams
- csp.mjs
- templates/index.vue
- forgot-password.vue
- accept.vue
- .Record
- .Refresh
- 4. Tenant isolation
- 4. Token lifecycle
- Errors
- testing.T
- GetOAuthStateByHashParams
- GetCredentialsForUpdateParams
- [section].vue
- verify-email.vue
- loadSpec
- DBTX
- tsconfig.json
- FormField.vue
- CLAUDE.md
- 01-roles.sh
- ConsumeUserTokenParams
- ShopTemplate
- InsertAuthorizationParams
- ListPendingWebhookEventsParams
- SetOAuthPendingParams
- UpsertCredentialsParams
- ConfirmDialog.vue
- auth.global.ts
- orgs/index.vue
- shopee-automation
- ShopTemplateVersion
- act
- DBTX
- New
- .applyPush
- schema.ts
- TestTemplateLifecycle
- ShopTemplateMapping
- Scope
- Selection
- Separator.vue
- ListAuthorizationsParams
- GetProfileParams
- CreateUserTokenParams
- ConsumeOAuthStateParams
- FinishOAuthSelectionParams
- FinishWebhookEventParams
- InsertOAuthStateParams
- SetAuthorizationExpiryParams
- SoftDeleteTemplateParams
- prune

## God Nodes (most connected - your core abstractions)
1. `Principal` - 53 edges
2. `Tenant` - 53 edges
3. `newEnv()` - 40 edges
4. `cn` - 40 edges
5. `vue` - 40 edges
6. `Decode()` - 32 edges
7. `MustPrincipal()` - 30 edges
8. `Service` - 30 edges
9. `Internal()` - 29 edges
10. `MustTenant()` - 26 edges

## Surprising Connections (you probably didn't know these)
- `11. Conventions` --references--> `Clock`  [INFERRED]
  docs/architecture.md → internal/platform/clock/clock.go
- `9. Phase 2 implementation notes` --references--> `Inline`  [INFERRED]
  docs/architecture.md → internal/platform/queue/queue.go
- `2. Permissions` --references--> `publish()`  [INFERRED]
  docs/api-design.md → web/app/pages/orgs/[orgId]/templates/[templateId]/index.vue
- `3.4 Templates` --references--> `publish()`  [INFERRED]
  docs/api-design.md → web/app/pages/orgs/[orgId]/templates/[templateId]/index.vue
- `How the pieces fit` --references--> `TestOpenAPIMatchesRoutes()`  [INFERRED]
  docs/development.md → internal/app/contract_test.go

## Import Cycles
- None detected.

## Communities (142 total, 38 thin omitted)

### Community 0 - "go_pkg_context"
Cohesion: 0.05
Nodes (31): orgResp, preview, previewChange, tplCreated, principalKey, tenantKey, main(), run() (+23 more)

### Community 1 - "net/http.Request"
Cohesion: 0.05
Nodes (49): Access, Route, cursor, statusRecorder, MembershipLister, App, decodeCursor(), encodeCursor() (+41 more)

### Community 2 - "github.com/google/uuid.UUID"
Cohesion: 0.03
Nodes (49): InvalidateUserTokensParams, TouchSessionParams, UpdatePasswordHashParams, Ptr(), parseStateToken(), CapabilityOverride, OauthState, Organization (+41 more)

### Community 3 - "time.Time"
Cohesion: 0.05
Nodes (64): AuditLog, AuditLogsDefault, CapabilityDefinition, UserSession, Real, AuditLog, AuditLogsDefault, CapabilityDefinition (+56 more)

### Community 4 - "apperr.go"
Cohesion: 0.17
Nodes (20): As(), BadRequest(), Conflict(), FailedDependency(), Error, Kind, Upstream, Gone() (+12 more)

### Community 5 - "context.Context"
Cohesion: 0.05
Nodes (18): CreateUserParams, UpdateDisplayNameParams, TenantFrom(), WithPrincipal(), WithTenant(), Decode(), nonNilMap(), ptr() (+10 more)

### Community 6 - "Service"
Cohesion: 0.11
Nodes (26): Forbidden(), CanAssign(), CanManageMember(), Permission, Role, Grants(), Has(), Permissions() (+18 more)

### Community 7 - "package.json"
Cohesion: 0.06
Nodes (42): ADR-0003, class-variance-authority, nuxt, openapi-fetch, openapi-typescript, pinia, @pinia/nuxt, @playwright/test (+34 more)

### Community 8 - "fake.go"
Cohesion: 0.09
Nodes (23): AuthURL(), VerifyPush(), TestAuthURLAndPush(), contains(), Server, New(), num(), random() (+15 more)

### Community 9 - "2. Architecture decisions"
Cohesion: 0.05
Nodes (32): 10. Phase 3 implementation notes, 11. Conventions, 1. Summary, 2. Architecture decisions, 3. Repository layout, 4. Module boundaries and dependency map, 5. Request lifecycle, 6. Security risk assessment (+24 more)

### Community 10 - "reka-ui"
Cohesion: 0.06
Nodes (31): @lucide/vue, reka-ui, @vueuse/core, delegatedProps, props, emits, forwarded, props (+23 more)

### Community 11 - "wrap"
Cohesion: 0.14
Nodes (11): UniqueViolation(), deref(), requireAdmin(), toApp(), wrap(), Access, App, ConnectResult (+3 more)

### Community 12 - "cn"
Cohesion: 0.08
Nodes (23): cn, vue, props, props, props, AlertVariants, props, props (+15 more)

### Community 13 - "environment"
Cohesion: 0.06
Nodes (39): APP_RW_PASSWORD, APP_SYSTEM_PASSWORD, MINIO_ROOT_PASSWORD, MINIO_ROOT_USER, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, interval (+31 more)

### Community 14 - "Internal"
Cohesion: 0.13
Nodes (20): Argon2Params, RegisterInput, Service, Session, User, New(), User, Hasher (+12 more)

### Community 15 - "Keyring"
Cohesion: 0.21
Nodes (8): 3. Phase 2 task breakdown, Row, WrappedKey, Keyring, newGCM(), open(), rowAAD(), seal()

### Community 16 - "members.vue"
Cohesion: 0.08
Nodes (28): api, changeRole(), error, fmt, Invitation, invitations, invite, inviteError (+20 more)

### Community 17 - "newEnv"
Cohesion: 0.22
Nodes (14): TestCSRFAndOrigin(), TestLoginFailuresAndRateLimit(), TestLoginRotatesSession(), TestPasswordResetRevokesSessions(), TestRegisterDoesNotRevealExistingEmail(), TestRegisterValidation(), TestRegisterVerifyLoginLogout(), TestResendVerification() (+6 more)

### Community 18 - "linters"
Cohesion: 0.08
Nodes (23): generated, rules, formatters, enable, settings, local-prefixes, excludes, linters (+15 more)

### Community 19 - "shops_test.go"
Cohesion: 0.19
Nodes (21): appResp, member, shopDetail, findMember(), TestTenantIsolationHTTP(), TestMembershipLifecycle(), callback(), env (+13 more)

### Community 20 - "tenancydb/db.go"
Cohesion: 0.22
Nodes (3): Queries, New(), DBTX

### Community 21 - "shopsdb/models.go"
Cohesion: 0.09
Nodes (18): OauthState, ShopCredential, ShopeeApp, CapabilityOverride, Organization, OrganizationInvitation, OrganizationMember, Permission (+10 more)

### Community 22 - "AlertDialogContent.vue"
Cohesion: 0.11
Nodes (12): emits, forwarded, props, delegatedProps, emits, forwarded, props, props (+4 more)

### Community 23 - "settings.vue"
Cohesion: 0.11
Nodes (14): api, confirmSlug, error, etag, fields, form, Member, members (+6 more)

### Community 24 - "client"
Cohesion: 0.26
Nodes (4): resp, validateResponse(), client, env

### Community 25 - "Input.vue"
Cohesion: 0.19
Nodes (5): emits, modelValue, props, delegatedProps, props

### Community 26 - "Principal"
Cohesion: 0.16
Nodes (20): Principal, Tenant, sectionETag(), toSection(), toTemplate(), toVersion(), wrap(), Queries (+12 more)

### Community 27 - "tenancydb/models.go"
Cohesion: 0.10
Nodes (20): Organization, OrganizationInvitation, CapabilityOverride, OauthState, OrganizationMember, Permission, Role, RolePermission (+12 more)

### Community 28 - "ShopConnection"
Cohesion: 0.12
Nodes (8): GetConnectionByShopParams, GetConnectionParams, InsertConnectionParams, ListConnectionsParams, LockConnectionParams, ReactivateConnectionParams, SetConnectionStatusParams, UpdateConnectionSyncedParams

### Community 29 - "go"
Cohesion: 0.12
Nodes (16): go, emit_empty_slices, emit_pointers_for_null_types, out, overrides, package, sql_package, db_type (+8 more)

### Community 30 - "shopee-apps.vue"
Cohesion: 0.15
Nodes (16): api, App, apps, busy, create(), error, fields, form (+8 more)

### Community 31 - "Client"
Cohesion: 0.12
Nodes (13): Tokens, beforeSend(), App, Client, retryable(), scrub(), truncate(), warningString() (+5 more)

### Community 32 - "components.json"
Cohesion: 0.12
Nodes (15): aliases, components, composables, ui, utils, iconLibrary, $schema, style (+7 more)

### Community 33 - "time.Duration"
Cohesion: 0.14
Nodes (9): Deps, New(), Config, Limiter, TestRedisLimiter(), backoff(), NewClient(), Redis (+1 more)

### Community 34 - "rbacdb/db.go"
Cohesion: 0.15
Nodes (3): New(), DBTX, Queries

### Community 35 - "shops/index.vue"
Cohesion: 0.10
Nodes (21): api, connectError, connecting, error, filter, fmt, loading, { orgId, can } (+13 more)

### Community 36 - "[shopId].vue"
Cohesion: 0.11
Nodes (21): 2. Permissions, 3.3 Shops and Shopee connection, connect(), act(), api, busy, Detail, disconnect() (+13 more)

### Community 37 - "run"
Cohesion: 0.13
Nodes (10): main(), run(), main(), run(), decodeKey(), ParseKeyring(), New(), TestLoggerRedactsSensitiveAttributes() (+2 more)

### Community 38 - "Shopee Integration — Shopee Shop Automation"
Cohesion: 0.14
Nodes (14): 10. Phase 2 implementation status, 11. Open items, 1. Sources and verification method, 2. Environments and hosts, 3. Request signing, 5. Token storage and encryption, 6.1 Error normalization, 6.2 Rate limiting and retries (+6 more)

### Community 39 - ".Get"
Cohesion: 0.20
Nodes (9): Detail, toProfile(), toShop(), utcPtr(), ShopConnection, ShopProfile, AuthorizationEvent, Profile (+1 more)

### Community 40 - "Service"
Cohesion: 0.17
Nodes (7): Recorder, Clock, Enqueuer, Service, Asynq, Options, Tenants

### Community 41 - "default.vue"
Cohesion: 0.15
Nodes (8): api, current, nav, open, orgId, resend, route, session

### Community 42 - "encoding/json.RawMessage"
Cohesion: 0.20
Nodes (8): versionDetail, LogView, Problem, FieldError, flatten(), CreateInput, OverridesInput, SectionSchemaView

### Community 43 - "DB"
Cohesion: 0.27
Nodes (7): TestAuditTrail(), DB, Open(), New(), OwnerURL(), System(), withDB()

### Community 44 - "Database Design — Shopee Shop Automation"
Cohesion: 0.17
Nodes (11): 1. Conventions, 2.1 Identity, tenancy, audit, 2.2 Shopee integration, 2.3 Templates and products, 2.4 Workflow and system, 2. ERD, 3. Key constraints and indexes, 5. Transactions and concurrency (+3 more)

### Community 45 - "steps"
Cohesion: 0.24
Nodes (12): steps, steps, DATABASE_MIGRATE_URL, GITHUB_TOKEN, steps, env, if, name (+4 more)

### Community 46 - "select.vue"
Cohesion: 0.17
Nodes (10): api, busy, chosen, error, fmt, loading, { orgId }, route (+2 more)

### Community 47 - "sections.go"
Cohesion: 0.08
Nodes (37): Context, Definition, Override, Mode, Resolved, overridesFor(), Resolve(), resolveOne() (+29 more)

### Community 48 - "Browser Extension — Shopee Shop Automation"
Cohesion: 0.18
Nodes (11): 10. Open items, 1. Decision, 2. Compliance guardrails, 3. Components, 4. Pairing and authentication, 5. Recipes, 6. Task flow, 7. Data and security (+3 more)

### Community 49 - "Workflow Engine Design — Shopee Shop Automation"
Cohesion: 0.18
Nodes (11): 1. Principles, 2. Concepts, 3.1 Run, 3.2 Step, 3. State machines, 4. Execution, 5. Idempotency against Shopee, 6. SHOP_SETUP v1 (+3 more)

### Community 50 - "ci.yml"
Cohesion: 0.18
Nodes (10): concurrency, cancel-in-progress, group, name, on, pull_request, push, permissions (+2 more)

### Community 51 - "OrganizationMember"
Cohesion: 0.18
Nodes (5): CreateMemberParams, GetMemberByUserForUpdateParams, GetMemberForUpdateParams, GetMembershipParams, UpdateMemberRoleParams

### Community 52 - "Badge.vue"
Cohesion: 0.50
Nodes (3): delegatedProps, props, BadgeVariants

### Community 53 - "api-error.ts"
Cohesion: 0.22
Nodes (6): ApiError, FetchResult, Problem, toApiError(), RFC-9457, unwrap()

### Community 54 - "[templateId]/index.vue"
Cohesion: 0.07
Nodes (23): activeKey, activeSchema, api, busy, Detail, editable, edits, error (+15 more)

### Community 55 - "Load"
Cohesion: 0.28
Nodes (9): Getenv, loader, Load(), base(), env(), TestLoadDefaults(), TestLoadFromFile(), TestLoadProductionRules() (+1 more)

### Community 56 - "with"
Cohesion: 0.20
Nodes (10): with, cache, cache-dependency-path, fetch-depth, go-version-file, name, node-version, path (+2 more)

### Community 57 - "AlertDialogAction.vue"
Cohesion: 0.29
Nodes (6): delegatedProps, props, delegatedProps, props, Props, ButtonVariants

### Community 59 - "3. Endpoint catalog"
Cohesion: 0.14
Nodes (14): 1. Conventions, 3.1 Authentication, 3.2 Organizations and members, 3.5 Products and imports, 3.6 Workflows and approvals, 3.7 Audit and notifications, 3.8 Platform admin, 3.9 Browser extension (+6 more)

### Community 60 - "3. Functional requirements"
Cohesion: 0.22
Nodes (9): 3.1 Identity and tenancy (Phase 1), 3.2 Shopee connection (Phase 2), 3.3 Capability registry (Phase 2), 3.4 Templates (Phase 3), 3.5 Products (Phase 5), 3.6 Workflow engine (Phase 4), 3.7 Browser automation (Phase 6) — browser extension, 3.8 Dashboard (Phase 7) (+1 more)

### Community 61 - "InsertAuditLogParams"
Cohesion: 0.25
Nodes (3): InsertAuditLogParams, ListAuditLogsParams, Queries

### Community 62 - "preview.vue"
Cohesion: 0.08
Nodes (21): api, busy, Change, chosen, Detail, error, { orgId, can }, overrideErrors (+13 more)

### Community 63 - "Development guide"
Cohesion: 0.25
Nodes (7): Connecting a Shopee sandbox shop, Database roles, Development guide, Encryption keys, Everyday commands, First run, Prerequisites

### Community 64 - "jobs"
Cohesion: 0.25
Nodes (8): run, jobs, secrets, web, working-directory, runs-on, defaults, runs-on

### Community 65 - "env"
Cohesion: 0.25
Nodes (8): env, APP_ENV, COOKIE_SECURE, DATABASE_URL, ENCRYPTION_KEYS, PUBLIC_BASE_URL, REDIS_URL, SMTP_ADDR

### Community 66 - "postgres"
Cohesion: 0.25
Nodes (8): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env, image, options, ports, postgres

### Community 67 - "auditdb/models.go"
Cohesion: 0.10
Nodes (20): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, Role, RolePermission (+12 more)

### Community 68 - "Organization"
Cohesion: 0.25
Nodes (3): CreateOrganizationParams, ListOrganizationsForUserRow, UpdateOrganizationParams

### Community 69 - "ref_types_api"
Cohesion: 0.13
Nodes (9): fmt, unsafe, activity, loadError, { orgId, membership, can }, stats, Membership, User (+1 more)

### Community 70 - "new.vue"
Cohesion: 0.25
Nodes (5): busy, error, fields, form, slugEdited

### Community 71 - "reset-password.vue"
Cohesion: 0.25
Nodes (6): busy, done, error, fields, password, route

### Community 72 - "scripts"
Cohesion: 0.25
Nodes (8): scripts, api:types, build, dev, postinstall, preview, test:e2e, typecheck

### Community 73 - "Requirements — Shopee Shop Automation"
Cohesion: 0.29
Nodes (7): 1. Repository assessment, 2. Actors, 4. Non-functional requirements, 5. Discovery findings that change the design, 6. Assumptions, 7. Open questions (need a business decision), Requirements — Shopee Shop Automation

### Community 74 - "backend"
Cohesion: 0.29
Nodes (7): runs-on, services, backend, image, options, ports, redis

### Community 75 - "e2e"
Cohesion: 0.29
Nodes (7): needs, runs-on, services, e2e, image, ports, mailpit

### Community 76 - "login.vue"
Cohesion: 0.29
Nodes (5): busy, error, form, route, session

### Community 77 - "audit.vue"
Cohesion: 0.29
Nodes (5): cursor, error, loading, { orgId, can }, rows

### Community 78 - "register.vue"
Cohesion: 0.29
Nodes (5): busy, done, error, fields, form

### Community 79 - "api.d.ts"
Cohesion: 0.29
Nodes (6): components, $defs, operations, paths, RFC-9457, webhooks

### Community 80 - "SchemaForm.vue"
Cohesion: 0.16
Nodes (8): addRow(), emit, fields, props, removeRow(), rows(), set(), setRow()

### Community 81 - "env"
Cohesion: 0.33
Nodes (6): env, TEST_DATABASE_OWNER_URL, TEST_DATABASE_RW_URL, TEST_DATABASE_SYSTEM_URL, TEST_REDIS_URL, TEST_REQUIRE_DB

### Community 82 - "Hash"
Cohesion: 0.47
Nodes (6): randomBytes(), Bytes(), Hash(), New(), stateToken(), invitationToken()

### Community 84 - "csp.mjs"
Cohesion: 0.33
Nodes (3): hashes, html, policy

### Community 85 - "templates/index.vue"
Cohesion: 0.14
Nodes (14): api, busy, clone(), error, fields, fmt, form, load() (+6 more)

### Community 86 - "forgot-password.vue"
Cohesion: 0.33
Nodes (4): busy, done, email, error

### Community 87 - "accept.vue"
Cohesion: 0.33
Nodes (4): busy, error, route, session

### Community 88 - ".Record"
Cohesion: 0.14
Nodes (11): Entry, 1. Phases at a glance, 2. Phase 1 task breakdown, 4. Phase 3 task breakdown, 5. Definition of done (every phase), 6. Decisions needed from the product owner, Implementation Roadmap — Shopee Shop Automation, nonNil() (+3 more)

### Community 89 - ".Refresh"
Cohesion: 0.26
Nodes (8): TestErrorsAreNormalizedAndCarryNoSecrets(), AsError(), classify(), IsKind(), shopeeMeta(), upstreamError(), Error, Kind

### Community 90 - "4. Tenant isolation"
Cohesion: 0.40
Nodes (5): 4.1 Roles, 4.2 Policy template, 4.3 Composite foreign keys, 4.4 Transaction helper, 4. Tenant isolation

### Community 91 - "4. Token lifecycle"
Cohesion: 0.40
Nodes (5): 4.1 Facts from the docs, 4.2 Connect flow, 4.3 Refresh, 4.4 Authorization expiry, revocation, reconnection, 4. Token lifecycle

### Community 92 - "Errors"
Cohesion: 0.26
Nodes (6): Errors, itoa(), TestEmail(), TestLength(), TestPassword(), TestSlug()

### Community 93 - "testing.T"
Cohesion: 0.11
Nodes (20): Fake, NewFake(), NewKeyring(), key(), TestBindingAndTampering(), TestRotation(), TestRoundTrip(), decodeProblem() (+12 more)

### Community 95 - "GetCredentialsForUpdateParams"
Cohesion: 0.15
Nodes (5): GetCredentialsForUpdateParams, GetCredentialsParams, ListSectionsParams, LockSectionParams, UpdateSectionParams

### Community 96 - "[section].vue"
Cohesion: 0.50
Nodes (3): route, s, sections

### Community 97 - "verify-email.vue"
Cohesion: 0.50
Nodes (3): message, route, state

### Community 98 - "loadSpec"
Cohesion: 0.33
Nodes (4): How the pieces fit, loadSpec(), TestOpenAPIMatchesRoutes(), TestTenantIsolationDatabase()

### Community 105 - "ShopTemplate"
Cohesion: 0.18
Nodes (5): GetTemplateParams, InsertTemplateParams, ListTemplatesRow, LockTemplateParams, UpdateTemplateParams

### Community 121 - "ShopTemplateVersion"
Cohesion: 0.18
Nodes (5): GetVersionParams, InsertVersionParams, ListVersionsParams, LockVersionParams, PublishVersionParams

### Community 122 - "act"
Cohesion: 0.31
Nodes (9): 3.4 Templates, act(), loadDetail(), loadVersion(), newDraft(), publish(), save(), selectVersion() (+1 more)

### Community 125 - ".applyPush"
Cohesion: 0.29
Nodes (4): WebhookEvent, SystemFindConnectionsRow, pushBody, SystemFindConnectionsParams

### Community 126 - "schema.ts"
Cohesion: 0.32
Nodes (6): SectionSchema, useTemplateSchemas(), FieldErrors, JsonObject, JsonSchema, labelOf()

### Community 127 - "TestTemplateLifecycle"
Cohesion: 0.48
Nodes (4): client, TestTemplateLifecycle(), TestTemplateOverridesAndPreview(), TestTemplatePermissions()

### Community 128 - "ShopTemplateMapping"
Cohesion: 0.29
Nodes (3): GetMappingParams, ListMappingsParams, UpsertMappingParams

### Community 130 - "Selection"
Cohesion: 0.33
Nodes (3): PendingShop, Selection, ExpireStaleInvitationParams

## Knowledge Gaps
- **5 isolated node(s):** `shopee-automation`, `RFC-9457`, `RFC-9457`, `ADR-0003`, `./.nuxt/tsconfig.json`
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 722 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **38 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `API Design — Shopee Shop Automation` connect `3. Endpoint catalog` to `README.md`, `[shopId].vue`?**
  _High betweenness centrality (0.277) - this node is a cross-community bridge._
- **What connects `shopee-automation`, `RFC-9457`, `RFC-9457` to the rest of the system?**
  _5 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `go_pkg_context` be split into smaller, more focused modules?**
  _Cohesion score 0.052150154430981234 - nodes in this community are weakly interconnected._
- **Why does `2. Permissions` connect `[shopId].vue` to `act`, `3. Endpoint catalog`?**
  _High betweenness centrality (0.272) - this node is a cross-community bridge._
- **Should `net/http.Request` be split into smaller, more focused modules?**
  _Cohesion score 0.052698412698412696 - nodes in this community are weakly interconnected._
- **Why does `Architecture — Shopee Shop Automation` connect `2. Architecture decisions` to `README.md`?**
  _High betweenness centrality (0.225) - this node is a cross-community bridge._
- **Should `github.com/google/uuid.UUID` be split into smaller, more focused modules?**
  _Cohesion score 0.0311587147030185 - nodes in this community are weakly interconnected._