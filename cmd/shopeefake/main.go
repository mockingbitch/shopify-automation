// Command shopeefake runs the Shopee test double as a local server, so the
// whole dashboard flow (connect a shop, apply a template, publish products)
// can be tried without a Shopee sandbox account.
//
// LOCAL DEVELOPMENT ONLY. It is not Shopee: its behavior follows the
// documented endpoints as implemented in internal/shopee/shopeefake, and
// nothing tested against it counts as verified against Shopee.
//
//	go run ./cmd/shopeefake            # listens on 127.0.0.1:8099
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"shopee-automation/internal/shopee/shopeefake"
)

// Fixed development credentials: type them into Platform admin → Shopee apps.
const (
	partnerID  = 2009001
	partnerKey = "local-fake-partner-key-not-a-real-shopee-key"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	flag.Parse()
	if os.Getenv("APP_ENV") == "production" {
		fmt.Fprintln(os.Stderr, "shopeefake: refusing to run with APP_ENV=production")
		os.Exit(2)
	}
	s, err := shopeefake.Listen(*addr, partnerID, []byte(partnerKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, "shopeefake:", err)
		os.Exit(1)
	}
	defer s.Close()
	seed(s)

	fmt.Printf(`FAKE Shopee (local development only) on %s

Add it in the dashboard: Platform admin → Shopee apps → new app
  Environment:  sandbox
  Partner ID:   %d
  Partner key:  %s
  API host:     %s
  Auth host:    %s

Test shops: 7001 "Minh An Hà Nội" (VN), 7002 "Minh An Sài Gòn" (VN).
Press Ctrl+C to stop. State is kept in memory only.
`, s.URL, partnerID, partnerKey, s.URL, s.URL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

func seed(s *shopeefake.Server) {
	channels := []shopeefake.Channel{
		{ID: 50011, Name: "Giao Hàng Nhanh", Enabled: true, SupportsCOD: true},
		{ID: 50012, Name: "Giao Hàng Tiết Kiệm", Enabled: false, SupportsCOD: true},
		{ID: 50018, Name: "Hàng cồng kềnh", Enabled: false},
	}
	s.AddShop(shopeefake.Shop{ID: 7001, Name: "Minh An Hà Nội", Region: "VN", Description: "Đồ gia dụng", Channels: channels,
		Addresses: []shopeefake.Address{{ID: 901, Region: "VN", State: "Hà Nội", City: "Hà Nội", District: "Ba Đình", Line: "12 Kim Mã",
			Zipcode: "100000", Types: []string{"DEFAULT_ADDRESS", "PICK_UP_ADDRESS", "RETURN_ADDRESS"}}}})
	s.AddShop(shopeefake.Shop{ID: 7002, Name: "Minh An Sài Gòn", Region: "VN", Description: "", Channels: channels,
		Addresses: []shopeefake.Address{{ID: 902, Region: "VN", State: "Hồ Chí Minh", City: "Hồ Chí Minh", District: "Quận 1", Line: "5 Lê Lợi",
			Zipcode: "700000", Types: []string{"DEFAULT_ADDRESS", "PICK_UP_ADDRESS"}}}})
	s.SetCatalog(shopeefake.Catalog{
		Categories: []map[string]any{
			{"category_id": 100001, "parent_category_id": 0, "original_category_name": "Thời trang nam", "display_category_name": "Thời trang nam", "has_children": true},
			{"category_id": 100017, "parent_category_id": 100001, "original_category_name": "Áo thun", "display_category_name": "Áo thun", "has_children": false},
			{"category_id": 100600, "parent_category_id": 0, "original_category_name": "Nhà cửa & Đời sống", "display_category_name": "Nhà cửa & Đời sống", "has_children": true},
			{"category_id": 100636, "parent_category_id": 100600, "original_category_name": "Dụng cụ nhà bếp", "display_category_name": "Dụng cụ nhà bếp", "has_children": false},
		},
		Attributes: map[int64][]map[string]any{
			100017: {{"attribute_id": 7001, "name": "Chất liệu", "mandatory": true, "attribute_value_list": []any{
				map[string]any{"value_id": 1, "name": "Cotton"}, map[string]any{"value_id": 2, "name": "Polyester"}}}},
			100636: {{"attribute_id": 7101, "name": "Xuất xứ", "mandatory": false, "attribute_value_list": []any{
				map[string]any{"value_id": 11, "name": "Việt Nam"}}}},
		},
		Brands: map[int64][]map[string]any{
			100017: {{"brand_id": 3001, "original_brand_name": "Minh An", "display_brand_name": "Minh An"}},
			100636: {{"brand_id": 3002, "original_brand_name": "Sunhouse", "display_brand_name": "Sunhouse"}},
		},
		Limits: map[string]any{"price_limit": map[string]any{"min_limit": 1000, "max_limit": 120000000},
			"stock_limit": map[string]any{"min_limit": 0, "max_limit": 999999}, "item_name_length_limit": map[string]any{"min_limit": 10, "max_limit": 120},
			"item_image_count_limit": map[string]any{"min_limit": 1, "max_limit": 9}, "item_description_length_limit": map[string]any{"min_limit": 5, "max_limit": 3000}},
		Variations: map[int64][]map[string]any{
			100017: {
				{"variation_id": 1, "variation_name": "Màu sắc", "variation_group_list": []any{map[string]any{"variation_group_id": 11, "variation_group_name": "Cơ bản",
					"variation_option_list": []any{map[string]any{"variation_option_id": 101, "variation_option_name": "Đỏ"},
						map[string]any{"variation_option_id": 102, "variation_option_name": "Xanh"}, map[string]any{"variation_option_id": 103, "variation_option_name": "Trắng"}}}}},
				{"variation_id": 2, "variation_name": "Kích cỡ", "variation_group_list": []any{map[string]any{"variation_group_id": 21, "variation_group_name": "Chữ",
					"variation_option_list": []any{map[string]any{"variation_option_id": 201, "variation_option_name": "S"},
						map[string]any{"variation_option_id": 202, "variation_option_name": "M"}, map[string]any{"variation_option_id": 203, "variation_option_name": "L"}}}}},
			},
		},
	})
}
