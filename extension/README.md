# Browser extension: Shopee Shop Automation

A Manifest V3 extension for Chromium browsers (Chrome, Edge, Cốc Cốc, Brave). It runs the Seller Centre steps that the Shopee Open API cannot do, such as changing the shop banner. It works in the seller's own logged-in tab, and only after the seller clicks **Run**. Design and contract: [`docs/browser-extension.md`](../docs/browser-extension.md) (§11 is the contract). Recipe format: [`internal/browser/recipe.schema.json`](../internal/browser/recipe.schema.json).

> **No real Seller Centre recipe ships with this extension.** Recipes are data that the backend serves and signs. A platform admin adds one only after it has been verified on a real seller account (Phase 6, open items E-1/E-2). The recipes in the tests drive invented local fixture pages, not Seller Centre.

Stack: WXT 0.21 · TypeScript (strict) · Vue 3 popup · Tailwind 4 · `@noble/ed25519` · Vitest + happy-dom · Playwright.

## Build

```bash
cd extension
npm ci
npm run build          # production → .output/chrome-mv3/
npm run build:test     # TEST build  → .output/chrome-mv3-test/
npm run dev            # WXT dev mode with reload
```

Settings are read from environment variables (or `.env`, `.env.production.local`, `.env.test`, … which WXT loads) **at build time** and baked into the bundle. See `.env.example`.

| Variable | Meaning | Default |
|---|---|---|
| `WXT_API_ORIGIN` | Origin of the Go API. The extension calls `${origin}/api/v1/ext/*`. `https://…` or `http://localhost[:port]` | `http://localhost:8080` |
| `WXT_SELLER_ORIGINS` | Comma- or space-separated Seller Centre origins the extension may act on. They become host permissions. Production builds accept `https://` only. | empty (a warning is printed) |
| `WXT_FILE_ORIGINS` | Extra origins that input files are downloaded from (presigned S3 URLs), if they are not on the API origin | empty |
| `WXT_RECIPE_PUBLIC_KEYS` | JSON `{"<key_id>":"<base64 raw 32-byte Ed25519 public key>"}`. `key_id` in a claim selects the key. Unknown key ids are rejected. | `{}` (every recipe is rejected) |

The build fails on malformed values, such as an `http://` seller origin in production or a key that is not 32 bytes.

Example production build:

```bash
WXT_API_ORIGIN=https://api.example.com \
WXT_SELLER_ORIGINS="https://seller-centre.example" \
WXT_RECIPE_PUBLIC_KEYS='{"2026-10":"<base64>"}' \
npm run build
```

There are no placeholder Seller Centre domains in the code. Add the real ones per market when E-1 is settled.

**TEST build** (`--mode test`): recipes may also use `http://localhost[:port]` and `http://127.0.0.1[:port]` for `start_url`/`allowed_origins`, so the Playwright fixtures can run. Production builds reject any `http` recipe origin.

## Load unpacked

1. `chrome://extensions` → turn on **Developer mode** → **Load unpacked** → choose `extension/.output/chrome-mv3` (or `-test`).
2. Copy the extension ID. The API must allow `chrome-extension://<id>` (`EXTENSION_ORIGINS`).
3. In the dashboard: **Settings → Browser extension → Connect**. Type the `XXXXX-XXXXX` code into the popup.

## Permissions and why

| Permission | Why |
|---|---|
| `storage` | Device token (`storage.local`); task list and run state (`storage.session`) |
| `alarms` | Poll tasks every minute (badge with the number of queued tasks) and rotate the token about once a day |
| `activeTab` | Needed for `chrome.tabs.captureVisibleTab` (failure screenshots). Chrome requires `activeTab` or `<all_urls>` for that call, and host permissions are **not** enough. The grant comes when the seller opens the popup from the toolbar. |
| `scripting` | Inject the step runner (`runner.js`) into the Seller Centre tab |
| host permissions | `WXT_API_ORIGIN`, `WXT_SELLER_ORIGINS`, `WXT_FILE_ORIGINS` only. With these, the service worker can call the API and download input files without CORS, and read the tab URL of Seller Centre tabs. |

No `cookies`, `webRequest`, `tabs` or `<all_urls>`.

## How a run works

1. The seller clicks **Run**. The service worker picks the active tab of the focused window. It never opens a new tab.
2. `POST /ext/tasks/{id}/claim`, then:
   1. Verify the Ed25519 signature over the exact `recipe.json` string, before parsing it.
   2. `JSON.parse`, then validate against the schema and the semantic rules (below).
   3. Check that the recipe's `capability`/`market` equal the task's, and that the build has host permission for every `allowed_origins` entry.
   4. Download the input files into memory, checking `sha256` if present.
3. If the tab is not on an allowed origin, navigate the same tab to `start_url`.
4. For each step, and then each `verify` step:
   1. Wait for the tab to finish loading.
   2. Check that the tab is still on an allowed origin and that no `stop_on` condition matches. If one fails, pause: send `heartbeat(WAITING_USER)`, show the reason, and wait for **Continue** (which re-checks before resuming).
   3. Run the action, then wait `pace_ms` (fixed, default 800 ms).
5. Heartbeat every 60 s, and on pause/resume. `POST /result` with a log for every step (`ok` / `failed` / `skipped`). On failure, the extension first takes a screenshot of the Seller Centre tab (if Chrome allows it) and uploads it.
6. A `409 lease_lost` from any call ends the run silently: no result, and the popup says "This task was taken over".

The run state (step index, pause, logs) lives in the service worker. The runner script in the page is stateless and executes one command at a time, so navigations (from `goto` or a click) do not lose progress. Before each command the service worker pings the page. If the runner is missing (new document), it injects it again. Read-only commands (`wait_for`, `expect`, `assert_shop_id`, `stop_check`) are retried if a navigation interrupts them. A write command is retried only if it provably never reached the page.

While a run is active, the service worker keeps itself alive with a cheap extension API call every 20 s. If the browser kills it anyway, the next start marks the run as `interrupted`, and the task returns to the queue when its lease expires. Runs are not resumed blindly.

### Recipe validation

The recipe schema is checked by a small interpreter (`src/lib/recipe/schema-check.ts`) for exactly the keywords the schema uses. It reads `internal/browser/recipe.schema.json` itself, bundled at build time, so the two cannot drift. Unknown keywords make it throw. Ajv is not used at run time, because Ajv generates code with `new Function`, which the MV3 CSP forbids. A unit test compares the checker against Ajv 2020 on many mutated recipes.

Semantic rules on top of the schema:

- `assert_shop_id` must come before the first `click`/`fill`/`select`/`upload`.
- `verify` may not contain write actions.
- Templates may only be `{{task.shopee_shop_id}}` or `{{input.<name>}}`, with the input declared and of type `text`. They are allowed only in `goto.url`, `fill.value` and `select.value`. `upload.files` must be `{{input.<files input>}}`. A `{{` anywhere else is an error.
- `allowed_origins` must be `https` (TEST builds: also localhost http). `start_url` and literal `goto` URLs must be on an allowed origin. A `goto` must start with a literal origin, and its rendered URL is checked again at run time.
- `assert_shop_id.pattern` must compile and have a capture group. `stop_on.url_matches` must compile.

### Element targeting

A `target` matches when **every** given field matches:

- `role`: explicit `role` attribute, or the implicit role of common HTML elements.
- `name_any`: the accessible name equals one of the values, ignoring case and whitespace. The name comes from `aria-labelledby`, `aria-label`, `<label>`, `alt`, the value of input buttons, text content for button/link/heading/tab/menuitem/option/checkbox/radio, `title`, then `placeholder`.
- `text_any`: the text contains one of the values. With text alone, only the innermost match counts.
- `selector`: a CSS selector.

Only visible elements count, except for `upload`, because file inputs are usually hidden behind a label. If more than one element matches, the step fails with `ambiguous_target`. Steps wait for the target until `timeout_ms` (default 10 s).

### What the extension reads from the page

- The text of the element that `assert_shop_id` targets.
- For `stop_on.text_any` / `expect.text_any`: whether the visible page text contains given strings. This is checked locally, and only the match is used.
- `location.href` for `stop_on.url_matches`.

Nothing from the page is sent to the API except the step log (action, outcome, duration, error text) and, on failure, a screenshot of the visible Seller Centre tab.

### Error codes reported in `result.error_code`

`signature_invalid`, `unknown_key`, `recipe_invalid`, `recipe_mismatch`, `origin_not_permitted`, `origin_not_allowed`, `missing_input`, `input_fetch_failed`, `input_checksum_mismatch`, `shop_mismatch`, `shop_id_not_found`, `shop_not_asserted`, `target_not_found`, `ambiguous_target`, `invalid_selector`, `target_disabled`, `unsupported_target`, `option_not_found`, `too_many_files`, `expect_failed`, `navigation_interrupted`, `tab_closed`, `stopped_by_user`, `step_failed`.

## Tests

```bash
npm run typecheck   # vue-tsc, strict
npm test            # Vitest + happy-dom unit tests
npm run test:e2e    # Playwright: builds the TEST extension, then runs it in Chromium
```

`test:e2e` uses Playwright's bundled Chromium (`npx playwright install chromium` if it is missing). It runs headless by default; set `HEADED=1` to watch. The global setup does three things:

1. Generates a fresh Ed25519 key pair.
2. Builds `.output/chrome-mv3-test` with that public key, `WXT_API_ORIGIN=http://localhost:4711` and `WXT_SELLER_ORIGINS=http://localhost:4710`.
3. Starts a fake API (`e2e/fake-api.ts`, the §11 endpoints in memory, signing recipes with the test key) and invented fixture pages (`e2e/fixture-site.ts`).

The tests open `chrome-extension://<id>/popup.html` in a tab and bring the "seller" tab to the front before clicking Run, as a toolbar popup would.

Covered end to end:

- pairing and unpairing, and a revoked token
- a two-page run with a file upload
- shop-ID mismatch with no write
- a pause on a verification page and resume after Continue
- a tampered recipe
- `lease_lost`

Screenshots are not covered end to end. A popup opened as a tab gets no `activeTab` grant, so `captureVisibleTab` is refused there, as Chrome documents.
