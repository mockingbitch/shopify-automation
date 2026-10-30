// Package products owns the product catalog (products, variations, images,
// per-shop mappings) and product imports.
package products

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"shopee-automation/internal/platform/apperr"
)

// Launch-market currencies (assumption A-2). A product is published only to
// shops whose market uses its currency.
var regionCurrency = map[string]string{
	"VN": "VND", "SG": "SGD", "MY": "MYR", "TH": "THB", "PH": "PHP", "ID": "IDR", "TW": "TWD",
}

// CurrencyFor returns the currency of a Shopee region, or "".
func CurrencyFor(region string) string { return regionCurrency[region] }

func validCurrency(c string) bool {
	for _, v := range regionCurrency {
		if v == c {
			return true
		}
	}
	return false
}

// Internal limits. Shopee's own limits come from get_item_limit per
// category and are enforced when publishing.
const (
	maxNameLen       = 255
	maxDescLen       = 10000
	maxSKULen        = 100 // model_sku is documented as ≤ 100; item_sku uses the same bound
	maxStock         = 10_000_000
	maxAttributes    = 100
	maxTiers         = 2
	maxOptions       = 50 // "Count of tier_variation options should be under 50"
	maxModels        = 50 // init_tier_variation: "model number at most 50"
	maxTierNameLen   = 50
	maxOptionNameLen = 50
	maxImages        = 20         // assumption A-7
	maxCategoryID    = 2147483647 // add_item category_id is int32
)

var (
	priceRe  = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,2})?$`)
	weightRe = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,3})?$`)
)

// Attribute is a product attribute value (add_item attribute_list).
type Attribute struct {
	AttributeID       int64  `json:"attribute_id"`
	ValueID           int64  `json:"value_id"`
	OriginalValueName string `json:"original_value_name,omitempty"`
	ValueUnit         string `json:"value_unit,omitempty"`
}

// Package is the parcel size in whole centimetres (add_item dimension).
type Package struct {
	Length int `json:"length"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Tier is one variation tier (e.g. Color: Red, Blue).
type Tier struct {
	Name    string   `json:"name"`
	Options []string `json:"options"`
}

// VariationInput is one model of a product with tiers.
type VariationInput struct {
	TierIndex []int   `json:"tier_index"`
	SKU       *string `json:"sku"`
	Price     string  `json:"price"`
	Stock     int     `json:"stock"`
}

// Input is a product as created or replaced by users and imports.
type Input struct {
	SKU              string           `json:"sku"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	ShopeeCategoryID *string          `json:"shopee_category_id"`
	CategoryLabel    *string          `json:"category_label"`
	BrandName        *string          `json:"brand_name"`
	Condition        *string          `json:"condition"`
	Attributes       []Attribute      `json:"attributes"`
	Price            string           `json:"price"`
	Currency         string           `json:"currency"`
	Stock            int              `json:"stock"`
	WeightKg         *string          `json:"weight_kg"`
	PackageCm        *Package         `json:"package_cm"`
	DaysToShip       *int             `json:"days_to_ship"`
	Tiers            []Tier           `json:"tiers"`
	Variations       []VariationInput `json:"variations"`
}

func (in *Input) normalize() {
	in.SKU = strings.TrimSpace(in.SKU)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	trim := func(p **string) {
		if *p != nil {
			v := strings.TrimSpace(**p)
			if v == "" {
				*p = nil
			} else {
				*p = &v
			}
		}
	}
	trim(&in.ShopeeCategoryID)
	trim(&in.CategoryLabel)
	trim(&in.BrandName)
	trim(&in.Condition)
	trim(&in.WeightKg)
	if in.Condition != nil {
		c := strings.ToUpper(*in.Condition)
		in.Condition = &c
	}
	for i := range in.Tiers {
		in.Tiers[i].Name = strings.TrimSpace(in.Tiers[i].Name)
		for j := range in.Tiers[i].Options {
			in.Tiers[i].Options[j] = strings.TrimSpace(in.Tiers[i].Options[j])
		}
	}
	for i := range in.Variations {
		trim(&in.Variations[i].SKU)
		in.Variations[i].Price = strings.TrimSpace(in.Variations[i].Price)
	}
}

// Validate checks a product. Field names match the JSON input.
func Validate(in *Input) []apperr.FieldError {
	in.normalize()
	var errs []apperr.FieldError
	add := func(field, code, msg string) {
		errs = append(errs, apperr.FieldError{Field: field, Code: code, Message: msg})
	}

	if in.SKU == "" {
		add("sku", "required", "SKU is required.")
	} else if utf8.RuneCountInString(in.SKU) > maxSKULen || strings.IndexFunc(in.SKU, unicode.IsSpace) >= 0 || strings.IndexFunc(in.SKU, unicode.IsControl) >= 0 {
		add("sku", "invalid", fmt.Sprintf("SKU must be 1 to %d characters without spaces.", maxSKULen))
	}
	if in.Name == "" {
		add("name", "required", "Name is required.")
	} else if utf8.RuneCountInString(in.Name) > maxNameLen {
		add("name", "too_long", fmt.Sprintf("Name must be at most %d characters.", maxNameLen))
	}
	if utf8.RuneCountInString(in.Description) > maxDescLen {
		add("description", "too_long", fmt.Sprintf("Description must be at most %d characters.", maxDescLen))
	}
	if in.ShopeeCategoryID != nil {
		if id, err := strconv.ParseInt(*in.ShopeeCategoryID, 10, 64); err != nil || id <= 0 || id > maxCategoryID {
			add("shopee_category_id", "invalid", "Category ID must be a positive number.")
		}
	}
	if in.Condition != nil && *in.Condition != "NEW" && *in.Condition != "USED" {
		add("condition", "invalid", "Condition must be NEW or USED.")
	}
	if !validPrice(in.Price) {
		add("price", "invalid", "Price must be a positive number with at most 2 decimals.")
	}
	if !validCurrency(in.Currency) {
		add("currency", "invalid", "Currency must be one of VND, SGD, MYR, THB, PHP, IDR, TWD.")
	}
	if in.Stock < 0 || in.Stock > maxStock {
		add("stock", "invalid", "Stock must be between 0 and 10,000,000.")
	}
	if in.WeightKg != nil && (!weightRe.MatchString(*in.WeightKg) || isZero(*in.WeightKg)) {
		add("weight_kg", "invalid", "Weight must be a positive number of kilograms with at most 3 decimals.")
	}
	if p := in.PackageCm; p != nil {
		for name, v := range map[string]int{"length": p.Length, "width": p.Width, "height": p.Height} {
			if v < 1 || v > 1000 {
				add("package_cm."+name, "invalid", "Package sizes must be whole centimetres between 1 and 1000.")
			}
		}
	}
	if in.DaysToShip != nil && (*in.DaysToShip < 1 || *in.DaysToShip > 90) {
		add("days_to_ship", "invalid", "Days to ship must be between 1 and 90.")
	}
	if len(in.Attributes) > maxAttributes {
		add("attributes", "too_many", "At most 100 attributes.")
	}
	for i, a := range in.Attributes {
		f := fmt.Sprintf("attributes.%d", i)
		if a.AttributeID <= 0 || a.AttributeID > maxCategoryID || a.ValueID < 0 {
			add(f, "invalid", "Attribute and value IDs must be positive numbers (value 0 for a custom value).")
		} else if a.ValueID == 0 && strings.TrimSpace(a.OriginalValueName) == "" {
			add(f+".original_value_name", "required", "A custom value (value_id 0) needs a value name.")
		}
	}
	validateTiers(in, add)
	return errs
}

func validateTiers(in *Input, add func(field, code, msg string)) {
	if len(in.Tiers) == 0 {
		if len(in.Variations) > 0 {
			add("variations", "invalid", "Variations need tiers (for example Color and Size).")
		}
		return
	}
	if len(in.Tiers) > maxTiers {
		add("tiers", "too_many", "At most 2 variation tiers.")
		return
	}
	for i, t := range in.Tiers {
		f := fmt.Sprintf("tiers.%d", i)
		if t.Name == "" || utf8.RuneCountInString(t.Name) > maxTierNameLen {
			add(f+".name", "invalid", "Tier name must be 1 to 50 characters.")
		}
		if len(t.Options) == 0 || len(t.Options) > maxOptions {
			add(f+".options", "invalid", "Each tier needs 1 to 50 options.")
		}
		seen := map[string]bool{}
		for j, o := range t.Options {
			k := strings.ToLower(o)
			if o == "" || utf8.RuneCountInString(o) > maxOptionNameLen {
				add(fmt.Sprintf("%s.options.%d", f, j), "invalid", "Option must be 1 to 50 characters.")
			} else if seen[k] {
				add(fmt.Sprintf("%s.options.%d", f, j), "duplicate", "Options of a tier must be unique.")
			}
			seen[k] = true
		}
	}
	if len(in.Variations) == 0 {
		add("variations", "required", "A product with tiers needs at least one variation.")
		return
	}
	if len(in.Variations) > maxModels {
		add("variations", "too_many", "At most 50 variations.")
	}
	combos := map[string]bool{}
	skus := map[string]bool{}
	for i, v := range in.Variations {
		f := fmt.Sprintf("variations.%d", i)
		if len(v.TierIndex) != len(in.Tiers) {
			add(f+".tier_index", "invalid", "Each variation needs one option per tier.")
			continue
		}
		for t, idx := range v.TierIndex {
			if idx < 0 || idx >= len(in.Tiers[t].Options) {
				add(f+".tier_index", "invalid", "Option index out of range.")
			}
		}
		key := fmt.Sprint(v.TierIndex)
		if combos[key] {
			add(f+".tier_index", "duplicate", "Two variations have the same options.")
		}
		combos[key] = true
		if !validPrice(v.Price) {
			add(f+".price", "invalid", "Price must be a positive number with at most 2 decimals.")
		}
		if v.Stock < 0 || v.Stock > maxStock {
			add(f+".stock", "invalid", "Stock must be between 0 and 10,000,000.")
		}
		if v.SKU != nil {
			if utf8.RuneCountInString(*v.SKU) > maxSKULen || strings.IndexFunc(*v.SKU, unicode.IsSpace) >= 0 {
				add(f+".sku", "invalid", "Variation SKU must be 1 to 100 characters without spaces.")
			} else if skus[strings.ToLower(*v.SKU)] {
				add(f+".sku", "duplicate", "Variation SKUs must be unique.")
			}
			skus[strings.ToLower(*v.SKU)] = true
		}
	}
}

func validPrice(p string) bool { return priceRe.MatchString(p) && !isZero(p) }

func isZero(s string) bool { return strings.Trim(s, "0.") == "" }
