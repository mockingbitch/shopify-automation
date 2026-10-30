package products

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func errsByRow(groups []*Group) map[int][]RowError {
	out := map[int][]RowError{}
	for _, g := range groups {
		for r, e := range g.Errors {
			out[r] = append(out[r], e...)
		}
	}
	return out
}

func TestSampleCSVIsValid(t *testing.T) {
	rows, err := ParseFile("CSV", []byte(SampleCSV))
	if err != nil || len(rows) != 4 {
		t.Fatalf("parse: %d %v", len(rows), err)
	}
	groups := BuildGroups(rows, Defaults{})
	if len(groups) != 2 {
		t.Fatalf("groups: %d", len(groups))
	}
	if e := errsByRow(groups); len(e) != 0 {
		t.Fatalf("sample has errors: %+v", e)
	}
	tee := groups[1].Input
	if len(tee.Tiers) != 2 || len(tee.Tiers[0].Options) != 2 || len(tee.Tiers[1].Options) != 2 || len(tee.Variations) != 3 {
		t.Fatalf("tiers: %+v %+v", tee.Tiers, tee.Variations)
	}
	if tee.Price != "159000" || tee.Stock != 65 || len(groups[1].ImageURLs) != 1 {
		t.Fatalf("aggregates: price=%s stock=%d images=%v", tee.Price, tee.Stock, groups[1].ImageURLs)
	}
	if len(groups[0].ImageURLs) != 2 {
		t.Fatalf("mug images: %v", groups[0].ImageURLs)
	}
}

func TestXLSX(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"SKU", "Name", "Description", "Category", "Price", "Currency", "Stock", "Weight kg", "Image URLs", "Notes"})
	_ = f.SetSheetRow("Sheet1", "A2", &[]any{"A-1", "Áo thun", "Cotton", "100017", "99000", "VND", 5, "0.2", "https://example.com/a.jpg", "ignored"})
	var b bytes.Buffer
	_ = f.Write(&b)
	rows, err := ParseFile("XLSX", b.Bytes())
	if err != nil || len(rows) != 1 || rows[0]["name"] != "Áo thun" || rows[0]["weight_kg"] != "0.2" || rows[0]["stock"] != "5" {
		t.Fatalf("xlsx: %+v %v", rows, err)
	}
	if _, ok := rows[0]["notes"]; ok {
		t.Fatal("unknown column kept")
	}
	if e := errsByRow(BuildGroups(rows, Defaults{})); len(e) != 0 {
		t.Fatalf("errors: %+v", e)
	}
	if _, err := ParseFile("XLSX", []byte("not a zip")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestParseLimits(t *testing.T) {
	if _, err := ParseFile("CSV", []byte("foo,bar\n1,2\n")); err == nil || !strings.Contains(err.Error(), "sku and name") {
		t.Fatalf("missing columns: %v", err)
	}
	if _, err := ParseFile("CSV", []byte("sku,name\n")); err == nil {
		t.Fatal("no data rows accepted")
	}
	if _, err := ParseFile("CSV", []byte{0xff, 0xfe, 's'}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := ParseFile("PDF", []byte("x")); err == nil {
		t.Fatal("pdf accepted")
	}
	var b strings.Builder
	b.WriteString("sku,name\n")
	for i := 0; i <= MaxImportRows; i++ {
		b.WriteString("a,b\n")
	}
	if _, err := ParseFile("CSV", []byte(b.String())); err == nil {
		t.Fatal("row limit ignored")
	}
}

func TestRowErrorsAndDefaults(t *testing.T) {
	csv := "sku,name,description,category,price,stock,image_urls,tier1_name,tier1_option\n" +
		",Mug,,Kitchen,89000,3,https://e.com/a.jpg,,\n" + // generated SKU, mapped category, default weight/desc
		"X-1,Bad,desc,Unknown cat,-5,abc,http://e.com/a.jpg,,\n" + // many errors
		"T-1,Tee,desc,100017,100,1,https://e.com/t.jpg,Color,Red\n" +
		"T-1,Tee,desc,100017,0,1,,Color,Blue\n" // bad price on second variation
	rows, err := ParseFile("CSV", []byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	secs := map[string]json.RawMessage{
		"general":              json.RawMessage(`{"region":"VN"}`),
		"category_mappings":    json.RawMessage(`{"mappings":[{"key":"kitchen","shopee_category_id":100636}]}`),
		"sku_rules":            json.RawMessage(`{"prefix":"ma","separator":"-","sequence_digits":4,"uppercase":true,"generate_when_missing":true}`),
		"product_defaults":     json.RawMessage(`{"weight_kg":0.5,"condition":"NEW","brand":{"brand_id":0,"original_brand_name":"No Brand"}}`),
		"default_descriptions": json.RawMessage(`{"body_template":"{name} chính hãng","footer":"Giao nhanh"}`),
		"default_attributes":   json.RawMessage(`{"attributes":[{"category_id":100636,"attribute_id":7,"value_id":0,"original_value_name":"Gốm"}]}`),
	}
	groups := BuildGroups(rows, DefaultsFromSections(secs))
	e := errsByRow(groups)

	mug := groups[0]
	if mug.SKU != "MA-0001" || len(e[0]) != 0 {
		t.Fatalf("mug: sku=%s errors=%+v", mug.SKU, e[0])
	}
	in := mug.Input
	if *in.ShopeeCategoryID != "100636" || *in.WeightKg != "0.5" || in.Currency != "VND" || *in.BrandName != "No Brand" ||
		in.Description != "Mug chính hãng\n\nGiao nhanh" || len(in.Attributes) != 1 {
		t.Fatalf("defaults not applied: %+v", in)
	}
	cols := map[string]bool{}
	for _, re := range e[1] {
		cols[re.Column] = true
	}
	for _, c := range []string{"category", "price", "stock", "image_urls"} {
		if !cols[c] {
			t.Errorf("row 2 missing error on %s: %+v", c, e[1])
		}
	}
	// The bad variation is reported on its own row; the other row is marked too.
	if len(e[3]) == 0 || e[3][0].Column != "price" {
		t.Errorf("variation error: %+v", e[3])
	}
	if len(e[2]) != 1 || e[2][0].Code != "group_invalid" {
		t.Errorf("sibling row: %+v", e[2])
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-2": "'-2", "@x": "'@x", "ok": "ok", "": ""} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q", in, got)
		}
	}
}

func TestValidateVariations(t *testing.T) {
	in := Input{SKU: "S", Name: "N", Price: "10", Currency: "VND",
		Tiers:      []Tier{{Name: "Color", Options: []string{"Red", "red"}}},
		Variations: []VariationInput{{TierIndex: []int{0}, Price: "10"}, {TierIndex: []int{0}, Price: "10"}, {TierIndex: []int{5}, Price: "0"}}}
	got := map[string]bool{}
	for _, e := range Validate(&in) {
		got[e.Field+"/"+e.Code] = true
	}
	for _, want := range []string{"tiers.0.options.1/duplicate", "variations.1.tier_index/duplicate", "variations.2.tier_index/invalid", "variations.2.price/invalid"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}
