-- Phase 7: in-app notifications, an encrypted email outbox and a cache of
-- Shopee catalog data for the product pickers.

-- +goose Up

CREATE TABLE notifications (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind IN ('approval_requested', 'run_needs_user', 'run_failed', 'shop_reconnect_required')),
  title       text NOT NULL CHECK (char_length(title) <= 200),
  body        text NOT NULL DEFAULT '' CHECK (char_length(body) <= 1000),
  link        text NOT NULL DEFAULT '' CHECK (link = '' OR link LIKE '/orgs/%'),
  dedupe_key  text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  read_at     timestamptz
);
-- One unread notification per subject and user.
CREATE UNIQUE INDEX notifications_unread_uk ON notifications (org_id, user_id, dedupe_key) WHERE read_at IS NULL;
CREATE INDEX notifications_user_idx ON notifications (org_id, user_id, created_at DESC);

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_tenant ON notifications USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

-- Emails waiting to be sent. Recipient, subject and body are sealed with
-- the envelope keyring (they can contain one-time links); the row is
-- deleted once the email is sent.
CREATE TABLE email_outbox (
  id              uuid PRIMARY KEY,
  kek_id          text NOT NULL,
  dek_wrapped     bytea NOT NULL,
  message_enc     bytea NOT NULL,
  attempts        int NOT NULL DEFAULT 0,
  last_error      text,
  failed_at       timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  next_attempt_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_outbox_due_idx ON email_outbox (next_attempt_at) WHERE failed_at IS NULL;

-- Shopee catalog responses per shop (categories, attribute trees, brands),
-- refreshed after a day.
CREATE TABLE shopee_catalog_cache (
  org_id             uuid NOT NULL,
  shop_connection_id uuid NOT NULL,
  kind               text NOT NULL CHECK (kind IN ('categories', 'attributes', 'brands')),
  cache_key          text NOT NULL,
  payload            jsonb NOT NULL,
  fetched_at         timestamptz NOT NULL,
  PRIMARY KEY (shop_connection_id, kind, cache_key),
  CONSTRAINT shopee_catalog_cache_shop_fk FOREIGN KEY (org_id, shop_connection_id) REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);

ALTER TABLE shopee_catalog_cache ENABLE ROW LEVEL SECURITY;
ALTER TABLE shopee_catalog_cache FORCE ROW LEVEL SECURITY;
CREATE POLICY shopee_catalog_cache_tenant ON shopee_catalog_cache USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

GRANT SELECT, INSERT, UPDATE ON notifications TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON email_outbox TO app_rw;
GRANT SELECT, INSERT, UPDATE ON shopee_catalog_cache TO app_rw;
-- Platform admins manage capability overrides (kill switches) from the dashboard.
GRANT INSERT, UPDATE, DELETE ON capability_overrides TO app_rw;
-- The sweeper finds emails that were never handed to the queue.
GRANT SELECT (id, attempts, failed_at, next_attempt_at) ON email_outbox TO app_system;

-- +goose Down
REVOKE INSERT, UPDATE, DELETE ON capability_overrides FROM app_rw;
DROP TABLE IF EXISTS shopee_catalog_cache;
DROP TABLE IF EXISTS email_outbox;
DROP TABLE IF EXISTS notifications;
