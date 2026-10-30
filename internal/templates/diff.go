package templates

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"

	"shopee-automation/internal/capability"
)

// Current is what we know about a shop's live configuration. Known lists the
// paths whose current value we actually read from Shopee; everything else is
// unknown until the workflow reads it before applying (Phase 4).
type Current struct {
	Region string
	Values map[string]any // path -> value, e.g. "general.shop_name"
	Known  map[string]bool
}

// Change is one field-level difference between a shop and the target.
type Change struct {
	Path            string          `json:"path"`
	Section         string          `json:"section"`
	Current         any             `json:"current"`
	CurrentKnown    bool            `json:"current_known"`
	Target          any             `json:"target"`
	Capability      string          `json:"capability"`
	Mode            capability.Mode `json:"mode"`
	Enabled         bool            `json:"enabled"`
	Reason          string          `json:"reason"`
	Sensitive       bool            `json:"sensitive"`
	DefaultSelected bool            `json:"default_selected"`
	Note            string          `json:"note,omitempty"`
}

// Diff compares the resolved target sections with what is known about the
// shop. Only sections applied to the shop itself produce changes; product
// sections are used later when products are created. Fields without a
// capability (e.g. general.region) are informational and not diffed.
func (r *Registry) Diff(target Sections, cur Current, caps map[string]capability.Resolved) []Change {
	out := []Change{}
	for _, key := range SectionKeys {
		s := r.sections[key]
		if s.AppliesTo != "shop" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(target[key], &obj); err != nil || len(obj) == 0 {
			continue
		}
		if key == "return_address" {
			out = append(out, r.addressChanges(obj, cur, caps)...)
			continue
		}
		if key == "logistics" {
			out = append(out, r.channelChanges(obj, cur, caps)...)
			continue
		}
		fields := make([]string, 0, len(obj))
		for f := range obj {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		for _, f := range fields {
			meta := s.Fields[f]
			if meta.Capability == "" {
				continue
			}
			path := key + "." + f
			c := Change{Path: path, Section: key, Target: obj[f], Capability: meta.Capability, Sensitive: meta.Sensitive}
			if cur.Known[path] {
				c.CurrentKnown = true
				c.Current = cur.Values[path]
				if equal(c.Current, c.Target) {
					continue
				}
			}
			if path == "branding.logo_url" {
				c.Note = "The image is uploaded to Shopee, so the stored logo URL always differs."
			}
			out = append(out, label(c, caps))
		}
	}
	return out
}

func (r *Registry) addressChanges(obj map[string]any, cur Current, caps map[string]capability.Resolved) []Change {
	var out []Change
	addr := map[string]any{}
	for k, v := range obj {
		if k != "use_as" {
			addr[k] = v
		}
	}
	if len(addr) > 0 {
		out = append(out, label(Change{Path: "return_address.address", Section: "return_address", Target: addr,
			Capability: "shop.address.update", Sensitive: true,
			Note: "Updates the shop's existing return address. If the shop has none, creating one has no Open API."}, caps))
	}
	if useAs, ok := obj["use_as"]; ok {
		out = append(out, label(Change{Path: "return_address.use_as", Section: "return_address", Target: useAs,
			Capability: "shop.address.config", Sensitive: true}, caps))
	}
	_ = cur
	return out
}

func (r *Registry) channelChanges(obj map[string]any, cur Current, caps map[string]capability.Resolved) []Change {
	var out []Change
	chans, _ := obj["channels"].([]any)
	for _, ch := range chans {
		m, _ := ch.(map[string]any)
		id, _ := m["logistics_channel_id"].(float64)
		path := "logistics.channels." + strconv.FormatInt(int64(id), 10)
		target := map[string]any{"enabled": m["enabled"]}
		if cod, ok := m["cod_enabled"]; ok {
			target["cod_enabled"] = cod
		}
		c := Change{Path: path, Section: "logistics", Target: target, Capability: "shop.logistics.update", Sensitive: true}
		if name, ok := m["name"].(string); ok && name != "" {
			c.Note = name
		}
		if cur.Known[path] {
			c.CurrentKnown = true
			c.Current = cur.Values[path]
			if equal(c.Current, c.Target) {
				continue
			}
		}
		out = append(out, label(c, caps))
	}
	return out
}

// label attaches the resolved capability. Sensitivity comes from the field
// (schema x-sensitive), not the capability: update_profile is sensitive for
// the shop name but not for the description. Sensitive changes are never
// selected by default (FR-TPL-7); disabled ones cannot be selected.
func label(c Change, caps map[string]capability.Resolved) Change {
	if rc, ok := caps[c.Capability]; ok {
		c.Mode, c.Enabled, c.Reason = rc.Mode, rc.Enabled, rc.Reason
	} else {
		c.Mode, c.Enabled, c.Reason = capability.ModeUnsupported, false, capability.ReasonUnsupported
	}
	c.DefaultSelected = c.Enabled && !c.Sensitive
	return c
}

func equal(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	var x, y any
	_ = json.Unmarshal(ja, &x)
	_ = json.Unmarshal(jb, &y)
	return reflect.DeepEqual(x, y)
}
