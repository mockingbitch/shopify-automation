-- Phase 5: products, variations, images, per-shop mappings and imports.

-- +goose Up

CREATE TABLE products (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  sku                citext NOT NULL CHECK (char_length(sku) BETWEEN 1 AND 100),
  name               text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
  description        text NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
  shopee_category_id bigint CHECK (shopee_category_id > 0),
  category_label     text,
  brand_name         text,
  condition          text CHECK (condition IN ('NEW', 'USED')),
  attributes         jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(attributes) = 'array'),
  price              numeric(14, 2) NOT NULL CHECK (price > 0),
  currency           char(3) NOT NULL,
  stock              int NOT NULL DEFAULT 0 CHECK (stock >= 0),
  weight_kg          numeric(10, 3) CHECK (weight_kg > 0),
  package_cm         jsonb,
  days_to_ship       int CHECK (days_to_ship > 0),
  tiers              jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tiers) = 'array' AND jsonb_array_length(tiers) <= 2),
  source             text NOT NULL DEFAULT 'MANUAL' CHECK (source IN ('MANUAL', 'IMPORT')),
  import_batch_id    uuid,
  created_by         uuid REFERENCES users (id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  deleted_at         timestamptz,
  CONSTRAINT products_org_id_uk UNIQUE (org_id, id)
);
CREATE UNIQUE INDEX products_sku_uk ON products (org_id, sku) WHERE deleted_at IS NULL;
CREATE INDEX products_org_created_idx ON products (org_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE TRIGGER products_updated_at BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE product_variations (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL,
  product_id uuid NOT NULL,
  tier_index int[] NOT NULL CHECK (array_length(tier_index, 1) BETWEEN 1 AND 2),
  sku        citext CHECK (char_length(sku) BETWEEN 1 AND 100),
  price      numeric(14, 2) NOT NULL CHECK (price > 0),
  stock      int NOT NULL DEFAULT 0 CHECK (stock >= 0),
  CONSTRAINT product_variations_product_fk FOREIGN KEY (org_id, product_id) REFERENCES products (org_id, id) ON DELETE CASCADE,
  CONSTRAINT product_variations_index_uk UNIQUE (product_id, tier_index)
);
CREATE UNIQUE INDEX product_variations_sku_uk ON product_variations (product_id, sku) WHERE sku IS NOT NULL;

CREATE TABLE product_images (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL,
  product_id   uuid NOT NULL,
  position     int NOT NULL CHECK (position >= 0),
  storage_key  text NOT NULL,
  content_type text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
  sha256       bytea NOT NULL,
  width        int NOT NULL,
  height       int NOT NULL,
  source_url   text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT product_images_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT product_images_product_fk FOREIGN KEY (org_id, product_id) REFERENCES products (org_id, id) ON DELETE CASCADE
);
CREATE INDEX product_images_product_idx ON product_images (product_id, position);

-- Per shop: which Shopee item a product became (FR-PRD-8) and where the
-- add_item protocol stands (workflow-design.md §5).
CREATE TABLE product_mappings (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL,
  shop_connection_id uuid NOT NULL,
  product_id         uuid NOT NULL,
  publish_state      text NOT NULL CHECK (publish_state IN ('PLANNED', 'CREATING', 'CREATED', 'VERIFIED', 'FAILED', 'UNKNOWN')),
  idempotency_key    text NOT NULL UNIQUE,
  item_sku           text NOT NULL,
  shopee_item_id     bigint,
  models             jsonb NOT NULL DEFAULT '[]'::jsonb,
  last_error         jsonb,
  last_run_id        uuid,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT product_mappings_uk UNIQUE (shop_connection_id, product_id),
  CONSTRAINT product_mappings_product_fk FOREIGN KEY (org_id, product_id) REFERENCES products (org_id, id) ON DELETE CASCADE,
  CONSTRAINT product_mappings_shop_fk FOREIGN KEY (org_id, shop_connection_id) REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);
CREATE TRIGGER product_mappings_updated_at BEFORE UPDATE ON product_mappings FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

-- media_space image IDs per shop. Whether an image_id can be reused across
-- shops is unverified (S-6), so uploads are per shop.
CREATE TABLE product_image_uploads (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL,
  product_image_id   uuid NOT NULL,
  shop_connection_id uuid NOT NULL,
  shopee_image_id    text NOT NULL,
  shopee_image_url   text,
  uploaded_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT product_image_uploads_uk UNIQUE (product_image_id, shop_connection_id),
  CONSTRAINT product_image_uploads_image_fk FOREIGN KEY (org_id, product_image_id) REFERENCES product_images (org_id, id) ON DELETE CASCADE,
  CONSTRAINT product_image_uploads_shop_fk FOREIGN KEY (org_id, shop_connection_id) REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);

CREATE TABLE product_import_batches (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  source             text NOT NULL CHECK (source IN ('CSV', 'XLSX')),
  filename           text NOT NULL,
  file_storage_key   text NOT NULL,
  file_sha256        bytea NOT NULL,
  status             text NOT NULL CHECK (status IN ('UPLOADED', 'PARSING', 'IMPORTING', 'COMPLETED', 'FAILED')),
  template_id        uuid,
  template_version_id uuid,
  total_rows         int NOT NULL DEFAULT 0,
  valid_rows         int NOT NULL DEFAULT 0,
  invalid_rows       int NOT NULL DEFAULT 0,
  imported_rows      int NOT NULL DEFAULT 0,
  failed_rows        int NOT NULL DEFAULT 0,
  report_storage_key text,
  error              text,
  created_by         uuid REFERENCES users (id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT product_import_batches_org_id_uk UNIQUE (org_id, id)
);
CREATE INDEX product_import_batches_org_idx ON product_import_batches (org_id, created_at DESC);
CREATE TRIGGER product_import_batches_updated_at BEFORE UPDATE ON product_import_batches FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE product_import_rows (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL,
  batch_id   uuid NOT NULL,
  row_number int NOT NULL CHECK (row_number > 0),
  raw        jsonb NOT NULL,
  status     text NOT NULL CHECK (status IN ('PENDING', 'VALID', 'INVALID', 'IMPORTED', 'FAILED')),
  errors     jsonb NOT NULL DEFAULT '[]'::jsonb,
  product_id uuid,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT product_import_rows_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT product_import_rows_no_uk UNIQUE (batch_id, row_number),
  CONSTRAINT product_import_rows_batch_fk FOREIGN KEY (org_id, batch_id) REFERENCES product_import_batches (org_id, id) ON DELETE CASCADE
);
CREATE INDEX product_import_rows_status_idx ON product_import_rows (batch_id, status, row_number);
CREATE TRIGGER product_import_rows_updated_at BEFORE UPDATE ON product_import_rows FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE ROW LEVEL SECURITY;
CREATE POLICY products_tenant ON products USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_variations ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_variations FORCE ROW LEVEL SECURITY;
CREATE POLICY product_variations_tenant ON product_variations USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_images FORCE ROW LEVEL SECURITY;
CREATE POLICY product_images_tenant ON product_images USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY product_mappings_tenant ON product_mappings USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_image_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_image_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY product_image_uploads_tenant ON product_image_uploads USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_import_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_import_batches FORCE ROW LEVEL SECURITY;
CREATE POLICY product_import_batches_tenant ON product_import_batches USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());
ALTER TABLE product_import_rows ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_import_rows FORCE ROW LEVEL SECURITY;
CREATE POLICY product_import_rows_tenant ON product_import_rows USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

GRANT SELECT, INSERT, UPDATE ON products, product_mappings, product_import_batches TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON product_import_rows TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON product_variations, product_images TO app_rw;
GRANT SELECT, INSERT ON product_image_uploads TO app_rw;

-- +goose Down
DROP TABLE IF EXISTS product_import_rows;
DROP TABLE IF EXISTS product_import_batches;
DROP TABLE IF EXISTS product_image_uploads;
DROP TABLE IF EXISTS product_mappings;
DROP TABLE IF EXISTS product_images;
DROP TABLE IF EXISTS product_variations;
DROP TABLE IF EXISTS products;
