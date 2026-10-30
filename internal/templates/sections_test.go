package templates

import (
	"encoding/json"
	"strings"
	"testing"
)

var reg = LoadRegistry()

func TestEverySectionHasASchema(t *testing.T) {
	for _, k := range SectionKeys {
		s, ok := reg.Section(k)
		if !ok || s.AppliesTo == "" {
			t.Errorf("%s: missing schema or x-applies-to", k)
		}
		if errs := reg.ValidateSection(k, json.RawMessage(`{}`)); errs != nil {
			t.Errorf("%s: empty section should be valid: %v", k, errs)
		}
	}
	if f := mustSection(t, "general").Fields["shop_name"]; f.Capability != "shop.profile.update" || !f.Sensitive {
		t.Errorf("general.shop_name meta: %+v", f)
	}
	if f := mustSection(t, "return_address").Fields["city"]; f.Capability != "shop.address.update" || !f.Sensitive {
		t.Errorf("return_address.city inherits section meta: %+v", f)
	}
	if f := mustSection(t, "return_address").Fields["use_as"]; f.Capability != "shop.address.config" {
		t.Errorf("return_address.use_as meta: %+v", f)
	}
}

func mustSection(t *testing.T, k string) *SectionSchema {
	t.Helper()
	s, ok := reg.Section(k)
	if !ok {
		t.Fatal(k)
	}
	return s
}

func TestStructuralValidation(t *testing.T) {
	cases := []struct {
		key, body string
		field     string // expected error field ("" = valid)
	}{
		{"general", `{"region":"VN","shop_name":"Minh An"}`, ""},
		{"general", `{"region":"US"}`, "sections.general.region"},
		{"general", `{"shop_name":""}`, "sections.general.shop_name"},
		{"general", `{"extra":1}`, "sections.general"},
		{"branding", `{"logo_url":"http://insecure.example/logo.png"}`, "sections.branding.logo_url"},
		{"branding", `{"logo_url":"https://cdn.example/logo.png","banner_urls":["https://cdn.example/b1.png"]}`, ""},
		{"contact", `{"phone":"0901234567"}`, "sections.contact.phone"},
		{"contact", `{"phone":"+84901234567","email":"shop@example.com"}`, ""},
		{"contact", `{"email":"not-an-email"}`, "sections.contact.email"},
		{"logistics", `{"channels":[{"logistics_channel_id":50011,"enabled":true,"cod_enabled":true}]}`, ""},
		{"logistics", `{"channels":[{"logistics_channel_id":0,"enabled":true}]}`, "sections.logistics.channels.0.logistics_channel_id"},
		{"logistics", `{"channels":[{"enabled":true}]}`, "sections.logistics.channels.0"},
		{"product_defaults", `{"weight_kg":0}`, "sections.product_defaults.weight_kg"},
		{"product_defaults", `{"package_cm":{"length":10,"width":10}}`, "sections.product_defaults.package_cm"},
		{"product_defaults", `{"condition":"NEW","brand":{"brand_id":0,"original_brand_name":"No Brand"}}`, ""},
		{"default_attributes", `{"attributes":[{"attribute_id":100,"value_id":0}]}`, "sections.default_attributes.attributes.0"},
		{"default_attributes", `{"attributes":[{"attribute_id":100,"value_id":0,"original_value_name":"Cotton"}]}`, ""},
		{"sku_rules", `{"prefix":"MA-01"}`, "sections.sku_rules.prefix"},
		{"description", `[]`, "sections.description"},
	}
	for _, c := range cases {
		errs := reg.ValidateSection(c.key, json.RawMessage(c.body))
		if c.field == "" {
			if errs != nil {
				t.Errorf("%s %s: unexpected %v", c.key, c.body, errs)
			}
			continue
		}
		found := false
		for _, e := range errs {
			found = found || e.Field == c.field
		}
		if !found {
			t.Errorf("%s %s: want error on %s, got %v", c.key, c.body, c.field, errs)
		}
	}
}

func TestSemanticValidation(t *testing.T) {
	secs := Empty()
	secs["return_address"] = json.RawMessage(`{"city":"Hà Nội","use_as":["RETURN_ADDRESS"]}`)
	secs["logistics"] = json.RawMessage(`{"channels":[{"logistics_channel_id":1,"enabled":false,"cod_enabled":true},{"logistics_channel_id":1,"enabled":true}]}`)
	secs["category_mappings"] = json.RawMessage(`{"mappings":[{"key":"Áo","shopee_category_id":1},{"key":" áo ","shopee_category_id":2}]}`)
	secs["sku_rules"] = json.RawMessage(`{"generate_when_missing":true}`)
	rep := reg.Validate(secs)
	want := []string{
		"sections.return_address.name", "sections.return_address.phone", "sections.return_address.address",
		"sections.logistics.channels.0.cod_enabled", "sections.logistics.channels.1.logistics_channel_id",
		"sections.category_mappings.mappings.1.key", "sections.sku_rules.sequence_digits",
	}
	got := map[string]bool{}
	for _, e := range rep.Errors {
		got[e.Field] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing error %s (got %+v)", w, rep.Errors)
		}
	}
	if rep.Valid || len(rep.Warnings) == 0 {
		t.Fatalf("report: %+v", rep)
	}

	ok := Empty()
	ok["general"] = json.RawMessage(`{"region":"VN"}`)
	if rep := reg.Validate(ok); !rep.Valid || len(rep.Warnings) != 0 {
		t.Fatalf("valid template: %+v", rep)
	}
}

func TestMerge(t *testing.T) {
	base := Sections{
		"general":   json.RawMessage(`{"region":"VN","shop_name":"Base"}`),
		"logistics": json.RawMessage(`{"channels":[{"logistics_channel_id":1,"enabled":true}]}`),
		"branding":  json.RawMessage(`{"logo_url":"https://a.example/x.png"}`),
	}
	over := Sections{
		"general":   json.RawMessage(`{"shop_name":"Override"}`),
		"logistics": json.RawMessage(`{"channels":[]}`),
		"contact":   json.RawMessage(`{"phone":"+84900000000"}`),
	}
	m, err := Merge(base, over)
	if err != nil {
		t.Fatal(err)
	}
	expect := map[string]string{
		"general":   `{"region":"VN","shop_name":"Override"}`,
		"logistics": `{"channels":[]}`, // arrays replace
		"branding":  `{"logo_url":"https://a.example/x.png"}`,
		"contact":   `{"phone":"+84900000000"}`,
	}
	for k, want := range expect {
		if got := string(m[k]); got != want {
			t.Errorf("%s: %s, want %s", k, got, want)
		}
	}
	c1, _ := Canonical(Sections{"a": json.RawMessage(`{"y":1,"x":2}`)})
	c2, _ := Canonical(Sections{"a": json.RawMessage(`{ "x":2, "y":1 }`)})
	if string(c1) != string(c2) || !strings.Contains(string(c1), `"x":2,"y":1`) {
		t.Fatalf("canonical: %s vs %s", c1, c2)
	}
}
