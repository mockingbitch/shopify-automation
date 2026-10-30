// Package shopsetup is the SHOP_SETUP workflow: apply a published template
// to one shop (workflow-design.md §6). Steps 9–11 (products) arrive in
// Phase 5.
package shopsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/capability"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/templates"
	"shopee-automation/internal/workflow"
	"shopee-automation/internal/workflows/shopstep"
)

// Key is the definition key.
const Key = "SHOP_SETUP"

// Version of the step list.
const Version = 1

// Input is the run input.
type Input struct {
	TemplateID   uuid.UUID `json:"template_id"`
	VersionID    uuid.UUID `json:"version_id"`
	TemplateName string    `json:"template_name"`
	VersionNo    int       `json:"version_no"`
}

// Setup holds the services the steps use.
type Setup struct {
	Shops     *shops.Service
	Templates *templates.Service
	Engine    *workflow.Engine
}

// Definition returns SHOP_SETUP v1.
func (s *Setup) Definition() workflow.Definition {
	return workflow.Definition{
		Key: Key, Version: Version, ValidateSelection: validateSelection,
		Steps: []workflow.StepSpec{
			{Key: "validate_connection", Capability: "shop.info.read", Timeout: time.Minute, Handler: shopstep.ValidateConnection(s.Shops)},
			{Key: "fetch_current_config", Capability: "shop.profile.read", Timeout: 2 * time.Minute, Handler: s.fetchCurrent},
			{Key: "validate_template", Timeout: time.Minute, MaxAttempts: 3, Handler: s.validateTemplate},
			{Key: "generate_diff", Timeout: time.Minute, MaxAttempts: 3, Handler: s.generateDiff},
			{Key: "approve_changes", Timeout: time.Minute, Handler: s.approve},
			{Key: "apply_profile", Capability: "shop.profile.update", Mutating: true, Timeout: time.Minute, Handler: s.applyProfile},
			{Key: "apply_branding", Capability: "shop.banner.upload", Timeout: time.Minute, Handler: s.manualTasks},
			{Key: "apply_logistics", Capability: "shop.logistics.update", Mutating: true, ContinueOnFailure: true, Timeout: 3 * time.Minute, Handler: s.applyLogistics},
			{Key: "verify_configuration", Capability: "shop.profile.read", Timeout: 2 * time.Minute, Handler: s.verify},
			{Key: "generate_report", Timeout: time.Minute, Report: true, Handler: s.report},
		},
	}
}

func tenant(sc *workflow.StepContext) authctx.Tenant { return authctx.Tenant{OrgID: sc.OrgID} }

func (s *Setup) input(sc *workflow.StepContext) (Input, error) {
	var in Input
	err := json.Unmarshal(sc.Input, &in)
	return in, err
}

// Config is what the workflow reads from a shop.
type Config struct {
	ShopName       string           `json:"shop_name"`
	Region         string           `json:"region"`
	Status         string           `json:"status"`
	Description    string           `json:"description"`
	Logo           string           `json:"logo"`
	ChannelsKnown  bool             `json:"channels_known"`
	Channels       []shopee.Channel `json:"channels"`
	AddressesKnown bool             `json:"addresses_known"`
	Addresses      []shopee.Address `json:"addresses"`
}

func (s *Setup) read(ctx context.Context, sc *workflow.StepContext) (Config, map[string]capability.Resolved, error) {
	detail, err := s.Shops.Get(ctx, tenant(sc), sc.ShopID)
	if err != nil {
		return Config{}, nil, err
	}
	caps := map[string]capability.Resolved{}
	for _, c := range detail.Capabilities {
		caps[c.Key] = c
	}
	var cfg Config
	err = shopstep.WithAccess(ctx, s.Shops, sc.OrgID, sc.ShopID, func(acc shops.Access) error {
		cfg = Config{}
		info, err := s.Shops.Shopee.GetShopInfo(ctx, acc.App, acc.AccessToken, acc.ShopID)
		if err != nil {
			return err
		}
		cfg.Region, cfg.Status, cfg.ShopName = info.Region, info.Status, info.ShopName
		prof, err := s.Shops.Shopee.GetProfile(ctx, acc.App, acc.AccessToken, acc.ShopID)
		if err != nil {
			return err
		}
		if prof.ShopName != "" {
			cfg.ShopName = prof.ShopName
		}
		cfg.Description, cfg.Logo = prof.Description, prof.ShopLogo
		if caps["shop.logistics.read"].Enabled {
			if cfg.Channels, err = s.Shops.Shopee.GetChannelList(ctx, acc.App, acc.AccessToken, acc.ShopID); err != nil {
				return err
			}
			cfg.ChannelsKnown = true
		}
		if caps["shop.address.read"].Enabled {
			list, err := s.Shops.Shopee.GetAddressList(ctx, acc.App, acc.AccessToken, acc.ShopID)
			if err != nil {
				return err
			}
			cfg.Addresses, cfg.AddressesKnown = list.Addresses, true
		}
		return nil
	})
	return cfg, caps, err
}

// Step 2.
func (s *Setup) fetchCurrent(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	cfg, _, err := s.read(ctx, sc)
	if err != nil {
		return shopstep.FromErr(err)
	}
	if err := s.Engine.Snapshot(ctx, sc.OrgID, sc.ShopID, sc.RunID, "BEFORE_APPLY", cfg); err != nil {
		return workflow.Retryable(err)
	}
	return workflow.Succeeded(cfg)
}

// returnAddress is the address flagged as the shop's return address.
func returnAddress(cfg Config) *shopee.Address {
	for i, a := range cfg.Addresses {
		for _, t := range a.AddressType {
			if t == shopee.AddressReturn {
				return &cfg.Addresses[i]
			}
		}
	}
	return nil
}

// current converts what was read into the diff's "current" values.
func current(cfg Config) templates.Current {
	c := templates.Current{Region: cfg.Region, Values: map[string]any{}, Known: map[string]bool{}}
	set := func(path string, v any) { c.Values[path], c.Known[path] = v, true }
	set("general.shop_name", cfg.ShopName)
	set("description.text", cfg.Description)
	set("branding.logo_url", cfg.Logo)
	if cfg.ChannelsKnown {
		for _, ch := range cfg.Channels {
			// get_channel_list's cod_enabled means "supports COD", not "COD is
			// on", so only the enabled flag is a known current value.
			set("logistics.channels."+strconv.FormatInt(ch.ID, 10), map[string]any{"enabled": ch.Enabled})
		}
	}
	if cfg.AddressesKnown {
		if a := returnAddress(cfg); a != nil {
			fields := map[string]any{}
			for k, v := range map[string]string{"state": a.State, "city": a.City, "district": a.District, "town": a.Town, "address": a.Address, "zipcode": a.Zipcode} {
				if v != "" {
					fields[k] = v
				}
			}
			set("return_address.address", fields)
			types := append([]string(nil), a.AddressType...)
			sort.Strings(types)
			set("return_address.use_as", types)
		}
	}
	return c
}

// Step 3.
func (s *Setup) validateTemplate(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	in, err := s.input(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	res, err := s.Templates.Resolve(ctx, tenant(sc), in.TemplateID, in.VersionID, sc.ShopID)
	if err != nil {
		return shopstep.FromErr(err)
	}
	if res.Status == templates.StatusDraft {
		return workflow.Permanent(errors.New("the template version is not published"))
	}
	rep := s.Templates.Registry.Validate(res.Sections)
	if !rep.Valid {
		msgs := make([]string, 0, len(rep.Errors))
		for _, e := range rep.Errors {
			msgs = append(msgs, strings.TrimPrefix(e.Field, "sections.")+": "+e.Message)
		}
		return workflow.Permanent(errors.New("template is invalid for this shop: " + strings.Join(msgs, "; ")))
	}
	var fetched Config
	if err := sc.Output("fetch_current_config", &fetched); err == nil {
		var g struct {
			Region string `json:"region"`
		}
		_ = json.Unmarshal(res.Sections["general"], &g)
		if g.Region != "" && fetched.Region != "" && g.Region != fetched.Region {
			sc.Log("warn", "The template targets "+g.Region+" but the shop is in "+fetched.Region+".", nil)
		}
	}
	for _, w := range rep.Warnings {
		sc.Log("warn", w.Message, map[string]any{"field": w.Field})
	}
	return workflow.Succeeded(map[string]any{"sections": res.Sections, "has_overrides": res.HasOverrides, "warnings": rep.Warnings})
}

// Diff is the output of generate_diff and the approval content.
type Diff struct {
	Changes []templates.Change `json:"changes"`
	Blocked []templates.Change `json:"blocked"`
}

// Step 4.
func (s *Setup) generateDiff(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	var fetched Config
	var tv struct {
		Sections templates.Sections `json:"sections"`
	}
	if err := sc.Output("fetch_current_config", &fetched); err != nil {
		return workflow.Permanent(err)
	}
	if err := sc.Output("validate_template", &tv); err != nil {
		return workflow.Permanent(err)
	}
	detail, err := s.Shops.Get(ctx, tenant(sc), sc.ShopID)
	if err != nil {
		return shopstep.FromErr(err)
	}
	caps := map[string]capability.Resolved{}
	for _, c := range detail.Capabilities {
		caps[c.Key] = c
	}
	d := Diff{Changes: []templates.Change{}, Blocked: []templates.Change{}}
	for _, ch := range s.Templates.Registry.Diff(tv.Sections, current(fetched), caps) {
		if ch.Enabled {
			d.Changes = append(d.Changes, ch)
		} else {
			d.Blocked = append(d.Blocked, ch)
		}
	}
	return workflow.Succeeded(d)
}

func validateSelection(diff json.RawMessage, selected []string) error {
	var d Diff
	if err := json.Unmarshal(diff, &d); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, c := range d.Changes {
		allowed[c.Path] = true
	}
	seen := map[string]bool{}
	for _, p := range selected {
		if !allowed[p] {
			return fmt.Errorf("%s is not a change that can be applied", p)
		}
		if seen[p] {
			return fmt.Errorf("%s is listed twice", p)
		}
		seen[p] = true
	}
	return nil
}

// Step 5.
func (s *Setup) approve(_ context.Context, sc *workflow.StepContext) workflow.Result {
	var d Diff
	if err := sc.Output("generate_diff", &d); err != nil {
		return workflow.Permanent(err)
	}
	if len(d.Changes) == 0 {
		return workflow.Skip("nothing_to_change")
	}
	if sc.Approval != nil && sc.Approval.Status == "APPROVED" {
		return workflow.Succeeded(map[string]any{"selected": sc.Approval.Selected, "approval_id": sc.Approval.ID.String()})
	}
	return workflow.AwaitApproval(d)
}

// selected returns the approved changes, keyed by path.
func selected(sc *workflow.StepContext) (map[string]templates.Change, error) {
	var d Diff
	if err := sc.Output("generate_diff", &d); err != nil {
		return nil, err
	}
	var ap struct {
		Selected []string `json:"selected"`
	}
	if raw, ok := sc.Outputs["approve_changes"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &ap); err != nil {
			return nil, err
		}
	}
	byPath := map[string]templates.Change{}
	for _, c := range d.Changes {
		byPath[c.Path] = c
	}
	out := map[string]templates.Change{}
	for _, p := range ap.Selected {
		if c, ok := byPath[p]; ok {
			out[p] = c
		}
	}
	return out, nil
}

// Step 6.
func (s *Setup) applyProfile(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	sel, err := selected(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	var u shopee.ProfileUpdate
	applied := []string{}
	if c, ok := sel["general.shop_name"]; ok && c.Mode == capability.ModeAPI {
		v, _ := c.Target.(string)
		u.ShopName, applied = &v, append(applied, c.Path)
	}
	if c, ok := sel["description.text"]; ok && c.Mode == capability.ModeAPI {
		v, _ := c.Target.(string)
		u.Description, applied = &v, append(applied, c.Path)
	}
	if len(applied) == 0 {
		return workflow.Skip("not_selected")
	}
	var meta shopee.Meta
	err = shopstep.WithAccess(ctx, s.Shops, sc.OrgID, sc.ShopID, func(acc shops.Access) error {
		var err error
		meta, err = s.Shops.Shopee.UpdateProfile(ctx, acc.App, acc.AccessToken, acc.ShopID, u)
		return err
	})
	if err != nil {
		return shopstep.FromErr(err)
	}
	sc.Log("info", "Shop profile updated.", map[string]any{"fields": applied, "shopee_request_id": meta.RequestID})
	return workflow.Succeeded(map[string]any{"applied": applied, "shopee_request_id": meta.RequestID})
}

var taskText = map[string][2]string{
	"branding.logo_url":         {"Set the shop logo", "Upload the image from the URL below as the shop logo in Seller Centre > Shop Profile."},
	"branding.banner_urls":      {"Upload shop banners", "Add the banner images below in Seller Centre > Shop Decoration."},
	"branding.decoration_notes": {"Update shop decoration", "Follow the decoration instructions below in Seller Centre > Shop Decoration."},
	"warehouse.instructions":    {"Update warehouse settings", "Follow the instructions below in Seller Centre > Shipping > Warehouse."},
}

// Step 7: everything selected that has no Open API (or whose extension
// is not available yet) becomes a manual task.
func (s *Setup) manualTasks(_ context.Context, sc *workflow.StepContext) workflow.Result {
	sel, err := selected(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	var tasks []workflow.ManualTask
	paths := make([]string, 0, len(sel))
	for p := range sel {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		c := sel[p]
		if c.Mode != capability.ModeManual && c.Mode != capability.ModeBrowser {
			continue
		}
		title, how := "Apply "+p, "Apply this change in Seller Centre."
		if t, ok := taskText[p]; ok {
			title, how = t[0], t[1]
		}
		tasks = append(tasks, workflow.ManualTask{Key: p, Title: title, Instructions: how, Capability: c.Capability, Data: c.Target})
	}
	// A return address can only be edited if the shop already has one.
	var fetched Config
	_ = sc.Output("fetch_current_config", &fetched)
	if c, ok := sel["return_address.address"]; ok && (!fetched.AddressesKnown || returnAddress(fetched) == nil) {
		tasks = append(tasks, workflow.ManualTask{Key: "return_address.create", Title: "Create the return address",
			Instructions: "The shop has no return address to edit and Shopee has no API to create one. Add it in Seller Centre > Shop Settings > Addresses and mark it as return address.",
			Capability:   "shop.address.create", Data: c.Target})
	}
	if len(tasks) == 0 {
		return workflow.Skip("no_manual_changes")
	}
	return workflow.Manual(tasks, map[string]any{"task_count": len(tasks)})
}

// Step 8.
func (s *Setup) applyLogistics(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	sel, err := selected(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	var fetched Config
	_ = sc.Output("fetch_current_config", &fetched)
	type item struct {
		Path      string `json:"path"`
		OK        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
		RequestID string `json:"shopee_request_id,omitempty"`
	}
	var results []item
	var acc shops.Access
	ensure := func() error {
		if acc.AccessToken != "" {
			return nil
		}
		a, err := s.Shops.AccessFor(ctx, sc.OrgID, sc.ShopID)
		acc = a
		return err
	}
	paths := make([]string, 0, len(sel))
	for p := range sel {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		c := sel[p]
		if c.Mode != capability.ModeAPI {
			continue
		}
		var call func() (shopee.Meta, error)
		switch {
		case strings.HasPrefix(p, "logistics.channels."):
			id, _ := strconv.ParseInt(strings.TrimPrefix(p, "logistics.channels."), 10, 64)
			t, _ := c.Target.(map[string]any)
			u := shopee.ChannelUpdate{ID: id}
			if v, ok := t["enabled"].(bool); ok {
				u.Enabled = &v
			}
			if v, ok := t["cod_enabled"].(bool); ok {
				u.CODEnabled = &v
			}
			call = func() (shopee.Meta, error) {
				return s.Shops.Shopee.UpdateChannel(ctx, acc.App, acc.AccessToken, acc.ShopID, u)
			}
		case p == "return_address.address":
			a := returnAddress(fetched)
			if a == nil {
				continue // manual task created in apply_branding
			}
			t, _ := c.Target.(map[string]any)
			str := func(k string) *string {
				if v, ok := t[k].(string); ok {
					return &v
				}
				return nil
			}
			u := shopee.AddressUpdate{ID: a.ID, State: str("state"), City: str("city"), District: str("district"), Town: str("town"),
				Address: str("address"), Zipcode: str("zipcode"), Name: str("name"), Phone: str("phone")}
			call = func() (shopee.Meta, error) {
				return s.Shops.Shopee.UpdateAddress(ctx, acc.App, acc.AccessToken, acc.ShopID, u)
			}
		case p == "return_address.use_as":
			a := returnAddress(fetched)
			if a == nil {
				continue
			}
			raw, _ := json.Marshal(c.Target)
			var types []string
			_ = json.Unmarshal(raw, &types)
			id := a.ID
			call = func() (shopee.Meta, error) {
				return s.Shops.Shopee.SetAddressConfig(ctx, acc.App, acc.AccessToken, acc.ShopID, id, types)
			}
		default:
			continue
		}
		if err := ensure(); err != nil {
			return shopstep.FromErr(err)
		}
		meta, err := call()
		if err != nil {
			r := shopstep.FromErr(err)
			// Lost authorization stops the step; per-item errors do not.
			if se, ok := shopee.AsError(err); ok && (se.Kind == shopee.KindAuthorizationLost || se.Kind == shopee.KindAmbiguous) {
				return r
			}
			results = append(results, item{Path: p, Error: errMessage(err)})
			sc.Log("error", p+" failed: "+errMessage(err), nil)
			continue
		}
		results = append(results, item{Path: p, OK: true, RequestID: meta.RequestID})
		sc.Log("info", p+" applied.", map[string]any{"shopee_request_id": meta.RequestID})
	}
	if len(results) == 0 {
		return workflow.Skip("not_selected")
	}
	var failed []string
	applied := []string{}
	for _, r := range results {
		if r.OK {
			applied = append(applied, r.Path)
		} else {
			failed = append(failed, r.Path+": "+r.Error)
		}
	}
	if len(failed) > 0 {
		return workflow.Permanent(fmt.Errorf("%d of %d changes failed: %s", len(failed), len(results), strings.Join(failed, "; ")))
	}
	return workflow.Succeeded(map[string]any{"applied": applied, "results": results})
}

func errMessage(err error) string {
	if se, ok := shopee.AsError(err); ok {
		return se.Code + " " + se.Message
	}
	return err.Error()
}

// Step 12.
func (s *Setup) verify(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	cfg, _, err := s.read(ctx, sc)
	if err != nil {
		return shopstep.FromErr(err)
	}
	if err := s.Engine.Snapshot(ctx, sc.OrgID, sc.ShopID, sc.RunID, "AFTER_APPLY", cfg); err != nil {
		return workflow.Retryable(err)
	}
	sel, err := selected(sc)
	if err != nil {
		return workflow.Permanent(err)
	}
	applied := map[string]bool{}
	for _, step := range []string{"apply_profile", "apply_logistics"} {
		var out struct {
			Applied []string `json:"applied"`
		}
		if sc.Output(step, &out) == nil {
			for _, p := range out.Applied {
				applied[p] = true
			}
		}
	}
	now := current(cfg)
	type mismatch struct {
		Path   string `json:"path"`
		Target any    `json:"target"`
		Actual any    `json:"actual"`
	}
	mismatches := []mismatch{}
	checked := 0
	for p := range applied {
		c, ok := sel[p]
		if !ok || !now.Known[p] {
			continue
		}
		checked++
		if !matches(p, c.Target, now.Values[p]) {
			mismatches = append(mismatches, mismatch{Path: p, Target: c.Target, Actual: now.Values[p]})
			sc.Log("warn", p+" does not match the template after applying.", nil)
		}
	}
	sort.Slice(mismatches, func(i, j int) bool { return mismatches[i].Path < mismatches[j].Path })
	return workflow.Succeeded(map[string]any{"checked": checked, "mismatches": mismatches})
}

// matches compares a target with what Shopee returns now. Fields Shopee
// does not return (recipient name/phone, COD state) are not compared.
func matches(path string, target, actual any) bool {
	t, tok := target.(map[string]any)
	a, aok := actual.(map[string]any)
	if tok && aok {
		for k, v := range a {
			if tv, ok := t[k]; ok && fmt.Sprint(tv) != fmt.Sprint(v) {
				return false
			}
		}
		return true
	}
	if path == "return_address.use_as" {
		ta, _ := json.Marshal(target)
		var want []string
		_ = json.Unmarshal(ta, &want)
		got, _ := actual.([]string)
		have := map[string]bool{}
		for _, g := range got {
			have[g] = true
		}
		for _, w := range want {
			if !have[w] {
				return false
			}
		}
		return true
	}
	return fmt.Sprint(target) == fmt.Sprint(actual)
}

// Step 13.
func (s *Setup) report(_ context.Context, sc *workflow.StepContext) workflow.Result {
	in, _ := s.input(sc)
	var d Diff
	_ = sc.Output("generate_diff", &d)
	sel, _ := selected(sc)
	applied := []string{}
	for _, step := range []string{"apply_profile", "apply_logistics"} {
		var out struct {
			Applied []string `json:"applied"`
		}
		if sc.Output(step, &out) == nil {
			applied = append(applied, out.Applied...)
		}
	}
	var manual struct {
		Tasks []workflow.ManualTask `json:"tasks"`
	}
	_ = sc.Output("apply_branding", &manual)
	var verify struct {
		Mismatches []any `json:"mismatches"`
	}
	_ = sc.Output("verify_configuration", &verify)
	notSelected := []string{}
	for _, c := range d.Changes {
		if _, ok := sel[c.Path]; !ok {
			notSelected = append(notSelected, c.Path)
		}
	}
	blocked := []string{}
	for _, c := range d.Blocked {
		blocked = append(blocked, c.Path)
	}
	sort.Strings(applied)
	return workflow.Succeeded(map[string]any{
		"template": in.TemplateName, "version_no": in.VersionNo,
		"changes_found": len(d.Changes) + len(d.Blocked), "selected": len(sel), "applied": applied,
		"manual_tasks": len(manual.Tasks), "not_selected": notSelected, "blocked": blocked,
		"verification_mismatches": len(verify.Mismatches),
	})
}
