package products

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RowError is an error on one cell of an import row.
type RowError struct {
	Column  string `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Defaults are the template sections an import applies (optional).
type Defaults struct {
	Region        string
	CategoryByKey map[string]int64
	SKUPrefix     string
	SKUSeparator  string
	SKUDigits     int
	SKUUppercase  bool
	GenerateSKU   bool
	Condition     string
	WeightKg      string
	Package       *Package
	Stock         *int
	Brand         string
	DescHeader    string
	DescBody      string
	DescFooter    string
	Attributes    []defaultAttr
}

type defaultAttr struct {
	CategoryID *int64
	Attribute
}

// DefaultsFromSections reads the template sections an import uses.
func DefaultsFromSections(secs map[string]json.RawMessage) Defaults {
	var d Defaults
	var g struct {
		Region string `json:"region"`
	}
	_ = json.Unmarshal(secs["general"], &g)
	d.Region = g.Region
	var cm struct {
		Mappings []struct {
			Key string `json:"key"`
			ID  int64  `json:"shopee_category_id"`
		} `json:"mappings"`
	}
	_ = json.Unmarshal(secs["category_mappings"], &cm)
	d.CategoryByKey = map[string]int64{}
	for _, m := range cm.Mappings {
		d.CategoryByKey[strings.ToLower(strings.TrimSpace(m.Key))] = m.ID
	}
	var sku struct {
		Prefix    string `json:"prefix"`
		Separator string `json:"separator"`
		Digits    int    `json:"sequence_digits"`
		Uppercase bool   `json:"uppercase"`
		Generate  bool   `json:"generate_when_missing"`
	}
	_ = json.Unmarshal(secs["sku_rules"], &sku)
	d.SKUPrefix, d.SKUSeparator, d.SKUDigits, d.SKUUppercase, d.GenerateSKU = sku.Prefix, sku.Separator, sku.Digits, sku.Uppercase, sku.Generate
	var pd struct {
		Condition string   `json:"condition"`
		Weight    *float64 `json:"weight_kg"`
		Package   *Package `json:"package_cm"`
		Stock     *int     `json:"stock"`
		Brand     *struct {
			Name string `json:"original_brand_name"`
		} `json:"brand"`
	}
	_ = json.Unmarshal(secs["product_defaults"], &pd)
	d.Condition, d.Package, d.Stock = pd.Condition, pd.Package, pd.Stock
	if pd.Weight != nil {
		d.WeightKg = strconv.FormatFloat(*pd.Weight, 'f', -1, 64)
	}
	if pd.Brand != nil {
		d.Brand = pd.Brand.Name
	}
	var dd struct {
		Header string `json:"header"`
		Body   string `json:"body_template"`
		Footer string `json:"footer"`
	}
	_ = json.Unmarshal(secs["default_descriptions"], &dd)
	d.DescHeader, d.DescBody, d.DescFooter = dd.Header, dd.Body, dd.Footer
	var da struct {
		Attributes []struct {
			CategoryID *int64 `json:"category_id"`
			Attribute
		} `json:"attributes"`
	}
	_ = json.Unmarshal(secs["default_attributes"], &da)
	for _, a := range da.Attributes {
		d.Attributes = append(d.Attributes, defaultAttr{CategoryID: a.CategoryID, Attribute: a.Attribute})
	}
	return d
}

// Group is the rows of one product (same SKU).
type Group struct {
	SKU       string
	Rows      []int // indexes into the row slice
	Input     Input
	ImageURLs []string
	Errors    map[int][]RowError // row index -> errors
}

var splitURLs = regexp.MustCompile(`[|\s]+`)

// BuildGroups applies defaults, groups rows by SKU and validates each
// group. Row errors are keyed by row index.
func BuildGroups(rows []RawRow, d Defaults) []*Group {
	// 1. Generate missing SKUs, in row order.
	seq := 0
	eff := make([]RawRow, len(rows))
	for i, r := range rows {
		c := RawRow{}
		for k, v := range r {
			c[k] = v
		}
		if c["sku"] == "" && d.GenerateSKU && d.SKUDigits > 0 {
			seq++
			prefix := d.SKUPrefix
			if d.SKUUppercase {
				prefix = strings.ToUpper(prefix)
			}
			c["sku"] = fmt.Sprintf("%s%s%0*d", prefix, d.SKUSeparator, d.SKUDigits, seq)
		}
		eff[i] = c
	}
	// 2. Group by SKU (case-insensitive), keeping first-seen order.
	var groups []*Group
	byKey := map[string]*Group{}
	for i, r := range eff {
		key := strings.ToLower(r["sku"])
		if key == "" {
			key = fmt.Sprintf("\x00row%d", i) // rows without SKU stay alone and fail
		}
		g, ok := byKey[key]
		if !ok {
			g = &Group{SKU: r["sku"], Errors: map[int][]RowError{}}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.Rows = append(g.Rows, i)
	}
	for _, g := range groups {
		buildGroup(g, eff, d)
	}
	return groups
}

func first(rows []RawRow, idx []int, col string) string {
	for _, i := range idx {
		if v := rows[i][col]; v != "" {
			return v
		}
	}
	return ""
}

func buildGroup(g *Group, rows []RawRow, d Defaults) {
	errf := func(row int, col, code, msg string) {
		g.Errors[row] = append(g.Errors[row], RowError{Column: col, Code: code, Message: msg})
	}
	head := g.Rows[0]
	in := Input{SKU: g.SKU, Name: first(rows, g.Rows, "name"), Currency: first(rows, g.Rows, "currency")}
	if in.Currency == "" && d.Region != "" {
		in.Currency = CurrencyFor(d.Region)
	}
	// Category: numeric ID or a label mapped by the template.
	if cat := first(rows, g.Rows, "category"); cat != "" {
		if id, err := strconv.ParseInt(cat, 10, 64); err == nil {
			s := strconv.FormatInt(id, 10)
			in.ShopeeCategoryID = &s
		} else {
			label := cat
			in.CategoryLabel = &label
			if id, ok := d.CategoryByKey[strings.ToLower(cat)]; ok {
				s := strconv.FormatInt(id, 10)
				in.ShopeeCategoryID = &s
			} else {
				errf(head, "category", "unknown_category", fmt.Sprintf("Unknown category %q. Use a Shopee category ID or add it to the template's category mappings.", cat))
			}
		}
	} else {
		errf(head, "category", "required", "Category is required.")
	}
	brand := first(rows, g.Rows, "brand")
	if brand == "" {
		brand = d.Brand
	}
	if brand != "" {
		in.BrandName = &brand
	}
	cond := first(rows, g.Rows, "condition")
	if cond == "" {
		cond = d.Condition
	}
	if cond != "" {
		in.Condition = &cond
	}
	w := first(rows, g.Rows, "weight_kg")
	if w == "" {
		w = d.WeightKg
	}
	if w != "" {
		in.WeightKg = &w
	} else {
		errf(head, "weight_kg", "required", "Weight is required (Shopee needs it to create the product).")
	}
	l, wd, h := first(rows, g.Rows, "length_cm"), first(rows, g.Rows, "width_cm"), first(rows, g.Rows, "height_cm")
	switch {
	case l != "" || wd != "" || h != "":
		li, e1 := strconv.Atoi(l)
		wi, e2 := strconv.Atoi(wd)
		hi, e3 := strconv.Atoi(h)
		if e1 != nil || e2 != nil || e3 != nil {
			errf(head, "length_cm", "invalid", "Length, width and height must all be whole centimetres.")
		} else {
			in.PackageCm = &Package{Length: li, Width: wi, Height: hi}
		}
	case d.Package != nil:
		p := *d.Package
		in.PackageCm = &p
	}
	if dts := first(rows, g.Rows, "days_to_ship"); dts != "" {
		if v, err := strconv.Atoi(dts); err == nil {
			in.DaysToShip = &v
		} else {
			errf(head, "days_to_ship", "invalid", "Days to ship must be a whole number.")
		}
	}
	// Description with template header/body/footer.
	desc := first(rows, g.Rows, "description")
	if desc == "" && d.DescBody != "" {
		desc = strings.ReplaceAll(d.DescBody, "{name}", in.Name)
	}
	var parts []string
	for _, p := range []string{strings.ReplaceAll(d.DescHeader, "{name}", in.Name), desc, strings.ReplaceAll(d.DescFooter, "{name}", in.Name)} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	in.Description = strings.Join(parts, "\n\n")
	if strings.TrimSpace(desc) == "" {
		errf(head, "description", "required", "Description is required (Shopee needs it to create the product).")
	}
	// Default attributes for this category.
	if in.ShopeeCategoryID != nil {
		cid, _ := strconv.ParseInt(*in.ShopeeCategoryID, 10, 64)
		for _, a := range d.Attributes {
			if a.CategoryID == nil || *a.CategoryID == cid {
				in.Attributes = append(in.Attributes, a.Attribute)
			}
		}
	}
	// Images.
	if urls := first(rows, g.Rows, "image_urls"); urls != "" {
		for _, u := range splitURLs.Split(urls, -1) {
			if u != "" {
				g.ImageURLs = append(g.ImageURLs, u)
			}
		}
	}
	switch {
	case len(g.ImageURLs) == 0:
		errf(head, "image_urls", "required", "At least one image URL is required.")
	case len(g.ImageURLs) > maxImageURLs:
		errf(head, "image_urls", "too_many", "At most 20 images per product.")
	default:
		for _, u := range g.ImageURLs {
			pu, err := url.Parse(u)
			if err != nil || pu.Scheme != "https" || pu.Host == "" {
				errf(head, "image_urls", "invalid_url", "Image URLs must be https URLs: "+truncateStr(u, 80))
				break
			}
		}
	}
	// Variations.
	t1, t2 := first(rows, g.Rows, "tier1_name"), first(rows, g.Rows, "tier2_name")
	stockOf := func(i int) (int, bool) {
		v := rows[i]["stock"]
		if v == "" && d.Stock != nil {
			return *d.Stock, true
		}
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	if t1 == "" {
		if len(g.Rows) > 1 {
			for _, i := range g.Rows[1:] {
				errf(i, "sku", "duplicate", fmt.Sprintf("SKU %q appears on several rows. Rows of one product need variation columns (tier1_name, tier1_option).", g.SKU))
			}
		}
		in.Price = rows[head]["price"]
		if n, ok := stockOf(head); ok {
			in.Stock = n
		} else {
			errf(head, "stock", "invalid", "Stock must be a whole number.")
		}
	} else {
		in.Tiers = []Tier{{Name: t1}}
		if t2 != "" {
			in.Tiers = append(in.Tiers, Tier{Name: t2})
		}
		optIndex := func(t int, opt string) int {
			for j, o := range in.Tiers[t].Options {
				if strings.EqualFold(o, opt) {
					return j
				}
			}
			in.Tiers[t].Options = append(in.Tiers[t].Options, opt)
			return len(in.Tiers[t].Options) - 1
		}
		minPrice := ""
		total := 0
		for n, i := range g.Rows {
			r := rows[i]
			if !strings.EqualFold(r["tier1_name"], t1) && r["tier1_name"] != "" || !strings.EqualFold(r["tier2_name"], t2) && r["tier2_name"] != "" {
				errf(i, "tier1_name", "mismatch", "All rows of a product must use the same variation names.")
			}
			if r["tier1_option"] == "" || (t2 != "" && r["tier2_option"] == "") {
				errf(i, "tier1_option", "required", "Each variation row needs its options.")
				continue
			}
			v := VariationInput{TierIndex: []int{optIndex(0, r["tier1_option"])}, Price: r["price"]}
			if t2 != "" {
				v.TierIndex = append(v.TierIndex, optIndex(1, r["tier2_option"]))
			}
			if vs := r["variation_sku"]; vs != "" {
				v.SKU = &vs
			}
			if st, ok := stockOf(i); ok {
				v.Stock = st
				total += st
			} else {
				errf(i, "stock", "invalid", "Stock must be a whole number.")
			}
			in.Variations = append(in.Variations, v)
			if validPrice(r["price"]) && (minPrice == "" || lessDecimal(r["price"], minPrice)) {
				minPrice = r["price"]
			}
			_ = n
		}
		in.Price, in.Stock = minPrice, total
		if minPrice == "" {
			in.Price = rows[head]["price"]
		}
	}
	// Generic validation, mapped back to columns and rows.
	for _, fe := range Validate(&in) {
		row, col := head, columnFor(fe.Field)
		if strings.HasPrefix(fe.Field, "variations.") {
			var n int
			if _, err := fmt.Sscanf(fe.Field, "variations.%d", &n); err == nil && n < len(g.Rows) {
				row = g.Rows[n]
			}
		}
		errf(row, col, fe.Code, fe.Message)
	}
	g.Input = in
	// A product is all or nothing: mark the other rows.
	if len(g.Errors) > 0 {
		var bad []int
		for i := range g.Errors {
			bad = append(bad, i)
		}
		sort.Ints(bad)
		for _, i := range g.Rows {
			if _, ok := g.Errors[i]; !ok {
				errf(i, "sku", "group_invalid", fmt.Sprintf("Another row of product %s has errors.", g.SKU))
			}
		}
	}
}

func columnFor(field string) string {
	switch {
	case field == "shopee_category_id":
		return "category"
	case field == "brand_name":
		return "brand"
	case strings.HasPrefix(field, "package_cm."):
		return strings.TrimPrefix(field, "package_cm.") + "_cm"
	case strings.HasPrefix(field, "tiers.1"):
		return "tier2_option"
	case strings.HasPrefix(field, "tiers."):
		return "tier1_option"
	case strings.HasSuffix(field, ".sku") && strings.HasPrefix(field, "variations."):
		return "variation_sku"
	case strings.HasSuffix(field, ".price"):
		return "price"
	case strings.HasSuffix(field, ".stock"):
		return "stock"
	case strings.HasPrefix(field, "variations"):
		return "tier1_option"
	case strings.HasPrefix(field, "attributes"):
		return "category"
	}
	return field
}

// lessDecimal compares two validated decimal strings.
func lessDecimal(a, b string) bool {
	fa, _ := strconv.ParseFloat(a, 64)
	fb, _ := strconv.ParseFloat(b, 64)
	return fa < fb
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
