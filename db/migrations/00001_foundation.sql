-- Phase 1 foundation: identity, organizations, membership, RBAC, audit.
-- Runtime roles (app_rw, app_system) must exist; they are created outside
-- migrations so that their passwords never live in SQL files.

-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_rw') THEN
    CREATE ROLE app_rw NOLOGIN NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_system') THEN
    CREATE ROLE app_system NOLOGIN BYPASSRLS;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE EXTENSION IF NOT EXISTS citext;

-- Tenant and user context helpers. Both return NULL when unset, which makes
-- every RLS comparison false (fail closed).
CREATE FUNCTION app_current_org_id() RETURNS uuid
  LANGUAGE sql STABLE
  AS $$ SELECT NULLIF(current_setting('app.org_id', true), '')::uuid $$;

CREATE FUNCTION app_current_user_id() RETURNS uuid
  LANGUAGE sql STABLE
  AS $$ SELECT NULLIF(current_setting('app.user_id', true), '')::uuid $$;

-- +goose StatementBegin
CREATE FUNCTION app_set_updated_at() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- Identity (global, not tenant-owned)
------------------------------------------------------------------------------

CREATE TABLE users (
  id                uuid PRIMARY KEY,
  email             citext NOT NULL,
  password_hash     text NOT NULL,
  display_name      text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 100),
  is_platform_admin boolean NOT NULL DEFAULT false,
  email_verified_at timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  deleted_at        timestamptz,
  CONSTRAINT users_email_len CHECK (char_length(email) <= 254)
);
CREATE UNIQUE INDEX users_email_uk ON users (email) WHERE deleted_at IS NULL;
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE user_sessions (
  id                  uuid PRIMARY KEY,
  user_id             uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash          bytea NOT NULL UNIQUE,
  csrf_secret         bytea NOT NULL,
  ip                  inet,
  user_agent          text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  last_seen_at        timestamptz NOT NULL DEFAULT now(),
  idle_expires_at     timestamptz NOT NULL,
  absolute_expires_at timestamptz NOT NULL,
  revoked_at          timestamptz
);
CREATE INDEX user_sessions_user_idx ON user_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE user_tokens (
  id          uuid PRIMARY KEY,
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose     text NOT NULL CHECK (purpose IN ('verify_email', 'password_reset')),
  token_hash  bytea NOT NULL UNIQUE,
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_tokens_user_idx ON user_tokens (user_id, purpose);

------------------------------------------------------------------------------
-- RBAC catalog (global, seeded)
------------------------------------------------------------------------------

CREATE TABLE roles (
  key  text PRIMARY KEY,
  rank int NOT NULL UNIQUE
);

CREATE TABLE permissions (
  key         text PRIMARY KEY,
  description text NOT NULL
);

CREATE TABLE role_permissions (
  role_key       text NOT NULL REFERENCES roles (key),
  permission_key text NOT NULL REFERENCES permissions (key),
  PRIMARY KEY (role_key, permission_key)
);

INSERT INTO roles (key, rank) VALUES
  ('viewer', 10), ('operator', 20), ('admin', 30), ('owner', 40);

INSERT INTO permissions (key, description) VALUES
  ('org.read',          'View the organization'),
  ('org.update',        'Edit organization settings'),
  ('org.delete',        'Delete the organization'),
  ('org.transfer',      'Transfer ownership'),
  ('members.read',      'View members'),
  ('members.manage',    'Invite, remove and change roles of members'),
  ('shops.read',        'View shops'),
  ('shops.connect',     'Connect and reconnect Shopee shops'),
  ('shops.disconnect',  'Disconnect Shopee shops'),
  ('shops.sync',        'Trigger shop synchronization'),
  ('templates.read',    'View templates'),
  ('templates.write',   'Create and edit template drafts'),
  ('templates.publish', 'Publish and delete templates'),
  ('products.read',     'View products and imports'),
  ('products.write',    'Create and edit products'),
  ('imports.write',     'Upload and retry product imports'),
  ('workflows.read',    'View workflows'),
  ('workflows.run',     'Start and resume workflows'),
  ('workflows.cancel',  'Cancel workflows'),
  ('workflows.retry',   'Retry failed workflows'),
  ('approvals.decide',  'Approve or reject workflow changes'),
  ('audit.read',        'View audit logs');

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'owner', key FROM permissions;

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'admin', key FROM permissions WHERE key NOT IN ('org.delete', 'org.transfer');

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'operator', key FROM permissions WHERE key IN (
  'org.read', 'members.read', 'shops.read', 'shops.sync',
  'templates.read', 'templates.write', 'products.read', 'products.write', 'imports.write',
  'workflows.read', 'workflows.run', 'workflows.cancel', 'workflows.retry', 'approvals.decide');

INSERT INTO role_permissions (role_key, permission_key)
SELECT 'viewer', key FROM permissions WHERE key IN (
  'org.read', 'members.read', 'shops.read', 'templates.read', 'products.read', 'workflows.read');

------------------------------------------------------------------------------
-- Organizations and membership (tenant-owned)
------------------------------------------------------------------------------

CREATE TABLE organizations (
  id         uuid PRIMARY KEY,
  name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  slug       citext NOT NULL CHECK (slug ~ '^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$'),
  plan       text NOT NULL DEFAULT 'free',
  settings   jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_by uuid NOT NULL REFERENCES users (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX organizations_slug_uk ON organizations (slug);
CREATE TRIGGER organizations_updated_at BEFORE UPDATE ON organizations
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE organization_members (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role_key   text NOT NULL REFERENCES roles (key),
  invited_by uuid REFERENCES users (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organization_members_org_user_uk UNIQUE (org_id, user_id),
  CONSTRAINT organization_members_org_id_uk UNIQUE (org_id, id)
);
-- Exactly one owner per organization (at most one enforced here; "at least one"
-- is enforced by the service and by the transfer procedure).
CREATE UNIQUE INDEX organization_members_one_owner ON organization_members (org_id)
  WHERE role_key = 'owner';
CREATE INDEX organization_members_user_idx ON organization_members (user_id);
CREATE TRIGGER organization_members_updated_at BEFORE UPDATE ON organization_members
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE organization_invitations (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  email       citext NOT NULL,
  role_key    text NOT NULL REFERENCES roles (key) CHECK (role_key <> 'owner'),
  token_hash  bytea NOT NULL UNIQUE,
  invited_by  uuid NOT NULL REFERENCES users (id),
  expires_at  timestamptz NOT NULL,
  accepted_at timestamptz,
  accepted_by uuid REFERENCES users (id),
  revoked_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT organization_invitations_org_id_uk UNIQUE (org_id, id)
);
CREATE UNIQUE INDEX organization_invitations_open_uk ON organization_invitations (org_id, email)
  WHERE accepted_at IS NULL AND revoked_at IS NULL;

------------------------------------------------------------------------------
-- Audit log (append-only, partitioned by month)
------------------------------------------------------------------------------

CREATE TABLE audit_logs (
  id            uuid NOT NULL,
  org_id        uuid, -- no FK: audit history outlives organizations
  actor_user_id uuid,
  actor_type    text NOT NULL CHECK (actor_type IN ('user', 'system', 'shopee')),
  action        text NOT NULL,
  target_type   text,
  target_id     uuid,
  outcome       text NOT NULL CHECK (outcome IN ('success', 'failure', 'denied')),
  metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
  ip            inet,
  user_agent    text,
  request_id    text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE INDEX audit_logs_org_created_idx ON audit_logs (org_id, created_at DESC);
CREATE INDEX audit_logs_created_brin ON audit_logs USING brin (created_at);

-- Creates the monthly partition containing `month` if it does not exist.
-- Called here for the next 15 months and later by a scheduled job.
-- +goose StatementBegin
CREATE FUNCTION app_ensure_monthly_partition(parent regclass, month date) RETURNS void
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = public
  AS $$
DECLARE
  start_date date := date_trunc('month', month)::date;
  end_date   date := (date_trunc('month', month) + interval '1 month')::date;
  part_name  text := format('%s_%s', parent::text, to_char(start_date, 'YYYYMM'));
BEGIN
  IF to_regclass(part_name) IS NULL THEN
    EXECUTE format('CREATE TABLE %I PARTITION OF %s FOR VALUES FROM (%L) TO (%L)',
                   part_name, parent::text, start_date, end_date);
  END IF;
END
$$;
-- +goose StatementEnd

SELECT app_ensure_monthly_partition('audit_logs', (date_trunc('month', now()) + make_interval(months => m))::date)
FROM generate_series(-1, 14) AS m;

CREATE TABLE audit_logs_default PARTITION OF audit_logs DEFAULT;

-- Append-only: no updates or deletes from the application.
-- +goose StatementBegin
CREATE FUNCTION app_audit_logs_immutable() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
BEGIN
  RAISE EXCEPTION 'audit_logs is append-only';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_logs_no_update BEFORE UPDATE OR DELETE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION app_audit_logs_immutable();

------------------------------------------------------------------------------
-- Row-level security
------------------------------------------------------------------------------

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
-- Visible when it is the current tenant, or when the current user is a member
-- (needed to list "my organizations" before a tenant is chosen).
CREATE POLICY organizations_select ON organizations FOR SELECT
  USING (id = app_current_org_id()
         OR EXISTS (SELECT 1 FROM organization_members m
                    WHERE m.org_id = organizations.id AND m.user_id = app_current_user_id()));
CREATE POLICY organizations_insert ON organizations FOR INSERT
  WITH CHECK (id = app_current_org_id());
CREATE POLICY organizations_update ON organizations FOR UPDATE
  USING (id = app_current_org_id()) WITH CHECK (id = app_current_org_id());
CREATE POLICY organizations_delete ON organizations FOR DELETE
  USING (id = app_current_org_id());

ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_members FORCE ROW LEVEL SECURITY;
CREATE POLICY organization_members_select ON organization_members FOR SELECT
  USING (org_id = app_current_org_id() OR user_id = app_current_user_id());
CREATE POLICY organization_members_write ON organization_members FOR ALL
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE organization_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY organization_invitations_tenant ON organization_invitations
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_logs_select ON audit_logs FOR SELECT
  USING (org_id = app_current_org_id());
-- Rows without an org (e.g. login events) may only be written when no tenant is set.
CREATE POLICY audit_logs_insert ON audit_logs FOR INSERT
  WITH CHECK (org_id IS NOT DISTINCT FROM app_current_org_id());

------------------------------------------------------------------------------
-- Grants
------------------------------------------------------------------------------

GRANT USAGE ON SCHEMA public TO app_rw, app_system;

GRANT SELECT, INSERT, UPDATE ON users, user_sessions, user_tokens TO app_rw;
GRANT SELECT ON roles, permissions, role_permissions TO app_rw;
GRANT SELECT, INSERT, UPDATE ON organizations TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON organization_members, organization_invitations TO app_rw;
GRANT SELECT, INSERT ON audit_logs TO app_rw;
GRANT EXECUTE ON FUNCTION app_current_org_id(), app_current_user_id() TO app_rw, app_system;

-- System role: narrow, read-mostly. Extended per phase as needed.
GRANT SELECT ON users, organizations, organization_members TO app_system;
GRANT SELECT, INSERT ON audit_logs TO app_system;
REVOKE EXECUTE ON FUNCTION app_ensure_monthly_partition(regclass, date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION app_ensure_monthly_partition(regclass, date) TO app_system;

-- +goose Down
DROP TABLE IF EXISTS audit_logs CASCADE;
DROP FUNCTION IF EXISTS app_audit_logs_immutable();
DROP FUNCTION IF EXISTS app_ensure_monthly_partition(regclass, date);
-- organizations' RLS policy references organization_members, so drop them together.
DROP TABLE IF EXISTS organization_invitations, organization_members, organizations CASCADE;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS user_tokens;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS app_set_updated_at();
DROP FUNCTION IF EXISTS app_current_user_id();
DROP FUNCTION IF EXISTS app_current_org_id();
-- Roles and the citext extension are intentionally left in place.
