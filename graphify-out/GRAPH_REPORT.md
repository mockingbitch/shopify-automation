# Graph Report - shopify-automation  (2026-10-30)

## Corpus Check
- 290 files · ~154,112 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 9 file(s) not represented in the graph (top: (none) 6, .example 1, .conf 1)

## Summary
- 2729 nodes · 7615 edges · 152 communities (120 shown, 32 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 253 edges (avg confidence: 0.86)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `9fbdb9b0`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- go_pkg_context
- net/http.Request
- github.com/google/uuid.UUID
- identitydb/models.go
- Principal
- context.Context
- Service
- package.json
- fake.go
- Inline
- reka-ui
- Error
- cn
- environment
- Internal
- Keyring
- members.vue
- Result
- linters
- shops_test.go
- [runId].vue
- shopsdb/models.go
- AlertDialogContent.vue
- settings.vue
- client
- Input.vue
- Principal
- time.Time
- ShopConnection
- go
- shopee-apps.vue
- Client
- components.json
- time.Duration
- NotFound
- shops/index.vue
- [shopId].vue
- run
- Shopee Integration — Shopee Shop Automation
- Queries
- workflowdb/models.go
- default.vue
- views.go
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
- net/netip.Addr
- preview.vue
- Development guide
- jobs
- env
- postgres
- rbacdb/models.go
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
- templatesdb/models.go
- CreateInvitationParams
- csp.mjs
- templates/index.vue
- forgot-password.vue
- accept.vue
- .Record
- setup
- Engine
- 4. Token lifecycle
- Errors
- testing.T
- GetOAuthStateByHashParams
- GetCredentialsForUpdateParams
- [section].vue
- verify-email.vue
- workflows/index.vue
- net/http.ResponseWriter
- tsconfig.json
- FormField.vue
- CLAUDE.md
- 01-roles.sh
- .Decide
- ShopTemplate
- Architecture — Shopee Shop Automation
- Handler
- SetOAuthPendingParams
- Handler
- ConfirmDialog.vue
- auth.global.ts
- orgs/index.vue
- shopee-automation
- ShopTemplateVersion
- act
- workflows_test.go
- WorkflowStep
- .applyPush
- schema.ts
- TestTemplateLifecycle
- ShopTemplateMapping
- Service
- WorkflowRun
- Sign
- ListAuthorizationsParams
- GetProfileParams
- logs.vue
- ConsumeOAuthStateParams
- WorkflowApproval
- FinishWebhookEventParams
- loader
- .CreateRuns
- SelectContent.vue
- prune
- asynqLogger
- .List
- Migrate
- GetActiveSessionByTokenHashRow
- Empty
- GetCredentialsForUpdateParams
- ListOrgEventsParams
- .RunSnapshot
- ListAttemptsParams
- RequestCancelParams

## God Nodes (most connected - your core abstractions)
1. `Tenant` - 73 edges
2. `Principal` - 70 edges
3. `newEnv()` - 43 edges
4. `cn` - 40 edges
5. `vue` - 40 edges
6. `Decode()` - 36 edges
7. `Service` - 35 edges
8. `Server` - 34 edges
9. `Internal()` - 32 edges
10. `MustPrincipal()` - 32 edges

## Surprising Connections (you probably didn't know these)
- `12. Conventions` --references--> `Clock`  [INFERRED]
  docs/architecture.md → internal/platform/clock/clock.go
- `5. Phase 4 task breakdown` --references--> `Transition()`  [INFERRED]
  docs/roadmap.md → internal/workflow/state.go
- `11. Phase 4 implementation notes` --references--> `EnsurePartitions()`  [INFERRED]
  docs/architecture.md → internal/platform/maintenance/partitions.go
- `9. Phase 2 implementation notes` --references--> `Inline`  [INFERRED]
  docs/architecture.md → internal/platform/queue/queue.go
- `2. Permissions` --references--> `sync()`  [INFERRED]
  docs/api-design.md → web/app/pages/orgs/[orgId]/shops/[shopId].vue

## Import Cycles
- None detected.

## Communities (152 total, 32 thin omitted)

### Community 0 - "go_pkg_context"
Cohesion: 0.06
Nodes (28): appResp, orgResp, shopDetail, tplCreated, principalKey, tenantKey, main(), run() (+20 more)

### Community 1 - "net/http.Request"
Cohesion: 0.15
Nodes (11): MustPrincipal(), MustTenant(), Decode(), NoContent(), safeJSONMsg(), Handler, optional(), pathUUID() (+3 more)

### Community 2 - "github.com/google/uuid.UUID"
Cohesion: 0.02
Nodes (58): ConsumeUserTokenParams, CreateUserTokenParams, InvalidateUserTokensParams, TouchSessionParams, UpdatePasswordHashParams, Ptr(), parseStateToken(), parseInvitationToken() (+50 more)

### Community 3 - "identitydb/models.go"
Cohesion: 0.07
Nodes (26): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, Role, RolePermission (+18 more)

### Community 4 - "Principal"
Cohesion: 0.10
Nodes (18): Conflict(), Invalid(), Principal, WithPrincipal(), TestWriteErrorMapsKinds(), waitReason(), wrap(), contains() (+10 more)

### Community 5 - "context.Context"
Cohesion: 0.03
Nodes (22): CreateUserParams, DBTX, UpdateDisplayNameParams, Queries, TenantFrom(), WithTenant(), Mailer, New() (+14 more)

### Community 6 - "Service"
Cohesion: 0.12
Nodes (24): approvalPolicy(), Forbidden(), CanAssign(), CanManageMember(), Permission, Role, Grants(), Has() (+16 more)

### Community 7 - "package.json"
Cohesion: 0.06
Nodes (40): ADR-0003, nuxt, openapi-fetch, openapi-typescript, pinia, @pinia/nuxt, @playwright/test, shadcn-nuxt (+32 more)

### Community 8 - "fake.go"
Cohesion: 0.14
Nodes (12): contains(), Server, num(), random(), remove(), remove2(), writeErr(), writeJSON() (+4 more)

### Community 9 - "Inline"
Cohesion: 0.10
Nodes (11): 9. Phase 2 implementation notes, Memory, Message, Inline, Memory, Log, SMTP, delayedTask (+3 more)

### Community 10 - "reka-ui"
Cohesion: 0.08
Nodes (24): @lucide/vue, reka-ui, emits, forwarded, props, props, delegatedProps, forwardedProps (+16 more)

### Community 11 - "Error"
Cohesion: 0.07
Nodes (26): As(), BadRequest(), FailedDependency(), Error, Kind, Gone(), IsKind(), newErr() (+18 more)

### Community 12 - "cn"
Cohesion: 0.09
Nodes (21): cn, vue, props, props, props, props, props, props (+13 more)

### Community 13 - "environment"
Cohesion: 0.06
Nodes (39): APP_RW_PASSWORD, APP_SYSTEM_PASSWORD, MINIO_ROOT_PASSWORD, MINIO_ROOT_USER, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, interval (+31 more)

### Community 14 - "Internal"
Cohesion: 0.12
Nodes (23): Argon2Params, RegisterInput, Service, Session, User, New(), User, Hasher (+15 more)

### Community 15 - "Keyring"
Cohesion: 0.21
Nodes (8): 3. Phase 2 task breakdown, Row, WrappedKey, Keyring, newGCM(), open(), rowAAD(), seal()

### Community 16 - "members.vue"
Cohesion: 0.08
Nodes (28): api, changeRole(), error, fmt, Invitation, invitations, invite, inviteError (+20 more)

### Community 17 - "Result"
Cohesion: 0.11
Nodes (40): Address, Ambiguous(), AwaitApproval(), Result, StepContext, Manual(), NeedsUser(), Permanent() (+32 more)

### Community 18 - "linters"
Cohesion: 0.08
Nodes (23): generated, rules, formatters, enable, settings, local-prefixes, excludes, linters (+15 more)

### Community 19 - "shops_test.go"
Cohesion: 0.23
Nodes (16): member, findMember(), TestTenantIsolationHTTP(), TestMembershipLifecycle(), callback(), env, startConnect(), TestCallbackRejectsBadState() (+8 more)

### Community 20 - "[runId].vue"
Cohesion: 0.06
Nodes (36): act(), api, approval, blocked, busy, canDecide, Change, changes (+28 more)

### Community 21 - "shopsdb/models.go"
Cohesion: 0.07
Nodes (26): toProfile(), OauthState, ShopCredential, ShopProfile, Profile, CapabilityOverride, Organization, OrganizationInvitation (+18 more)

### Community 22 - "AlertDialogContent.vue"
Cohesion: 0.11
Nodes (12): emits, forwarded, props, delegatedProps, emits, forwarded, props, props (+4 more)

### Community 23 - "settings.vue"
Cohesion: 0.11
Nodes (14): api, confirmSlug, error, etag, fields, form, Member, members (+6 more)

### Community 24 - "client"
Cohesion: 0.15
Nodes (5): resp, loadSpec(), validateResponse(), client, env

### Community 25 - "Input.vue"
Cohesion: 0.12
Nodes (10): @vueuse/core, delegatedProps, props, emits, modelValue, props, delegatedProps, props (+2 more)

### Community 26 - "Principal"
Cohesion: 0.13
Nodes (24): Tenant, Canonical(), Merge(), TestMerge(), derefStr(), Service, sectionETag(), toSection() (+16 more)

### Community 27 - "time.Time"
Cohesion: 0.05
Nodes (70): AuditLog, AuditLogsDefault, CapabilityDefinition, CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember (+62 more)

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
Nodes (16): beforeSend(), App, Client, retryable(), scrub(), truncate(), warningString(), Channel (+8 more)

### Community 32 - "components.json"
Cohesion: 0.12
Nodes (15): aliases, components, composables, ui, utils, iconLibrary, $schema, style (+7 more)

### Community 33 - "time.Duration"
Cohesion: 0.12
Nodes (7): Enqueuer, TestRedisLimiter(), backoff(), backoff(), Asynq, Options, Redis

### Community 34 - "NotFound"
Cohesion: 0.09
Nodes (27): Access, Deps, Route, App, New(), NotFound(), PrincipalFrom(), Adapter (+19 more)

### Community 35 - "shops/index.vue"
Cohesion: 0.10
Nodes (21): api, connectError, connecting, error, filter, fmt, loading, { orgId, can } (+13 more)

### Community 36 - "[shopId].vue"
Cohesion: 0.11
Nodes (20): 3.3 Shops and Shopee connection, connect(), act(), api, busy, Detail, disconnect(), error (+12 more)

### Community 37 - "run"
Cohesion: 0.17
Nodes (12): main(), run(), main(), run(), decodeKey(), ParseKeyring(), New(), TestLoggerRedactsSensitiveAttributes() (+4 more)

### Community 38 - "Shopee Integration — Shopee Shop Automation"
Cohesion: 0.14
Nodes (14): 10. Phase 2 implementation status, 11. Open items, 1. Sources and verification method, 2. Environments and hosts, 3. Request signing, 5. Token storage and encryption, 6.1 Error normalization, 6.2 Rate limiting and retries (+6 more)

### Community 39 - "Queries"
Cohesion: 0.12
Nodes (9): UniqueViolation(), nonNilMap(), Detail, toShop(), utcPtr(), Queries, ShopConnection, ListFilter (+1 more)

### Community 40 - "workflowdb/models.go"
Cohesion: 0.07
Nodes (29): From(), With(), WorkflowApproval, WorkflowEvent, ctxKey, Meta, CapabilityOverride, OauthState (+21 more)

### Community 41 - "default.vue"
Cohesion: 0.15
Nodes (8): api, current, nav, open, orgId, resend, route, session

### Community 42 - "views.go"
Cohesion: 0.14
Nodes (24): versionDetail, LogView, rawJSON(), RawJSON(), rawOrEmpty(), toApproval(), toEvent(), toRun() (+16 more)

### Community 43 - "DB"
Cohesion: 0.36
Nodes (7): DB, Open(), TestEnsurePartitions(), New(), OwnerURL(), System(), withDB()

### Community 44 - "Database Design — Shopee Shop Automation"
Cohesion: 0.12
Nodes (16): 1. Conventions, 2.1 Identity, tenancy, audit, 2.2 Shopee integration, 2.3 Templates and products, 2.4 Workflow and system, 2. ERD, 3. Key constraints and indexes, 4.1 Roles (+8 more)

### Community 45 - "steps"
Cohesion: 0.24
Nodes (12): steps, steps, DATABASE_MIGRATE_URL, GITHUB_TOKEN, steps, env, if, name (+4 more)

### Community 46 - "select.vue"
Cohesion: 0.17
Nodes (10): api, busy, chosen, error, fmt, loading, { orgId }, route (+2 more)

### Community 47 - "sections.go"
Cohesion: 0.08
Nodes (33): Context, Definition, Override, Problem, Mode, Resolved, overridesFor(), Resolve() (+25 more)

### Community 48 - "Browser Extension — Shopee Shop Automation"
Cohesion: 0.18
Nodes (11): 10. Open items, 1. Decision, 2. Compliance guardrails, 3. Components, 4. Pairing and authentication, 5. Recipes, 6. Task flow, 7. Data and security (+3 more)

### Community 49 - "Workflow Engine Design — Shopee Shop Automation"
Cohesion: 0.17
Nodes (12): 1. Principles, 2. Concepts, 3.1 Run, 3.2 Step, 3. State machines, 4. Execution, 5. Idempotency against Shopee, 6. SHOP_SETUP v1 (+4 more)

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

### Community 54 - "[templateId]/index.vue"
Cohesion: 0.07
Nodes (23): activeKey, activeSchema, api, busy, Detail, editable, edits, error (+15 more)

### Community 55 - "Load"
Cohesion: 0.24
Nodes (9): Getenv, Config, Load(), base(), env(), TestLoadDefaults(), TestLoadFromFile(), TestLoadProductionRules() (+1 more)

### Community 56 - "with"
Cohesion: 0.20
Nodes (10): with, cache, cache-dependency-path, fetch-depth, go-version-file, name, node-version, path (+2 more)

### Community 57 - "AlertDialogAction.vue"
Cohesion: 0.29
Nodes (6): delegatedProps, props, delegatedProps, props, Props, ButtonVariants

### Community 59 - "3. Endpoint catalog"
Cohesion: 0.13
Nodes (17): 1. Conventions, 2. Permissions, 3.1 Authentication, 3.2 Organizations and members, 3.5 Products and imports, 3.6 Workflows and approvals, 3.7 Audit and notifications, 3.8 Platform admin (+9 more)

### Community 60 - "3. Functional requirements"
Cohesion: 0.22
Nodes (9): 3.1 Identity and tenancy (Phase 1), 3.2 Shopee connection (Phase 2), 3.3 Capability registry (Phase 2), 3.4 Templates (Phase 3), 3.5 Products (Phase 5), 3.6 Workflow engine (Phase 4), 3.7 Browser automation (Phase 6) — browser extension, 3.8 Dashboard (Phase 7) (+1 more)

### Community 61 - "net/netip.Addr"
Cohesion: 0.09
Nodes (20): InsertAuditLogParams, AuditLog, AuditLogsDefault, CreateSessionParams, UserSession, AuditLog, AuditLogsDefault, UserSession (+12 more)

### Community 62 - "preview.vue"
Cohesion: 0.07
Nodes (24): api, applyKey, busy, canApply, Change, chosen, Detail, error (+16 more)

### Community 63 - "Development guide"
Cohesion: 0.22
Nodes (8): Connecting a Shopee sandbox shop, Database roles, Development guide, Encryption keys, Everyday commands, First run, Prerequisites, Workflows locally

### Community 64 - "jobs"
Cohesion: 0.25
Nodes (8): run, jobs, secrets, web, working-directory, runs-on, defaults, runs-on

### Community 65 - "env"
Cohesion: 0.25
Nodes (8): env, APP_ENV, COOKIE_SECURE, DATABASE_URL, ENCRYPTION_KEYS, PUBLIC_BASE_URL, REDIS_URL, SMTP_ADDR

### Community 66 - "postgres"
Cohesion: 0.25
Nodes (8): POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, env, image, options, ports, postgres

### Community 67 - "rbacdb/models.go"
Cohesion: 0.07
Nodes (27): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, Role, RolePermission (+19 more)

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

### Community 82 - "templatesdb/models.go"
Cohesion: 0.07
Nodes (26): ShopTemplate, ShopTemplateSection, CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission (+18 more)

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
Cohesion: 0.08
Nodes (13): DBTX, ListAuditLogsParams, Queries, 1. Phases at a glance, 2. Phase 1 task breakdown, 4. Phase 3 task breakdown, 5. Phase 4 task breakdown, 6. Definition of done (every phase) (+5 more)

### Community 89 - "setup"
Cohesion: 0.10
Nodes (22): Fake, NewFake(), setup(), TestConnectionRefusedIsRetriedForWrites(), TestConnectRefreshAndRead(), TestErrorsAreNormalizedAndCarryNoSecrets(), TestLogisticsAndProfileWrites(), TestMainAccountTokensRefreshPerShop() (+14 more)

### Community 90 - "Engine"
Cohesion: 0.17
Nodes (10): Definition, Engine, jsonOrNil(), modeFor(), orDefaultDur(), Queries, WorkflowStep, claimed (+2 more)

### Community 91 - "4. Token lifecycle"
Cohesion: 0.40
Nodes (5): 4.1 Facts from the docs, 4.2 Connect flow, 4.3 Refresh, 4.4 Authorization expiry, revocation, reconnection, 4. Token lifecycle

### Community 92 - "Errors"
Cohesion: 0.26
Nodes (6): Errors, itoa(), TestEmail(), TestLength(), TestPassword(), TestSlug()

### Community 93 - "testing.T"
Cohesion: 0.09
Nodes (36): How the pieces fit, TestCSRFAndOrigin(), TestLoginFailuresAndRateLimit(), TestLoginRotatesSession(), TestPasswordResetRevokesSessions(), TestRegisterDoesNotRevealExistingEmail(), TestRegisterValidation(), TestRegisterVerifyLoginLogout() (+28 more)

### Community 95 - "GetCredentialsForUpdateParams"
Cohesion: 0.29
Nodes (3): ListSectionsParams, LockSectionParams, UpdateSectionParams

### Community 96 - "[section].vue"
Cohesion: 0.50
Nodes (3): route, s, sections

### Community 97 - "verify-email.vue"
Cohesion: 0.50
Nodes (3): message, route, state

### Community 98 - "workflows/index.vue"
Cohesion: 0.09
Nodes (21): api, batch, error, fmt, loading, next, { orgId }, pendingApprovals (+13 more)

### Community 99 - "net/http.ResponseWriter"
Cohesion: 0.21
Nodes (5): statusRecorder, JSON(), ctxParts(), Handler, ids()

### Community 104 - ".Decide"
Cohesion: 0.15
Nodes (16): 11. Phase 4 implementation notes, marshal(), safeMessage(), truncate(), CanMoveRun(), DeriveRunStatus(), IsTerminalRun(), IsTerminalStep() (+8 more)

### Community 105 - "ShopTemplate"
Cohesion: 0.18
Nodes (5): GetTemplateParams, InsertTemplateParams, ListTemplatesRow, LockTemplateParams, UpdateTemplateParams

### Community 106 - "Architecture — Shopee Shop Automation"
Cohesion: 0.10
Nodes (21): 10. Phase 3 implementation notes, 12. Conventions, 1. Summary, 2. Architecture decisions, 3. Repository layout, 4. Module boundaries and dependency map, 5. Request lifecycle, 6. Security risk assessment (+13 more)

### Community 107 - "Handler"
Cohesion: 0.23
Nodes (7): QueryInt(), ApprovalPolicy, Handler, optStr(), optUUID(), parts(), pathID()

### Community 109 - "Handler"
Cohesion: 0.22
Nodes (3): MembershipLister, clientKey(), Handler

### Community 121 - "ShopTemplateVersion"
Cohesion: 0.18
Nodes (5): GetVersionParams, InsertVersionParams, ListVersionsParams, LockVersionParams, PublishVersionParams

### Community 122 - "act"
Cohesion: 0.31
Nodes (9): 3.4 Templates, act(), loadDetail(), loadVersion(), newDraft(), publish(), save(), selectVersion() (+1 more)

### Community 123 - "workflows_test.go"
Cohesion: 0.26
Nodes (12): applyResp, preview, previewChange, runView, byPath(), TestTemplateOverridesAndPreview(), client, publishTemplate() (+4 more)

### Community 124 - "WorkflowStep"
Cohesion: 0.15
Nodes (6): ClaimStepParams, FinishStepParams, GetStepParams, ListStepsParams, LockStepParams, SetStepStatusParams

### Community 125 - ".applyPush"
Cohesion: 0.29
Nodes (4): WebhookEvent, SystemFindConnectionsRow, pushBody, SystemFindConnectionsParams

### Community 126 - "schema.ts"
Cohesion: 0.32
Nodes (6): SectionSchema, useTemplateSchemas(), FieldErrors, JsonObject, JsonSchema, labelOf()

### Community 127 - "TestTemplateLifecycle"
Cohesion: 0.60
Nodes (3): client, TestTemplateLifecycle(), TestTemplatePermissions()

### Community 128 - "ShopTemplateMapping"
Cohesion: 0.29
Nodes (3): GetMappingParams, ListMappingsParams, UpsertMappingParams

### Community 129 - "Service"
Cohesion: 0.19
Nodes (6): Recorder, Clock, applyScope(), Scope, Service, Tenants

### Community 130 - "WorkflowRun"
Cohesion: 0.15
Nodes (6): GetRunParams, InsertRunParams, ListRunsParams, LockRunParams, RunsByIdempotencyKeyParams, SetRunStatusParams

### Community 131 - "Sign"
Cohesion: 0.23
Nodes (12): signPush(), AuthURL(), VerifyPush(), TestAuthURLAndPush(), BaseString(), Equal(), Kind, HMAC() (+4 more)

### Community 134 - "logs.vue"
Cohesion: 0.18
Nodes (9): api, error, events, fmt, level, loading, next, { orgId } (+1 more)

### Community 136 - "WorkflowApproval"
Cohesion: 0.18
Nodes (5): DecideApprovalParams, GetApprovalParams, InsertApprovalParams, LatestApprovalForStepParams, LockApprovalParams

### Community 139 - ".CreateRuns"
Cohesion: 0.25
Nodes (4): orDefault(), New(), StartOptions, StartSpec

### Community 140 - "SelectContent.vue"
Cohesion: 0.29
Nodes (4): delegatedProps, emits, forwarded, props

### Community 143 - ".List"
Cohesion: 0.50
Nodes (4): cursor, decodeCursor(), encodeCursor(), Handler

### Community 144 - "Migrate"
Cohesion: 0.50
Nodes (4): main(), Migrate(), MigrateDownAll(), runGoose()

### Community 146 - "Empty"
Cohesion: 0.40
Nodes (5): caps(), TestDiff(), TestSensitivityIsPerField(), Empty(), TestSemanticValidation()

## Knowledge Gaps
- **5 isolated node(s):** `shopee-automation`, `RFC-9457`, `RFC-9457`, `ADR-0003`, `./.nuxt/tsconfig.json`
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 797 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **32 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `2. Permissions` connect `3. Endpoint catalog` to `Result`, `act`, `[shopId].vue`?**
  _High betweenness centrality (0.207) - this node is a cross-community bridge._
- **What connects `shopee-automation`, `RFC-9457`, `RFC-9457` to the rest of the system?**
  _5 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `go_pkg_context` be split into smaller, more focused modules?**
  _Cohesion score 0.05761128876706766 - nodes in this community are weakly interconnected._
- **Why does `3.6 Workflows and approvals` connect `3. Endpoint catalog` to `Result`?**
  _High betweenness centrality (0.074) - this node is a cross-community bridge._
- **Should `github.com/google/uuid.UUID` be split into smaller, more focused modules?**
  _Cohesion score 0.018858307849133536 - nodes in this community are weakly interconnected._
- **Why does `StepContext` connect `Result` to `go_pkg_context`, `views.go`, `github.com/google/uuid.UUID`, `Engine`?**
  _High betweenness centrality (0.071) - this node is a cross-community bridge._
- **Should `identitydb/models.go` be split into smaller, more focused modules?**
  _Cohesion score 0.07407407407407407 - nodes in this community are weakly interconnected._