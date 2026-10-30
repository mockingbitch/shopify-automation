package app_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/shopee/shopeefake"
)

type tplCreated struct {
	Data struct {
		Template struct {
			ID                        string  `json:"id"`
			Name                      string  `json:"name"`
			DraftVersionID            *string `json:"draft_version_id"`
			CurrentPublishedVersionID *string `json:"current_published_version_id"`
		} `json:"template"`
		DraftVersion struct {
			ID        string `json:"id"`
			VersionNo int    `json:"version_no"`
			Status    string `json:"status"`
		} `json:"draft_version"`
	} `json:"data"`
}

type versionDetail struct {
	Data struct {
		Version struct {
			ID            string  `json:"id"`
			Status        string  `json:"status"`
			ContentSHA256 *string `json:"content_sha256"`
		} `json:"version"`
		Sections []struct {
			Key     string          `json:"key"`
			Content json.RawMessage `json:"content"`
			ETag    string          `json:"etag"`
		} `json:"sections"`
	} `json:"data"`
}

func (c *client) createTemplate(orgID, name string, sections map[string]any) (tplID, versionID string) {
	c.e.t.Helper()
	var out tplCreated
	c.must(201, "POST", "/orgs/"+orgID+"/templates", map[string]any{"name": name, "sections": sections}).json(c.e.t, &out)
	return out.Data.Template.ID, out.Data.DraftVersion.ID
}

func (c *client) sectionETag(orgID, tplID, versionID, key string) string {
	c.e.t.Helper()
	var v versionDetail
	c.must(200, "GET", "/orgs/"+orgID+"/templates/"+tplID+"/versions/"+versionID, nil).json(c.e.t, &v)
	for _, s := range v.Data.Sections {
		if s.Key == key {
			return s.ETag
		}
	}
	c.e.t.Fatalf("section %s not found", key)
	return ""
}

func (c *client) putSection(orgID, tplID, versionID, key string, body any) resp {
	c.e.t.Helper()
	path := "/orgs/" + orgID + "/templates/" + tplID + "/versions/" + versionID + "/sections/" + key
	return c.do("PUT", path, body, "If-Match", c.sectionETag(orgID, tplID, versionID, key))
}

func TestTemplateLifecycle(t *testing.T) {
	e := newEnv(t)
	alice := e.signUp("alice@a.example", "Alice")
	org := alice.createOrg("Org A", "org-a")
	base := "/orgs/" + org + "/templates"

	alice.must(422, "POST", base, map[string]any{"name": "Bad", "sections": map[string]any{"contact": map[string]any{"phone": "0901"}}})
	alice.must(422, "POST", base, map[string]any{"name": "Bad", "sections": map[string]any{"nope": map[string]any{}}})

	tpl, v1 := alice.createTemplate(org, "Vietnam default", map[string]any{"general": map[string]any{"region": "VN"}})
	if code := alice.must(422, "POST", base, map[string]any{"name": "vietnam DEFAULT"}).code(t); code != "validation_failed" {
		t.Fatalf("duplicate name: %s", code)
	}

	var v versionDetail
	alice.must(200, "GET", base+"/"+tpl+"/versions/"+v1, nil).json(t, &v)
	if len(v.Data.Sections) != 12 || v.Data.Version.Status != "DRAFT" || v.Data.Version.ContentSHA256 != nil {
		t.Fatalf("draft: %+v", v.Data)
	}

	// Optimistic concurrency on sections.
	secPath := base + "/" + tpl + "/versions/" + v1 + "/sections/description"
	alice.must(412, "PUT", secPath, map[string]any{"text": "x"})
	etag := alice.sectionETag(org, tpl, v1, "description")
	r := alice.must(200, "PUT", secPath, map[string]any{"text": "Đồ gia dụng chính hãng"}, "If-Match", etag)
	if r.Header.Get("ETag") == "" || r.Header.Get("ETag") == etag {
		t.Fatalf("etag not rotated: %q", r.Header.Get("ETag"))
	}
	alice.must(412, "PUT", secPath, map[string]any{"text": "lost update"}, "If-Match", etag)
	alice.must(422, "PUT", secPath, map[string]any{"text": 5}, "If-Match", r.Header.Get("ETag"))
	alice.must(404, "PUT", base+"/"+tpl+"/versions/"+v1+"/sections/unknown", map[string]any{}, "If-Match", etag)

	// Semantic errors block publishing; warnings do not.
	alice.putSection(org, tpl, v1, "logistics", map[string]any{"channels": []any{
		map[string]any{"logistics_channel_id": 1, "enabled": true}, map[string]any{"logistics_channel_id": 1, "enabled": false}}})
	var rep struct {
		Data struct {
			Valid  bool `json:"valid"`
			Errors []struct {
				Field string `json:"field"`
			} `json:"errors"`
		} `json:"data"`
	}
	alice.must(200, "POST", base+"/"+tpl+"/versions/"+v1+"/validate", nil).json(t, &rep)
	if rep.Data.Valid || len(rep.Data.Errors) != 1 {
		t.Fatalf("validate: %+v", rep)
	}
	alice.must(422, "POST", base+"/"+tpl+"/versions/"+v1+"/publish", nil)
	alice.putSection(org, tpl, v1, "logistics", map[string]any{"channels": []any{map[string]any{"logistics_channel_id": 1, "enabled": true}}})

	alice.must(200, "POST", base+"/"+tpl+"/versions/"+v1+"/publish", nil)
	alice.must(409, "POST", base+"/"+tpl+"/versions/"+v1+"/publish", nil)
	if code := alice.putSection(org, tpl, v1, "description", map[string]any{"text": "changed"}).code(t); code != "version_not_draft" {
		t.Fatalf("edit published: %s", code)
	}
	alice.must(200, "GET", base+"/"+tpl+"/versions/"+v1, nil).json(t, &v)
	if v.Data.Version.Status != "PUBLISHED" || v.Data.Version.ContentSHA256 == nil || len(*v.Data.Version.ContentSHA256) != 64 {
		t.Fatalf("published: %+v", v.Data.Version)
	}

	// The database itself refuses to change a published section.
	ctx := context.Background()
	err := e.db.Tx(ctx, db.Scope{OrgID: uuid.MustParse(org)}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE shop_template_sections SET content = '{"text":"sneaky"}' WHERE version_id = $1 AND section_key = 'description'`, v1)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("db immutability: %v", err)
	}

	// New draft from v1, only one open draft.
	var d2 struct {
		Data struct {
			ID        string `json:"id"`
			VersionNo int    `json:"version_no"`
		} `json:"data"`
	}
	alice.must(201, "POST", base+"/"+tpl+"/versions", map[string]any{"from_version_id": v1}).json(t, &d2)
	if d2.Data.VersionNo != 2 {
		t.Fatalf("v2: %+v", d2)
	}
	alice.must(409, "POST", base+"/"+tpl+"/versions", map[string]any{"from_version_id": v1})
	alice.must(200, "GET", base+"/"+tpl+"/versions/"+d2.Data.ID, nil).json(t, &v)
	for _, s := range v.Data.Sections {
		if s.Key == "description" && !strings.Contains(string(s.Content), "chính hãng") {
			t.Fatalf("draft did not copy content: %s", s.Content)
		}
	}
	alice.putSection(org, tpl, d2.Data.ID, "general", map[string]any{"region": "VN", "shop_name": "Minh An"})
	alice.must(200, "POST", base+"/"+tpl+"/versions/"+d2.Data.ID+"/publish", nil)

	var detail struct {
		Data struct {
			Template struct {
				CurrentPublishedVersionID *string `json:"current_published_version_id"`
				DraftVersionID            *string `json:"draft_version_id"`
				LatestVersionNo           int     `json:"latest_version_no"`
			} `json:"template"`
			Versions []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"versions"`
		} `json:"data"`
	}
	alice.must(200, "GET", base+"/"+tpl, nil).json(t, &detail)
	if *detail.Data.Template.CurrentPublishedVersionID != d2.Data.ID || detail.Data.Template.DraftVersionID != nil ||
		detail.Data.Template.LatestVersionNo != 2 || detail.Data.Versions[1].Status != "ARCHIVED" {
		t.Fatalf("after second publish: %+v", detail.Data)
	}

	// Clone copies the published version.
	var cl tplCreated
	alice.must(201, "POST", base+"/"+tpl+"/clone", map[string]any{"name": "Copy"}).json(t, &cl)
	alice.must(200, "GET", base+"/"+cl.Data.Template.ID+"/versions/"+cl.Data.DraftVersion.ID, nil).json(t, &v)
	for _, s := range v.Data.Sections {
		if s.Key == "general" && !strings.Contains(string(s.Content), "Minh An") {
			t.Fatalf("clone content: %s", s.Content)
		}
	}

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "PATCH", base+"/"+cl.Data.Template.ID, map[string]any{"name": "Copy renamed"})
	alice.must(422, "PATCH", base+"/"+cl.Data.Template.ID, map[string]any{"name": "Vietnam default"})
	alice.must(204, "DELETE", base+"/"+cl.Data.Template.ID, nil)
	alice.must(404, "GET", base+"/"+cl.Data.Template.ID, nil)
	alice.must(200, "GET", base, nil).json(t, &list)
	if len(list.Data) != 1 {
		t.Fatalf("list after delete: %+v", list)
	}

	var schemas struct {
		Data []struct {
			Key    string         `json:"key"`
			Schema map[string]any `json:"schema"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/template-sections", nil).json(t, &schemas)
	if len(schemas.Data) != 12 || schemas.Data[0].Key != "general" || schemas.Data[0].Schema["$schema"] == nil {
		t.Fatalf("schemas: %+v", schemas.Data[0])
	}
}

type preview struct {
	Data []struct {
		ShopID           string `json:"shop_id"`
		ConnectionStatus string `json:"connection_status"`
		HasOverrides     bool   `json:"has_overrides"`
		Errors           []struct {
			Field string `json:"field"`
		} `json:"errors"`
		Warnings []struct {
			Code string `json:"code"`
		} `json:"warnings"`
		Changes []previewChange `json:"changes"`
		Blocked []previewChange `json:"blocked"`
	} `json:"data"`
}

type previewChange struct {
	Path            string `json:"path"`
	Current         any    `json:"current"`
	CurrentKnown    bool   `json:"current_known"`
	Target          any    `json:"target"`
	Mode            string `json:"mode"`
	Enabled         bool   `json:"enabled"`
	Reason          string `json:"reason"`
	Sensitive       bool   `json:"sensitive"`
	DefaultSelected bool   `json:"default_selected"`
	Note            string `json:"note"`
}

func byPath(cs []previewChange) map[string]previewChange {
	m := map[string]previewChange{}
	for _, c := range cs {
		m[c.Path] = c
	}
	return m
}

func TestTemplateOverridesAndPreview(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 701, Name: "Old Name", Region: "VN", Description: "Old description"})
	e.fake.AddShop(shopeefake.Shop{ID: 702, Name: "Thai shop", Region: "TH", Description: "Same"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	vn := e.connectShop(alice, org, 701)
	th := e.connectShop(alice, org, 702)

	tpl, v1 := alice.createTemplate(org, "Default", map[string]any{
		"general":     map[string]any{"region": "VN", "shop_name": "Minh An"},
		"description": map[string]any{"text": "Same"},
		"branding":    map[string]any{"banner_urls": []string{"https://cdn.example/banner.png"}},
		"logistics":   map[string]any{"channels": []any{map[string]any{"logistics_channel_id": 50011, "name": "Express", "enabled": true}}},
		"return_address": map[string]any{"name": "Kho", "phone": "+84901234567", "address": "1 Lê Lợi", "city": "Hà Nội",
			"use_as": []string{"RETURN_ADDRESS"}},
	})
	alice.must(200, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/publish", nil)

	ovPath := "/orgs/" + org + "/templates/" + tpl + "/shops/" + vn + "/overrides"
	alice.must(422, "PUT", ovPath, map[string]any{"overrides": map[string]any{"general": map[string]any{"shop_name": ""}}})
	alice.must(422, "PUT", ovPath, map[string]any{"overrides": map[string]any{"bogus": map[string]any{}}})
	alice.must(404, "PUT", "/orgs/"+org+"/templates/"+tpl+"/shops/"+uuid.NewString()+"/overrides", map[string]any{"overrides": map[string]any{}})
	alice.must(200, "PUT", ovPath, map[string]any{"overrides": map[string]any{"general": map[string]any{"shop_name": "Minh An VN"}}})
	var ov struct {
		Data struct {
			Overrides map[string]map[string]any `json:"overrides"`
		} `json:"data"`
	}
	alice.must(200, "GET", ovPath, nil).json(t, &ov)
	if ov.Data.Overrides["general"]["shop_name"] != "Minh An VN" {
		t.Fatalf("overrides: %+v", ov)
	}

	var pv preview
	alice.must(422, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/preview", map[string]any{"shop_ids": []string{}})
	alice.must(422, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/preview", map[string]any{"shop_ids": []string{uuid.NewString()}})
	alice.must(200, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/preview", map[string]any{"shop_ids": []string{vn, th}}).json(t, &pv)
	if len(pv.Data) != 2 {
		t.Fatalf("previews: %+v", pv)
	}

	vnp := pv.Data[0]
	ch := byPath(vnp.Changes)
	if !vnp.HasOverrides || len(vnp.Warnings) != 0 || len(vnp.Errors) != 0 {
		t.Fatalf("vn preview: %+v", vnp)
	}
	name := ch["general.shop_name"]
	if name.Current != "Old Name" || !name.CurrentKnown || name.Target != "Minh An VN" || !name.Sensitive || name.DefaultSelected {
		t.Errorf("shop name: %+v", name)
	}
	if desc, ok := ch["description.text"]; !ok || desc.Current != "Old description" || !desc.DefaultSelected {
		t.Errorf("description: %+v", desc)
	}
	if b := ch["branding.banner_urls"]; b.Mode != "MANUAL" || b.CurrentKnown || b.Reason != "browser_recipe_unavailable" {
		t.Errorf("banner: %+v", b)
	}
	if l := ch["logistics.channels.50011"]; !l.Sensitive || l.DefaultSelected || l.CurrentKnown {
		t.Errorf("logistics: %+v", l)
	}
	if a := ch["return_address.address"]; !a.Sensitive || a.DefaultSelected {
		t.Errorf("address: %+v", a)
	}

	thp := pv.Data[1]
	if thp.HasOverrides || len(thp.Warnings) != 1 || thp.Warnings[0].Code != "region_mismatch" {
		t.Errorf("th warnings: %+v", thp.Warnings)
	}
	if _, ok := byPath(thp.Changes)["description.text"]; ok {
		t.Error("unchanged description listed for TH shop")
	}

	// A disconnected shop: every change is blocked.
	alice.must(204, "DELETE", "/orgs/"+org+"/shops/"+th, map[string]any{"confirm": true})
	alice.must(200, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/preview", map[string]any{"shop_ids": []string{th}}).json(t, &pv)
	if len(pv.Data[0].Changes) != 0 || len(pv.Data[0].Blocked) == 0 || pv.Data[0].Blocked[0].Reason != "shop_not_active" {
		t.Fatalf("disconnected preview: %+v", pv.Data[0])
	}
	codes := map[string]bool{}
	for _, w := range pv.Data[0].Warnings {
		codes[w.Code] = true
	}
	if !codes["not_synced"] {
		t.Errorf("profile removed on disconnect should warn: %+v", pv.Data[0].Warnings)
	}
}

func TestTemplatePermissions(t *testing.T) {
	e := newEnv(t)
	owner := e.signUp("owner@a.example", "Owner")
	org := owner.createOrg("Org", "org-p")
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")
	viewer := e.signUp("viewer@a.example", "Viewer")
	e.addMember(owner, org, viewer, "viewer@a.example", "viewer")

	tpl, v1 := op.createTemplate(org, "By operator", nil)
	base := "/orgs/" + org + "/templates/" + tpl
	op.putSection(org, tpl, v1, "description", map[string]any{"text": "ok"})
	op.must(403, "POST", base+"/versions/"+v1+"/publish", nil)
	op.must(403, "DELETE", base, nil)
	viewer.must(200, "GET", base+"/versions/"+v1, nil)
	viewer.must(200, "POST", base+"/versions/"+v1+"/validate", nil)
	viewer.must(403, "POST", "/orgs/"+org+"/templates", map[string]any{"name": "x"})
	if r := viewer.putSection(org, tpl, v1, "description", map[string]any{"text": "no"}); r.Status != 403 {
		t.Fatalf("viewer edit: %d", r.Status)
	}
	owner.must(200, "POST", base+"/versions/"+v1+"/publish", nil)
	owner.must(204, "DELETE", base, nil)
}
