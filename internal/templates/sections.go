// Package templates owns shop templates: versions, sections, validation,
// shop overrides and the field-level diff used for previews.
package templates

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"shopee-automation/internal/platform/apperr"
)

//go:embed schema/*.json
var schemaFS embed.FS

// SectionKeys lists the sections in display order (FR-TPL-1).
var SectionKeys = []string{
	"general", "branding", "description", "contact", "warehouse", "return_address",
	"logistics", "product_defaults", "category_mappings", "sku_rules",
	"default_descriptions", "default_attributes",
}

// SectionSchema is one section's JSON Schema plus metadata used by the diff.
type SectionSchema struct {
	Key       string
	Raw       json.RawMessage
	AppliesTo string // shop | products | defaults
	// Field metadata for top-level properties (capability, sensitive).
	Fields map[string]FieldMeta
	schema *jsonschema.Schema
}

// FieldMeta labels a top-level field of a section.
type FieldMeta struct {
	Capability string
	Sensitive  bool
}

// Registry holds the compiled section schemas.
type Registry struct {
	sections map[string]*SectionSchema
}

// LoadRegistry compiles the embedded schemas. It panics on a broken schema,
// which is a build error, not a runtime condition.
func LoadRegistry() *Registry {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	r := &Registry{sections: map[string]*SectionSchema{}}
	for _, key := range SectionKeys {
		raw, err := schemaFS.ReadFile("schema/" + key + ".json")
		if err != nil {
			panic(fmt.Errorf("templates: missing schema %s: %w", key, err))
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			panic(fmt.Errorf("templates: parse schema %s: %w", key, err))
		}
		url := "https://schemas.shopee-automation.local/templates/" + key + ".json"
		if err := c.AddResource(url, doc); err != nil {
			panic(err)
		}
		sch, err := c.Compile(url)
		if err != nil {
			panic(fmt.Errorf("templates: compile schema %s: %w", key, err))
		}
		var meta struct {
			AppliesTo  string `json:"x-applies-to"`
			Capability string `json:"x-capability"`
			Sensitive  bool   `json:"x-sensitive"`
			Properties map[string]struct {
				Capability string `json:"x-capability"`
				Sensitive  *bool  `json:"x-sensitive"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(raw, &meta)
		s := &SectionSchema{Key: key, Raw: raw, AppliesTo: meta.AppliesTo, Fields: map[string]FieldMeta{}, schema: sch}
		for name, p := range meta.Properties {
			fm := FieldMeta{Capability: p.Capability, Sensitive: meta.Sensitive}
			if fm.Capability == "" {
				fm.Capability = meta.Capability
			}
			if p.Sensitive != nil {
				fm.Sensitive = *p.Sensitive
			}
			s.Fields[name] = fm
		}
		r.sections[key] = s
	}
	return r
}

// Section returns the schema of key.
func (r *Registry) Section(key string) (*SectionSchema, bool) {
	s, ok := r.sections[key]
	return s, ok
}

// Sections is the content of all sections of a version, keyed by section.
type Sections map[string]json.RawMessage

// Empty returns every section as an empty object.
func Empty() Sections {
	s := Sections{}
	for _, k := range SectionKeys {
		s[k] = json.RawMessage(`{}`)
	}
	return s
}

// ValidateSection checks one section's structure. Errors use the field path
// "sections.<key>.<path>".
func (r *Registry) ValidateSection(key string, content json.RawMessage) []apperr.FieldError {
	s, ok := r.sections[key]
	if !ok {
		return []apperr.FieldError{{Field: "section_key", Code: "unknown_section", Message: "Unknown section."}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return []apperr.FieldError{{Field: "sections." + key, Code: "invalid_json", Message: "Section must be a JSON object."}}
	}
	err = s.schema.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []apperr.FieldError{{Field: "sections." + key, Code: "invalid", Message: "Section is invalid."}}
	}
	return flatten(key, ve.BasicOutput())
}

// flatten turns the basic output into field errors, keeping the most
// specific (deepest) message per location.
func flatten(key string, out *jsonschema.OutputUnit) []apperr.FieldError {
	byLoc := map[string]string{}
	for _, u := range out.Errors {
		if u.Error == nil {
			continue
		}
		msg := u.Error.String()
		if strings.HasPrefix(msg, "validation failed") || strings.Contains(msg, "doesn't validate with") {
			continue // wrapper messages without detail
		}
		loc := u.InstanceLocation
		if _, seen := byLoc[loc]; !seen {
			byLoc[loc] = msg
		}
	}
	if len(byLoc) == 0 {
		return []apperr.FieldError{{Field: "sections." + key, Code: "invalid", Message: "Section is invalid."}}
	}
	locs := make([]string, 0, len(byLoc))
	for l := range byLoc {
		locs = append(locs, l)
	}
	sort.Strings(locs)
	out2 := make([]apperr.FieldError, 0, len(locs))
	for _, l := range locs {
		field := "sections." + key + strings.ReplaceAll(l, "/", ".")
		out2 = append(out2, apperr.FieldError{Field: field, Code: "invalid", Message: byLoc[l]})
	}
	return out2
}

// Issue is a semantic validation result.
type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Report is the result of validating a whole version.
type Report struct {
	Valid    bool    `json:"valid"`
	Errors   []Issue `json:"errors"`
	Warnings []Issue `json:"warnings"`
}

// Validate checks every section structurally and semantically.
func (r *Registry) Validate(secs Sections) Report {
	rep := Report{Errors: []Issue{}, Warnings: []Issue{}}
	for _, key := range SectionKeys {
		content, ok := secs[key]
		if !ok {
			content = json.RawMessage(`{}`)
		}
		for _, fe := range r.ValidateSection(key, content) {
			rep.Errors = append(rep.Errors, Issue(fe))
		}
	}
	if len(rep.Errors) == 0 {
		semantic(secs, &rep)
	}
	rep.Valid = len(rep.Errors) == 0
	return rep
}

// Typed views used by semantic checks and the diff.
type general struct {
	Region   string  `json:"region"`
	ShopName *string `json:"shop_name"`
}

type returnAddress struct {
	Name     *string  `json:"name"`
	Phone    *string  `json:"phone"`
	State    *string  `json:"state"`
	City     *string  `json:"city"`
	District *string  `json:"district"`
	Town     *string  `json:"town"`
	Address  *string  `json:"address"`
	Zipcode  *string  `json:"zipcode"`
	UseAs    []string `json:"use_as"`
}

type logistics struct {
	Channels []struct {
		ID         int64  `json:"logistics_channel_id"`
		Name       string `json:"name"`
		Enabled    bool   `json:"enabled"`
		CODEnabled *bool  `json:"cod_enabled"`
	} `json:"channels"`
}

func semantic(secs Sections, rep *Report) {
	errf := func(field, code, msg string) { rep.Errors = append(rep.Errors, Issue{field, code, msg}) }
	warn := func(field, code, msg string) { rep.Warnings = append(rep.Warnings, Issue{field, code, msg}) }

	var g general
	_ = json.Unmarshal(secs["general"], &g)
	if g.Region == "" {
		warn("sections.general.region", "no_region", "No target market set. Logistics channel IDs and categories are market-specific.")
	}

	var ra returnAddress
	_ = json.Unmarshal(secs["return_address"], &ra)
	anyAddr := ra.Name != nil || ra.Phone != nil || ra.State != nil || ra.City != nil || ra.District != nil ||
		ra.Town != nil || ra.Address != nil || ra.Zipcode != nil
	if anyAddr {
		for f, v := range map[string]*string{"name": ra.Name, "phone": ra.Phone, "address": ra.Address, "city": ra.City} {
			if v == nil {
				errf("sections.return_address."+f, "required", "Required when an address is set.")
			}
		}
	}
	if len(ra.UseAs) > 0 && !anyAddr {
		errf("sections.return_address.use_as", "address_required", "Set the address fields before choosing how it is used.")
	}

	var lg logistics
	_ = json.Unmarshal(secs["logistics"], &lg)
	seen := map[int64]bool{}
	for i, ch := range lg.Channels {
		if seen[ch.ID] {
			errf(fmt.Sprintf("sections.logistics.channels.%d.logistics_channel_id", i), "duplicate", "Each channel may appear once.")
		}
		seen[ch.ID] = true
		if ch.CODEnabled != nil && *ch.CODEnabled && !ch.Enabled {
			errf(fmt.Sprintf("sections.logistics.channels.%d.cod_enabled", i), "cod_requires_enabled", "COD needs the channel to be enabled.")
		}
	}
	if len(lg.Channels) > 0 && g.Region == "" {
		warn("sections.logistics.channels", "channels_without_region", "Channel IDs differ per market; set the target market.")
	}

	var cm struct {
		Mappings []struct {
			Key string `json:"key"`
		} `json:"mappings"`
	}
	_ = json.Unmarshal(secs["category_mappings"], &cm)
	keys := map[string]bool{}
	for i, m := range cm.Mappings {
		k := strings.ToLower(strings.TrimSpace(m.Key))
		if keys[k] {
			errf(fmt.Sprintf("sections.category_mappings.mappings.%d.key", i), "duplicate", "Each category label may appear once.")
		}
		keys[k] = true
	}

	var da struct {
		Attributes []struct {
			CategoryID  *int64 `json:"category_id"`
			AttributeID int64  `json:"attribute_id"`
		} `json:"attributes"`
	}
	_ = json.Unmarshal(secs["default_attributes"], &da)
	attrs := map[string]bool{}
	for i, a := range da.Attributes {
		k := fmt.Sprintf("%v/%d", deref(a.CategoryID), a.AttributeID)
		if attrs[k] {
			errf(fmt.Sprintf("sections.default_attributes.attributes.%d.attribute_id", i), "duplicate", "This attribute is already set for this category.")
		}
		attrs[k] = true
	}

	var sku struct {
		Generate bool    `json:"generate_when_missing"`
		Digits   *int    `json:"sequence_digits"`
		Prefix   *string `json:"prefix"`
	}
	_ = json.Unmarshal(secs["sku_rules"], &sku)
	if sku.Generate && sku.Digits == nil {
		errf("sections.sku_rules.sequence_digits", "required", "Required when SKUs are generated.")
	}
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// Merge deep-merges overrides on top of base: objects merge key by key,
// everything else (arrays, scalars, null) replaces. Sections missing from
// overrides are kept.
func Merge(base, overrides Sections) (Sections, error) {
	out := Sections{}
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range overrides {
		var b, o any
		if cur, ok := out[k]; ok {
			if err := json.Unmarshal(cur, &b); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(ov, &o); err != nil {
			return nil, err
		}
		merged, err := json.Marshal(mergeValue(b, o))
		if err != nil {
			return nil, err
		}
		out[k] = merged
	}
	return out, nil
}

func mergeValue(base, over any) any {
	bm, bok := base.(map[string]any)
	om, ook := over.(map[string]any)
	if !bok || !ook {
		return over
	}
	out := make(map[string]any, len(bm)+len(om))
	for k, v := range bm {
		out[k] = v
	}
	for k, v := range om {
		out[k] = mergeValue(bm[k], v)
	}
	return out
}

// Canonical returns a stable JSON encoding of the sections (sorted keys at
// every level), used for the version content hash.
func Canonical(secs Sections) ([]byte, error) {
	m := map[string]any{}
	for k, v := range secs {
		var x any
		if err := json.Unmarshal(v, &x); err != nil {
			return nil, err
		}
		m[k] = x
	}
	return json.Marshal(m) // encoding/json sorts map keys
}
