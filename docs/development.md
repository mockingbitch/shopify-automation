# Development guide

## Prerequisites

- Go 1.27+
- Node.js 24+ (CI uses 26)
- Docker with Compose

## First run

```bash
make env                      # .env from .env.example with a fresh encryption key
make up                       # Postgres :5433, Redis :6379, S3 (SeaweedFS) :8333, Mailpit :8025, Jaeger :16686
make tools                    # sqlc + golangci-lint
make run                      # applies migrations, starts the API on :8080
make worker                   # second terminal: background tasks + scheduler (needed for workflows)

cd web
npm ci
npm run dev                   # dashboard on http://localhost:3100 (proxies /api to :8080)
```

Open http://localhost:3100, create an account, then open Mailpit at http://localhost:8025 to click the verification link.

## Connecting a Shopee sandbox shop

Needs a Shopee Open Platform developer account with a sandbox app (open item S-1). Never use production partner keys or real seller accounts locally.

1. Make yourself a platform admin: `go run ./cmd/admin promote -email you@example.com` (uses `DATABASE_URL` from `.env`).
2. Open *Platform admin: Shopee apps* in the sidebar and add the app with environment **sandbox**, its test partner ID and test partner key. Hosts default to the documented sandbox hosts.
3. In the Shopee Console, set the test redirect URL domain to the host of `PUBLIC_BASE_URL`, and the push callback to the URL shown for the app. Shopee must be able to reach both, so for local work expose the dashboard port with a tunnel and set `PUBLIC_BASE_URL` to the tunnel URL.
4. In an organization, open *Shops* → *Connect shop* and sign in with a sandbox test shop.

Without a sandbox app, the Go integration tests cover the same flows against `internal/shopee/shopeefake`, a test double of the documented endpoints.

### Trying the dashboard with the FAKE Shopee (no account needed)

`make fake-shopee` (or `go run ./cmd/shopeefake`) runs the test double on http://127.0.0.1:8099 with two test shops (7001, 7002, market VN), shipping channels, addresses and a small catalog (categories 100017 "Áo thun" with a mandatory attribute and standard variations Màu sắc / Kích cỡ, and 100636 "Dụng cụ nhà bếp"). It prints the partner ID and key to enter under *Platform admin → Shopee apps* (environment sandbox, API and auth host `http://127.0.0.1:8099`; http hosts are accepted outside production). *Connect shop* then opens a page marked **FAKE Shopee** where you pick a test shop. State lives in memory: restarting the fake loses its shops' tokens, so reconnect afterwards.

This is a development aid only. Nothing tested against it counts as verified against Shopee, and it refuses to start with `APP_ENV=production`.

## Everyday commands

| Command | What it does |
|---|---|
| `make test` | Unit tests; integration tests are skipped without a database |
| `make test-integration` | All Go tests against the dev Postgres/Redis (`make up` first) |
| `make worker` | Run the background worker with the scheduler |
| `go run ./cmd/admin gen-key` | Print a new 32-byte encryption key for `ENCRYPTION_KEYS` |
| `go run ./cmd/admin promote -email …` | Grant platform admin (`demote` revokes) |
| `make lint` | `go vet` + `golangci-lint` |
| `make sqlc` | Regenerate database code after editing `db/queries/**` or migrations |
| `make migrate` | Apply migrations to the dev database |
| `cd web && npm run api:types` | Regenerate TypeScript API types after editing `api/openapi.yaml` |
| `cd web && npm run typecheck` | Type-check the dashboard |
| `cd web && npx playwright test` | Browser smoke test (API and Mailpit must be running) |

## How the pieces fit

- **Adding an endpoint:** add the route to `internal/app/app.go` (with its access rule and permission), document it in `api/openapi.yaml`, regenerate web types. `TestOpenAPIMatchesRoutes` fails until both sides match, and every response in the integration tests is validated against the document.
- **Adding a tenant table:** include `org_id`, a composite unique `(org_id, id)`, `ENABLE` + `FORCE ROW LEVEL SECURITY` and the tenant policy, then extend `TestTenantIsolationDatabase`.
- **Database access:** always through `db.Tx(ctx, db.Scope{OrgID, UserID}, ...)`. Without a scope, tenant tables return no rows.
- **Errors:** services return `apperr` errors; handlers never write error responses themselves.
- **Audit:** every state change calls `audit.Recorder.Record` inside the same transaction.

## Database roles

| Role | Used by | Notes |
|---|---|---|
| `app_owner` | migrations | owns the schema |
| `app_rw` | API at run time | subject to RLS |
| `app_system` | worker cross-tenant lookups | `BYPASSRLS`, column-level `SELECT` only |

Integration tests create a throwaway database per test (`internal/platform/testdb`), migrate it as `app_owner` and connect as `app_rw` (and as `app_system` when `TEST_DATABASE_SYSTEM_URL` is set; CI always sets it).

## Workflows locally

Workflow steps run on the worker. Without `make worker`, applied templates stay `PENDING`. The scheduler (`-scheduler`, included in `make worker`) re-enqueues steps every 15 seconds, so a restarted worker picks up where it stopped. Run only one worker build at a time: an older worker without a handler for a task type consumes and drops those tasks (the sweeper re-enqueues them, but the old worker may take them again).

## Encryption keys

`ENCRYPTION_KEYS` holds one or more `id:base64key` pairs; `ENCRYPTION_ACTIVE_KEY` chooses the key for new data. To rotate, add a new key, make it active and keep the old one until every row has been re-wrapped (job planned for Phase 8). Losing all keys makes stored Shopee tokens unreadable; shops then need to be reconnected.

## Browser extension

The extension lives in `extension/` (see its README). For local work:

1. `go run ./cmd/admin gen-recipe-key` prints a `RECIPE_SIGNING_KEY` for `.env` and the public key for the extension build (`WXT_RECIPE_PUBLIC_KEYS={"<RECIPE_SIGNING_KEY_ID>":"<public key>"}`).
2. Build with `WXT_API_ORIGIN=http://localhost:8080` and load `extension/.output/chrome-mv3` as an unpacked extension. Put its `chrome-extension://<id>` origin in `EXTENSION_ORIGINS`.
3. Pair it from **Browser Extension** in the dashboard.
4. Recipes are added under **Platform admin → Browser recipes**. They stay disabled until marked verified, which is only honest after a run on a real seller account. No real recipe ships with the code.

Tests: `cd extension && npm test && npm run test:e2e` (fixture pages and a fake API; no Shopee account needed).


## Email and notifications

All email (verification, password reset, invitations) is stored encrypted in `email_outbox` and sent by the **worker** (`mail:send`), so run `make worker` to receive mail in Mailpit (http://localhost:8025). In-app notifications appear under the bell at the top of each organization page.

If repeated local test runs hit the sign-up rate limit (10 per hour per IP), clear the `rl:*` keys in the dev Redis.

Accessibility: `cd web && npx playwright test e2e/a11y.spec.ts` (needs the API, worker and Mailpit) runs axe-core over every main page.
