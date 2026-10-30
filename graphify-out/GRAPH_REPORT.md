# Graph Report - shopify-automation  (2026-10-30)

## Corpus Check
- 327 files · ~196,707 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 9 file(s) not represented in the graph (top: (none) 6, .example 1, .conf 1)

## Summary
- 3322 nodes · 9587 edges · 196 communities (134 shown, 62 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 339 edges (avg confidence: 0.86)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `be75726f`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- go_pkg_context
- net/http.Request
- github.com/google/uuid.UUID
- identitydb/models.go
- wrap
- context.Context
- Service
- package.json
- fake.go
- Message
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
- .Do
- components.json
- Deps
- NotFound
- shops/index.vue
- [shopId].vue
- run
- Shopee Integration — Shopee Shop Automation
- .Get
- model.go
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
- Principal
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
- productsdb/models.go
- backend
- e2e
- login.vue
- tenancydb/models.go
- register.vue
- api.d.ts
- SchemaForm.vue
- env
- net/http.ResponseWriter
- CreateInvitationParams
- csp.mjs
- templates/index.vue
- forgot-password.vue
- accept.vue
- DBTX
- setup
- NewID
- [batchId].vue
- Errors
- testing.T
- GetOAuthStateByHashParams
- GetCredentialsForUpdateParams
- products.ts
- verify-email.vue
- workflows/index.vue
- net/http.ResponseWriter
- tsconfig.json
- FormField.vue
- CLAUDE.md
- 01-roles.sh
- [productId].vue
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
- imports/index.vue
- WorkflowRun
- Sign
- ListAuthorizationsParams
- GetProfileParams
- logs.vue
- ConsumeOAuthStateParams
- WorkflowApproval
- FinishWebhookEventParams
- loader
- ProductForm.vue
- NewFetcher
- prune
- asynqLogger
- .List
- Migrate
- GetActiveSessionByTokenHashRow
- Product
- GetCredentialsForUpdateParams
- ListOrgEventsParams
- .RunSnapshot
- ListAttemptsParams
- RequestCancelParams
- rbacdb/db.go
- PublishPanel.vue
- Inline
- ProductMapping
- Architecture — Shopee Shop Automation
- DBTX
- ProductImportBatch
- TestProductPublish
- github.com/jackc/pgx/v5/pgconn.CommandTag
- .Record
- Implementation Roadmap — Shopee Shop Automation
- .Hash
- NewS3
- DBTX
- products/new.vue
- ProductImportRow
- Queries
- session.ts
- InsertImageParams
- decodeProblem
- GetImageUploadParams
- ListVariationsParams
- SystemRunnableStepsRow
- ConsumeUserTokenParams
- CreateUserTokenParams
- SoftDeleteProductParams
- Selection
- FinishOAuthSelectionParams
- InsertAuthorizationParams
- InsertOAuthStateParams
- ListPendingWebhookEventsParams
- SetAuthorizationExpiryParams
- UpsertProfileParams
- SoftDeleteTemplateParams
- ExpireStaleInvitationParams
- DelayStepParams
- ExpirePendingApprovalsParams
- InsertAttemptParams
- InsertEventParams
- InsertItemStepParams
- InsertSnapshotParams
- InsertStepParams
- ResetStepForRetryParams

## God Nodes (most connected - your core abstractions)
1. `Tenant` - 95 edges
2. `Principal` - 94 edges
3. `newEnv()` - 48 edges
4. `Invalid()` - 45 edges
5. `Decode()` - 40 edges
6. `cn` - 40 edges
7. `vue` - 40 edges
8. `Server` - 36 edges
9. `Service` - 36 edges
10. `Internal()` - 35 edges

## Surprising Connections (you probably didn't know these)
- `5. Phase 4 task breakdown` --references--> `Transition()`  [INFERRED]
  docs/roadmap.md → internal/workflow/state.go
- `6. Assumptions` --references--> `plan()`  [INFERRED]
  docs/requirements.md → internal/workflows/productpublish/prepare.go
- `How the pieces fit` --references--> `TestOpenAPIMatchesRoutes()`  [INFERRED]
  docs/development.md → internal/app/contract_test.go
- `13. Conventions` --references--> `Clock`  [INFERRED]
  docs/architecture.md → internal/platform/clock/clock.go
- `9. Phase 2 implementation notes` --references--> `Inline`  [INFERRED]
  docs/architecture.md → internal/platform/queue/queue.go

## Import Cycles
- None detected.

## Communities (196 total, 62 thin omitted)

### Community 0 - "go_pkg_context"
Cohesion: 0.05
Nodes (34): appResp, batchResp, orgResp, productResp, rowsResp, shopDetail, tplCreated, principalKey (+26 more)

### Community 1 - "net/http.Request"
Cohesion: 0.11
Nodes (12): NotFound(), MustPrincipal(), MustTenant(), PrincipalFrom(), NoContent(), requireAdmin(), Handler, optional() (+4 more)

### Community 2 - "github.com/google/uuid.UUID"
Cohesion: 0.02
Nodes (94): Product, ProductVariation, InvalidateUserTokensParams, Product, ProductVariation, TouchSessionParams, UpdatePasswordHashParams, Ptr() (+86 more)

### Community 3 - "identitydb/models.go"
Cohesion: 0.06
Nodes (31): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, ProductImage, ProductImageUpload (+23 more)

### Community 4 - "wrap"
Cohesion: 0.11
Nodes (11): FailedDependency(), deref(), toApp(), wrap(), App, ConnectInput, ConnectResult, CreateAppInput (+3 more)

### Community 5 - "context.Context"
Cohesion: 0.03
Nodes (28): CreateUserParams, UpdateDisplayNameParams, TenantFrom(), WithPrincipal(), WithTenant(), applyScope(), Scope, Mailer (+20 more)

### Community 6 - "Service"
Cohesion: 0.10
Nodes (29): Conflict(), Forbidden(), UniqueViolation(), CanAssign(), CanManageMember(), Permission, Role, Grants() (+21 more)

### Community 7 - "package.json"
Cohesion: 0.06
Nodes (38): ADR-0003, nuxt, openapi-fetch, openapi-typescript, @pinia/nuxt, @playwright/test, shadcn-nuxt, tailwindcss (+30 more)

### Community 8 - "fake.go"
Cohesion: 0.11
Nodes (14): contains(), Server, num(), random(), remove(), remove2(), writeErr(), writeJSON() (+6 more)

### Community 9 - "Message"
Cohesion: 0.13
Nodes (7): Memory, Message, Memory, Memory, Log, SMTP, memWindow

### Community 10 - "reka-ui"
Cohesion: 0.06
Nodes (28): @lucide/vue, reka-ui, emits, forwarded, props, delegatedProps, emits, forwarded (+20 more)

### Community 11 - "Error"
Cohesion: 0.08
Nodes (27): 13. Conventions, Problem, Recorder, As(), BadRequest(), Error, FieldError, Kind (+19 more)

### Community 12 - "cn"
Cohesion: 0.09
Nodes (21): cn, vue, props, props, props, props, props, props (+13 more)

### Community 13 - "environment"
Cohesion: 0.06
Nodes (37): APP_RW_PASSWORD, APP_SYSTEM_PASSWORD, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER, interval, retries, test (+29 more)

### Community 14 - "Internal"
Cohesion: 0.10
Nodes (22): RegisterInput, Service, Session, User, DBTX, Queries, New(), User (+14 more)

### Community 15 - "Keyring"
Cohesion: 0.21
Nodes (8): 3. Phase 2 task breakdown, Row, WrappedKey, Keyring, newGCM(), open(), rowAAD(), seal()

### Community 16 - "members.vue"
Cohesion: 0.08
Nodes (28): api, changeRole(), error, fmt, Invitation, invitations, invite, inviteError (+20 more)

### Community 17 - "Result"
Cohesion: 0.08
Nodes (51): PublishItem, Address, Service, Access, Ambiguous(), AwaitApproval(), Expand(), Result (+43 more)

### Community 18 - "linters"
Cohesion: 0.08
Nodes (23): generated, rules, formatters, enable, settings, local-prefixes, excludes, linters (+15 more)

### Community 19 - "shops_test.go"
Cohesion: 0.25
Nodes (15): member, findMember(), TestTenantIsolationHTTP(), callback(), env, startConnect(), TestCallbackRejectsBadState(), TestConcurrentRefreshCallsShopeeOnce() (+7 more)

### Community 20 - "[runId].vue"
Cohesion: 0.05
Nodes (38): act(), api, approval, blocked, busy, canDecide, Change, changes (+30 more)

### Community 21 - "shopsdb/models.go"
Cohesion: 0.06
Nodes (29): OauthState, ShopCredential, ShopeeApp, CapabilityOverride, Organization, OrganizationInvitation, OrganizationMember, Permission (+21 more)

### Community 22 - "AlertDialogContent.vue"
Cohesion: 0.11
Nodes (12): emits, forwarded, props, delegatedProps, emits, forwarded, props, delegatedProps (+4 more)

### Community 23 - "settings.vue"
Cohesion: 0.11
Nodes (14): api, confirmSlug, error, etag, fields, form, Member, members (+6 more)

### Community 24 - "client"
Cohesion: 0.19
Nodes (5): resp, loadSpec(), validateResponse(), client, env

### Community 25 - "Input.vue"
Cohesion: 0.11
Nodes (11): @vueuse/core, delegatedProps, props, emits, modelValue, props, delegatedProps, props (+3 more)

### Community 26 - "Principal"
Cohesion: 0.13
Nodes (21): Tenant, derefStr(), Service, sectionETag(), toSection(), toTemplate(), toVersion(), wrap() (+13 more)

### Community 27 - "time.Time"
Cohesion: 0.03
Nodes (100): AuditLog, AuditLogsDefault, CapabilityDefinition, CapabilityOverride, InsertAuditLogParams, OauthState, Organization, OrganizationInvitation (+92 more)

### Community 28 - "ShopConnection"
Cohesion: 0.12
Nodes (8): GetConnectionByShopParams, GetConnectionParams, InsertConnectionParams, ListConnectionsParams, LockConnectionParams, ReactivateConnectionParams, SetConnectionStatusParams, UpdateConnectionSyncedParams

### Community 29 - "go"
Cohesion: 0.12
Nodes (16): go, emit_empty_slices, emit_pointers_for_null_types, out, overrides, package, sql_package, db_type (+8 more)

### Community 30 - "shopee-apps.vue"
Cohesion: 0.15
Nodes (16): api, App, apps, busy, create(), error, fields, form (+8 more)

### Community 31 - ".Do"
Cohesion: 0.05
Nodes (41): backoff(), beforeSend(), App, Client, retryable(), scrub(), truncate(), warningString() (+33 more)

### Community 32 - "components.json"
Cohesion: 0.12
Nodes (15): aliases, components, composables, ui, utils, iconLibrary, $schema, style (+7 more)

### Community 33 - "Deps"
Cohesion: 0.22
Nodes (3): Deps, Config, Limiter

### Community 34 - "NotFound"
Cohesion: 0.09
Nodes (26): Access, Route, App, Adapter, HandlerFunc, statusFor(), adapter(), TestDecodeStrict() (+18 more)

### Community 35 - "shops/index.vue"
Cohesion: 0.10
Nodes (20): api, connectError, connecting, error, filter, fmt, loading, { orgId, can } (+12 more)

### Community 36 - "[shopId].vue"
Cohesion: 0.12
Nodes (18): act(), api, busy, Detail, disconnect(), error, eventLabels, fmt (+10 more)

### Community 37 - "run"
Cohesion: 0.15
Nodes (14): main(), run(), main(), run(), approvalPolicy(), New(), decodeKey(), ParseKeyring() (+6 more)

### Community 38 - "Shopee Integration — Shopee Shop Automation"
Cohesion: 0.11
Nodes (19): 10. Phase 2 implementation status, 11. Open items, 1. Sources and verification method, 2. Environments and hosts, 3. Request signing, 4.1 Facts from the docs, 4.2 Connect flow, 4.3 Refresh (+11 more)

### Community 39 - ".Get"
Cohesion: 0.18
Nodes (9): Detail, toProfile(), toShop(), utcPtr(), ShopConnection, ShopProfile, AuthorizationEvent, Profile (+1 more)

### Community 40 - "model.go"
Cohesion: 0.08
Nodes (36): FinalAttempt(), errsByRow(), TestCSVSafe(), TestParseLimits(), TestRowErrorsAndDefaults(), TestSampleCSVIsValid(), TestValidateVariations(), TestXLSX() (+28 more)

### Community 41 - "default.vue"
Cohesion: 0.15
Nodes (8): api, current, nav, open, orgId, resend, route, session

### Community 42 - "views.go"
Cohesion: 0.11
Nodes (27): versionDetail, LogView, rawJSON(), RawJSON(), wrap(), IsTerminalStep(), contains(), decodeEventCursor() (+19 more)

### Community 43 - "DB"
Cohesion: 0.24
Nodes (9): 11. Phase 4 implementation notes, DB, Open(), EnsurePartitions(), TestEnsurePartitions(), New(), OwnerURL(), System() (+1 more)

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
Cohesion: 0.07
Nodes (40): Context, Definition, Override, Mode, Resolved, overridesFor(), Resolve(), resolveOne() (+32 more)

### Community 48 - "Browser Extension — Shopee Shop Automation"
Cohesion: 0.18
Nodes (11): 10. Open items, 1. Decision, 2. Compliance guardrails, 3. Components, 4. Pairing and authentication, 5. Recipes, 6. Task flow, 7. Data and security (+3 more)

### Community 49 - "Workflow Engine Design — Shopee Shop Automation"
Cohesion: 0.15
Nodes (13): 10. As built (Phase 5), 1. Principles, 2. Concepts, 3.1 Run, 3.2 Step, 3. State machines, 4. Execution, 5. Idempotency against Shopee (+5 more)

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
Nodes (7): useUpload(), ApiError, FetchResult, Problem, toApiError(), RFC-9457, unwrap()

### Community 54 - "[templateId]/index.vue"
Cohesion: 0.07
Nodes (23): activeKey, activeSchema, api, busy, Detail, editable, edits, error (+15 more)

### Community 55 - "Load"
Cohesion: 0.44
Nodes (8): Getenv, Load(), base(), env(), TestLoadDefaults(), TestLoadFromFile(), TestLoadProductionRules(), TestLoadRequiredAndInvalid()

### Community 56 - "with"
Cohesion: 0.20
Nodes (10): with, cache, cache-dependency-path, fetch-depth, go-version-file, name, node-version, path (+2 more)

### Community 57 - "AlertDialogAction.vue"
Cohesion: 0.29
Nodes (6): delegatedProps, props, delegatedProps, props, Props, ButtonVariants

### Community 59 - "3. Endpoint catalog"
Cohesion: 0.12
Nodes (19): 1. Conventions, 2. Permissions, 3.1 Authentication, 3.2 Organizations and members, 3.3 Shops and Shopee connection, 3.5 Products and imports, 3.6 Workflows and approvals, 3.7 Audit and notifications (+11 more)

### Community 60 - "3. Functional requirements"
Cohesion: 0.12
Nodes (16): 1. Repository assessment, 2. Actors, 3.1 Identity and tenancy (Phase 1), 3.2 Shopee connection (Phase 2), 3.3 Capability registry (Phase 2), 3.4 Templates (Phase 3), 3.5 Products (Phase 5), 3.6 Workflow engine (Phase 4) (+8 more)

### Community 61 - "Principal"
Cohesion: 0.10
Nodes (22): Invalid(), Principal, TestWriteErrorMapsKinds(), toBatch(), toRow(), Queries, Product, ProductImportBatch (+14 more)

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
Cohesion: 0.06
Nodes (32): CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, ProductImage, ProductImageUpload (+24 more)

### Community 68 - "Organization"
Cohesion: 0.25
Nodes (3): CreateOrganizationParams, ListOrganizationsForUserRow, UpdateOrganizationParams

### Community 69 - "ref_types_api"
Cohesion: 0.13
Nodes (10): fmt, cursor, error, loading, { orgId, can }, rows, activity, loadError (+2 more)

### Community 70 - "new.vue"
Cohesion: 0.25
Nodes (5): busy, error, fields, form, slugEdited

### Community 71 - "reset-password.vue"
Cohesion: 0.25
Nodes (6): busy, done, error, fields, password, route

### Community 72 - "scripts"
Cohesion: 0.25
Nodes (8): scripts, api:types, build, dev, postinstall, preview, test:e2e, typecheck

### Community 73 - "productsdb/models.go"
Cohesion: 0.06
Nodes (31): ProductImage, CapabilityOverride, OauthState, Organization, OrganizationInvitation, OrganizationMember, Permission, ProductImageUpload (+23 more)

### Community 74 - "backend"
Cohesion: 0.29
Nodes (7): runs-on, services, backend, image, options, ports, redis

### Community 75 - "e2e"
Cohesion: 0.29
Nodes (7): needs, runs-on, services, e2e, image, ports, mailpit

### Community 76 - "login.vue"
Cohesion: 0.29
Nodes (5): busy, error, form, route, session

### Community 77 - "tenancydb/models.go"
Cohesion: 0.06
Nodes (31): Organization, CapabilityOverride, OauthState, OrganizationMember, Permission, ProductImage, ProductImageUpload, ProductImportBatch (+23 more)

### Community 78 - "register.vue"
Cohesion: 0.29
Nodes (5): busy, done, error, fields, form

### Community 79 - "api.d.ts"
Cohesion: 0.29
Nodes (6): components, $defs, operations, paths, RFC-9457, webhooks

### Community 80 - "SchemaForm.vue"
Cohesion: 0.17
Nodes (8): addRow(), emit, fields, props, removeRow(), rows(), set(), setRow()

### Community 81 - "env"
Cohesion: 0.33
Nodes (6): env, TEST_DATABASE_OWNER_URL, TEST_DATABASE_RW_URL, TEST_DATABASE_SYSTEM_URL, TEST_REDIS_URL, TEST_REQUIRE_DB

### Community 82 - "net/http.ResponseWriter"
Cohesion: 0.20
Nodes (7): statusRecorder, JSON(), Handler, optUUID(), parts(), pathID(), readFile()

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

### Community 89 - "setup"
Cohesion: 0.12
Nodes (19): Fake, NewFake(), setup(), TestConnectionRefusedIsRetriedForWrites(), TestConnectRefreshAndRead(), TestErrorsAreNormalizedAndCarryNoSecrets(), TestLogisticsAndProfileWrites(), TestMainAccountTokensRefreshPerShop() (+11 more)

### Community 90 - "NewID"
Cohesion: 0.06
Nodes (35): NewID(), Enqueuer, TestRedisLimiter(), MappingState, waitReason(), backoff(), Definition, Engine (+27 more)

### Community 91 - "[batchId].vue"
Cohesion: 0.08
Nodes (21): api, batch, columns, draft, editing, error, fileColumns, id (+13 more)

### Community 92 - "Errors"
Cohesion: 0.26
Nodes (6): Errors, itoa(), TestEmail(), TestLength(), TestPassword(), TestSlug()

### Community 93 - "testing.T"
Cohesion: 0.11
Nodes (35): TestCSRFAndOrigin(), TestLoginFailuresAndRateLimit(), TestLoginRotatesSession(), TestPasswordResetRevokesSessions(), TestRegisterDoesNotRevealExistingEmail(), TestRegisterValidation(), TestRegisterVerifyLoginLogout(), TestResendVerification() (+27 more)

### Community 95 - "GetCredentialsForUpdateParams"
Cohesion: 0.29
Nodes (3): ListSectionsParams, LockSectionParams, UpdateSectionParams

### Community 96 - "products.ts"
Cohesion: 0.09
Nodes (21): allSelected, api, batch, error, loading, next, { orgId, can }, products (+13 more)

### Community 97 - "verify-email.vue"
Cohesion: 0.50
Nodes (3): message, route, state

### Community 98 - "workflows/index.vue"
Cohesion: 0.09
Nodes (21): api, batch, error, fmt, loading, next, { orgId }, pendingApprovals (+13 more)

### Community 99 - "net/http.ResponseWriter"
Cohesion: 0.32
Nodes (3): ctxParts(), Handler, ids()

### Community 104 - "[productId].vue"
Cohesion: 0.10
Nodes (21): addImages(), api, busy, error, errors, etag, fileInput, fmt (+13 more)

### Community 105 - "ShopTemplate"
Cohesion: 0.18
Nodes (5): GetTemplateParams, InsertTemplateParams, ListTemplatesRow, LockTemplateParams, UpdateTemplateParams

### Community 106 - "Architecture — Shopee Shop Automation"
Cohesion: 0.18
Nodes (11): 2. Architecture decisions, ADR-10 Observability, ADR-1 Modular monolith, ADR-2 sqlc + pgx, not GORM, ADR-3 Frontend: Nuxt 3 in SPA mode, not Inertia.js, ADR-4 Authentication: server-side sessions, ADR-5 Tenant in the URL path, ADR-6 Defense in depth with PostgreSQL RLS (+3 more)

### Community 107 - "Handler"
Cohesion: 0.25
Nodes (6): QueryInt(), Handler, optStr(), optUUID(), parts(), pathID()

### Community 109 - "Handler"
Cohesion: 0.20
Nodes (5): MembershipLister, clientKey(), Handler, Decode(), safeJSONMsg()

### Community 121 - "ShopTemplateVersion"
Cohesion: 0.18
Nodes (5): GetVersionParams, InsertVersionParams, ListVersionsParams, LockVersionParams, PublishVersionParams

### Community 122 - "act"
Cohesion: 0.31
Nodes (9): 3.4 Templates, act(), loadDetail(), loadVersion(), newDraft(), publish(), save(), selectVersion() (+1 more)

### Community 123 - "workflows_test.go"
Cohesion: 0.29
Nodes (11): applyResp, preview, previewChange, runView, byPath(), client, publishTemplate(), setupShopForWorkflow() (+3 more)

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

### Community 129 - "imports/index.vue"
Cohesion: 0.12
Nodes (16): api, batches, busy, error, file, fmt, load(), loading (+8 more)

### Community 130 - "WorkflowRun"
Cohesion: 0.15
Nodes (6): GetRunParams, InsertRunParams, ListRunsParams, LockRunParams, RunsByIdempotencyKeyParams, SetRunStatusParams

### Community 131 - "Sign"
Cohesion: 0.24
Nodes (11): signPush(), AuthURL(), VerifyPush(), TestAuthURLAndPush(), BaseString(), Kind, HMAC(), Sign() (+3 more)

### Community 134 - "logs.vue"
Cohesion: 0.18
Nodes (9): api, error, events, fmt, level, loading, next, { orgId } (+1 more)

### Community 136 - "WorkflowApproval"
Cohesion: 0.18
Nodes (5): DecideApprovalParams, GetApprovalParams, InsertApprovalParams, LatestApprovalForStepParams, LockApprovalParams

### Community 139 - "ProductForm.vue"
Cohesion: 0.13
Nodes (14): attributeError, attributes, combos, emit, f, parsedTiers, props, Row (+6 more)

### Community 140 - "NewFetcher"
Cohesion: 0.16
Nodes (13): Fetcher, NewFetcher(), NewTestFetcher(), Public(), Sanitize(), Sniff(), pngBytes(), pngHeader() (+5 more)

### Community 143 - ".List"
Cohesion: 0.50
Nodes (4): cursor, decodeCursor(), encodeCursor(), Handler

### Community 144 - "Migrate"
Cohesion: 0.50
Nodes (4): main(), Migrate(), MigrateDownAll(), runGoose()

### Community 146 - "Product"
Cohesion: 0.13
Nodes (7): GetProductBySKUParams, GetProductParams, InsertProductParams, ListProductsParams, LockProductParams, ProductsByIDsParams, UpdateProductParams

### Community 152 - "rbacdb/db.go"
Cohesion: 0.15
Nodes (3): New(), DBTX, Queries

### Community 153 - "PublishPanel.vue"
Cohesion: 0.15
Nodes (11): api, busy, emit, error, key, { orgId }, props, selected (+3 more)

### Community 154 - "Inline"
Cohesion: 0.20
Nodes (5): 9. Phase 2 implementation notes, Inline, delayedTask, Handler, Mux

### Community 155 - "ProductMapping"
Cohesion: 0.18
Nodes (5): GetMappingParams, InsertMappingParams, ListMappingsParams, LockMappingParams, SetMappingStateParams

### Community 156 - "Architecture — Shopee Shop Automation"
Cohesion: 0.20
Nodes (10): 10. Phase 3 implementation notes, 12. Phase 5 implementation notes, 1. Summary, 3. Repository layout, 4. Module boundaries and dependency map, 5. Request lifecycle, 6. Security risk assessment, 7. Deployment (initial) (+2 more)

### Community 157 - "DBTX"
Cohesion: 0.20
Nodes (3): Queries, New(), DBTX

### Community 158 - "ProductImportBatch"
Cohesion: 0.20
Nodes (4): GetBatchParams, InsertBatchParams, LockBatchParams, RefreshBatchCountsParams

### Community 159 - "TestProductPublish"
Cohesion: 0.25
Nodes (7): How the pieces fit, TestTenantIsolationDatabase(), baseProduct(), createProduct(), publishCatalog(), TestProductPublish(), Catalog

### Community 161 - ".Record"
Cohesion: 0.25
Nodes (6): 2. Phase 1 task breakdown, Entry, nonNil(), IsSensitiveKey(), RedactMap(), TestRedactMapNested()

### Community 162 - "Implementation Roadmap — Shopee Shop Automation"
Cohesion: 0.25
Nodes (7): 1. Phases at a glance, 4. Phase 3 task breakdown, 5. Phase 4 task breakdown, 6. Phase 5 task breakdown, 7. Definition of done (every phase), 8. Decisions needed from the product owner, Implementation Roadmap — Shopee Shop Automation

### Community 163 - ".Hash"
Cohesion: 0.32
Nodes (5): Argon2Params, Hasher, TestHashAndVerify(), TestVerifyFlagsWeakParams(), TestVerifyMalformed()

### Community 164 - "NewS3"
Cohesion: 0.25
Nodes (4): NewS3(), TestS3(), Config, S3

### Community 166 - "products/new.vue"
Cohesion: 0.25
Nodes (6): api, busy, error, errors, { orgId }, ProductInput

### Community 167 - "ProductImportRow"
Cohesion: 0.29
Nodes (3): AllRowsParams, ListRowsParams, LockRowParams

### Community 169 - "session.ts"
Cohesion: 0.33
Nodes (5): pinia, Membership, User, useSessionStore, pinia@^4.1.0

## Knowledge Gaps
- **6 isolated node(s):** `shopee-automation`, `client`, `RFC-9457`, `RFC-9457`, `ADR-0003` (+1 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 928 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **62 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `2. Permissions` connect `3. Endpoint catalog` to `Result`, `act`, `[shopId].vue`?**
  _High betweenness centrality (0.249) - this node is a cross-community bridge._
- **What connects `shopee-automation`, `client`, `RFC-9457` to the rest of the system?**
  _6 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `go_pkg_context` be split into smaller, more focused modules?**
  _Cohesion score 0.05343403184058434 - nodes in this community are weakly interconnected._
- **Why does `cancel()` connect `3. Endpoint catalog` to `[runId].vue`?**
  _High betweenness centrality (0.083) - this node is a cross-community bridge._
- **Should `net/http.Request` be split into smaller, more focused modules?**
  _Cohesion score 0.1109936575052854 - nodes in this community are weakly interconnected._
- **Why does `retry()` connect `3. Endpoint catalog` to `[runId].vue`?**
  _High betweenness centrality (0.083) - this node is a cross-community bridge._
- **Should `github.com/google/uuid.UUID` be split into smaller, more focused modules?**
  _Cohesion score 0.018642350557244173 - nodes in this community are weakly interconnected._