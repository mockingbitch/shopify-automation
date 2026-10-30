-- Phase 4: workflow engine (runs, steps, attempts, approvals, events) and
-- shop configuration snapshots.

-- +goose Up

CREATE TABLE workflow_runs (
  id                  uuid PRIMARY KEY,
  org_id              uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  definition_key      text NOT NULL,
  definition_version  int NOT NULL,
  shop_connection_id  uuid NOT NULL,
  batch_id            uuid,
  status              text NOT NULL CHECK (status IN ('PENDING', 'RUNNING', 'WAITING_APPROVAL', 'WAITING_USER',
                        'COMPLETED', 'PARTIALLY_COMPLETED', 'FAILED', 'CANCELLED')),
  input               jsonb NOT NULL DEFAULT '{}'::jsonb,
  output              jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_key     text,
  request_hash        bytea,
  created_by          uuid REFERENCES users (id),
  started_at          timestamptz,
  finished_at         timestamptz,
  cancel_requested_at timestamptz,
  cancel_reason       text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workflow_runs_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT workflow_runs_shop_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);
-- Double-submit protection: one key per org may start one batch.
CREATE INDEX workflow_runs_idem_idx ON workflow_runs (org_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
-- One active mutating setup per shop.
CREATE UNIQUE INDEX workflow_runs_one_active_setup ON workflow_runs (shop_connection_id)
  WHERE definition_key = 'SHOP_SETUP' AND status IN ('PENDING', 'RUNNING', 'WAITING_APPROVAL', 'WAITING_USER');
CREATE INDEX workflow_runs_org_created_idx ON workflow_runs (org_id, created_at DESC);
CREATE INDEX workflow_runs_batch_idx ON workflow_runs (org_id, batch_id) WHERE batch_id IS NOT NULL;
CREATE TRIGGER workflow_runs_updated_at BEFORE UPDATE ON workflow_runs
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE workflow_steps (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL,
  run_id           uuid NOT NULL,
  step_key         text NOT NULL,
  item_key         text NOT NULL DEFAULT '',
  position         int NOT NULL,
  status           text NOT NULL CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED',
                     'CANCELLED', 'WAITING_APPROVAL', 'WAITING_USER')),
  blocking         boolean NOT NULL DEFAULT true,
  mutating         boolean NOT NULL DEFAULT false,
  capability_key   text,
  execution_mode   text,
  attempt_count    int NOT NULL DEFAULT 0,
  max_attempts     int NOT NULL CHECK (max_attempts > 0),
  next_attempt_at  timestamptz NOT NULL DEFAULT now(),
  lease_token      uuid,
  lease_expires_at timestamptz,
  output           jsonb,
  error            jsonb,
  skip_reason      text,
  started_at       timestamptz,
  finished_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workflow_steps_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT workflow_steps_key_uk UNIQUE (run_id, step_key, item_key),
  CONSTRAINT workflow_steps_run_fk FOREIGN KEY (org_id, run_id) REFERENCES workflow_runs (org_id, id) ON DELETE CASCADE,
  CONSTRAINT workflow_steps_lease_ck CHECK ((status = 'RUNNING') = (lease_token IS NOT NULL))
);
CREATE INDEX workflow_steps_run_idx ON workflow_steps (run_id, position);
CREATE INDEX workflow_steps_runnable_idx ON workflow_steps (status, next_attempt_at)
  WHERE status IN ('PENDING', 'RUNNING');
CREATE TRIGGER workflow_steps_updated_at BEFORE UPDATE ON workflow_steps
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE workflow_step_attempts (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL,
  step_id     uuid NOT NULL,
  attempt_no  int NOT NULL,
  outcome     text NOT NULL CHECK (outcome IN ('SUCCEEDED', 'FAILED', 'RETRY', 'TIMED_OUT', 'AMBIGUOUS',
                'WAITING_USER', 'WAITING_APPROVAL', 'SKIPPED', 'CANCELLED')),
  error       jsonb,
  trace_id    text,
  started_at  timestamptz NOT NULL,
  finished_at timestamptz NOT NULL,
  CONSTRAINT workflow_step_attempts_step_fk FOREIGN KEY (org_id, step_id) REFERENCES workflow_steps (org_id, id) ON DELETE CASCADE
);
CREATE INDEX workflow_step_attempts_step_idx ON workflow_step_attempts (step_id, attempt_no);

CREATE TABLE workflow_approvals (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL,
  run_id           uuid NOT NULL,
  step_id          uuid NOT NULL,
  status           text NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED')),
  diff_snapshot    jsonb NOT NULL,
  selected_changes jsonb,
  decided_by       uuid REFERENCES users (id),
  comment          text CHECK (char_length(comment) <= 1000),
  decided_at       timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workflow_approvals_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT workflow_approvals_step_fk FOREIGN KEY (org_id, step_id) REFERENCES workflow_steps (org_id, id) ON DELETE CASCADE,
  CONSTRAINT workflow_approvals_run_fk FOREIGN KEY (org_id, run_id) REFERENCES workflow_runs (org_id, id) ON DELETE CASCADE,
  CONSTRAINT workflow_approvals_decided_ck CHECK ((status = 'PENDING') = (decided_at IS NULL))
);
CREATE UNIQUE INDEX workflow_approvals_one_pending ON workflow_approvals (step_id) WHERE status = 'PENDING';
CREATE INDEX workflow_approvals_org_status_idx ON workflow_approvals (org_id, status, created_at DESC);

-- Append-only event log, partitioned by month like audit_logs.
CREATE TABLE workflow_events (
  id         uuid NOT NULL,
  org_id     uuid NOT NULL,
  run_id     uuid NOT NULL,
  step_id    uuid,
  type       text NOT NULL,
  level      text NOT NULL DEFAULT 'info' CHECK (level IN ('info', 'warn', 'error')),
  message    text NOT NULL DEFAULT '',
  data       jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE INDEX workflow_events_run_idx ON workflow_events (run_id, created_at, id);
CREATE INDEX workflow_events_org_idx ON workflow_events (org_id, created_at DESC);

SELECT app_ensure_monthly_partition('workflow_events', (date_trunc('month', now()) + make_interval(months => m))::date)
FROM generate_series(-1, 14) AS m;
CREATE TABLE workflow_events_default PARTITION OF workflow_events DEFAULT;

-- +goose StatementBegin
CREATE FUNCTION app_workflow_events_immutable() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
BEGIN
  RAISE EXCEPTION 'workflow_events is append-only';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_events_no_update BEFORE UPDATE OR DELETE ON workflow_events
  FOR EACH ROW EXECUTE FUNCTION app_workflow_events_immutable();

-- Configuration read from Shopee before and after applying, and at sync.
CREATE TABLE shop_config_snapshots (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL,
  shop_connection_id uuid NOT NULL,
  kind               text NOT NULL CHECK (kind IN ('BEFORE_APPLY', 'AFTER_APPLY', 'SYNC')),
  workflow_run_id    uuid,
  config             jsonb NOT NULL,
  captured_at        timestamptz NOT NULL,
  CONSTRAINT shop_config_snapshots_shop_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE,
  CONSTRAINT shop_config_snapshots_run_fk FOREIGN KEY (org_id, workflow_run_id)
    REFERENCES workflow_runs (org_id, id) ON DELETE CASCADE
);
CREATE INDEX shop_config_snapshots_shop_idx ON shop_config_snapshots (shop_connection_id, captured_at DESC);

-- Until the browser extension exists and S-4 (logo upload scene) is
-- verified, the shop logo is a manual task.
UPDATE capability_definitions
   SET default_mode = 'MANUAL', doc_reference = 'v2.media_space.upload_image; manual until S-4 is verified'
 WHERE key = 'shop.avatar.upload';

ALTER TABLE workflow_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY workflow_runs_tenant ON workflow_runs
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE workflow_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_steps FORCE ROW LEVEL SECURITY;
CREATE POLICY workflow_steps_tenant ON workflow_steps
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE workflow_step_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_step_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY workflow_step_attempts_tenant ON workflow_step_attempts
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE workflow_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_approvals FORCE ROW LEVEL SECURITY;
CREATE POLICY workflow_approvals_tenant ON workflow_approvals
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE workflow_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_events FORCE ROW LEVEL SECURITY;
CREATE POLICY workflow_events_tenant ON workflow_events
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE shop_config_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_config_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_config_snapshots_tenant ON shop_config_snapshots
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

GRANT SELECT, INSERT, UPDATE ON workflow_runs, workflow_steps, workflow_approvals TO app_rw;
GRANT SELECT, INSERT ON workflow_step_attempts, workflow_events, shop_config_snapshots TO app_rw;

-- System role: the sweeper finds runnable steps across tenants. Read-only,
-- no step input/output.
GRANT SELECT (id, org_id, run_id, position, status, blocking, attempt_count, next_attempt_at, lease_expires_at)
  ON workflow_steps TO app_system;
GRANT SELECT (id, status, cancel_requested_at) ON workflow_runs TO app_system;

-- +goose Down
UPDATE capability_definitions SET default_mode = 'API', doc_reference = 'v2.media_space.upload_image; S-4 open'
 WHERE key = 'shop.avatar.upload';
DROP TABLE IF EXISTS shop_config_snapshots;
DROP TABLE IF EXISTS workflow_events CASCADE;
DROP FUNCTION IF EXISTS app_workflow_events_immutable();
DROP TABLE IF EXISTS workflow_approvals;
DROP TABLE IF EXISTS workflow_step_attempts;
DROP TABLE IF EXISTS workflow_steps;
DROP TABLE IF EXISTS workflow_runs;
