# Graph Report - shopify-automation  (2026-10-30)

## Corpus Check
- 224 files · ~97,365 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 9 file(s) not represented in the graph (top: (none) 6, .example 1, .conf 1)

## Summary
- 1891 nodes · 4741 edges · 121 communities (92 shown, 29 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 148 edges (avg confidence: 0.86)
- Token cost: 0 input · 0 output

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
- Principal
- cn
- environment
- Internal
- .Record
- members.vue
- testing.T
- linters
- shops_test.go
- DBTX
- shopsdb/models.go
- AlertDialogContent.vue
- settings.vue
- client
- @vueuse/core
- rbacdb/models.go
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
- Queries
- Service
- default.vue
- Tokens
- DB
- Database Design — Shopee Shop Automation
- steps
- select.vue
- capability.go
- Browser Extension — Shopee Shop Automation
- Workflow Engine Design — Shopee Shop Automation
- ci.yml
- OrganizationMember
- Badge.vue
- api-error.ts
- shops.ts
- Load
- with
- AlertDialogAction.vue
- README.md
- 3. Endpoint catalog
- 3. Functional requirements
- InsertAuditLogParams
- disconnect
- Development guide
- jobs
- env
- postgres
- config_test.go
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
- API Design — Shopee Shop Automation
- env
- Hash
- CreateInvitationParams
- csp.mjs
- session.ts
- forgot-password.vue
- accept.vue
- asynqLogger
- Migrate
- 4. Tenant isolation
- 4. Token lifecycle
- GetActiveSessionByTokenHashRow
- NewKeyring
- GetOAuthStateByHashParams
- GetCredentialsForUpdateParams
- [section].vue
- verify-email.vue
- loadSpec
- decodeProblem
- tsconfig.json
- FormField.vue
- CLAUDE.md
- 01-roles.sh
- ConsumeUserTokenParams
- TouchSessionParams
- InsertAuthorizationParams
- ListPendingWebhookEventsParams
- SetOAuthPendingParams
- UpsertCredentialsParams
- ConfirmDialog.vue
- auth.global.ts
- orgs/index.vue
- shopee-automation

## God Nodes (most connected - your core abstractions)
1. `cn` - 40 edges
2. `vue` - 40 edges
3. `newEnv()` - 37 edges
4. `Principal` - 35 edges
5. `Tenant` - 31 edges
6. `MustPrincipal()` - 29 edges
7. `Internal()` - 28 edges
8. `Server` - 26 edges
9. `Service` - 25 edges
10. `MustTenant()` - 25 edges

## Surprising Connections (you probably didn't know these)
- `10. Conventions` --references--> `Clock`  [INFERRED]
  docs/architecture.md → internal/platform/clock/clock.go
- `9. Phase 2 implementation notes` --references--> `Inline`  [INFERRED]
  docs/architecture.md → internal/platform/queue/queue.go
- `How the pieces fit` --references--> `TestOpenAPIMatchesRoutes()`  [INFERRED]
  docs/development.md → internal/app/contract_test.go
- `How the pieces fit` --references--> `TestTenantIsolationDatabase()`  [INFERRED]
  docs/development.md → internal/app/isolation_test.go
- `2. Permissions` --references--> `sync()`  [INFERRED]
  docs/api-design.md → web/app/pages/orgs/[orgId]/shops/[shopId].vue

## Import Cycles
- None detected.

## Communities (121 total, 29 thin omitted)

### Community 0 - "go_pkg_context"
Cohesion: 0.06
Nodes (19): orgResp, principalKey, tenantKey, main(), run(), setAdmin(), emailInput, loginInput (+11 more)

### Community 1 - "net/http.Request"
Cohesion: 0.05
Nodes (53): Access, Route, cursor, Data, List, statusRecorder, MembershipLister, App (+45 more)

### Community 2 - "github.com/google/uuid.UUID"
Cohesion: 0.03
Nodes (34): CreateUserTokenParams, InvalidateUserTokensParams, UpdatePasswordHashParams, Ptr(), parseStateToken(), SystemFindConnectionsRow, parseInvitationToken(), ConnectInput (+26 more)

### Community 3 - "time.Time"
Cohesion: 0.06
Nodes (53): AuditLog, AuditLogsDefault, CapabilityDefinition, CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember (+45 more)

### Community 4 - "apperr.go"
Cohesion: 0.06
Nodes (40): Problem, As(), BadRequest(), FailedDependency(), Error, FieldError, Kind, Upstream (+32 more)

### Community 5 - "context.Context"
Cohesion: 0.05
Nodes (17): CreateUserParams, UpdateDisplayNameParams, TenantFrom(), WithPrincipal(), WithTenant(), applyScope(), Scope, Mailer (+9 more)

### Community 6 - "Service"
Cohesion: 0.13
Nodes (23): Forbidden(), UniqueViolation(), CanAssign(), CanManageMember(), Permission, Role, Grants(), Has() (+15 more)

### Community 7 - "package.json"
Cohesion: 0.06
Nodes (38): ADR-0003, nuxt, openapi-fetch, openapi-typescript, @pinia/nuxt, @playwright/test, shadcn-nuxt, tailwindcss (+30 more)

### Community 8 - "fake.go"
Cohesion: 0.10
Nodes (23): AuthURL(), VerifyPush(), TestAuthURLAndPush(), contains(), Server, New(), num(), random() (+15 more)

### Community 9 - "2. Architecture decisions"
Cohesion: 0.05
Nodes (30): 10. Conventions, 1. Summary, 2. Architecture decisions, 3. Repository layout, 4. Module boundaries and dependency map, 5. Request lifecycle, 6. Security risk assessment, 7. Deployment (initial) (+22 more)

### Community 10 - "reka-ui"
Cohesion: 0.06
Nodes (28): @lucide/vue, reka-ui, emits, forwarded, props, delegatedProps, emits, forwarded (+20 more)

### Community 11 - "Principal"
Cohesion: 0.11
Nodes (22): Conflict(), Invalid(), NotFound(), Principal, Tenant, TestWriteErrorMapsKinds(), deref(), requireAdmin() (+14 more)

### Community 12 - "cn"
Cohesion: 0.09
Nodes (21): cn, vue, props, props, props, props, props, props (+13 more)

### Community 13 - "environment"
Cohesion: 0.06
Nodes (39): APP_RW_PASSWORD, APP_SYSTEM_PASSWORD, MINIO_ROOT_PASSWORD, MINIO_ROOT_USER, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, interval (+31 more)

### Community 14 - "Internal"
Cohesion: 0.15
Nodes (17): Argon2Params, RegisterInput, Service, Session, User, New(), User, Hasher (+9 more)

### Community 15 - ".Record"
Cohesion: 0.08
Nodes (17): Entry, DBTX, 1. Phases at a glance, 2. Phase 1 task breakdown, 3. Phase 2 task breakdown, 4. Definition of done (every phase), 5. Decisions needed from the product owner, Implementation Roadmap — Shopee Shop Automation (+9 more)

### Community 16 - "members.vue"
Cohesion: 0.08
Nodes (28): api, changeRole(), error, fmt, Invitation, invitations, invite, inviteError (+20 more)

### Community 17 - "testing.T"
Cohesion: 0.13
Nodes (27): How the pieces fit, TestCSRFAndOrigin(), TestLoginFailuresAndRateLimit(), TestLoginRotatesSession(), TestPasswordResetRevokesSessions(), TestRegisterDoesNotRevealExistingEmail(), TestRegisterValidation(), TestRegisterVerifyLoginLogout() (+19 more)

### Community 18 - "linters"
Cohesion: 0.08
Nodes (23): generated, rules, formatters, enable, settings, local-prefixes, excludes, linters (+15 more)

### Community 19 - "shops_test.go"
Cohesion: 0.20
Nodes (20): appResp, member, shopDetail, findMember(), TestTenantIsolationHTTP(), callback(), env, rawJSON() (+12 more)

### Community 20 - "DBTX"
Cohesion: 0.09
Nodes (5): DBTX, Queries, Queries, New(), DBTX

### Community 21 - "shopsdb/models.go"
Cohesion: 0.09
Nodes (19): toProfile(), OauthState, ShopCredential, ShopProfile, AuthorizationEvent, Detail, Profile, AuditLogsDefault (+11 more)

### Community 22 - "AlertDialogContent.vue"
Cohesion: 0.11
Nodes (12): emits, forwarded, props, delegatedProps, emits, forwarded, props, delegatedProps (+4 more)

### Community 23 - "settings.vue"
Cohesion: 0.11
Nodes (14): api, confirmSlug, error, etag, fields, form, Member, members (+6 more)

### Community 24 - "client"
Cohesion: 0.24
Nodes (4): client, resp, validateResponse(), env

### Community 25 - "@vueuse/core"
Cohesion: 0.11
Nodes (11): @vueuse/core, delegatedProps, props, emits, modelValue, props, delegatedProps, props (+3 more)

### Community 26 - "rbacdb/models.go"
Cohesion: 0.12
Nodes (16): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, Role, RolePermission (+8 more)

### Community 27 - "tenancydb/models.go"
Cohesion: 0.12
Nodes (16): Organization, OrganizationInvitation, CapabilityOverride, OauthState, OrganizationMember, Permission, Role, RolePermission (+8 more)

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
Cohesion: 0.22
Nodes (10): beforeSend(), App, Client, retryable(), scrub(), truncate(), warningString(), Limits (+2 more)

### Community 32 - "components.json"
Cohesion: 0.12
Nodes (15): aliases, components, composables, ui, utils, iconLibrary, $schema, style (+7 more)

### Community 33 - "time.Duration"
Cohesion: 0.15
Nodes (7): Deps, New(), Config, Limiter, TestRedisLimiter(), backoff(), Redis

### Community 34 - "rbacdb/db.go"
Cohesion: 0.15
Nodes (3): New(), DBTX, Queries

### Community 35 - "shops/index.vue"
Cohesion: 0.13
Nodes (11): api, connectError, connecting, error, filter, fmt, loading, { orgId, can } (+3 more)

### Community 36 - "[shopId].vue"
Cohesion: 0.13
Nodes (13): api, busy, Detail, error, eventLabels, fmt, loading, notice (+5 more)

### Community 37 - "run"
Cohesion: 0.19
Nodes (11): main(), run(), main(), run(), decodeKey(), ParseKeyring(), New(), TestLoggerRedactsSensitiveAttributes() (+3 more)

### Community 38 - "Shopee Integration — Shopee Shop Automation"
Cohesion: 0.14
Nodes (14): 10. Phase 2 implementation status, 11. Open items, 1. Sources and verification method, 2. Environments and hosts, 3. Request signing, 5. Token storage and encryption, 6.1 Error normalization, 6.2 Rate limiting and retries (+6 more)

### Community 39 - "Queries"
Cohesion: 0.19
Nodes (3): nonNilMap(), Queries, ShopConnection

### Community 40 - "Service"
Cohesion: 0.17
Nodes (7): Recorder, Clock, Enqueuer, Service, Asynq, Options, Tenants

### Community 41 - "default.vue"
Cohesion: 0.15
Nodes (8): api, current, nav, open, orgId, resend, route, session

### Community 42 - "Tokens"
Cohesion: 0.20
Nodes (5): LogView, Tokens, nonEmptyJSON(), Profile, ShopInfo

### Community 43 - "DB"
Cohesion: 0.30
Nodes (6): DB, Open(), New(), OwnerURL(), System(), withDB()

### Community 44 - "Database Design — Shopee Shop Automation"
Cohesion: 0.17
Nodes (11): 1. Conventions, 2.1 Identity, tenancy, audit, 2.2 Shopee integration, 2.3 Templates and products, 2.4 Workflow and system, 2. ERD, 3. Key constraints and indexes, 5. Transactions and concurrency (+3 more)

### Community 45 - "steps"
Cohesion: 0.24
Nodes (12): steps, steps, DATABASE_MIGRATE_URL, GITHUB_TOKEN, steps, env, if, name (+4 more)

### Community 46 - "select.vue"
Cohesion: 0.17
Nodes (10): api, busy, chosen, error, fmt, loading, { orgId }, route (+2 more)

### Community 47 - "capability.go"
Cohesion: 0.45
Nodes (10): Context, Definition, Mode, Override, Resolved, overridesFor(), Resolve(), resolveOne() (+2 more)

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
Cohesion: 0.22
Nodes (7): class-variance-authority, props, AlertVariants, delegatedProps, props, BadgeVariants, class-variance-authority@^0.7.1

### Community 53 - "api-error.ts"
Cohesion: 0.22
Nodes (6): ApiError, FetchResult, Problem, toApiError(), RFC-9457, unwrap()

### Community 54 - "shops.ts"
Cohesion: 0.18
Nodes (10): Capability, connectErrors, ConnectionStatus, daysUntil(), modeLabels, needsReconnect(), reasonText, Shop (+2 more)

### Community 55 - "Load"
Cohesion: 0.53
Nodes (3): Getenv, loader, Load()

### Community 56 - "with"
Cohesion: 0.20
Nodes (10): with, cache, cache-dependency-path, fetch-depth, go-version-file, name, node-version, path (+2 more)

### Community 57 - "AlertDialogAction.vue"
Cohesion: 0.29
Nodes (6): delegatedProps, props, delegatedProps, props, Props, ButtonVariants

### Community 59 - "3. Endpoint catalog"
Cohesion: 0.22
Nodes (9): 3.1 Authentication, 3.2 Organizations and members, 3.4 Templates, 3.5 Products and imports, 3.6 Workflows and approvals, 3.7 Audit and notifications, 3.8 Platform admin, 3.9 Browser extension (+1 more)

### Community 60 - "3. Functional requirements"
Cohesion: 0.22
Nodes (9): 3.1 Identity and tenancy (Phase 1), 3.2 Shopee connection (Phase 2), 3.3 Capability registry (Phase 2), 3.4 Templates (Phase 3), 3.5 Products (Phase 5), 3.6 Workflow engine (Phase 4), 3.7 Browser automation (Phase 6) — browser extension, 3.8 Dashboard (Phase 7) (+1 more)

### Community 61 - "InsertAuditLogParams"
Cohesion: 0.25
Nodes (3): InsertAuditLogParams, ListAuditLogsParams, Queries

### Community 62 - "disconnect"
Cohesion: 0.36
Nodes (8): 2. Permissions, 3.3 Shops and Shopee connection, connect(), act(), disconnect(), load(), reconnect(), sync()

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

### Community 67 - "config_test.go"
Cohesion: 0.50
Nodes (6): base(), env(), TestLoadDefaults(), TestLoadFromFile(), TestLoadProductionRules(), TestLoadRequiredAndInvalid()

### Community 68 - "Organization"
Cohesion: 0.25
Nodes (3): CreateOrganizationParams, ListOrganizationsForUserRow, UpdateOrganizationParams

### Community 69 - "ref_types_api"
Cohesion: 0.25
Nodes (5): fmt, activity, loadError, { orgId, membership, can }, stats

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

### Community 80 - "API Design — Shopee Shop Automation"
Cohesion: 0.33
Nodes (6): 1. Conventions, 4. Validation rules (examples), 5. Versioning, API Design — Shopee Shop Automation, Error format, Success envelope

### Community 81 - "env"
Cohesion: 0.33
Nodes (6): env, TEST_DATABASE_OWNER_URL, TEST_DATABASE_RW_URL, TEST_DATABASE_SYSTEM_URL, TEST_REDIS_URL, TEST_REQUIRE_DB

### Community 82 - "Hash"
Cohesion: 0.47
Nodes (6): randomBytes(), Bytes(), Hash(), New(), stateToken(), invitationToken()

### Community 84 - "csp.mjs"
Cohesion: 0.33
Nodes (3): hashes, html, policy

### Community 85 - "session.ts"
Cohesion: 0.33
Nodes (5): pinia, Membership, User, useSessionStore, pinia@^4.1.0

### Community 86 - "forgot-password.vue"
Cohesion: 0.33
Nodes (4): busy, done, email, error

### Community 87 - "accept.vue"
Cohesion: 0.33
Nodes (4): busy, error, route, session

### Community 89 - "Migrate"
Cohesion: 0.50
Nodes (4): main(), Migrate(), MigrateDownAll(), runGoose()

### Community 90 - "4. Tenant isolation"
Cohesion: 0.40
Nodes (5): 4.1 Roles, 4.2 Policy template, 4.3 Composite foreign keys, 4.4 Transaction helper, 4. Tenant isolation

### Community 91 - "4. Token lifecycle"
Cohesion: 0.40
Nodes (5): 4.1 Facts from the docs, 4.2 Connect flow, 4.3 Refresh, 4.4 Authorization expiry, revocation, reconnection, 4. Token lifecycle

### Community 93 - "NewKeyring"
Cohesion: 0.60
Nodes (5): NewKeyring(), key(), TestBindingAndTampering(), TestRotation(), TestRoundTrip()

### Community 96 - "[section].vue"
Cohesion: 0.50
Nodes (3): route, s, sections

### Community 97 - "verify-email.vue"
Cohesion: 0.50
Nodes (3): message, route, state

## Knowledge Gaps
- **5 isolated node(s):** `shopee-automation`, `RFC-9457`, `RFC-9457`, `ADR-0003`, `./.nuxt/tsconfig.json`
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 632 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **29 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `API Design — Shopee Shop Automation` connect `API Design — Shopee Shop Automation` to `README.md`, `3. Endpoint catalog`, `disconnect`?**
  _High betweenness centrality (0.274) - this node is a cross-community bridge._
- **What connects `shopee-automation`, `RFC-9457`, `RFC-9457` to the rest of the system?**
  _5 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `go_pkg_context` be split into smaller, more focused modules?**
  _Cohesion score 0.06425055928411633 - nodes in this community are weakly interconnected._
- **Why does `2. Permissions` connect `disconnect` to `API Design — Shopee Shop Automation`?**
  _High betweenness centrality (0.262) - this node is a cross-community bridge._
- **Should `net/http.Request` be split into smaller, more focused modules?**
  _Cohesion score 0.050128708847039696 - nodes in this community are weakly interconnected._
- **Why does `Architecture — Shopee Shop Automation` connect `2. Architecture decisions` to `README.md`?**
  _High betweenness centrality (0.216) - this node is a cross-community bridge._
- **Should `github.com/google/uuid.UUID` be split into smaller, more focused modules?**
  _Cohesion score 0.02981366459627329 - nodes in this community are weakly interconnected._