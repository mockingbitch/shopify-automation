package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/shopee/shopeefake"
)

// publishCatalog is a small VN catalog: one leaf category with a mandatory
// attribute and the standardised "Color" variation.
func publishCatalog() shopeefake.Catalog {
	return shopeefake.Catalog{
		Categories: []map[string]any{
			{"category_id": 100001, "parent_category_id": 0, "original_category_name": "Fashion", "has_children": true},
			{"category_id": 100017, "parent_category_id": 100001, "original_category_name": "T-Shirts", "has_children": false},
		},
		Attributes: map[int64][]map[string]any{100017: {
			{"attribute_id": 7001, "name": "Material", "mandatory": true, "attribute_value_list": []any{map[string]any{"value_id": 1, "name": "Cotton"}}},
		}},
		Brands: map[int64][]map[string]any{100017: {{"brand_id": 3001, "original_brand_name": "Minh An", "display_brand_name": "Minh An"}}},
		Limits: map[string]any{"price_limit": map[string]any{"min_limit": 1000, "max_limit": 120000000},
			"stock_limit": map[string]any{"min_limit": 0, "max_limit": 999999}, "item_name_length_limit": map[string]any{"min_limit": 10, "max_limit": 120},
			"item_image_count_limit": map[string]any{"min_limit": 1, "max_limit": 9}, "item_description_length_limit": map[string]any{"min_limit": 5, "max_limit": 3000}},
		Variations: map[int64][]map[string]any{100017: {{"variation_id": 1, "variation_name": "Color", "variation_group_list": []any{
			map[string]any{"variation_group_id": 11, "variation_group_name": "Basic", "variation_option_list": []any{
				map[string]any{"variation_option_id": 101, "variation_option_name": "Red"},
				map[string]any{"variation_option_id": 102, "variation_option_name": "Blue"},
			}}}}}},
	}
}

func createProduct(c *client, org string, body map[string]any, withImage bool) string {
	c.e.t.Helper()
	var p productResp
	c.must(201, "POST", "/orgs/"+org+"/products", body).json(c.e.t, &p)
	if withImage {
		if r := c.upload("/orgs/"+org+"/products/"+p.Data.ID+"/images", "a.png", testPNG(40, 30), nil); r.Status != 201 {
			c.e.t.Fatalf("image: %d %s", r.Status, r.Body)
		}
	}
	return p.Data.ID
}

func baseProduct(sku string) map[string]any {
	return map[string]any{"sku": sku, "name": "Áo thun cotton " + sku, "description": "Cotton tee", "shopee_category_id": "100017",
		"price": "159000", "currency": "VND", "stock": 10, "weight_kg": "0.2", "brand_name": "Minh An",
		"package_cm": map[string]int{"length": 30, "width": 25, "height": 2},
		"attributes": []map[string]any{{"attribute_id": 7001, "value_id": 1}}}
}

func TestProductPublish(t *testing.T) {
	e := newEnv(t)
	e.app.Publish.VariationDelay = 0
	e.setupApp()
	setupShopForWorkflow(e, 811, "Shop")
	e.fake.SetCatalog(publishCatalog())
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 811)

	withTiers := baseProduct("TEE-V")
	withTiers["tiers"] = []map[string]any{{"name": "Color", "options": []string{"Red", "Blue"}}}
	withTiers["variations"] = []map[string]any{{"tier_index": []int{0}, "price": "159000", "stock": 5, "sku": "TEE-V-R"},
		{"tier_index": []int{1}, "price": "165000", "stock": 5, "sku": "TEE-V-B"}}
	pTiers := createProduct(owner, org, withTiers, true)
	pSimple := createProduct(owner, org, baseProduct("TEE-S"), true)

	noAttr := baseProduct("TEE-A")
	delete(noAttr, "attributes")
	pNoAttr := createProduct(owner, org, noAttr, true)
	badTier := baseProduct("TEE-T")
	badTier["tiers"] = []map[string]any{{"name": "Size", "options": []string{"M"}}}
	badTier["variations"] = []map[string]any{{"tier_index": []int{0}, "price": "159000", "stock": 1}}
	pBadTier := createProduct(owner, org, badTier, true)
	usd := baseProduct("TEE-U")
	usd["currency"] = "SGD"
	usd["price"] = "10"
	pUSD := createProduct(owner, org, usd, true)
	pNoImage := createProduct(owner, org, baseProduct("TEE-N"), false)

	path := "/orgs/" + org + "/products/publish"
	all := []string{pTiers, pSimple, pNoAttr, pBadTier, pUSD, pNoImage}
	owner.must(422, "POST", path, map[string]any{"shop_ids": []string{shop}})
	owner.must(422, "POST", path, map[string]any{"shop_ids": []string{shop}, "product_ids": []string{uuid.NewString()}})
	owner.must(422, "POST", path, map[string]any{"shop_ids": []string{uuid.NewString()}, "product_ids": all})

	// The first add_item response is lost after the item was created.
	e.fake.LoseNextAddItemResponse()
	key := uuid.NewString()
	var a, again applyResp
	owner.must(202, "POST", path, map[string]any{"shop_ids": []string{shop}, "product_ids": all}, "Idempotency-Key", key).json(t, &a)
	owner.must(202, "POST", path, map[string]any{"shop_ids": []string{shop}, "product_ids": all}, "Idempotency-Key", key).json(t, &again)
	if !again.Data.Replay || again.Data.RunIDs[0] != a.Data.RunIDs[0] {
		t.Fatalf("replay: %+v", again)
	}
	runID := a.Data.RunIDs[0]

	v := owner.runView(org, runID)
	if v.Data.Run.Status != "WAITING_APPROVAL" || v.Data.PendingApproval == nil {
		t.Fatalf("waiting: %s %+v", v.Data.Run.Status, v.Data.Steps)
	}
	changes, blocked := byPath(v.Data.PendingApproval.Diff.Changes), byPath(v.Data.PendingApproval.Diff.Blocked)
	if len(changes) != 2 || len(blocked) != 4 {
		t.Fatalf("diff: %d changes, %d blocked: %+v", len(changes), len(blocked), v.Data.PendingApproval.Diff)
	}
	wantBlocked := map[string]string{pNoAttr: "Mandatory attributes", pBadTier: "not one of Shopee's variations", pUSD: "uses VND", pNoImage: "no images"}
	for id, msg := range wantBlocked {
		if c, ok := blocked["products."+id]; !ok || !strings.Contains(c.Note, msg) {
			t.Errorf("blocked %s: %+v", id, c)
		}
	}
	decision := "/orgs/" + org + "/approvals/" + v.Data.PendingApproval.ID + "/decision"
	owner.must(422, "POST", decision, map[string]any{"decision": "APPROVE", "selected_change_paths": []string{"products." + pUSD}})
	owner.must(200, "POST", decision, map[string]any{"decision": "APPROVE",
		"selected_change_paths": []string{"products." + pTiers, "products." + pSimple}})

	// One product was created; the other's outcome is unknown and retried.
	for i := 0; i < 5 && owner.runView(org, runID).Data.Run.Status != "COMPLETED"; i++ {
		e.clock.Advance(2 * time.Minute)
		e.queue.RunDelayed(context.Background())
	}
	if len(e.queue.Errors) > 0 {
		t.Fatalf("queue errors: %v", e.queue.Errors)
	}
	v = owner.runView(org, runID)
	if v.Data.Run.Status != "COMPLETED" {
		t.Fatalf("final: %s %+v", v.Data.Run.Status, v.Data.Steps)
	}
	items := e.fake.Items(811)
	bySKU := map[string]int{}
	for _, it := range items {
		bySKU[it.SKU]++
		if it.Body["item_status"] != "UNLIST" || it.Body["brand"].(map[string]any)["brand_id"] != float64(3001) {
			t.Errorf("body: %+v", it.Body)
		}
	}
	if len(items) != 2 || bySKU["TEE-V"] != 1 || bySKU["TEE-S"] != 1 {
		t.Fatalf("items (no duplicates expected): %+v", bySKU)
	}
	for _, it := range items {
		if it.SKU == "TEE-V" && len(it.Models) != 2 {
			t.Fatalf("models: %+v", it.Models)
		}
	}
	if !strings.Contains(string(v.Data.Run.Output), `"published":2`) || !strings.Contains(string(v.Data.Run.Output), `"adopted":true`) {
		t.Fatalf("report: %s", v.Data.Run.Output)
	}

	// Publishing again is a no-op: the mappings are VERIFIED.
	addCalls := e.fake.Calls("/api/v2/product/add_item")
	var b applyResp
	owner.must(202, "POST", path, map[string]any{"shop_ids": []string{shop}, "product_ids": []string{pSimple}}).json(t, &b)
	v = owner.runView(org, b.Data.RunIDs[0])
	owner.must(200, "POST", "/orgs/"+org+"/approvals/"+v.Data.PendingApproval.ID+"/decision", map[string]any{"decision": "APPROVE",
		"selected_change_paths": []string{"products." + pSimple}})
	if v = owner.runView(org, b.Data.RunIDs[0]); v.Data.Run.Status != "COMPLETED" || e.fake.Calls("/api/v2/product/add_item") != addCalls {
		t.Fatalf("republish: %s, add_item %d -> %d", v.Data.Run.Status, addCalls, e.fake.Calls("/api/v2/product/add_item"))
	}
}
