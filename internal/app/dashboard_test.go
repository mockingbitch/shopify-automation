package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/shopee/shopeefake"
)

type notesResp struct {
	Data []struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Link  string `json:"link"`
	} `json:"data"`
	Unread int `json:"unread"`
}

func (c *client) notes(org string) notesResp {
	c.e.t.Helper()
	var n notesResp
	c.must(200, "GET", "/orgs/"+org+"/notifications", nil).json(c.e.t, &n)
	return n
}

func TestOverviewAndNotifications(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	setupShopForWorkflow(e, 841, "Shop")
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")
	viewer := e.signUp("viewer@a.example", "Viewer")
	e.addMember(owner, org, viewer, "viewer@a.example", "viewer")
	shop := e.connectShop(owner, org, 841)

	tpl, v := publishTemplate(t, owner, org, map[string]any{"description": map[string]any{"text": "New"}})
	var a applyResp
	op.must(202, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v+"/apply", map[string]any{"shop_ids": []string{shop}}).json(t, &a)
	runID := a.Data.RunIDs[0]

	var ov struct {
		Data map[string]int `json:"data"`
	}
	viewer.must(200, "GET", "/orgs/"+org+"/overview", nil).json(t, &ov)
	if ov.Data["connected_shops"] != 1 || ov.Data["active_shops"] != 1 || ov.Data["waiting_workflows"] != 1 || ov.Data["pending_approvals"] != 1 {
		t.Fatalf("overview: %+v", ov.Data)
	}

	// Approvers are notified; the viewer is not.
	for _, c := range []*client{owner, op} {
		n := c.notes(org)
		if n.Unread != 1 || n.Data[0].Kind != "approval_requested" || !strings.HasSuffix(n.Data[0].Link, "/workflows/"+runID) {
			t.Fatalf("approver notification: %+v", n)
		}
	}
	if n := viewer.notes(org); len(n.Data) != 0 {
		t.Fatalf("viewer notified: %+v", n)
	}
	n := owner.notes(org)
	owner.must(204, "POST", "/orgs/"+org+"/notifications/"+n.Data[0].ID+"/read", nil)
	owner.must(404, "POST", "/orgs/"+org+"/notifications/"+n.Data[0].ID+"/read", nil)
	// Another user's notification is not reachable.
	opNote := op.notes(org).Data[0].ID
	owner.must(404, "POST", "/orgs/"+org+"/notifications/"+opNote+"/read", nil)
	if owner.notes(org).Unread != 0 {
		t.Fatal("still unread")
	}

	// Approve; the step that cannot finish (lost authorization) notifies
	// the operator who started the run.
	e.fake.RevokeTokens(841)
	e.fake.ExpireAccessTokens()
	rv := owner.runView(org, runID)
	owner.must(200, "POST", "/orgs/"+org+"/approvals/"+rv.Data.PendingApproval.ID+"/decision",
		map[string]any{"decision": "APPROVE", "selected_change_paths": []string{"description.text"}})
	kinds := func(c *client) map[string]bool {
		k := map[string]bool{}
		for _, x := range c.notes(org).Data {
			k[x.Kind] = true
		}
		return k
	}
	// The operator who started the run must act; owners and admins (who
	// can connect shops) hear about the lost authorization.
	if k := kinds(op); !k["run_needs_user"] || k["shop_reconnect_required"] {
		t.Fatalf("operator notifications: %+v", op.notes(org))
	}
	if k := kinds(owner); !k["shop_reconnect_required"] {
		t.Fatalf("owner notifications: %+v", owner.notes(org))
	}
	if n := viewer.notes(org); len(n.Data) != 0 {
		t.Fatalf("viewer notified about reconnect: %+v", n)
	}
	op.must(204, "POST", "/orgs/"+org+"/notifications/read-all", nil)
	if op.notes(org).Unread != 0 {
		t.Fatal("read-all")
	}
	op.must(422, "GET", "/orgs/"+org+"/notifications?limit=0", nil)

	viewer.must(200, "GET", "/orgs/"+org+"/overview", nil).json(t, &ov)
	if ov.Data["reconnect_required"] != 1 || ov.Data["disconnected_shops"] != 1 || ov.Data["active_shops"] != 0 {
		t.Fatalf("overview after revoke: %+v", ov.Data)
	}
	var act struct {
		Data struct {
			LastSetup *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"last_setup"`
			Runs []struct {
				ID string `json:"id"`
			} `json:"runs"`
		} `json:"data"`
	}
	viewer.must(200, "GET", "/orgs/"+org+"/shops/"+shop+"/activity", nil).json(t, &act)
	if act.Data.LastSetup == nil || act.Data.LastSetup.ID != runID || act.Data.LastSetup.Status != "WAITING_USER" || len(act.Data.Runs) != 1 {
		t.Fatalf("activity: %+v", act.Data)
	}
}

// A mail outage does not lose emails: they wait encrypted in the outbox and
// go out when the server is back.
func TestEmailOutbox(t *testing.T) {
	e := newEnv(t)
	e.requireSystem()
	e.app.Outbox.System = e.system
	e.mail.SetFail(true)
	e.client().must(202, "POST", "/auth/register", map[string]string{"email": "late@a.example", "password": testPassword, "display_name": "Late"})
	if _, ok := e.mail.Last("late@a.example"); ok {
		t.Fatal("sent during the outage")
	}
	var n int
	var enc []byte
	if err := e.db.Tx(context.Background(), db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) OVER (), message_enc FROM email_outbox LIMIT 1").Scan(&n, &enc)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 || bytes.Contains(enc, []byte("late@a.example")) || bytes.Contains(enc, []byte("verify")) {
		t.Fatalf("outbox: %d rows, plaintext visible: %v", n, bytes.Contains(enc, []byte("late@")))
	}
	e.mail.SetFail(false)
	e.clock.Advance(10 * time.Minute)
	if sent, err := e.app.Outbox.Sweep(context.Background()); err != nil || sent != 1 {
		t.Fatalf("sweep: %d %v", sent, err)
	}
	if m, ok := e.mail.Last("late@a.example"); !ok || !strings.Contains(m.Text, "/verify-email") {
		t.Fatalf("email after recovery: %+v", m)
	}
	if err := e.db.Tx(context.Background(), db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM email_outbox").Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("sent email kept: %d %v", n, err)
	}
}

func TestCatalogPickers(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 851, Name: "Shop", Region: "VN"})
	e.fake.SetCatalog(publishCatalog())
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 851)
	base := "/orgs/" + org + "/shops/" + shop + "/catalog/categories"

	var cats struct {
		Data []struct {
			ID          int64 `json:"category_id"`
			HasChildren bool  `json:"has_children"`
		} `json:"data"`
	}
	owner.must(200, "GET", base, nil).json(t, &cats)
	if len(cats.Data) != 2 {
		t.Fatalf("categories: %+v", cats)
	}
	calls := e.fake.Calls("/api/v2/product/get_category")
	owner.must(200, "GET", base, nil)
	if e.fake.Calls("/api/v2/product/get_category") != calls {
		t.Fatal("categories not cached")
	}
	owner.must(200, "GET", base+"?refresh=true", nil)
	if e.fake.Calls("/api/v2/product/get_category") != calls+1 {
		t.Fatal("refresh ignored")
	}
	// After a day the cache is stale.
	if err := e.db.Tx(context.Background(), db.Scope{OrgID: uuid.MustParse(org)}, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE shopee_catalog_cache SET fetched_at = fetched_at - interval '25 hours'")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	owner.must(200, "GET", base, nil)
	if e.fake.Calls("/api/v2/product/get_category") != calls+2 {
		t.Fatal("cache did not expire")
	}
	r := owner.must(200, "GET", base+"/100017/attributes", nil)
	if !strings.Contains(string(r.Body), `"mandatory":true`) {
		t.Fatalf("attributes: %s", r.Body)
	}
	r = owner.must(200, "GET", base+"/100017/brands", nil)
	if !strings.Contains(string(r.Body), "Minh An") {
		t.Fatalf("brands: %s", r.Body)
	}
	owner.must(404, "GET", base+"/abc/brands", nil)

	// Other organizations cannot read through this shop.
	bob := e.signUp("bob@b.example", "Bob")
	orgB := bob.createOrg("Org B", "org-b")
	bob.must(404, "GET", "/orgs/"+orgB+"/shops/"+shop+"/catalog/categories", nil)
}

func TestCapabilityOverridesAndAppOptions(t *testing.T) {
	e := newEnv(t)
	admin, appID, _ := e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 861, Name: "Shop", Region: "VN"})
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, 861)

	r := owner.must(200, "GET", "/orgs/"+org+"/shopee-apps", nil)
	if !strings.Contains(string(r.Body), appID) || strings.Contains(string(r.Body), string(testPartnerKey)) {
		t.Fatalf("app options: %s", r.Body)
	}
	owner.must(404, "GET", "/admin/capabilities", nil)
	owner.must(404, "PUT", "/admin/capability-overrides", map[string]any{"capability_key": "shop.profile.update", "enabled": false, "reason": "incident"})
	admin.must(422, "PUT", "/admin/capability-overrides", map[string]any{"capability_key": "shop.profile.update", "reason": "no change"})
	admin.must(422, "PUT", "/admin/capability-overrides", map[string]any{"capability_key": "nope", "enabled": false, "reason": "incident"})

	var o struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	admin.must(200, "PUT", "/admin/capability-overrides", map[string]any{"capability_key": "shop.profile.update", "region": "VN", "enabled": false,
		"reason": "Shopee incident"}).json(t, &o)
	capOf := func() (bool, string) {
		var caps struct {
			Data []struct {
				Key     string `json:"key"`
				Enabled bool   `json:"enabled"`
				Reason  string `json:"reason"`
			} `json:"data"`
		}
		owner.must(200, "GET", "/orgs/"+org+"/shops/"+shop+"/capabilities", nil).json(t, &caps)
		for _, c := range caps.Data {
			if c.Key == "shop.profile.update" {
				return c.Enabled, c.Reason
			}
		}
		t.Fatal("capability missing")
		return false, ""
	}
	if on, why := capOf(); on || why != "Shopee incident" {
		t.Fatalf("kill switch: %v %s", on, why)
	}
	r = admin.must(200, "GET", "/admin/capabilities", nil)
	if !strings.Contains(string(r.Body), "Shopee incident") {
		t.Fatalf("registry: %s", r.Body)
	}
	admin.must(204, "DELETE", "/admin/capability-overrides/"+o.Data.ID, nil)
	admin.must(404, "DELETE", "/admin/capability-overrides/"+o.Data.ID, nil)
	if on, _ := capOf(); !on {
		t.Fatal("override not removed")
	}
}

// Stop in the extension gives the task back without counting a failure.
func TestExtensionRelease(t *testing.T) {
	b := newBannerEnv(t, 871, nil, []string{"branding.banner_urls"})
	e := b.e
	id := e.extTasks(b.token)[0].ID
	var c claimResp
	e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).json(t, &c)
	e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/release", map[string]any{"lease_token": c.Data.LeaseToken})
	if code := e.extMust(409, b.token, "POST", "/ext/tasks/"+id+"/release", map[string]any{"lease_token": c.Data.LeaseToken}).code(t); code != "lease_lost" {
		t.Fatalf("release twice: %s", code)
	}
	tasks := e.extTasks(b.token)
	if len(tasks) != 1 || tasks[0].Status != "QUEUED" {
		t.Fatalf("released task: %+v", tasks)
	}
	var recipes struct {
		Data []struct {
			FailureCount int `json:"failure_count"`
		} `json:"data"`
	}
	b.admin.must(200, "GET", "/admin/browser-recipes", nil).json(t, &recipes)
	if recipes.Data[0].FailureCount != 0 {
		t.Fatalf("release counted as failure: %+v", recipes)
	}
	e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil)
}
