package app_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.RGBA{0, 128, 255, 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// upload posts a multipart form with one file and extra fields.
func (c *client) upload(path, filename string, data []byte, fields map[string]string) resp {
	c.e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/v1"+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", c.csrf)
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	validateResponse(c.e.t, req, res.StatusCode, res.Header, b)
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}

type productResp struct {
	Data struct {
		ID     string `json:"id"`
		SKU    string `json:"sku"`
		Name   string `json:"name"`
		Price  struct{ Amount, Currency string }
		Stock  int `json:"stock"`
		Images []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"images"`
		Variations []struct {
			TierIndex []int `json:"tier_index"`
		} `json:"variations"`
		Attributes []struct {
			AttributeID int64 `json:"attribute_id"`
		} `json:"attributes"`
		Description   string  `json:"description"`
		CategoryID    *string `json:"shopee_category_id"`
		ImportBatchID *string `json:"import_batch_id"`
	} `json:"data"`
}

func TestProductCRUD(t *testing.T) {
	e := newEnv(t)
	alice := e.signUp("alice@a.example", "Alice")
	org := alice.createOrg("Org A", "org-a")
	base := "/orgs/" + org + "/products"

	bad := map[string]any{"sku": "has space", "name": "", "price": "0", "currency": "USD", "stock": -1}
	r := alice.must(422, "POST", base, bad)
	for _, f := range []string{`"sku"`, `"name"`, `"price"`, `"currency"`, `"stock"`} {
		if !strings.Contains(string(r.Body), f) {
			t.Errorf("missing error for %s: %s", f, r.Body)
		}
	}
	good := map[string]any{"sku": "TEE-1", "name": "Áo thun", "description": "Cotton", "shopee_category_id": "100017",
		"price": "159000", "currency": "VND", "stock": 10, "weight_kg": "0.2", "package_cm": map[string]int{"length": 30, "width": 25, "height": 2},
		"tiers":      []map[string]any{{"name": "Color", "options": []string{"Red", "Blue"}}},
		"variations": []map[string]any{{"tier_index": []int{0}, "price": "159000", "stock": 5, "sku": "TEE-1-R"}, {"tier_index": []int{1}, "price": "165000", "stock": 5}}}
	var p productResp
	r = alice.must(201, "POST", base, good)
	r.json(t, &p)
	if p.Data.Price.Amount != "159000.00" || p.Data.Price.Currency != "VND" || len(p.Data.Variations) != 2 {
		t.Fatalf("created: %s", r.Body)
	}
	alice.must(422, "POST", base, good) // same SKU
	id := p.Data.ID
	etag := r.Header.Get("ETag")

	// Images: sanitized and stored; bad files rejected.
	img := alice.upload(base+"/"+id+"/images", "a.png", testPNG(40, 30), nil)
	if img.Status != 201 || !strings.Contains(string(img.Body), "storage.test") {
		t.Fatalf("image: %d %s", img.Status, img.Body)
	}
	if r := alice.upload(base+"/"+id+"/images", "a.gif", []byte("GIF89a not an image"), nil); r.Status != 422 {
		t.Fatalf("gif: %d", r.Status)
	}

	// Replace with If-Match.
	good["name"] = "Áo thun cotton"
	alice.must(412, "PUT", base+"/"+id, good)
	r = alice.must(200, "PUT", base+"/"+id, good, "If-Match", etag)
	alice.must(412, "PUT", base+"/"+id, good, "If-Match", etag)
	r.json(t, &p)
	if p.Data.Name != "Áo thun cotton" || len(p.Data.Images) != 1 {
		t.Fatalf("replaced: %s", r.Body)
	}
	alice.must(204, "DELETE", base+"/"+id+"/images/"+p.Data.Images[0].ID, nil)
	alice.must(404, "DELETE", base+"/"+id+"/images/"+p.Data.Images[0].ID, nil)

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "GET", base+"?q=tee", nil).json(t, &list)
	if len(list.Data) != 1 {
		t.Fatalf("search: %+v", list)
	}
	alice.must(204, "DELETE", base+"/"+id, nil)
	alice.must(404, "GET", base+"/"+id, nil)
	alice.must(201, "POST", base, good) // SKU free again after delete
}

type batchResp struct {
	Data struct {
		ID           string  `json:"id"`
		Status       string  `json:"status"`
		TotalRows    int     `json:"total_rows"`
		ValidRows    int     `json:"valid_rows"`
		InvalidRows  int     `json:"invalid_rows"`
		ImportedRows int     `json:"imported_rows"`
		FailedRows   int     `json:"failed_rows"`
		HasReport    bool    `json:"has_report"`
		Error        *string `json:"error"`
	} `json:"data"`
}

type rowsResp struct {
	Data []struct {
		ID        string            `json:"id"`
		RowNumber int               `json:"row_number"`
		Status    string            `json:"status"`
		Values    map[string]string `json:"values"`
		Errors    []struct {
			Column string `json:"column"`
			Code   string `json:"code"`
		} `json:"errors"`
		ProductID *string `json:"product_id"`
	} `json:"data"`
}

func TestImportFlow(t *testing.T) {
	e := newEnv(t)
	images := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken.png" {
			_, _ = w.Write([]byte("not an image"))
			return
		}
		_, _ = w.Write(testPNG(20, 20))
	}))
	defer images.Close()
	e.app.Products.Fetcher.Client = images.Client()

	alice := e.signUp("alice@a.example", "Alice")
	org := alice.createOrg("Org A", "org-a")
	tpl, v := alice.createTemplate(org, "Defaults", map[string]any{
		"general":           map[string]any{"region": "VN"},
		"category_mappings": map[string]any{"mappings": []any{map[string]any{"key": "Kitchen", "shopee_category_id": 100636}}},
		"product_defaults":  map[string]any{"weight_kg": 0.5, "condition": "NEW"},
		"sku_rules":         map[string]any{"prefix": "MA", "separator": "-", "sequence_digits": 3, "uppercase": true, "generate_when_missing": true},
	})
	alice.must(200, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v+"/publish", nil)

	u := images.URL
	csv := "sku,name,description,category,price,stock,image_urls,tier1_name,tier1_option\n" +
		",Mug,Gốm sứ,Kitchen,89000,3," + u + "/mug.png,,\n" + // row 2: generated SKU, defaults
		"BAD-1,Bad,desc,Unknown,abc,1," + u + "/x.png,,\n" + // row 3: invalid
		"T-1,Tee,desc,100017,100000,2," + u + "/t.png,Color,Red\n" + // rows 4-5: variations
		"T-1,Tee,desc,100017,120000,3,,Color,Blue\n" +
		"IMG-1,Broken image,desc,100017,5000,1," + u + "/broken.png,,\n" // row 6: image fails at import
	base := "/orgs/" + org + "/imports"
	if r := alice.upload(base, "products.pdf", []byte("x"), nil); r.Status != 422 {
		t.Fatalf("pdf: %d", r.Status)
	}
	r := alice.upload(base, "products.csv", []byte(csv), map[string]string{"template_id": tpl, "template_version_id": v})
	if r.Status != 202 {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}
	var b batchResp
	r.json(t, &b)
	batch := b.Data.ID
	if len(e.queue.Errors) > 0 {
		t.Fatalf("queue: %v", e.queue.Errors)
	}
	alice.must(200, "GET", base+"/"+batch, nil).json(t, &b)
	if b.Data.Status != "COMPLETED" || b.Data.TotalRows != 5 || b.Data.ImportedRows != 3 || b.Data.InvalidRows != 1 || b.Data.FailedRows != 1 || !b.Data.HasReport {
		t.Fatalf("batch: %+v", b.Data)
	}

	var rows rowsResp
	alice.must(200, "GET", base+"/"+batch+"/rows", nil).json(t, &rows)
	byNo := map[int]int{}
	for i, row := range rows.Data {
		byNo[row.RowNumber] = i
	}
	bad := rows.Data[byNo[3]]
	cols := map[string]bool{}
	for _, er := range bad.Errors {
		cols[er.Column] = true
	}
	if bad.Status != "INVALID" || !cols["category"] || !cols["price"] {
		t.Fatalf("row 3: %+v", bad)
	}
	if fr := rows.Data[byNo[6]]; fr.Status != "FAILED" || fr.Errors[0].Column != "image_urls" {
		t.Fatalf("row 6: %+v", fr)
	}
	mugID := rows.Data[byNo[2]].ProductID
	if mugID == nil || rows.Data[byNo[4]].ProductID == nil || *rows.Data[byNo[4]].ProductID != *rows.Data[byNo[5]].ProductID {
		t.Fatalf("product ids: %+v", rows.Data)
	}
	var mug productResp
	alice.must(200, "GET", "/orgs/"+org+"/products/"+*mugID, nil).json(t, &mug)
	if mug.Data.SKU != "MA-001" || *mug.Data.CategoryID != "100636" || len(mug.Data.Images) != 1 || mug.Data.ImportBatchID == nil {
		t.Fatalf("mug: %+v", mug.Data)
	}

	// Fix the invalid row; it becomes valid, then a retry imports it.
	alice.must(422, "PATCH", base+"/"+batch+"/rows/"+bad.ID, map[string]any{"values": map[string]string{"nope": "x"}})
	var fixed struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	alice.must(200, "PATCH", base+"/"+batch+"/rows/"+bad.ID, map[string]any{"values": map[string]string{"category": "Kitchen", "price": "45000"}}).json(t, &fixed)
	if fixed.Data.Status != "VALID" {
		t.Fatalf("fixed: %+v", fixed)
	}
	alice.must(409, "PATCH", base+"/"+batch+"/rows/"+rows.Data[byNo[2]].ID, map[string]any{"values": map[string]string{"name": "x"}})
	alice.must(202, "POST", base+"/"+batch+"/retry", nil)
	alice.must(200, "GET", base+"/"+batch, nil).json(t, &b)
	if b.Data.ImportedRows != 4 || b.Data.InvalidRows != 0 || b.Data.FailedRows != 1 {
		r := alice.must(200, "GET", base+"/"+b.Data.ID+"/rows", nil)
		t.Fatalf("after retry: %+v %s", b.Data, r.Body)
	}

	var rep struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	alice.must(200, "GET", base+"/"+batch+"/report", nil).json(t, &rep)
	key := strings.TrimPrefix(rep.Data.URL, "https://storage.test/")
	report, err := e.store.Get(t.Context(), strings.ReplaceAll(key, "%2F", "/"), 1<<20)
	if err != nil || !strings.Contains(string(report), "IMPORTED") || !strings.Contains(string(report), "image_urls:") {
		t.Fatalf("report: %v %s", err, report)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/orgs/"+org+"/products?batch_id="+batch, nil).json(t, &list)
	if len(list.Data) != 4 { // mug, tee, the fixed row; not the broken image
		t.Fatalf("products of batch: %d", len(list.Data))
	}
	sample := alice.must(200, "GET", base+"/sample.csv", nil)
	if !strings.HasPrefix(string(sample.Body), "sku,name") {
		t.Fatalf("sample: %s", sample.Body)
	}

	// Importing the same SKU again updates the product instead of duplicating.
	again := "sku,name,description,category,price,stock,weight_kg,currency,image_urls\nMA-001,Mug v2,Gốm,100636,99000,4,0.5,VND," + u + "/mug2.png\n"
	r = alice.upload(base, "again.csv", []byte(again), nil)
	if r.Status != 202 {
		t.Fatalf("again: %d", r.Status)
	}
	alice.must(200, "GET", "/orgs/"+org+"/products/"+*mugID, nil).json(t, &mug)
	if mug.Data.Name != "Mug v2" || mug.Data.Price.Amount != "99000.00" {
		t.Fatalf("not updated: %+v", mug.Data)
	}
}

// An import whose file cannot be read is marked FAILED (not left busy), and
// retrying it reads the file again.
func TestImportFailureAndRetry(t *testing.T) {
	e := newEnv(t)
	alice := e.signUp("alice@a.example", "Alice")
	org := alice.createOrg("Org A", "org-a")
	base := "/orgs/" + org + "/imports"
	e.store.FailNextGets(1)
	var b batchResp
	alice.upload(base, "p.csv", []byte("sku,name,price,currency,stock\nF-1,Fork,1000,VND,1\n"), nil).json(t, &b)
	alice.must(200, "GET", base+"/"+b.Data.ID, nil).json(t, &b)
	if b.Data.Status != "FAILED" || b.Data.Error == nil || !strings.Contains(*b.Data.Error, "thử lại") {
		t.Fatalf("failed batch: %+v", b.Data)
	}
	alice.must(202, "POST", base+"/"+b.Data.ID+"/retry", nil)
	alice.must(200, "GET", base+"/"+b.Data.ID, nil).json(t, &b)
	// The file was read this time (the row itself lacks Shopee-required fields).
	if b.Data.Status != "COMPLETED" || b.Data.TotalRows != 1 || b.Data.Error != nil {
		t.Fatalf("after retry: %+v", b.Data)
	}
}
