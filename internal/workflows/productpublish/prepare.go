// Package productpublish is the PRODUCT_PUBLISH workflow: create products on
// one Shopee shop without ever creating a duplicate (workflow-design.md §5).
package productpublish

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"shopee-automation/internal/products"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/workflow"
	"shopee-automation/internal/workflows/shopstep"
)

// Plan is what prepare decided for one product.
type Plan struct {
	ProductID  uuid.UUID      `json:"product_id"`
	SKU        string         `json:"sku"`
	Name       string         `json:"name"`
	Price      string         `json:"price"`
	Currency   string         `json:"currency"`
	CategoryID int64          `json:"category_id"`
	OK         bool           `json:"ok"`
	Problems   []string       `json:"problems"`
	Warnings   []string       `json:"warnings"`
	Brand      map[string]any `json:"brand,omitempty"`
	Attributes []any          `json:"attribute_list,omitempty"`
	Logistics  []any          `json:"logistic_info,omitempty"`
	StdTiers   []any          `json:"standardise_tier_variation,omitempty"`
}

// Prepared is the output of the prepare step.
type Prepared struct {
	Region string `json:"region"`
	Plans  []Plan `json:"plans"`
}

type categoryInfo struct {
	exists, leaf bool
	attrs        []shopee.Attribute
	limit        shopee.ItemLimit
	brands       shopee.BrandPage
	variations   []shopee.StdVariation
	loaded       bool
	err          error
}

// maxBrandPages bounds brand lookups (100 brands per page).
const maxBrandPages = 10

func (w *Workflow) prepare(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	in, err := w.input(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	items, err := w.Products.LoadForPublish(ctx, sc.OrgID, in.ProductIDs)
	if err != nil {
		return workflow.Retryable(err)
	}
	var out Prepared
	err = shopstep.WithAccess(ctx, w.Shops, sc.OrgID, sc.ShopID, func(acc shops.Access) error {
		out = Prepared{Region: acc.Region, Plans: nil}
		cats, err := w.Shops.Shopee.GetCategories(ctx, acc.App, acc.AccessToken, acc.ShopID)
		if err != nil {
			return err
		}
		catByID := map[int64]shopee.Category{}
		for _, c := range cats {
			catByID[c.ID] = c
		}
		channels, err := w.Shops.Shopee.GetChannelList(ctx, acc.App, acc.AccessToken, acc.ShopID)
		if err != nil {
			return err
		}
		info := map[int64]*categoryInfo{}
		load := func(id int64, needTiers bool) *categoryInfo {
			ci := info[id]
			if ci == nil {
				c, ok := catByID[id]
				ci = &categoryInfo{exists: ok, leaf: ok && !c.HasChildren}
				info[id] = ci
			}
			if !ci.exists || !ci.leaf || ci.err != nil {
				return ci
			}
			if !ci.loaded {
				ci.loaded = true
				tree, err := w.Shops.Shopee.GetAttributeTree(ctx, acc.App, acc.AccessToken, acc.ShopID, []int64{id})
				if err != nil {
					ci.err = err
					return ci
				}
				ci.attrs = tree[id]
				if ci.limit, err = w.Shops.Shopee.GetItemLimit(ctx, acc.App, acc.AccessToken, acc.ShopID, id); err != nil {
					ci.err = err
					return ci
				}
				var offset int64
				for page := 0; page < maxBrandPages; page++ {
					bp, err := w.Shops.Shopee.GetBrandList(ctx, acc.App, acc.AccessToken, acc.ShopID, id, offset)
					if err != nil {
						ci.err = err
						return ci
					}
					ci.brands.IsMandatory = bp.IsMandatory
					ci.brands.Brands = append(ci.brands.Brands, bp.Brands...)
					if !bp.HasNextPage {
						break
					}
					offset = bp.NextOffset
				}
			}
			if needTiers && ci.variations == nil {
				v, err := w.Shops.Shopee.GetVariationTree(ctx, acc.App, acc.AccessToken, acc.ShopID, id)
				if err != nil {
					ci.err = err
					return ci
				}
				if v == nil {
					v = []shopee.StdVariation{}
				}
				ci.variations = v
			}
			return ci
		}
		for _, it := range items {
			p := plan(it, acc.Region, channels, load, w.ItemStatus)
			out.Plans = append(out.Plans, p)
		}
		// A transport error on a category is a step failure (retried), not a
		// product problem.
		for _, ci := range info {
			if ci.err != nil {
				return ci.err
			}
		}
		return nil
	})
	if err != nil {
		return shopstep.FromErr(err)
	}
	found := map[uuid.UUID]bool{}
	for _, p := range out.Plans {
		found[p.ProductID] = true
	}
	for _, id := range in.ProductIDs {
		if !found[id] {
			out.Plans = append(out.Plans, Plan{ProductID: id, Problems: []string{"Sản phẩm đã bị xóa."}})
		}
	}
	sort.SliceStable(out.Plans, func(i, j int) bool { return out.Plans[i].SKU < out.Plans[j].SKU })
	return workflow.Succeeded(out)
}

func plan(it products.PublishItem, region string, channels []shopee.Channel, load func(int64, bool) *categoryInfo, _ string) Plan {
	pr := it.Product
	p := Plan{ProductID: pr.ID, SKU: pr.SKU, Name: pr.Name, Price: pr.Price.Amount, Currency: pr.Price.Currency, Problems: []string{}, Warnings: []string{}}
	bad := func(format string, a ...any) { p.Problems = append(p.Problems, fmt.Sprintf(format, a...)) }

	if want := products.CurrencyFor(region); want == "" || want != pr.Price.Currency {
		bad("Giá đang tính bằng %s nhưng thị trường của shop (%s) dùng %s.", pr.Price.Currency, region, want)
	}
	if pr.WeightKg == nil {
		bad("Thiếu cân nặng.")
	}
	if strings.TrimSpace(pr.Description) == "" {
		bad("Thiếu mô tả.")
	}
	if len(it.Images) == 0 {
		bad("Sản phẩm chưa có hình ảnh.")
	}
	var logistics []any
	for _, ch := range channels {
		// SIZE_SELECTION channels need a size_id per item, which products do
		// not carry yet; they are left out.
		if ch.Enabled && ch.FeeType != "SIZE_SELECTION" {
			logistics = append(logistics, map[string]any{"logistic_id": ch.ID, "enabled": true})
		}
	}
	if len(logistics) == 0 {
		bad("Shop chưa bật đơn vị vận chuyển nào dùng được cho sản phẩm.")
	}
	p.Logistics = logistics

	if pr.ShopeeCategoryID == nil {
		bad("Thiếu ngành hàng Shopee.")
		p.OK = len(p.Problems) == 0
		return p
	}
	cid, _ := strconv.ParseInt(*pr.ShopeeCategoryID, 10, 64)
	p.CategoryID = cid
	ci := load(cid, len(pr.Tiers) > 0)
	switch {
	case ci.err != nil:
		return p // reported by the caller as a step failure
	case !ci.exists:
		bad("Ngành hàng %d không có ở thị trường của shop này.", cid)
	case !ci.leaf:
		bad("Ngành hàng %d chưa phải ngành hàng cấp cuối; hãy chọn ngành hàng cụ thể hơn.", cid)
	default:
		checkLimits(&p, pr, len(it.Images), ci.limit, bad)
		checkAttributes(&p, pr, ci.attrs, bad)
		checkBrand(&p, pr, ci.brands, bad)
		if len(pr.Tiers) > 0 {
			checkTiers(&p, pr, ci.variations, bad)
		}
	}
	p.OK = len(p.Problems) == 0
	return p
}

func checkLimits(p *Plan, pr products.Product, images int, l shopee.ItemLimit, bad func(string, ...any)) {
	inRange := func(v float64, r shopee.Range) bool {
		return (r.Min == 0 || v >= r.Min) && (r.Max == 0 || v <= r.Max)
	}
	if n := float64(utf8.RuneCountInString(pr.Name)); !inRange(n, l.NameLength) {
		bad("Tên có %d ký tự; Shopee cho phép từ %g đến %g ký tự ở ngành hàng này.", int(n), l.NameLength.Min, l.NameLength.Max)
	}
	if n := float64(utf8.RuneCountInString(pr.Description)); !inRange(n, l.DescLength) {
		bad("Mô tả có %d ký tự; Shopee cho phép từ %g đến %g ký tự ở ngành hàng này.", int(n), l.DescLength.Min, l.DescLength.Max)
	}
	if !inRange(float64(images), l.ImageCount) {
		bad("Sản phẩm có %d hình ảnh; Shopee cho phép từ %g đến %g.", images, l.ImageCount.Min, l.ImageCount.Max)
	}
	prices := []string{pr.Price.Amount}
	for _, v := range pr.Variations {
		prices = append(prices, v.Price.Amount)
	}
	for _, s := range prices {
		f, _ := strconv.ParseFloat(s, 64)
		if !inRange(f, l.Price) {
			bad("Giá %s nằm ngoài khoảng Shopee cho phép (%g đến %g).", s, l.Price.Min, l.Price.Max)
			break
		}
	}
	if !inRange(float64(pr.Stock), l.Stock) {
		bad("Tồn kho %d nằm ngoài khoảng Shopee cho phép (%g đến %g).", pr.Stock, l.Stock.Min, l.Stock.Max)
	}
}

func checkAttributes(p *Plan, pr products.Product, tree []shopee.Attribute, bad func(string, ...any)) {
	have := map[int64]products.Attribute{}
	for _, a := range pr.Attributes {
		have[a.AttributeID] = a
	}
	known := map[int64]bool{}
	var missing []string
	for _, a := range tree {
		known[a.ID] = true
		if a.Mandatory {
			if _, ok := have[a.ID]; !ok {
				missing = append(missing, fmt.Sprintf("%s (%d)", a.Name, a.ID))
			}
		}
	}
	if len(missing) > 0 {
		bad("Thiếu thuộc tính bắt buộc: %s.", strings.Join(missing, ", "))
	}
	var list []any
	for _, a := range pr.Attributes {
		if !known[a.AttributeID] {
			p.Warnings = append(p.Warnings, fmt.Sprintf("Thuộc tính %d không dùng cho ngành hàng này nên sẽ được bỏ qua.", a.AttributeID))
			continue
		}
		v := map[string]any{"value_id": a.ValueID}
		if a.OriginalValueName != "" {
			v["original_value_name"] = a.OriginalValueName
		}
		if a.ValueUnit != "" {
			v["value_unit"] = a.ValueUnit
		}
		list = append(list, map[string]any{"attribute_id": a.AttributeID, "attribute_value_list": []any{v}})
	}
	p.Attributes = list
}

func checkBrand(p *Plan, pr products.Product, b shopee.BrandPage, bad func(string, ...any)) {
	if pr.BrandName != nil && !strings.EqualFold(*pr.BrandName, "No Brand") {
		for _, br := range b.Brands {
			if strings.EqualFold(br.Name, *pr.BrandName) || strings.EqualFold(br.DisplayName, *pr.BrandName) {
				p.Brand = map[string]any{"brand_id": br.ID, "original_brand_name": br.Name}
				return
			}
		}
		if b.IsMandatory {
			bad("Thương hiệu %q không có trong danh sách thương hiệu Shopee của ngành hàng này, mà ngành hàng này bắt buộc có thương hiệu.", *pr.BrandName)
			return
		}
		p.Warnings = append(p.Warnings, fmt.Sprintf("Thương hiệu %q không có trong danh sách của Shopee; sản phẩm sẽ được tạo với \"No Brand\" (không thương hiệu).", *pr.BrandName))
	} else if b.IsMandatory {
		bad("Ngành hàng này bắt buộc có thương hiệu.")
		return
	}
	// add_item: brand_id 0 with "No Brand" when the product has no brand.
	p.Brand = map[string]any{"brand_id": 0, "original_brand_name": "No Brand"}
}

// checkTiers maps the product's tiers to Shopee's standardised variations.
// Since 2025-09-12 the custom tier_variation structure is deprecated, so
// every tier and option must match the category's variation tree exactly.
func checkTiers(p *Plan, pr products.Product, tree []shopee.StdVariation, bad func(string, ...any)) {
	var std []any
	for _, t := range pr.Tiers {
		var v *shopee.StdVariation
		var names []string
		for i := range tree {
			names = append(names, tree[i].Name)
			if strings.EqualFold(tree[i].Name, t.Name) {
				v = &tree[i]
			}
		}
		if v == nil {
			bad("Nhóm phân loại %q không thuộc các nhóm phân loại Shopee của ngành hàng này (%s).", t.Name, listOf(names))
			continue
		}
		var opts []any
		var groupID int64
		for _, o := range t.Options {
			found := false
			var available []string
			for _, g := range v.Groups {
				for _, so := range g.Options {
					available = append(available, so.Name)
					if !found && strings.EqualFold(so.Name, o) {
						found = true
						groupID = g.ID
						opts = append(opts, map[string]any{"variation_option_id": so.ID, "variation_option_name": so.Name})
					}
				}
			}
			if !found {
				bad("Giá trị %q của %q không thuộc các giá trị Shopee cho phép (%s).", o, t.Name, listOf(available))
			}
		}
		entry := map[string]any{"variation_id": v.ID, "variation_name": v.Name, "variation_option_list": opts}
		if groupID != 0 {
			entry["variation_group_id"] = groupID
		}
		std = append(std, entry)
	}
	p.StdTiers = std
}

func listOf(names []string) string {
	if len(names) == 0 {
		return "không có"
	}
	if len(names) > 15 {
		return strings.Join(names[:15], ", ") + ", …"
	}
	return strings.Join(names, ", ")
}
