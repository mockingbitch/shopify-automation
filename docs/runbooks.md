# Runbooks

Short procedures for production incidents. The setup itself is in [deployment.md](deployment.md). The commands assume the single-host reference in `deploy/production/`. On a managed platform, use the platform's equivalent (logs, restart, exec).

**Conventions used below**

```bash
# On the host, from the checkout:
dc() { docker compose -f deploy/production/docker-compose.yml --env-file deploy/production/production.env "$@"; }
# psql as the schema owner. Keep the connection in ~/.pg_service.conf (and the
# password in ~/.pgpass) so it never appears on a command line:
#   [shopee_owner]
#   host=db.internal dbname=shopee_automation user=app_owner sslmode=verify-full
psql service=shopee_owner
```

SQL below runs as `app_owner`, the table owner. Tables use `FORCE ROW LEVEL SECURITY`, so for cross-tenant reads first run `SET row_security = off;` as a role with `BYPASSRLS` (`app_backup`), or query from a superuser/admin session your provider gives you. Do not grant `BYPASSRLS` to `app_rw`.

Logs are JSON on stdout (`dc logs --since 15m api worker nginx`). Every request and task log line carries a trace ID. The messages quoted below are the real `msg` values to grep for.

---

## API down / readiness failing

**Symptoms:** users see 502/504 from nginx; the nginx container is `unhealthy`; `/readyz` monitoring alerts.

**Checks**
1. `dc ps` shows whether `api` is running or restarting, and whether `migrate` exited 0 (`api` does not start until it does).
2. `dc logs --since 10m api | grep -E 'readiness|error|panic' | tail`
   - `readiness: database` or `readiness: redis`: `/readyz` pings Postgres (app_rw pool) and Redis with a 2 s timeout. Go to [DB failover](#db-failover) or [Redis loss](#redis-loss).
   - Config errors at start (`config X: …`) mean a bad env or secret file. In production the API refuses `COOKIE_SECURE=false`, `S3_USE_SSL=false`, a non-https `PUBLIC_BASE_URL` and non-`chrome-extension://` `EXTENSION_ORIGINS`. `config X_FILE: cannot read file` means the secret file is missing or not readable by uid 65532.
3. From inside the network: `dc exec nginx wget -qO- -S http://api:8080/readyz` (expect `204`).
4. `dc logs --since 10m nginx | grep -E '"status":5'` shows which routes fail and the upstream time.
5. Resources: `docker stats --no-stream`. An OOM-killed API (512 MB limit) restarts in a loop.

**Actions**
- Fix the config or secret, then `dc up -d api`.
- If the last deploy caused it, roll back: set `IMAGE_TAG` to the previous version and `dc up -d --no-deps api worker`. Migrations are backward compatible, so the old code runs on the new schema.
- If a dependency is down, the API cannot serve without it. Fix the dependency; the API recovers on its own once pings succeed.

**Follow-up:** add the failure mode to monitoring; check `migrate` logs if a migration was involved.

---

## DB failover

**Symptoms:** `readiness: database`; errors such as `connection refused`, `the database system is in recovery mode`, `cannot execute … in a read-only transaction`; worker `task failed` spikes.

**Checks**
1. The provider console: is a failover in progress, or has one finished? Which endpoint is now primary?
2. `psql service=shopee_owner -c 'select pg_is_in_recovery(), now()'`. `t` means you are connected to a replica.
3. Do the DSNs in `secrets/database_url`, `database_system_url` and `database_migrate_url` use the provider's **cluster/writer endpoint** (which follows the primary) and not an instance hostname?

**Actions**
- With a writer endpoint: wait for the failover to finish (usually 1–2 min). The connection pools reconnect. If errors continue for more than 5 minutes after the provider reports healthy, restart: `dc restart api worker`.
- With an instance hostname: update the three secret files to the new primary, then `dc up -d --force-recreate api worker`.
- Work is safe across the outage. Workflow steps that were RUNNING lose their lease and are re-run by the sweeper after the lease expires (step timeout + 30 s), up to `max_attempts` (5). Steps whose outcome is unknown become `WAITING_USER` with reason `ambiguous` instead of being retried blindly.
- If the primary was lost with data (async replica promoted), treat it as a partial [restore](#restore-from-backup): check recent workflow runs and Shopee changes against the shop.

**Follow-up:** confirm PITR/WAL archiving resumed on the new primary; record the downtime against NFR-2.

---

## Redis loss

Redis holds only **asynq queues** (pending, scheduled, retry and archived tasks) and **rate-limit counters** (`rl:*`). Sessions, CSRF secrets, workflow state, the email outbox and webhook events are in Postgres. **The database is the source of truth.**

**Symptoms:** `readiness: redis` (the API returns 503 for readiness); worker logs connection errors; `rate limiter unavailable` warnings. The limiters fail open, so login throttling and Shopee rate limits are **not enforced** while Redis is down.

**What is lost, and how it comes back once Redis is back (empty)**

| Work | Recovery |
|---|---|
| Workflow steps (`workflow:step`) | The scheduler enqueues `workflow:sweep` every 15 s. It re-enqueues PENDING steps whose `next_attempt_at` passed more than 30 s ago and RUNNING steps whose lease expired. Expect recovery within about a minute. |
| Emails (`mail:send`) | `mail:sweep` every 5 min re-sends `email_outbox` rows with `failed_at IS NULL AND next_attempt_at < now() - 1 min`. |
| Webhooks (`webhook:process`) | `webhook:sweep` every 2 min re-enqueues `webhook_events` in `RECEIVED` older than 1 min, 200 at a time. |
| Token refresh | `shopee:token_refresh_due` every 5 min enqueues refreshes for credentials expiring within 30 min. The API also refreshes just in time (5 min before expiry). |
| Shop syncs (`shop:sync`) | **Lost.** Users can click Sync again (`POST …/shops/{id}/sync`). The next sync trigger also covers it. |
| Product imports (`products:import`) | **Lost.** Batches stay in their current state. Retry with `POST /api/v1/orgs/{org}/imports/{batch}/retry` (the dashboard Retry button). |
| Retry history / archived tasks | Lost. Only diagnostics. |

**Actions**
1. Restore Redis (provider failover or restart). Use `maxmemory-policy noeviction`: an evicting policy silently drops queued tasks.
2. If `REDIS_URL` changed, update `secrets/redis_url` and `dc up -d --force-recreate api worker`.
3. The scheduler re-registers its cron entries in-process. Restart the worker only if it logs repeated connection errors after Redis is healthy: `dc restart worker`.
4. Find imports that need a manual retry:
   ```sql
   SELECT id, org_id, status, created_at FROM product_import_batches
   WHERE status IN ('UPLOADED','PARSING','IMPORTING') AND created_at > now() - interval '1 day';
   ```

**Follow-up:** check for login abuse during the outage window, because throttling failed open (`audit_logs`, nginx logs).

---

## Shopee outage / rate limiting

**Symptoms:** API responses with `upstream_unavailable` (503); workflow steps retrying; logs `task failed` with Shopee errors; Shopee status page incident.

**How the system behaves**
- Rate limits are enforced in Redis before each call: `SHOPEE_RATE_PER_APP` (default 50/s) and `SHOPEE_RATE_PER_SHOP` (default 5/s). Callers wait for the next window.
- Each call is tried at most 4 times with jittered backoff (≤ 5 s). The client retries rate-limited responses (except `error_limit`), network errors from requests that never reached Shopee, and transient errors on idempotent requests. A write that may have reached Shopee is **not** retried. The step becomes `WAITING_USER` (reason `ambiguous`) so a person can check the shop and resume.
- Workflow steps retry with exponential backoff (2 s up to 5 min, full jitter), up to 5 attempts.
- There is no circuit breaker.

**Checks**
```sql
-- Recent step outcomes
SELECT outcome, count(*) FROM workflow_step_attempts
WHERE finished_at > now() - interval '1 hour' GROUP BY 1;
-- Steps waiting for a person because the result is unknown
SELECT run_id, step_key, updated_at FROM workflow_steps
WHERE status = 'WAITING_USER' AND output->>'reason' = 'ambiguous' ORDER BY updated_at DESC;
```
Traces carry `shopee.error`, `shopee.attempts` and `shopee.request_id`. Quote the request id when you contact Shopee.

**Actions**
- **Daily quota (`error_limit`):** not retried. It resets at 00:00 UTC+8. Find which shop or app is hot from the traces, and lower `SHOPEE_RATE_PER_SHOP`/`_PER_APP` if needed (then `dc up -d api worker`).
- **Frequent 429s:** lower `SHOPEE_RATE_PER_APP` below the app's real quota.
- **Long outage:** nothing is lost. Steps that use up their attempts end `FAILED`; once Shopee is back, retry the runs from the dashboard (`POST …/workflows/{run}/retry`). Do not stop the worker: emails and sweeps would stop too.
- **App configuration errors** (`error_sign`, `invalid_partner_id`, `error_partner_key_expired`, `source_ip_undeclared`, `api_suspended`) are not outages. Fix the partner key or IP allow-list in the Shopee console and the admin UI.
- After recovery, ask affected users to check runs waiting with reason `ambiguous` and resume them (`POST …/workflows/{run}/resume`).

---

## Token refresh failures and mass RECONNECT_REQUIRED

**How it works:** every 5 min the scheduler enqueues `shopee:token_refresh` for `ACTIVE` connections of enabled apps whose access token expires within 30 min (log `due refreshes enqueued`). A refresh takes a row lock in Postgres. On success it resets `shop_credentials.consecutive_failures`. On a Shopee error that means authorization is lost, the connection becomes **`RECONNECT_REQUIRED`**: event `REFRESH_FAILED`, audit `shop.reconnect_required`, and an in-app notification (`shop_reconnect_required`) to members who can connect shops. The error codes that trigger this are `shop_access_expired`, `refresh_token_expired`, `error_shop_refresh_token`, `error_merchant_refresh_token`, `merchant_access_expired`, `shop_no_linked`, `partner_shop_no_link`, `merchant_no_linked` and `shop_banned`. Other errors keep the tokens, increase `consecutive_failures` and are retried (5 times).

**Checks**
```sql
SELECT status, count(*) FROM shop_connections GROUP BY 1;
-- Who is failing and why
SELECT s.id, s.org_id, s.shop_name, s.status, c.access_expires_at, c.refresh_expires_at,
       c.consecutive_failures, c.last_error_code, c.last_refreshed_at
FROM shop_connections s JOIN shop_credentials c ON c.shop_connection_id = s.id
WHERE c.consecutive_failures > 0 OR s.status = 'RECONNECT_REQUIRED'
ORDER BY c.consecutive_failures DESC;
SELECT last_error_code, count(*) FROM shop_credentials WHERE consecutive_failures > 0 GROUP BY 1;
-- One shop's history
SELECT * FROM shop_authorizations WHERE shop_connection_id = '…' ORDER BY created_at DESC LIMIT 20;
```
Logs: `grep '"msg":"token refresh"'` in worker logs.

**Interpretation and actions**
- **Many shops of one app become RECONNECT_REQUIRED at once:** suspect the Shopee app, not the shops. Check the app in the Shopee console (partner key reset, app suspended, authorization revoked in bulk). If the partner key changed, update it (admin UI → Shopee apps → edit partner key). Shops still have to reconnect: refresh tokens rejected by Shopee cannot be revived.
- **Single shops:** normal expiry or the seller revoked access. The seller reconnects from the dashboard (Reconnect, `POST …/shops/{id}/reconnect`); the OAuth callback reactivates the connection.
- **Transient errors (high `consecutive_failures`, status still ACTIVE):** usually a Shopee outage ([above](#shopee-outage--rate-limiting)). The just-in-time refresh keeps working once Shopee answers.
- **Refreshes not being enqueued for a connection** although it is due: a refresh task whose retries ran out stays archived in asynq under its task id (`refresh:<conn>:<expiry>`) and blocks re-enqueue for that expiry. Inspect and delete archived tasks with the asynq CLI (`asynq task ls --queue default --state archived`, `asynq task delete …`), or rely on the just-in-time refresh.
- **Never** set `status = 'ACTIVE'` by hand on a `RECONNECT_REQUIRED` connection: its tokens are invalid.

**Follow-up:** if many sellers must reconnect, send them a notice; the in-app notification is deduplicated per connection.

---

## Webhook backlog

**How it works:** `POST /api/v1/webhooks/shopee/{appID}` verifies the HMAC signature, deduplicates on `(shopee_app_id, body_sha256)`, stores the event in `webhook_events` as `RECEIVED`, and enqueues `webhook:process`. Processing marks it `PROCESSED` or `IGNORED`, or `FAILED` after 10 attempts. `webhook:sweep` re-enqueues `RECEIVED` events older than 1 min, up to 200 every 2 min.

**Symptoms:** reconnect or expiry notices arrive late; `webhook_events` keeps growing in `RECEIVED`.

**Checks**
```sql
SELECT status, count(*), min(received_at) FROM webhook_events GROUP BY 1;
SELECT id, push_code, shopee_shop_ids, attempts, last_error, received_at
FROM webhook_events WHERE status IN ('RECEIVED','FAILED') ORDER BY received_at LIMIT 50;
```
Logs: `webhook: bad signature` (401s; usually a wrong partner key or a push URL that differs from the app's `push_callback_url`), `webhook: enqueue failed` (Redis).

**Actions**
- Worker down or Redis down: fix that first. The sweeper drains the backlog afterwards at up to 200 events per 2 min plus each event's own task.
- Many `FAILED` with the same `last_error`: fix the cause, then replay:
  ```sql
  UPDATE webhook_events SET status = 'RECEIVED', attempts = 0 WHERE status = 'FAILED' AND received_at > now() - interval '1 day';
  ```
  The sweeper picks them up within about 3 minutes.
- Many 401s in nginx logs for the webhook path: check the partner key and push URL in the admin UI against the Shopee console.

---

## A browser recipe is failing

**How it works:** the extension runs signed recipes (`browser_recipes`). After **3 failures in a row** that are the recipe's fault, the recipe is disabled automatically (`disabled_reason = 'Disabled automatically after 3 failures in a row'`, log `browser recipe disabled after repeated failures`). Failures caused by the user or environment do not count: `stopped_by_user`, `shop_mismatch`, `tab_closed`, input fetch or checksum problems, `origin_not_permitted`, `unknown_key`, `signature_invalid`, `navigation_interrupted`. While a recipe is disabled, new claims fail with `recipe_disabled` and the workflow step waits.

**Checks**
```sql
SELECT id, capability_key, market, version, enabled, disabled_reason,
       success_count, failure_count, consecutive_failures, verified_at
FROM browser_recipes ORDER BY capability_key, market, version DESC;
SELECT recipe_id, status, last_error->>'code' AS code, count(*) FROM browser_tasks
WHERE created_at > now() - interval '1 day' GROUP BY 1, 2, 3 ORDER BY 4 DESC;
```
Screenshots of failed tasks (`browser_task_screenshots`) usually show the Seller Centre change that broke the selectors.

**Actions**
- **Kill switch:** a platform admin disables the recipe in the admin UI (Browser recipes → disable), or with `PATCH /api/v1/admin/browser-recipes/{id}` and `{"enabled": false}`. The reason is recorded as "Disabled by a platform admin".
- Fix the recipe as a **new version**, test it, mark it verified, and enable it. Enabling one version disables the others for the same capability and market.
- Steps waiting on the capability continue once an enabled recipe exists; users resume or retry the run.
- `unknown_key` or `signature_invalid` from many devices is **not** a recipe bug. The extension build and `RECIPE_SIGNING_KEY_ID` disagree (see deployment.md §9.2).

---

## Suspected token or secret leak

Act in this order. Write down the timeline as you go.

1. **Contain.**
   - Leaked DB credential: change that role's password now and update the secret file (deployment.md §9.4).
   - Leaked server host or backups: assume every secret on it is exposed.
2. **Rotate the KEK** (deployment.md §9.1): put the new key first in `ENCRYPTION_KEYS`, set it active, roll `api`/`worker`, run `admin rewrap` (see `admin --help`), check that no rows still use the old `kek_id`, then remove the old key.
3. **Rotate the Shopee partner key** in the Shopee console, then in the admin UI (Shopee apps → edit partner key). If ciphertext and the KEK may both have leaked, Shopee access and refresh tokens must be treated as exposed. Revoke them on the Shopee side where possible; the affected shops move to `RECONNECT_REQUIRED` and sellers reconnect.
4. **Rotate the recipe signing key** if `RECIPE_SIGNING_KEY` leaked (deployment.md §9.2, emergency variant).
5. **Revoke extension devices.** One device: `DELETE /api/v1/orgs/{org}/extension/devices/{device}` or the dashboard device list. All devices (there is no API for this):
   ```sql
   UPDATE extension_devices SET revoked_at = now() WHERE revoked_at IS NULL;
   ```
   Users pair again from the dashboard.
6. **Force logout.** There is no API to revoke all sessions. Use SQL:
   ```sql
   -- one user
   UPDATE user_sessions SET revoked_at = now()
   WHERE user_id = (SELECT id FROM users WHERE email = 'user@example.com') AND revoked_at IS NULL;
   -- everyone
   UPDATE user_sessions SET revoked_at = now() WHERE revoked_at IS NULL;
   ```
   A password reset also revokes all of that user's sessions.
7. **Rotate the rest** that shared the exposure: S3 keys, Redis password, SMTP credentials, backup keys.
8. **Investigate** with `audit_logs` (for example `shopee_app.updated`, `extension.device_revoked`, `org.deleted`), nginx access logs, and provider audit logs. Notify affected customers as your policy and law require.

---

## Restore from backup

Use PITR from the provider when it is available (lowest RPO). Use logical backups (`deploy/backup/`) when PITR is not possible, or to recover a single table into a scratch database.

1. **Decide the target time.** Stop writers so nothing diverges: `dc stop api worker`. Nginx then returns 502.
2. **Restore into a new database or instance.** Never restore over the live one.
   - PITR: the provider console → restore to time T → new instance.
   - Logical: create an empty database owned by `app_owner` on a cluster that has the roles `app_owner`, `app_rw` and `app_system`, then
     ```bash
     PGHOST=… PGUSER=app_backup PGSSLMODE=verify-full PGPASSFILE=… \
     GPG_HOMEDIR=/secure/gpg deploy/backup/restore.sh --target-db shopee_automation_restored FILE.dump.gpg
     ```
     (or `BACKUP_PASSPHRASE_FILE=…` for symmetric backups). It verifies the `.sha256`, refuses a non-empty target without `--force`, and runs in a single transaction.
3. **Check the restore:** row counts of key tables (`organizations`, `shop_connections`, `workflow_runs`), the latest `created_at` values, and `SELECT max(version_id) FROM goose_db_version` (must not be newer than the image you will run).
4. **Point the app at it:** update `database_url`, `database_system_url` and `database_migrate_url`, then `dc run --rm migrate` (applies anything newer) and `dc up -d api worker`.
5. **Clean up state that diverged:**
   - Redis may still hold tasks for rows that no longer exist. Those tasks fail and are dropped. To start clean, flush the asynq data (`FLUSHDB` on the app's Redis database); the sweepers rebuild the queue from Postgres.
   - Shopee tokens refreshed after the backup time are gone (Shopee rotates refresh tokens). Those shops go to `RECONNECT_REQUIRED` on their next refresh, and sellers must reconnect.
   - Workflow steps that ran on Shopee after the backup time are not recorded. Tell affected organizations to check their shops.
   - The `ENCRYPTION_KEYS` set must include every KEK used at the backup time.
6. **Follow-up:** record the RPO and RTO you achieved, and keep the old database for investigation until the incident is closed.

The quarterly drill is `make restore-drill` (dev) or the same scripts against a staging copy.

---

## Purge of deleted organizations

Deleting an organization (`DELETE /api/v1/orgs/{org}` with the slug as confirmation) is a **soft delete**: it sets `organizations.deleted_at` and records audit `org.deleted`. Normal reads hide the organization.

**Automatic purge:** the scheduler runs an organization purge every hour at minute 47. It removes up to 20 organizations whose `deleted_at` is older than `ORG_RETENTION` (default `720h` = 30 days, minimum 24 h). For each one it:

- deletes its stored files (S3 prefix `orgs/<org_id>/`) first;
- deletes its `workflow_events` (partitioned, so the cascade does not reach them);
- deletes the `organizations` row, which cascades to every tenant table.

Audit logs are kept on purpose and follow their own retention. Each purge logs `organization purged` with `org_id` and `files`.

**Manual purge** (for example after lowering `ORG_RETENTION`, or to drain a backlog): run `admin purge-orgs` (see `admin --help`). It uses the same retention and loops until fewer than 20 remain:

```bash
dc run --rm --no-deps --entrypoint /usr/local/bin/admin worker purge-orgs
```

**Checks**
```sql
SELECT id, slug, deleted_at, deleted_at < now() - interval '30 days' AS due
FROM organizations WHERE deleted_at IS NOT NULL ORDER BY deleted_at;
```

**Before a purge removes data you need**
- Legal hold or open support case: raise `ORG_RETENTION` for the whole deployment, or restore `deleted_at = NULL` for that organization (an un-delete; record it in the incident).
- Until the purge, background jobs keep working for soft-deleted organizations (token refresh, workflow sweeps). Disconnect their shops if they should stop at once.
- Backups still contain purged data until they expire. State this in the retention policy.

**If the purge fails:** worker logs show the task error (for example S3 permission denied on `DeletePrefix`). Files are deleted before rows, so a retry is safe. Fix the cause and wait for the next hourly run, or run `admin purge-orgs`.
