-- Phase 3: shop templates, versions, sections and per-shop overrides.

-- +goose Up

CREATE TABLE shop_templates (
  id                           uuid PRIMARY KEY,
  org_id                       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name                         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  description                  text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
  current_published_version_id uuid,
  cloned_from_version_id       uuid,
  created_by                   uuid REFERENCES users (id),
  created_at                   timestamptz NOT NULL DEFAULT now(),
  updated_at                   timestamptz NOT NULL DEFAULT now(),
  deleted_at                   timestamptz,
  CONSTRAINT shop_templates_org_id_uk UNIQUE (org_id, id)
);
CREATE UNIQUE INDEX shop_templates_name_uk ON shop_templates (org_id, lower(name)) WHERE deleted_at IS NULL;
CREATE TRIGGER shop_templates_updated_at BEFORE UPDATE ON shop_templates
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

CREATE TABLE shop_template_versions (
  id             uuid PRIMARY KEY,
  org_id         uuid NOT NULL,
  template_id    uuid NOT NULL,
  version_no     int NOT NULL CHECK (version_no > 0),
  status         text NOT NULL CHECK (status IN ('DRAFT', 'PUBLISHED', 'ARCHIVED')),
  based_on_id    uuid,
  content_sha256 bytea,
  created_by     uuid REFERENCES users (id),
  published_at   timestamptz,
  published_by   uuid REFERENCES users (id),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_template_versions_org_id_uk UNIQUE (org_id, id),
  CONSTRAINT shop_template_versions_no_uk UNIQUE (template_id, version_no),
  CONSTRAINT shop_template_versions_template_fk FOREIGN KEY (org_id, template_id)
    REFERENCES shop_templates (org_id, id) ON DELETE CASCADE,
  CONSTRAINT shop_template_versions_published_ck CHECK (
    (status = 'DRAFT') = (content_sha256 IS NULL AND published_at IS NULL))
);
-- At most one open draft per template.
CREATE UNIQUE INDEX shop_template_versions_one_draft ON shop_template_versions (template_id) WHERE status = 'DRAFT';
CREATE TRIGGER shop_template_versions_updated_at BEFORE UPDATE ON shop_template_versions
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

ALTER TABLE shop_templates
  ADD CONSTRAINT shop_templates_published_fk FOREIGN KEY (org_id, current_published_version_id)
    REFERENCES shop_template_versions (org_id, id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE shop_template_sections (
  id             uuid PRIMARY KEY,
  org_id         uuid NOT NULL,
  version_id     uuid NOT NULL,
  section_key    text NOT NULL CHECK (section_key IN ('general', 'branding', 'description', 'contact', 'warehouse',
                   'return_address', 'logistics', 'product_defaults', 'category_mappings', 'sku_rules',
                   'default_descriptions', 'default_attributes')),
  content        jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(content) = 'object'),
  schema_version int NOT NULL DEFAULT 1,
  updated_by     uuid REFERENCES users (id),
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_template_sections_key_uk UNIQUE (version_id, section_key),
  CONSTRAINT shop_template_sections_version_fk FOREIGN KEY (org_id, version_id)
    REFERENCES shop_template_versions (org_id, id) ON DELETE CASCADE
);
CREATE TRIGGER shop_template_sections_updated_at BEFORE UPDATE ON shop_template_sections
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

-- Published (and archived) versions are immutable: their sections cannot be
-- inserted, changed or deleted, whatever the application does. Deleting a
-- whole template still cascades (the version row goes first in that case).
-- +goose StatementBegin
CREATE FUNCTION app_template_sections_guard() RETURNS trigger
  LANGUAGE plpgsql
  AS $$
DECLARE
  v_status text;
BEGIN
  SELECT status INTO v_status FROM shop_template_versions
   WHERE id = COALESCE(NEW.version_id, OLD.version_id);
  IF v_status IS NOT NULL AND v_status <> 'DRAFT' THEN
    RAISE EXCEPTION 'sections of a % template version cannot be changed', v_status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN COALESCE(NEW, OLD);
END
$$;
-- +goose StatementEnd
CREATE TRIGGER shop_template_sections_immutable BEFORE INSERT OR UPDATE OR DELETE ON shop_template_sections
  FOR EACH ROW EXECUTE FUNCTION app_template_sections_guard();

-- Shop-specific overrides: partial sections merged on top of a version.
CREATE TABLE shop_template_mappings (
  id                      uuid PRIMARY KEY,
  org_id                  uuid NOT NULL,
  template_id             uuid NOT NULL,
  shop_connection_id      uuid NOT NULL,
  overrides               jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(overrides) = 'object'),
  last_applied_version_id uuid,
  last_applied_at         timestamptz,
  updated_by              uuid REFERENCES users (id),
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shop_template_mappings_uk UNIQUE (template_id, shop_connection_id),
  CONSTRAINT shop_template_mappings_template_fk FOREIGN KEY (org_id, template_id)
    REFERENCES shop_templates (org_id, id) ON DELETE CASCADE,
  CONSTRAINT shop_template_mappings_shop_fk FOREIGN KEY (org_id, shop_connection_id)
    REFERENCES shop_connections (org_id, id) ON DELETE CASCADE
);
CREATE TRIGGER shop_template_mappings_updated_at BEFORE UPDATE ON shop_template_mappings
  FOR EACH ROW EXECUTE FUNCTION app_set_updated_at();

ALTER TABLE shop_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_templates_tenant ON shop_templates
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_template_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_template_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_template_versions_tenant ON shop_template_versions
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_template_sections ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_template_sections FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_template_sections_tenant ON shop_template_sections
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

ALTER TABLE shop_template_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE shop_template_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY shop_template_mappings_tenant ON shop_template_mappings
  USING (org_id = app_current_org_id()) WITH CHECK (org_id = app_current_org_id());

GRANT SELECT, INSERT, UPDATE ON shop_templates TO app_rw;
GRANT SELECT, INSERT, UPDATE ON shop_template_versions TO app_rw;
GRANT SELECT, INSERT, UPDATE ON shop_template_sections TO app_rw;
GRANT SELECT, INSERT, UPDATE, DELETE ON shop_template_mappings TO app_rw;

-- +goose Down
DROP TABLE IF EXISTS shop_template_mappings;
ALTER TABLE IF EXISTS shop_templates DROP CONSTRAINT IF EXISTS shop_templates_published_fk;
DROP TABLE IF EXISTS shop_template_sections;
DROP FUNCTION IF EXISTS app_template_sections_guard();
DROP TABLE IF EXISTS shop_template_versions;
DROP TABLE IF EXISTS shop_templates;
