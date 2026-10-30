-- Phase 6: browser extension devices, pairing, signed recipes and browser tasks.

-- +goose Up

-- One-time codes shown in the dashboard and typed into the extension.
CREATE TABLE extension_pairing_codes (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash   bytea NOT NULL UNIQUE,
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT extension_pairing_codes_member_fk FOREIGN KEY (org_id, user_id) REFERENCES organization_members (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX extension_pairing_codes_expiry_idx ON extension_pairing_codes (expires_at);

-- A paired browser. The token is stored as a SHA-256 hash only.
CREATE TABLE extension_devices (
  id                uuid PRIMARY KEY,
  org_id            uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name              text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  extension_version text NOT NULL DEFAULT '' CHECK (char_length(extension_version) <= 40),
  token_hash        bytea NOT NULL UNIQUE,
  expires_at        timestamptz NOT NULL,
  last_used_at      timestamptz,
  revoked_at        timestamptz,
  revoked_by        uuid REFERENCES users (id),
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT extension_devices_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT extension_devices_member_fk FOREIGN KEY (org_id, user_id) REFERENCES organization_members (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX extension_devices_org_idx ON extension_devices (org_id, created_at DESC);

-- Recipes are platform-wide (written by platform admins). The exact JSON
-- text is kept because it is what gets signed.
CREATE TABLE browser_recipes (
  id                   uuid PRIMARY KEY,
  capability_key       text NOT NULL REFERENCES capability_definitions (key),
  market               text NOT NULL CHECK (market IN ('VN', 'SG', 'MY', 'TH', 'PH', 'ID', 'TW')),
  version              int NOT NULL CHECK (version > 0),
  recipe_json          text NOT NULL CHECK (octet_length(recipe_json) <= 65536),
  enabled              boolean NOT NULL DEFAULT false,
  disabled_reason      text,
  verified_at          timestamptz,
  verified_by          uuid REFERENCES users (id),
  notes                text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
  success_count        int NOT NULL DEFAULT 0,
  failure_count        int NOT NULL DEFAULT 0,
  consecutive_failures int NOT NULL DEFAULT 0,
  created_by           uuid REFERENCES users (id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT browser_recipes_version_uk UNIQUE (capability_key, market, version),
  -- Only a recipe verified on a real seller account can be enabled.
  CONSTRAINT browser_recipes_enabled_ck CHECK (NOT enabled OR verified_at IS NOT NULL)
);
-- At most one enabled version per capability and market.
CREATE UNIQUE INDEX browser_recipes_enabled_uk ON browser_recipes (capability_key, market) WHERE enabled;
CREATE TRIGGER browser_recipes_updated_at BEFORE UPDATE ON browser_recipes FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

-- One operation for the extension, created by a workflow step.
CREATE TABLE browser_tasks (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  shop_connection_id uuid NOT NULL,
  run_id             uuid NOT NULL,
  step_id            uuid NOT NULL,
  task_key           text NOT NULL CHECK (char_length(task_key) BETWEEN 1 AND 200),
  capability_key     text NOT NULL REFERENCES capability_definitions (key),
  market             text NOT NULL,
  title              text NOT NULL,
  status             text NOT NULL DEFAULT 'QUEUED'
                       CHECK (status IN ('QUEUED', 'RUNNING', 'WAITING_USER', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
  input              jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(input) = 'object'),
  preview            jsonb NOT NULL DEFAULT '{}'::jsonb,
  recipe_id          uuid REFERENCES browser_recipes (id),
  device_id          uuid,
  lease_token        uuid,
  lease_expires_at   timestamptz,
  attempt_count      int NOT NULL DEFAULT 0,
  status_message     text,
  last_error         jsonb,
  step_log           jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  finished_at        timestamptz,
  CONSTRAINT browser_tasks_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT browser_tasks_step_key_uk UNIQUE (step_id, task_key),
  CONSTRAINT browser_tasks_shop_fk FOREIGN KEY (org_id, shop_connection_id) REFERENCES shop_connections (org_id, id) ON DELETE CASCADE,
  CONSTRAINT browser_tasks_run_fk FOREIGN KEY (org_id, run_id) REFERENCES workflow_runs (org_id, id) ON DELETE CASCADE,
  CONSTRAINT browser_tasks_step_fk FOREIGN KEY (org_id, step_id) REFERENCES workflow_steps (org_id, id) ON DELETE CASCADE,
  CONSTRAINT browser_tasks_device_fk FOREIGN KEY (org_id, device_id) REFERENCES extension_devices (org_id, id),
  CONSTRAINT browser_tasks_lease_ck CHECK ((status IN ('RUNNING', 'WAITING_USER')) = (lease_token IS NOT NULL))
);
CREATE INDEX browser_tasks_queue_idx ON browser_tasks (org_id, status, created_at);
CREATE INDEX browser_tasks_step_idx ON browser_tasks (step_id);
CREATE TRIGGER browser_tasks_updated_at BEFORE UPDATE ON browser_tasks FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE browser_task_screenshots (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL,
  task_id     uuid NOT NULL,
  storage_key text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT browser_task_screenshots_task_fk FOREIGN KEY (org_id, task_id) REFERENCES browser_tasks (org_id, id) ON DELETE CASCADE
);
CREATE INDEX browser_task_screenshots_task_idx ON browser_task_screenshots (task_id, created_at);

ALTER TABLE extension_pairing_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE extension_pairing_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY extension_pairing_codes_tenant ON extension_pairing_codes USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE extension_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE extension_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY extension_devices_tenant ON extension_devices USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE browser_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE browser_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY browser_tasks_tenant ON browser_tasks USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE browser_task_screenshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE browser_task_screenshots FORCE ROW LEVEL SECURITY;
CREATE POLICY browser_task_screenshots_tenant ON browser_task_screenshots USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

GRANT SELECT, INSERT, UPDATE ON extension_pairing_codes, extension_devices, browser_tasks TO app_rw;
GRANT SELECT, INSERT ON browser_task_screenshots TO app_rw;
GRANT SELECT, INSERT, UPDATE ON browser_recipes TO app_rw;
-- The system role finds the organization of a pairing code or device token
-- (both arrive without a session) and nothing else.
GRANT SELECT (id, org_id, code_hash, expires_at, used_at) ON extension_pairing_codes TO app_system;
GRANT SELECT (id, org_id, token_hash) ON extension_devices TO app_system;

-- +goose Down
DROP TABLE IF EXISTS browser_task_screenshots;
DROP TABLE IF EXISTS browser_tasks;
DROP TABLE IF EXISTS browser_recipes;
DROP TABLE IF EXISTS extension_devices;
DROP TABLE IF EXISTS extension_pairing_codes;
