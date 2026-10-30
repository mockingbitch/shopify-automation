-- Phase 2: Shopee partner apps, shop connections, encrypted credentials,
-- OAuth state, capability registry and webhook events.

-- +goose Up

------------------------------------------------------------------------------
-- Partner apps (platform-level, managed by platform admins)
------------------------------------------------------------------------------

CREATE TABLE shopee_apps (
  id                uuid PRIMARY KEY,
  name              text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  environment       text NOT NULL CHECK (environment IN ('sandbox', 'production')),
  api_host          text NOT NULL CHECK (api_host ~ '^https?://'),
  auth_host         text NOT NULL CHECK (auth_host ~ '^https?://'),
  partner_id        bigint NOT NULL CHECK (partner_id > 0),
  partner_key_enc   bytea NOT NULL,
  dek_wrapped       bytea NOT NULL,
  kek_id            text NOT NULL,
  app_type          text NOT NULL,
  redirect_uri      text NOT NULL CHECK (redirect_uri ~ '^https?://'),
  push_callback_url text CHECK (push_callback_url ~ '^https?://'),
  enabled           boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shopee_apps_env_partner_uk UNIQUE (environment, partner_id)
);
CREATE TRIGGER shopee_apps_updated_at BEFORE UPDATE ON shopee_apps
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

------------------------------------------------------------------------------
-- Shop connections (tenant-owned)
------------------------------------------------------------------------------

CREATE TABLE shop_connections (
  id                       uuid PRIMARY KEY,
  org_id                   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  shopee_app_id            uuid NOT NULL REFERENCES shopee_apps (id),
  shopee_shop_id           bigint NOT NULL CHECK (shopee_shop_id > 0),
  region                   text,
  shop_name                text,
  status                   text NOT NULL CHECK (status IN
                             ('PENDING', 'ACTIVE', 'RECONNECT_REQUIRED', 'REVOKED', 'EXPIRED', 'DISCONNECTED')),
  authorization_expires_at timestamptz,
  last_synced_at           timestamptz,
  last_sync_error          text,
  connected_by             uuid REFERENCES users (id),
  connected_at             timestamptz NOT NULL DEFAULT now(),
  disconnected_at          timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_connections_org_id_uk UNIQUE (org_id, id),
  -- Reconnecting in the same org reuses the row and keeps its history.
  CONSTRAINT shop_connections_org_shop_uk UNIQUE (org_id, shopee_app_id, shopee_shop_id)
);
-- A shop belongs to at most one organization while it is connected. Unique
-- indexes are not filtered by RLS, so this holds across tenants.
CREATE UNIQUE INDEX shop_connections_live_shop_uk ON shop_connections (shopee_app_id, shopee_shop_id)
  WHERE status NOT IN ('DISCONNECTED', 'REVOKED');
CREATE INDEX shop_connections_org_status_idx ON shop_connections (org_id, status);
CREATE TRIGGER shop_connections_updated_at BEFORE UPDATE ON shop_connections
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE shop_credentials (
  shop_connection_id   uuid PRIMARY KEY,
  org_id               uuid NOT NULL,
  access_token_enc     bytea NOT NULL,
  refresh_token_enc    bytea NOT NULL,
  dek_wrapped          bytea NOT NULL,
  kek_id               text NOT NULL,
  access_expires_at    timestamptz NOT NULL,
  refresh_expires_at   timestamptz NOT NULL,
  version              bigint NOT NULL DEFAULT 1,
  consecutive_failures int NOT NULL DEFAULT 0,
  last_error_code      text,
  last_refreshed_at    timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_credentials_connection_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);
CREATE INDEX shop_credentials_access_expiry_idx ON shop_credentials (access_expires_at);
CREATE TRIGGER shop_credentials_updated_at BEFORE UPDATE ON shop_credentials
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE shop_authorizations (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL,
  shop_connection_id uuid NOT NULL,
  event              text NOT NULL CHECK (event IN
                       ('GRANTED', 'REAUTHORIZED', 'REFRESHED', 'REFRESH_FAILED', 'EXPIRING', 'EXPIRED', 'REVOKED', 'DISCONNECTED')),
  source             text NOT NULL CHECK (source IN ('callback', 'push', 'sync', 'refresh', 'user')),
  auth_time          timestamptz,
  expire_time        timestamptz,
  detail             jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_authorizations_connection_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);
CREATE INDEX shop_authorizations_connection_idx ON shop_authorizations (shop_connection_id, created_at DESC);

CREATE TABLE shop_profiles (
  shop_connection_id uuid PRIMARY KEY,
  org_id             uuid NOT NULL,
  shop_name          text,
  region             text,
  status             text,
  shop_logo_url      text,
  description        text,
  is_cb              boolean,
  is_sip             boolean,
  merchant_id        bigint,
  raw                jsonb NOT NULL DEFAULT '{}'::jsonb,
  fetched_at         timestamptz NOT NULL,
  CONSTRAINT shop_profiles_connection_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);

-- Pending OAuth flows. The state token embeds the org ID so the callback can
-- set the tenant scope before looking the row up.
CREATE TABLE oauth_states (
  id                  uuid PRIMARY KEY,
  org_id              uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id             uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  shopee_app_id       uuid NOT NULL REFERENCES shopee_apps (id),
  token_hash          bytea NOT NULL UNIQUE,
  shop_connection_id  uuid, -- set for reconnect
  expires_at          timestamptz NOT NULL,
  consumed_at         timestamptz,
  -- Main-account flow: tokens wait here (encrypted) until the user picks shops.
  main_account_id     bigint,
  pending_shop_ids    bigint[],
  pending_tokens_enc  bytea,
  pending_dek_wrapped bytea,
  pending_kek_id      text,
  pending_expires_at  timestamptz,
  selected_at         timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT oauth_states_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT oauth_states_connection_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);

------------------------------------------------------------------------------
-- Capability registry (global, seeded; see docs/shopee-integration.md §7)
------------------------------------------------------------------------------

CREATE TABLE capability_definitions (
  key                text PRIMARY KEY,
  title              text NOT NULL,
  default_mode       text NOT NULL CHECK (default_mode IN ('API', 'BROWSER', 'MANUAL', 'UNSUPPORTED')),
  fallback_mode      text CHECK (fallback_mode IN ('BROWSER', 'MANUAL')),
  apis               text[] NOT NULL DEFAULT '{}',
  required_app_types text[], -- NULL: any app type
  supported_regions  text[], -- NULL: every region
  requires_approval  boolean NOT NULL,
  sensitive          boolean NOT NULL DEFAULT false,
  enabled            boolean NOT NULL DEFAULT true,
  verification       text NOT NULL DEFAULT 'DOC_VERIFIED' CHECK (verification IN ('DOC_VERIFIED', 'SANDBOX_VERIFIED')),
  doc_reference      text NOT NULL,
  last_verified_at   date NOT NULL,
  updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE capability_overrides (
  id             uuid PRIMARY KEY,
  capability_key text NOT NULL REFERENCES capability_definitions (key) ON DELETE CASCADE,
  shopee_app_id  uuid REFERENCES shopee_apps (id) ON DELETE CASCADE,
  region         text,
  mode           text CHECK (mode IN ('API', 'BROWSER', 'MANUAL', 'UNSUPPORTED')),
  enabled        boolean,
  reason         text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT capability_overrides_scope_uk UNIQUE NULLS NOT DISTINCT (capability_key, shopee_app_id, region)
);

-- App types listed in the docs metadata of every endpoint below include
-- "ERP System" (assumption A-1); NULL keeps the check open until S-2.
INSERT INTO capability_definitions
  (key, title, default_mode, fallback_mode, apis, requires_approval, sensitive, doc_reference, last_verified_at) VALUES
  ('shop.info.read', 'Read shop information', 'API', NULL, '{GET /api/v2/shop/get_shop_info}', false, false, 'v2.shop.get_shop_info', '2026-10-30'),
  ('shop.profile.read', 'Read shop profile', 'API', NULL, '{GET /api/v2/shop/get_profile}', false, false, 'v2.shop.get_profile', '2026-10-30'),
  ('shop.profile.update', 'Update shop name and description', 'API', NULL, '{POST /api/v2/shop/update_profile}', true, true, 'v2.shop.update_profile', '2026-10-30'),
  ('shop.avatar.upload', 'Upload shop logo', 'API', NULL, '{POST /api/v2/media_space/upload_image,POST /api/v2/shop/update_profile}', true, false, 'v2.media_space.upload_image; S-4 open', '2026-10-30'),
  ('shop.banner.upload', 'Upload shop banner', 'BROWSER', 'MANUAL', '{}', true, false, 'No Open API found in the Shop module', '2026-10-30'),
  ('shop.decoration.update', 'Update shop decoration', 'BROWSER', 'MANUAL', '{}', true, false, 'No Open API found', '2026-10-30'),
  ('shop.warehouse.read', 'Read warehouses', 'API', NULL, '{GET /api/v2/shop/get_warehouse_detail}', false, false, 'v2.shop.get_warehouse_detail', '2026-10-30'),
  ('shop.warehouse.update', 'Update warehouses', 'BROWSER', 'MANUAL', '{}', true, false, 'No Open API found', '2026-10-30'),
  ('shop.address.read', 'Read pickup and return addresses', 'API', NULL, '{GET /api/v2/logistics/get_address_list}', false, false, 'v2.logistics.get_address_list', '2026-10-30'),
  ('shop.address.update', 'Edit an existing address', 'API', NULL, '{POST /api/v2/logistics/update_address}', true, false, 'v2.logistics.update_address', '2026-10-30'),
  ('shop.address.create', 'Create an address', 'BROWSER', 'MANUAL', '{}', true, false, 'No Open API for address creation', '2026-10-30'),
  ('shop.address.config', 'Set default, pickup and return address', 'API', NULL, '{POST /api/v2/logistics/set_address_config}', true, false, 'v2.logistics.set_address_config', '2026-10-30'),
  ('shop.logistics.read', 'Read shipping channels', 'API', NULL, '{GET /api/v2/logistics/get_channel_list}', false, false, 'v2.logistics.get_channel_list', '2026-10-30'),
  ('shop.logistics.update', 'Enable or disable shipping channels and COD', 'API', NULL, '{POST /api/v2/logistics/update_channel}', true, true, 'v2.logistics.update_channel', '2026-10-30'),
  ('product.category.read', 'Read product categories', 'API', NULL, '{GET /api/v2/product/get_category,GET /api/v2/product/category_recommend}', false, false, 'v2.product.get_category', '2026-10-30'),
  ('product.attribute.read', 'Read category attributes and brands', 'API', NULL, '{GET /api/v2/product/get_attribute_tree,GET /api/v2/product/get_brand_list}', false, false, 'v2.product.get_attribute_tree', '2026-10-30'),
  ('product.limits.read', 'Read product limits', 'API', NULL, '{GET /api/v2/product/get_item_limit}', false, false, 'v2.product.get_item_limit', '2026-10-30'),
  ('product.image.upload', 'Upload product images', 'API', NULL, '{POST /api/v2/media_space/upload_image}', false, false, 'v2.media_space.upload_image', '2026-10-30'),
  ('product.create', 'Create products', 'API', NULL, '{POST /api/v2/product/add_item}', false, false, 'v2.product.add_item', '2026-10-30'),
  ('product.variation.create', 'Create product variations', 'API', NULL, '{POST /api/v2/product/init_tier_variation,POST /api/v2/product/add_model}', false, false, 'v2.product.init_tier_variation', '2026-10-30'),
  ('product.update', 'Update products', 'API', NULL, '{POST /api/v2/product/update_item,POST /api/v2/product/update_tier_variation}', true, false, 'v2.product.update_item', '2026-10-30'),
  ('product.price.update', 'Update prices', 'API', NULL, '{POST /api/v2/product/update_price}', true, false, 'v2.product.update_price', '2026-10-30'),
  ('product.stock.update', 'Update stock', 'API', NULL, '{POST /api/v2/product/update_stock}', true, false, 'v2.product.update_stock', '2026-10-30'),
  ('product.read', 'Read products', 'API', NULL, '{GET /api/v2/product/get_item_list,GET /api/v2/product/get_item_base_info,GET /api/v2/product/get_model_list}', false, false, 'v2.product.get_item_list', '2026-10-30'),
  ('product.batch_create', 'Create products in batches', 'UNSUPPORTED', NULL, '{POST /api/v2/product/batch_add_item}', false, false, 'Exists; evaluated in Phase 5', '2026-10-30');

------------------------------------------------------------------------------
-- Webhook events (platform-level inbox; org resolved during processing)
------------------------------------------------------------------------------

CREATE TABLE webhook_events (
  id              uuid PRIMARY KEY,
  shopee_app_id   uuid NOT NULL REFERENCES shopee_apps (id),
  push_code       int NOT NULL,
  shopee_shop_ids bigint[] NOT NULL DEFAULT '{}',
  body_sha256     bytea NOT NULL,
  body            jsonb NOT NULL,
  status          text NOT NULL DEFAULT 'RECEIVED' CHECK (status IN ('RECEIVED', 'PROCESSED', 'IGNORED', 'FAILED')),
  attempts        int NOT NULL DEFAULT 0,
  last_error      text,
  received_at     timestamptz NOT NULL DEFAULT now(),
  processed_at    timestamptz,
  CONSTRAINT webhook_events_dedupe_uk UNIQUE (shopee_app_id, body_sha256)
);
CREATE INDEX webhook_events_pending_idx ON webhook_events (received_at) WHERE status = 'RECEIVED';

------------------------------------------------------------------------------
-- Row-level security
------------------------------------------------------------------------------

ALTER TABLE shop_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_connections FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_connections_tenant ON shop_connections
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_credentials_tenant ON shop_credentials
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_authorizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_authorizations FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_authorizations_tenant ON shop_authorizations
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_profiles_tenant ON shop_profiles
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE oauth_states ENABLE ROW LEVEL SECURITY;
ALTER TABLE oauth_states FORCE ROW LEVEL SECURITY;
CREATE POLICY oauth_states_tenant ON oauth_states
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

------------------------------------------------------------------------------
-- Grants
------------------------------------------------------------------------------

GRANT SELECT, INSERT, UPDATE ON shopee_apps TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON shop_connections, shop_credentials, shop_profiles, oauth_states TO app_rw;
GRANT SELECT, INSERT ON shop_authorizations TO app_rw;
GRANT SELECT ON capability_definitions, capability_overrides TO app_rw;
GRANT SELECT, INSERT, UPDATE ON webhook_events TO app_rw;

-- System role: cross-tenant lookups only (which org owns a Shopee shop, which
-- credentials are due for refresh). No secrets and no writes.
GRANT SELECT (id, org_id, shopee_app_id, shopee_shop_id, status) ON shop_connections TO app_system;
GRANT SELECT (shop_connection_id, org_id, access_expires_at, refresh_expires_at, consecutive_failures) ON shop_credentials TO app_system;
GRANT SELECT (id, enabled) ON shopee_apps TO app_system;

-- +goose Down
DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS capability_overrides;
DROP TABLE IF EXISTS capability_definitions;
DROP TABLE IF EXISTS oauth_states;
DROP TABLE IF EXISTS shop_profiles;
DROP TABLE IF EXISTS shop_authorizations;
DROP TABLE IF EXISTS shop_credentials;
DROP TABLE IF EXISTS shop_connections;
DROP TABLE IF EXISTS shopee_apps;
