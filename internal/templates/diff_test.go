package templates

import (
	"encoding/json"
	"testing"

	"shopee-automation/internal/capability"
)

func caps(enabled map[string]bool) map[string]capability.Resolved {
	out := map[string]capability.Resolved{}
	for k, e := range enabled {
		out[k] = capability.Resolved{Key: k, Mode: capability.ModeAPI, Enabled: e}
	}
	return out
}

func TestDiff(t *testing.T) {
	target := Empty()
	target["general"] = json.RawMessage(`{"region":"VN","shop_name":"New Name"}`)
	target["description"] = json.RawMessage(`{"text":"Same text"}`)
	target["branding"] = json.RawMessage(`{"logo_url":"https://cdn.example/logo.png","banner_urls":["https://cdn.example/b.png"]}`)
	target["return_address"] = json.RawMessage(`{"name":"A","phone":"+84900000000","address":"1 Street","city":"HN","use_as":["RETURN_ADDRESS"]}`)
	target["logistics"] = json.RawMessage(`{"channels":[{"logistics_channel_id":5001,"name":"Express","enabled":true,"cod_enabled":false}]}`)
	target["product_defaults"] = json.RawMessage(`{"condition":"NEW"}`) // not a shop change

	cur := Current{Region: "VN",
		Values: map[string]any{"general.shop_name": "Old Name", "description.text": "Same text", "branding.logo_url": "https://cf.shopee.vn/file/x"},
		Known:  map[string]bool{"general.shop_name": true, "description.text": true, "branding.logo_url": true}}
	c := caps(map[string]bool{"shop.profile.update": true, "shop.avatar.upload": true, "shop.address.update": true,
		"shop.address.config": true, "shop.logistics.update": true})
	c["shop.banner.upload"] = capability.Resolved{Key: "shop.banner.upload", Mode: capability.ModeManual, Enabled: true,
		Reason: capability.ReasonExtensionUnavailable}

	got := map[string]Change{}
	for _, ch := range reg.Diff(target, cur, c) {
		got[ch.Path] = ch
	}
	if _, ok := got["description.text"]; ok {
		t.Error("unchanged description listed")
	}
	if _, ok := got["general.region"]; ok {
		t.Error("region is informational, not a change")
	}
	if _, ok := got["product_defaults.condition"]; ok {
		t.Error("product sections are not shop changes")
	}
	name := got["general.shop_name"]
	if name.Current != "Old Name" || !name.CurrentKnown || !name.Sensitive || name.DefaultSelected || !name.Enabled {
		t.Errorf("shop name: %+v", name)
	}
	if logo := got["branding.logo_url"]; !logo.DefaultSelected || logo.Note == "" {
		t.Errorf("logo: %+v", logo)
	}
	if b := got["branding.banner_urls"]; b.Mode != capability.ModeManual || b.CurrentKnown || !b.DefaultSelected {
		t.Errorf("banner: %+v", b)
	}
	if a := got["return_address.address"]; !a.Sensitive || a.DefaultSelected {
		t.Errorf("address: %+v", a)
	}
	if u := got["return_address.use_as"]; u.Capability != "shop.address.config" || u.DefaultSelected {
		t.Errorf("use_as: %+v", u)
	}
	if ch := got["logistics.channels.5001"]; !ch.Sensitive || ch.DefaultSelected || ch.Note != "Express" || ch.CurrentKnown {
		t.Errorf("channel: %+v", ch)
	}
	if len(got) != 6 {
		t.Errorf("changes: %d %+v", len(got), got)
	}

	// A disabled capability blocks the change and it is never pre-selected.
	c["shop.avatar.upload"] = capability.Resolved{Key: "shop.avatar.upload", Mode: capability.ModeAPI, Enabled: false, Reason: capability.ReasonShopNotActive}
	for _, ch := range reg.Diff(target, cur, c) {
		if ch.Path == "branding.logo_url" && (ch.Enabled || ch.DefaultSelected || ch.Reason != capability.ReasonShopNotActive) {
			t.Errorf("disabled logo: %+v", ch)
		}
	}
	// Unknown capability is treated as unsupported.
	for _, ch := range reg.Diff(target, cur, map[string]capability.Resolved{}) {
		if ch.Enabled || ch.Mode != capability.ModeUnsupported {
			t.Errorf("no capability: %+v", ch)
		}
	}
}

func TestSensitivityIsPerField(t *testing.T) {
	target := Empty()
	target["general"] = json.RawMessage(`{"shop_name":"New"}`)
	target["description"] = json.RawMessage(`{"text":"New text"}`)
	// The registry marks update_profile sensitive as a whole (shop name).
	c := map[string]capability.Resolved{"shop.profile.update": {Key: "shop.profile.update", Mode: capability.ModeAPI, Enabled: true, Sensitive: true}}
	got := map[string]Change{}
	for _, ch := range reg.Diff(target, Current{}, c) {
		got[ch.Path] = ch
	}
	if !got["general.shop_name"].Sensitive || got["general.shop_name"].DefaultSelected {
		t.Errorf("shop name: %+v", got["general.shop_name"])
	}
	if got["description.text"].Sensitive || !got["description.text"].DefaultSelected {
		t.Errorf("description: %+v", got["description.text"])
	}
}
