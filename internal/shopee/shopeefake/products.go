package shopeefake

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shopee-automation/internal/shopee/signer"
)

// Catalog configures the product side of the fake (categories, attributes,
// brands, limits, standardised variations). Tests set it directly.
type Catalog struct {
	Categories     []map[string]any           // get_category entries
	Attributes     map[int64][]map[string]any // category -> attribute_tree
	Brands         map[int64][]map[string]any // category -> brand_list
	BrandMandatory map[int64]bool
	Limits         map[string]any             // get_item_limit response
	Variations     map[int64][]map[string]any // category -> standardise_variation_list
}

// Item is a created fake item.
type Item struct {
	ID      int64
	ShopID  int64
	Body    map[string]any
	SKU     string
	Models  []map[string]any
	Created time.Time
}

// SetCatalog replaces the product catalog.
func (s *Server) SetCatalog(c Catalog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalog = c
}

// Items returns the items created in a shop.
func (s *Server) Items(shopID int64) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Item
	for _, it := range s.items {
		if it.ShopID == shopID {
			out = append(out, *it)
		}
	}
	return out
}

// LoseNextAddItemResponse makes the next add_item create the item but fail
// with HTTP 500, as if the response was lost (ambiguous outcome).
func (s *Server) LoseNextAddItemResponse() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loseAddItem = true
}

func (s *Server) registerProducts(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/product/get_category", s.shopGET(func(_ int64, _ *http.Request) any {
		return map[string]any{"category_list": s.catalog.Categories}
	}))
	mux.HandleFunc("GET /api/v2/product/get_attribute_tree", s.shopGET(func(_ int64, r *http.Request) any {
		list := []map[string]any{}
		for _, raw := range strings.Split(r.URL.Query().Get("category_id_list"), ",") {
			id, _ := strconv.ParseInt(raw, 10, 64)
			tree := s.catalog.Attributes[id]
			if tree == nil {
				tree = []map[string]any{}
			}
			list = append(list, map[string]any{"category_id": id, "attribute_tree": tree})
		}
		return map[string]any{"list": list}
	}))
	mux.HandleFunc("GET /api/v2/product/get_brand_list", s.shopGET(func(_ int64, r *http.Request) any {
		id, _ := strconv.ParseInt(r.URL.Query().Get("category_id"), 10, 64)
		brands := s.catalog.Brands[id]
		if brands == nil {
			brands = []map[string]any{}
		}
		return map[string]any{"brand_list": brands, "has_next_page": false, "next_offset": 0, "is_mandatory": s.catalog.BrandMandatory[id]}
	}))
	mux.HandleFunc("GET /api/v2/product/get_item_limit", s.shopGET(func(_ int64, _ *http.Request) any { return s.catalog.Limits }))
	mux.HandleFunc("GET /api/v2/product/get_variation_tree", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.begin(w, r, signer.Shop); !ok {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		id, _ := strconv.ParseInt(r.URL.Query().Get("category_id"), 10, 64)
		list := s.catalog.Variations[id]
		if list == nil {
			list = []map[string]any{}
		}
		writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16], "data": map[string]any{"standardise_variation_list": list}})
	})
	mux.HandleFunc("POST /api/v2/media_space/upload_image", s.uploadImage)
	mux.HandleFunc("POST /api/v2/product/add_item", s.addItem)
	mux.HandleFunc("POST /api/v2/product/init_tier_variation", s.initTier)
	mux.HandleFunc("GET /api/v2/product/search_item", s.shopGET(func(shopID int64, r *http.Request) any {
		sku := r.URL.Query().Get("item_sku")
		ids := []int64{}
		for _, it := range s.items {
			if it.ShopID == shopID && it.SKU == sku {
				ids = append(ids, it.ID)
			}
		}
		return map[string]any{"item_id_list": ids, "total_count": len(ids), "next_offset": ""}
	}))
	mux.HandleFunc("GET /api/v2/product/get_item_base_info", s.shopGET(func(shopID int64, r *http.Request) any {
		list := []map[string]any{}
		for _, raw := range strings.Split(r.URL.Query().Get("item_id_list"), ",") {
			id, _ := strconv.ParseInt(raw, 10, 64)
			if it, ok := s.items[id]; ok && it.ShopID == shopID {
				list = append(list, map[string]any{"item_id": it.ID, "category_id": it.Body["category_id"], "item_name": it.Body["item_name"],
					"item_sku": it.SKU, "item_status": "NORMAL", "has_model": len(it.Models) > 0})
			}
		}
		return map[string]any{"item_list": list}
	}))
	mux.HandleFunc("GET /api/v2/product/get_model_list", s.shopGET(func(shopID int64, r *http.Request) any {
		id, _ := strconv.ParseInt(r.URL.Query().Get("item_id"), 10, 64)
		models := []map[string]any{}
		if it, ok := s.items[id]; ok && it.ShopID == shopID {
			models = it.Models
		}
		return map[string]any{"model": models, "tier_variation": []any{}}
	}))
}

// shopGET wraps a read endpoint: signature/token check, then fn under lock.
func (s *Server) shopGET(fn func(shopID int64, r *http.Request) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, shopID, ok := s.begin(w, r, signer.Shop)
		if !ok {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16], "response": fn(shopID, r)})
	}
}

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls[r.URL.Path]++
	f, failing := s.failNext[r.URL.Path]
	delete(s.failNext, r.URL.Path)
	s.mu.Unlock()
	if failing {
		writeErr(w, f.status, f.code, f.message)
		return
	}
	// begin() parses JSON bodies; multipart is checked here instead.
	q := r.URL.Query()
	shopID, _ := strconv.ParseInt(q.Get("shop_id"), 10, 64)
	ts, _ := strconv.ParseInt(q.Get("timestamp"), 10, 64)
	want := signer.Sign(s.PartnerKey, s.PartnerID, r.URL.Path, ts, signer.Shop, q.Get("access_token"), shopID)
	if !signer.Equal(q.Get("sign"), want) {
		writeErr(w, 403, "error_sign", "Wrong sign.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil { //nolint:gosec // G120: body is bounded by MaxBytesReader above
		writeErr(w, 400, "error_param", "There is no image.")
		return
	}
	file, hdr, err := r.FormFile("image")
	if err != nil {
		writeErr(w, 400, "error_param", "There is no image.")
		return
	}
	defer func() { _ = file.Close() }()
	b, _ := io.ReadAll(file)
	ct := hdr.Header.Get("Content-Type")
	if len(b) == 0 || (ct != "image/jpeg" && ct != "image/png") || hdr.Size > 10<<20 {
		writeErr(w, 200, "error_param", "Image format not supported.")
		return
	}
	id := random()
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16], "response": map[string]any{
		"image_info": map[string]any{"image_id": id, "image_url_list": []map[string]any{{"image_url_region": "VN", "image_url": "https://cf.shopee.test/file/" + id}}}}})
}

func (s *Server) addItem(w http.ResponseWriter, r *http.Request) {
	body, shopID, ok := s.begin(w, r, signer.Shop)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range []string{"original_price", "description", "weight", "item_name", "logistic_info", "category_id", "image"} {
		if _, ok := body[k]; !ok {
			writeErr(w, 200, "error_param", "Wrong parameters, detail: "+k+" is required.")
			return
		}
	}
	img, _ := body["image"].(map[string]any)
	if list, _ := img["image_id_list"].([]any); len(list) == 0 {
		writeErr(w, 200, "error_param", "Image not exist.")
		return
	}
	s.nextItemID++
	sku, _ := body["item_sku"].(string)
	it := &Item{ID: 900000 + s.nextItemID, ShopID: shopID, Body: body, SKU: sku, Created: s.Now()}
	s.items[it.ID] = it
	if s.loseAddItem {
		s.loseAddItem = false
		writeErr(w, 500, "error_server", "Something wrong. Please try later.")
		return
	}
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16], "response": map[string]any{"item_id": it.ID}})
}

func (s *Server) initTier(w http.ResponseWriter, r *http.Request) {
	body, shopID, ok := s.begin(w, r, signer.Shop)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := num(body["item_id"])
	it, found := s.items[id]
	if !found || it.ShopID != shopID {
		writeErr(w, 200, "error_item_not_found", "Item_id is not found.")
		return
	}
	if len(it.Models) > 0 {
		writeErr(w, 200, "error_param", "Item already has tier variation.")
		return
	}
	if _, ok := body["standardise_tier_variation"].([]any); !ok {
		writeErr(w, 200, "error_param", "standardise_tier_variation is required.")
		return
	}
	models, _ := body["model"].([]any)
	out := []map[string]any{}
	for i, m := range models {
		mm, _ := m.(map[string]any)
		model := map[string]any{"model_id": 500000 + int64(i) + id*100, "tier_index": mm["tier_index"], "model_sku": mm["model_sku"]}
		out = append(out, model)
	}
	it.Models = out
	raw, _ := json.Marshal(out)
	var resp []any
	_ = json.Unmarshal(raw, &resp)
	writeJSON(w, map[string]any{"error": "", "message": "", "request_id": random()[:16], "response": map[string]any{"item_id": id, "model": resp}})
}
