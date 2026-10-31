-- Phase 8: two-step verification (TOTP + recovery codes) and the purge of
-- deleted organizations.

-- +goose Up

-- A user's TOTP secret, sealed with the envelope keyring.
CREATE TABLE user_mfa (
  user_id        uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  kek_id         text NOT NULL,
  dek_wrapped    bytea NOT NULL,
  secret_enc     bytea NOT NULL,
  confirmed_at   timestamptz,
  -- Last accepted 30-second step: a code is accepted once.
  last_used_step bigint NOT NULL DEFAULT 0,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER user_mfa_updated_at BEFORE UPDATE ON user_mfa FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

-- One-time recovery codes, stored as SHA-256 hashes.
CREATE TABLE user_recovery_codes (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash  bytea NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_recovery_codes_uk UNIQUE (user_id, code_hash)
);

-- The second login step: issued after a correct password, exchanged for a
-- session with a valid code. At most 5 attempts.
CREATE TABLE mfa_challenges (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash  bytea NOT NULL UNIQUE,
  expires_at  timestamptz NOT NULL,
  attempts    int NOT NULL DEFAULT 0,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mfa_challenges_user_idx ON mfa_challenges (user_id, created_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON user_mfa, user_recovery_codes TO app_rw;
GRANT SELECT, INSERT, UPDATE ON mfa_challenges TO app_rw;

-- The worker purges organizations deleted longer ago than the retention
-- period. Deleting the row cascades to every tenant table.
GRANT DELETE ON organizations TO app_system;
-- workflow_events is partitioned and has no foreign key: purged explicitly.
-- (DELETE ... WHERE org_id needs SELECT on that column.)
GRANT DELETE ON workflow_events TO app_system;
GRANT SELECT (org_id) ON workflow_events TO app_system;

-- KEK rotation (admin rewrap) runs as the system role: it reads and
-- rewrites only the wrapped data keys, never the encrypted columns.
GRANT SELECT (id, kek_id, dek_wrapped), UPDATE (kek_id, dek_wrapped) ON shopee_apps TO app_system;
GRANT SELECT (shop_connection_id, kek_id, dek_wrapped), UPDATE (kek_id, dek_wrapped) ON shop_credentials TO app_system;
GRANT SELECT (id, pending_kek_id, pending_dek_wrapped), UPDATE (pending_kek_id, pending_dek_wrapped) ON oauth_states TO app_system;
GRANT SELECT (id, kek_id, dek_wrapped), UPDATE (kek_id, dek_wrapped) ON email_outbox TO app_system;
GRANT SELECT (user_id, kek_id, dek_wrapped), UPDATE (kek_id, dek_wrapped) ON user_mfa TO app_system;

-- Events stay append-only for the application; only the system role may
-- delete them (the purge job). Updates remain impossible.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_workflow_events_immutable() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_user = 'app_system' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'workflow_events is append-only';
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_workflow_events_immutable() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
BEGIN
  RAISE EXCEPTION 'workflow_events is append-only';
END
$$;
-- +goose StatementEnd
REVOKE UPDATE (kek_id, dek_wrapped) ON shopee_apps, shop_credentials, email_outbox FROM app_system;
REVOKE UPDATE (pending_kek_id, pending_dek_wrapped) ON oauth_states FROM app_system;
REVOKE SELECT (kek_id, dek_wrapped) ON shopee_apps, shop_credentials FROM app_system;
REVOKE SELECT (pending_kek_id, pending_dek_wrapped) ON oauth_states FROM app_system;
REVOKE DELETE ON organizations FROM app_system;
REVOKE DELETE ON workflow_events FROM app_system;
REVOKE SELECT (org_id) ON workflow_events FROM app_system;
DROP TABLE IF EXISTS mfa_challenges;
DROP TABLE IF EXISTS user_recovery_codes;
DROP TABLE IF EXISTS user_mfa;
