package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"shopee-automation/internal/shopee/shopeefake"
)

type runView struct {
	Data struct {
		Run struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Output json.RawMessage `json:"output"`
		} `json:"run"`
		Steps []struct {
			ID         string          `json:"id"`
			Key        string          `json:"key"`
			Status     string          `json:"status"`
			Output     json.RawMessage `json:"output"`
			Error      json.RawMessage `json:"error"`
			SkipReason *string         `json:"skip_reason"`
		} `json:"steps"`
		PendingApproval *struct {
			ID   string `json:"id"`
			Diff struct {
				Changes []previewChange `json:"changes"`
				Blocked []previewChange `json:"blocked"`
			} `json:"diff"`
		} `json:"pending_approval"`
	} `json:"data"`
}

func (c *client) runView(orgID, runID string) runView {
	c.e.t.Helper()
	var v runView
	c.must(200, "GET", "/orgs/"+orgID+"/workflows/"+runID, nil).json(c.e.t, &v)
	return v
}

func (v runView) step(key string) (id, status string, output json.RawMessage) {
	for _, s := range v.Data.Steps {
		if s.Key == key {
			return s.ID, s.Status, s.Output
		}
	}
	return "", "", nil
}

type applyResp struct {
	Data struct {
		BatchID string   `json:"batch_id"`
		RunIDs  []string `json:"workflow_run_ids"`
		Replay  bool     `json:"replayed"`
	} `json:"data"`
}

func setupShopForWorkflow(e *env, id int64, name string) {
	e.fake.AddShop(shopeefake.Shop{ID: id, Name: name, Region: "VN", Description: "Old description",
		Channels: []shopeefake.Channel{{ID: 50011, Name: "Express", SupportsCOD: true}, {ID: 50012, Name: "Bulky", Enabled: true}},
		Addresses: []shopeefake.Address{{ID: 900, Region: "VN", City: "HCM", Line: "1 Old St", Zipcode: "700000",
			Types: []string{"DEFAULT_ADDRESS", "PICK_UP_ADDRESS", "RETURN_ADDRESS"}}}})
}

func publishTemplate(t *testing.T, c *client, org string, sections map[string]any) (string, string) {
	t.Helper()
	tpl, v := c.createTemplate(org, "Setup "+uuid.NewString()[:8], sections)
	c.must(200, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v+"/publish", nil)
	return tpl, v
}

var fullTemplate = map[string]any{
	"general":     map[string]any{"region": "VN", "shop_name": "Minh An Official"},
	"description": map[string]any{"text": "Đồ gia dụng chính hãng"},
	"branding":    map[string]any{"banner_urls": []string{"https://cdn.example/banner.png"}},
	"warehouse":   map[string]any{"instructions": "Use the HCM warehouse"},
	"logistics": map[string]any{"channels": []any{
		map[string]any{"logistics_channel_id": 50011, "name": "Express", "enabled": true, "cod_enabled": true},
		map[string]any{"logistics_channel_id": 50012, "enabled": true}, // already enabled: no change
	}},
	"return_address": map[string]any{"name": "Kho Minh An", "phone": "+84901234567", "address": "12 Lê Lợi", "city": "Hà Nội",
		"use_as": []string{"RETURN_ADDRESS"}},
}

func TestShopSetupEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	setupShopForWorkflow(e, 801, "Old Name")
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 801)
	tpl, v1 := publishTemplate(t, owner, org, fullTemplate)
	applyPath := "/orgs/" + org + "/templates/" + tpl + "/versions/" + v1 + "/apply"

	// Idempotency.
	key := uuid.NewString()
	var a1, a2 applyResp
	owner.must(400, "POST", applyPath, map[string]any{"shop_ids": []string{shop}}, "Idempotency-Key", "not-a-uuid")
	owner.must(202, "POST", applyPath, map[string]any{"shop_ids": []string{shop}}, "Idempotency-Key", key).json(t, &a1)
	owner.must(202, "POST", applyPath, map[string]any{"shop_ids": []string{shop}}, "Idempotency-Key", key).json(t, &a2)
	if len(a1.Data.RunIDs) != 1 || a2.Data.RunIDs[0] != a1.Data.RunIDs[0] || !a2.Data.Replay || a1.Data.Replay {
		t.Fatalf("idempotency: %+v %+v", a1, a2)
	}
	owner.must(409, "POST", applyPath, map[string]any{"shop_ids": []string{shop, uuid.NewString()}}, "Idempotency-Key", key)
	if code := owner.must(409, "POST", applyPath, map[string]any{"shop_ids": []string{shop}}).code(t); code != "shop_busy" {
		t.Fatalf("second run on same shop: %s", code)
	}
	if code := owner.must(409, "DELETE", "/orgs/"+org+"/templates/"+tpl, nil).code(t); code != "template_in_use" {
		t.Fatalf("delete while running: %s", code)
	}
	runID := a1.Data.RunIDs[0]

	// The run read the shop and waits for approval.
	v := owner.runView(org, runID)
	if v.Data.Run.Status != "WAITING_APPROVAL" || v.Data.PendingApproval == nil {
		t.Fatalf("waiting: %+v", v.Data.Run)
	}
	ch := byPath(v.Data.PendingApproval.Diff.Changes)
	if _, ok := ch["logistics.channels.50012"]; ok {
		t.Error("already-enabled channel listed as a change")
	}
	for _, p := range []string{"general.shop_name", "logistics.channels.50011", "return_address.address", "return_address.use_as"} {
		if c, ok := ch[p]; !ok || !c.Sensitive || c.DefaultSelected {
			t.Errorf("%s: %+v", p, c)
		}
	}
	if c := ch["return_address.use_as"]; !c.CurrentKnown {
		t.Errorf("use_as current should be read from Shopee: %+v", c)
	}
	if c := ch["description.text"]; !c.DefaultSelected || c.Current != "Old description" {
		t.Errorf("description: %+v", c)
	}
	aid := v.Data.PendingApproval.ID
	decision := "/orgs/" + org + "/approvals/" + aid + "/decision"
	owner.must(422, "POST", decision, map[string]any{"decision": "APPROVE", "selected_change_paths": []string{"logistics.channels.50012"}})

	// Approve everything except the warehouse instructions.
	selected := []string{"general.shop_name", "description.text", "branding.banner_urls", "logistics.channels.50011",
		"return_address.address", "return_address.use_as"}
	owner.must(200, "POST", decision, map[string]any{"decision": "APPROVE", "selected_change_paths": selected, "comment": "ok"})
	owner.must(409, "POST", decision, map[string]any{"decision": "REJECT"})

	// Profile applied; the banner is a manual task.
	v = owner.runView(org, runID)
	brandingID, status, out := v.step("apply_branding")
	if v.Data.Run.Status != "WAITING_USER" || status != "WAITING_USER" || !strings.Contains(string(out), "Upload shop banners") ||
		strings.Contains(string(out), "warehouse") {
		t.Fatalf("manual step: %s %s %s", v.Data.Run.Status, status, out)
	}
	if sh := e.fake.Shop(801); sh.Name != "Minh An Official" || sh.Description != "Đồ gia dụng chính hãng" {
		t.Fatalf("profile not applied: %+v", sh)
	}
	owner.must(409, "POST", "/orgs/"+org+"/workflows/"+runID+"/resume", nil)
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+runID+"/steps/"+brandingID+"/complete", map[string]any{"note": "Uploaded"})

	if len(e.queue.Errors) > 0 {
		t.Fatalf("queue errors: %v", e.queue.Errors)
	}
	v = owner.runView(org, runID)
	if v.Data.Run.Status != "COMPLETED" {
		t.Fatalf("final: %s %+v", v.Data.Run.Status, v.Data.Steps)
	}
	sh := e.fake.Shop(801)
	if !sh.Channels[0].Enabled || !sh.Channels[0].CODEnabled || sh.Addresses[0].City != "Hà Nội" || sh.Addresses[0].Name != "Kho Minh An" {
		t.Fatalf("logistics/address not applied: %+v", sh)
	}
	var report struct {
		Applied      []string `json:"applied"`
		ManualTasks  int      `json:"manual_tasks"`
		NotSelected  []string `json:"not_selected"`
		Mismatches   int      `json:"verification_mismatches"`
		ChangesFound int      `json:"changes_found"`
	}
	if err := json.Unmarshal(v.Data.Run.Output, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) != 5 || report.ManualTasks != 1 || len(report.NotSelected) != 1 || report.NotSelected[0] != "warehouse.instructions" || report.Mismatches != 0 {
		t.Fatalf("report: %s", v.Data.Run.Output)
	}

	// Execution log and events.
	var evs struct {
		Data []struct {
			Type string `json:"type"`
		} `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	owner.must(200, "GET", "/orgs/"+org+"/workflows/"+runID+"/events", nil).json(t, &evs)
	types := map[string]bool{}
	for _, ev := range evs.Data {
		types[ev.Type] = true
	}
	for _, want := range []string{"RUN_CREATED", "APPROVAL_REQUESTED", "APPROVAL_DECIDED", "USER_ACTION_REQUESTED", "USER_ACTION_COMPLETED", "RUN_STATUS_CHANGED"} {
		if !types[want] {
			t.Errorf("missing event %s", want)
		}
	}
	owner.must(200, "GET", "/orgs/"+org+"/workflow-events?level=warn", nil)
	owner.must(200, "GET", "/orgs/"+org+"/workflows?status=COMPLETED", nil)
	owner.must(422, "GET", "/orgs/"+org+"/workflows?status=NOPE", nil)
	owner.must(204, "DELETE", "/orgs/"+org+"/templates/"+tpl, nil)
}

func TestShopSetupPartialFailureRetryAndReconnect(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	setupShopForWorkflow(e, 802, "Shop")
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 802)
	tpl, v1 := publishTemplate(t, owner, org, map[string]any{
		"description": map[string]any{"text": "New"},
		"logistics":   map[string]any{"channels": []any{map[string]any{"logistics_channel_id": 50011, "enabled": true}}},
	})
	var a applyResp
	owner.must(202, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/apply", map[string]any{"shop_ids": []string{shop}}).json(t, &a)
	runID := a.Data.RunIDs[0]
	v := owner.runView(org, runID)

	e.fake.FailNext("/api/v2/logistics/update_channel", 200, "error_param", "Shipping channel cannot be enabled.")
	owner.must(200, "POST", "/orgs/"+org+"/approvals/"+v.Data.PendingApproval.ID+"/decision",
		map[string]any{"decision": "APPROVE", "selected_change_paths": []string{"description.text", "logistics.channels.50011"}})
	v = owner.runView(org, runID)
	_, status, _ := v.step("apply_logistics")
	if v.Data.Run.Status != "PARTIALLY_COMPLETED" || status != "FAILED" {
		t.Fatalf("partial: %s %s", v.Data.Run.Status, status)
	}
	if !strings.Contains(string(v.Data.Steps[7].Error), "cannot be enabled") {
		t.Fatalf("error: %s", v.Data.Steps[7].Error)
	}
	profileCalls := e.fake.Calls("/api/v2/shop/update_profile")
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+runID+"/retry", nil)
	v = owner.runView(org, runID)
	if v.Data.Run.Status != "COMPLETED" || e.fake.Calls("/api/v2/shop/update_profile") != profileCalls {
		t.Fatalf("after retry: %s, profile calls %d -> %d", v.Data.Run.Status, profileCalls, e.fake.Calls("/api/v2/shop/update_profile"))
	}

	// Authorization lost before the next run: the run waits for a reconnect.
	tpl2, v2 := publishTemplate(t, owner, org, map[string]any{"description": map[string]any{"text": "Newer"}})
	e.fake.RevokeTokens(802)
	e.fake.ExpireAccessTokens()
	owner.must(202, "POST", "/orgs/"+org+"/templates/"+tpl2+"/versions/"+v2+"/apply", map[string]any{"shop_ids": []string{shop}}).json(t, &a)
	runID = a.Data.RunIDs[0]
	v = owner.runView(org, runID)
	_, status, out := v.step("validate_connection")
	if v.Data.Run.Status != "WAITING_USER" || status != "WAITING_USER" || !strings.Contains(string(out), "reconnect_required") {
		t.Fatalf("lost auth: %s %s %s", v.Data.Run.Status, status, out)
	}
	// Reconnect, then resume.
	var rc struct {
		Data struct {
			AuthorizationURL string `json:"authorization_url"`
		} `json:"data"`
	}
	owner.must(200, "POST", "/orgs/"+org+"/shops/"+shop+"/reconnect", nil).json(t, &rc)
	state := rc.Data.AuthorizationURL[strings.Index(rc.Data.AuthorizationURL, "state=")+6:]
	callback(t, owner, map[string][]string{"code": {e.fake.Authorize(802)}, "shop_id": {"802"}, "state": {state}})
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+runID+"/resume", nil)
	if v = owner.runView(org, runID); v.Data.Run.Status != "WAITING_APPROVAL" {
		t.Fatalf("after resume: %s", v.Data.Run.Status)
	}
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+runID+"/cancel", map[string]any{"reason": "not today"})
	if v = owner.runView(org, runID); v.Data.Run.Status != "CANCELLED" {
		t.Fatalf("cancelled: %s", v.Data.Run.Status)
	}
	if code := owner.must(409, "POST", "/orgs/"+org+"/workflows/"+runID+"/cancel", map[string]any{}).code(t); code != "not_cancellable" {
		t.Fatalf("cancel twice: %s", code)
	}
}

func TestSelfApprovalRule(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	setupShopForWorkflow(e, 803, "Shop")
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 803)
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")
	viewer := e.signUp("viewer@a.example", "Viewer")
	e.addMember(owner, org, viewer, "viewer@a.example", "viewer")
	tpl, v1 := publishTemplate(t, owner, org, map[string]any{"description": map[string]any{"text": "By operator"}})

	viewer.must(403, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/apply", map[string]any{"shop_ids": []string{shop}})
	var a applyResp
	op.must(202, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v1+"/apply", map[string]any{"shop_ids": []string{shop}}).json(t, &a)
	v := op.runView(org, a.Data.RunIDs[0])
	decision := "/orgs/" + org + "/approvals/" + v.Data.PendingApproval.ID + "/decision"
	body := map[string]any{"decision": "APPROVE", "selected_change_paths": []string{"description.text"}}

	viewer.must(403, "POST", decision, body)
	if code := op.must(403, "POST", decision, body).code(t); code != "self_approval_not_allowed" {
		t.Fatalf("self approval: %s", code)
	}
	// The org allows self-approval: the operator may approve.
	r := owner.must(200, "GET", "/orgs/"+org, nil)
	owner.must(200, "PATCH", "/orgs/"+org, map[string]any{"settings": map[string]any{"allow_self_approval": true}}, "If-Match", r.Header.Get("ETag"))
	op.must(200, "POST", decision, body)
	if v = op.runView(org, a.Data.RunIDs[0]); v.Data.Run.Status != "COMPLETED" {
		t.Fatalf("after approval: %s", v.Data.Run.Status)
	}
	var list struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	viewer.must(200, "GET", "/orgs/"+org+"/approvals?status=APPROVED", nil).json(t, &list)
	if len(list.Data) != 1 {
		t.Fatalf("approvals: %+v", list)
	}
}
