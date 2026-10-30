package app_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/app"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/shopee/shopeefake"
)

// TestTenantIsolationHTTP signs in as a member of org B and calls every
// org-scoped route with org A's IDs. Every call must look like "not found",
// never succeed, and never reveal that org A exists. New routes are covered
// automatically because the test walks the route table.
func TestTenantIsolationHTTP(t *testing.T) {
	e := newEnv(t)
	alice := e.signUp("alice@a.example", "Alice")
	orgA := alice.createOrg("Org A", "org-a")
	bob := e.signUp("bob@b.example", "Bob")
	orgB := bob.createOrg("Org B", "org-b")

	// Objects inside org A that B might try to reach.
	aMember := findMember(alice.members(orgA), "alice@a.example").ID
	var inv struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(201, "POST", "/orgs/"+orgA+"/invitations", map[string]string{"email": "victim@a.example", "role": "viewer"}).json(t, &inv)

	// A connected shop and a pending main-account selection in org A.
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 1001, Name: "Org A shop", Region: "VN"})
	e.fake.AddShop(shopeefake.Shop{ID: 1002, Name: "Org A other", Region: "VN"})
	alice.noRedirect()
	aShop := e.connectShop(alice, orgA, 1001)
	state := startConnect(t, alice, orgA)
	loc := callback(t, alice, url.Values{"code": {e.fake.AuthorizeMain(5, 1002)}, "main_account_id": {"5"}, "state": {state}})
	aSelection := loc.Query().Get("state")

	aTpl, aVersion := alice.createTemplate(orgA, "Org A template", map[string]any{"description": map[string]any{"text": "Org A secret text"}})
	// A workflow run of org A, waiting for approval.
	alice.must(200, "POST", "/orgs/"+orgA+"/templates/"+aTpl+"/versions/"+aVersion+"/publish", nil)
	var applied applyResp
	alice.must(202, "POST", "/orgs/"+orgA+"/templates/"+aTpl+"/versions/"+aVersion+"/apply", map[string]any{"shop_ids": []string{aShop}}).json(t, &applied)
	aRun := applied.Data.RunIDs[0]
	aRunView := alice.runView(orgA, aRun)
	if aRunView.Data.PendingApproval == nil {
		t.Fatalf("org A run not waiting for approval: %s", aRunView.Data.Run.Status)
	}
	aApproval := aRunView.Data.PendingApproval.ID
	aStep := aRunView.Data.Steps[0].ID

	// Products, an image and an import of org A.
	aProduct := createProduct(alice, orgA, baseProduct("ORG-A-SKU"), true)
	var ap productResp
	alice.must(200, "GET", "/orgs/"+orgA+"/products/"+aProduct, nil).json(t, &ap)
	aImage := ap.Data.Images[0].ID
	var ab batchResp
	alice.upload("/orgs/"+orgA+"/imports", "a.csv", []byte("sku,name,price,stock\nA-1,Org A secret name,abc,1\n"), nil).json(t, &ab)
	var ar rowsResp
	alice.must(200, "GET", "/orgs/"+orgA+"/imports/"+ab.Data.ID+"/rows", nil).json(t, &ar)
	if len(ar.Data) != 1 {
		t.Fatalf("org A import rows: %+v", ar)
	}

	// A paired browser, a browser task and a screenshot of org A.
	e.requireSystem()
	_, aDevice := e.pair(alice, orgA)
	aTask, aShot := insertBrowserTask(t, e, uuid.MustParse(orgA), uuid.MustParse(aRun), uuid.MustParse(aStep), uuid.MustParse(aShop))

	// Alice was notified about the approval.
	var notes struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/orgs/"+orgA+"/notifications", nil).json(t, &notes)
	if len(notes.Data) == 0 {
		t.Fatal("org A has no notification")
	}
	ids := map[string]string{"{notificationID}": notes.Data[0].ID, "{categoryID}": "100017", "{deviceID}": aDevice, "{taskID}": aTask.String(), "{screenshotID}": aShot.String(), "{productID}": aProduct, "{imageID}": aImage, "{batchID}": ab.Data.ID, "{rowID}": ar.Data[0].ID,
		"{memberID}": aMember, "{invitationID}": inv.Data.ID, "{shopID}": aShop, "{stateID}": aSelection,
		"{templateID}": aTpl, "{versionID}": aVersion, "{sectionKey}": "description",
		"{runID}": aRun, "{stepID}": aStep, "{approvalID}": aApproval}
	bodies := map[string]any{
		"PATCH /orgs/{orgID}":                                                                 map[string]any{"name": "pwned"},
		"DELETE /orgs/{orgID}":                                                                map[string]string{"confirm_slug": "org-a"},
		"POST /orgs/{orgID}/transfer-ownership":                                               map[string]string{"new_owner_user_id": uuid.NewString()},
		"PATCH /orgs/{orgID}/members/{memberID}":                                              map[string]string{"role": "viewer"},
		"POST /orgs/{orgID}/invitations":                                                      map[string]string{"email": "bob@b.example", "role": "admin"},
		"DELETE /orgs/{orgID}/shops/{shopID}":                                                 map[string]any{"confirm": true},
		"POST /orgs/{orgID}/shops/connect/{stateID}/select":                                   map[string]any{"shopee_shop_ids": []string{"1002"}},
		"POST /orgs/{orgID}/templates":                                                        map[string]any{"name": "pwned"},
		"PATCH /orgs/{orgID}/templates/{templateID}":                                          map[string]any{"name": "pwned"},
		"POST /orgs/{orgID}/templates/{templateID}/clone":                                     map[string]any{"name": "stolen"},
		"POST /orgs/{orgID}/templates/{templateID}/versions":                                  map[string]any{"from_version_id": aVersion},
		"PUT /orgs/{orgID}/templates/{templateID}/versions/{versionID}/sections/{sectionKey}": map[string]any{"text": "pwned"},
		"POST /orgs/{orgID}/templates/{templateID}/versions/{versionID}/preview":              map[string]any{"shop_ids": []string{aShop}},
		"PUT /orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides":                   map[string]any{"overrides": map[string]any{}},
		"POST /orgs/{orgID}/templates/{templateID}/versions/{versionID}/apply":                map[string]any{"shop_ids": []string{aShop}},
		"POST /orgs/{orgID}/approvals/{approvalID}/decision":                                  map[string]any{"decision": "REJECT"},
		"POST /orgs/{orgID}/products/publish":                                                 map[string]any{"shop_ids": []string{aShop}, "product_ids": []string{aProduct}},
		"PUT /orgs/{orgID}/products/{productID}":                                              baseProduct("PWNED"),
		"PATCH /orgs/{orgID}/imports/{batchID}/rows/{rowID}":                                  map[string]any{"values": map[string]string{"price": "1"}},
	}

	checked := 0
	for _, rt := range e.app.Routes() {
		if rt.Access != app.Tenant && rt.Access != app.TenantSelf {
			continue
		}
		key := rt.Method + " " + rt.Path
		for _, target := range []struct{ name, org string }{{"foreign org", orgA}, {"own org, foreign ids", orgB}} {
			path := strings.ReplaceAll(rt.Path, "{orgID}", target.org)
			hasChildID := false
			for ph, id := range ids {
				if strings.Contains(path, ph) {
					hasChildID = true
					path = strings.ReplaceAll(path, ph, id)
				}
			}
			if target.org == orgB && !hasChildID {
				continue // own org without foreign child IDs is a normal call
			}
			body := bodies[key]
			if body == nil && rt.Method != "GET" {
				body = map[string]any{}
			}
			var r resp
			if strings.HasSuffix(rt.Path, "/images") || rt.Path == "/orgs/{orgID}/imports" {
				r = bob.upload(path, "a.png", testPNG(10, 10), nil)
			} else {
				r = bob.do(rt.Method, path, body, "If-Match", `W/"0"`)
			}
			if r.Status != 404 && (target.org != orgB || r.Status != 403) {
				t.Errorf("%s (%s): status %d, want 404: %s", key, target.name, r.Status, r.Body)
			}
			if strings.Contains(string(r.Body), "Org A") || strings.Contains(string(r.Body), "alice@a.example") ||
				strings.Contains(string(r.Body), `"1001"`) || strings.Contains(string(r.Body), "Org A secret") {
				t.Errorf("%s (%s): response leaks org A data: %s", key, target.name, r.Body)
			}
			checked++
		}
	}
	// Publishing org A's products or shops from org B finds nothing.
	for _, body := range []map[string]any{
		{"shop_ids": []string{aShop}, "product_ids": []string{createProduct(bob, orgB, baseProduct("B-1"), false)}},
		{"shop_ids": []string{uuid.NewString()}, "product_ids": []string{aProduct}},
		{"shop_ids": []string{uuid.NewString()}, "import_batch_id": ab.Data.ID},
	} {
		if r := bob.do("POST", "/orgs/"+orgB+"/products/publish", body); r.Status != 422 || strings.Contains(string(r.Body), "Org A") {
			t.Errorf("cross-org publish %v: %d %s", body, r.Status, r.Body)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d checks ran; route table changed?", checked)
	}

	// Org A is untouched.
	var o orgResp
	alice.must(200, "GET", "/orgs/"+orgA, nil).json(t, &o)
	if o.Data.Name != "Org A" || len(alice.members(orgA)) != 1 {
		t.Fatalf("org A was modified: %+v", o)
	}
	var shop shopDetail
	alice.must(200, "GET", "/orgs/"+orgA+"/shops/"+aShop, nil).json(t, &shop)
	if shop.Data.Shop.Status != "ACTIVE" {
		t.Fatalf("org A shop was modified: %+v", shop.Data.Shop)
	}
	alice.must(200, "GET", "/orgs/"+orgA+"/shops/connect/"+aSelection, nil)
	if v := alice.runView(orgA, aRun); v.Data.Run.Status != "WAITING_APPROVAL" || v.Data.Run.ID != aRun {
		t.Fatalf("org A run was modified: %s", v.Data.Run.Status)
	}
	// Bob's org list only shows org B.
	var list struct {
		Data []struct {
			Organization struct {
				ID string `json:"id"`
			} `json:"organization"`
		} `json:"data"`
	}
	bob.must(200, "GET", "/orgs", nil).json(t, &list)
	if len(list.Data) != 1 || list.Data[0].Organization.ID != orgB {
		t.Fatalf("bob sees: %+v", list)
	}
}

// TestTenantIsolationDatabase checks the RLS policies directly, as the
// runtime role, independent of application code.
func TestTenantIsolationDatabase(t *testing.T) {
	e := newEnv(t)
	alice := e.signUp("alice@a.example", "Alice")
	orgA := uuid.MustParse(alice.createOrg("Org A", "org-a"))
	alice.must(201, "POST", "/orgs/"+orgA.String()+"/invitations", map[string]string{"email": "x@a.example", "role": "viewer"})
	bob := e.signUp("bob@b.example", "Bob")
	orgB := uuid.MustParse(bob.createOrg("Org B", "org-b"))
	ctx := context.Background()
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 1001, Name: "Org A shop", Region: "VN"})
	e.connectShop(alice.noRedirect(), orgA.String(), 1001)
	tplA, verA := alice.createTemplate(orgA.String(), "Org A template", map[string]any{"description": map[string]any{"text": "x"}})
	alice.must(200, "POST", "/orgs/"+orgA.String()+"/templates/"+tplA+"/versions/"+verA+"/publish", nil)
	var shopsA struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/orgs/"+orgA.String()+"/shops", nil).json(t, &shopsA)
	alice.must(200, "PUT", "/orgs/"+orgA.String()+"/templates/"+tplA+"/shops/"+shopsA.Data[0].ID+"/overrides",
		map[string]any{"overrides": map[string]any{"description": map[string]any{"text": "y"}}})
	var run applyResp
	alice.must(202, "POST", "/orgs/"+orgA.String()+"/templates/"+tplA+"/versions/"+verA+"/apply", map[string]any{"shop_ids": []string{shopsA.Data[0].ID}}).json(t, &run)

	// Product rows of every kind in org A.
	pv := baseProduct("A-V")
	pv["tiers"] = []map[string]any{{"name": "Color", "options": []string{"Red"}}}
	pv["variations"] = []map[string]any{{"tier_index": []int{0}, "price": "1000", "stock": 1}}
	pid := uuid.MustParse(createProduct(alice, orgA.String(), pv, true))
	alice.upload("/orgs/"+orgA.String()+"/imports", "a.csv", []byte("sku,name,price,stock\nA-1,x,abc,1\n"), nil)
	shopA := uuid.MustParse(shopsA.Data[0].ID)
	items, err := e.app.Products.LoadForPublish(ctx, orgA, []uuid.UUID{pid})
	if err != nil || len(items) != 1 {
		t.Fatalf("load: %v", err)
	}
	if _, err := e.app.Products.EnsureMapping(ctx, orgA, shopA, pid, "A-V", uuid.MustParse(run.Data.RunIDs[0])); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Products.RecordImageUpload(ctx, orgA, items[0].Images[0].ID, shopA, "img-1", ""); err != nil {
		t.Fatal(err)
	}

	// Extension rows of every kind in org A.
	_, _ = e.pair(alice, orgA.String())
	var code struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	alice.must(201, "POST", "/orgs/"+orgA.String()+"/extension/pairing-codes", nil).json(t, &code)
	runA := uuid.MustParse(run.Data.RunIDs[0])
	var stepA uuid.UUID
	if err := e.db.Tx(ctx, db.Scope{OrgID: orgA}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id FROM workflow_steps WHERE run_id = $1 LIMIT 1", runA).Scan(&stepA)
	}); err != nil {
		t.Fatal(err)
	}
	insertBrowserTask(t, e, orgA, runA, stepA, shopA)
	if err := e.db.Tx(ctx, db.Scope{OrgID: orgA}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO shopee_catalog_cache (org_id, shop_connection_id, kind, cache_key, payload, fetched_at)
			VALUES ($1, $2, 'categories', '', '[]', now())`, orgA, shopA)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	tenantTables := []string{"organizations", "organization_members", "organization_invitations", "audit_logs",
		"shop_connections", "shop_credentials", "shop_authorizations", "shop_profiles", "oauth_states",
		"shop_templates", "shop_template_versions", "shop_template_sections", "shop_template_mappings",
		"workflow_runs", "workflow_steps", "workflow_step_attempts", "workflow_approvals", "workflow_events", "shop_config_snapshots",
		"products", "product_variations", "product_images", "product_mappings", "product_image_uploads",
		"product_import_batches", "product_import_rows",
		"extension_pairing_codes", "extension_devices", "browser_tasks", "browser_task_screenshots",
		"notifications", "shopee_catalog_cache"}

	// Every table with an org_id column has forced RLS and is listed above.
	listed := map[string]bool{}
	for _, tbl := range tenantTables {
		listed[tbl] = true
	}
	rows, err := e.db.Pool.Query(ctx, `SELECT c.relname, c.relrowsecurity AND c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'org_id' AND NOT a.attisdropped)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var forced bool
		if err := rows.Scan(&name, &forced); err != nil {
			t.Fatal(err)
		}
		if !forced {
			t.Errorf("%s has org_id but no forced row-level security", name)
		}
		if !listed[name] {
			t.Errorf("%s has org_id but is not in the isolation test", name)
		}
	}
	rows.Close()
	count := func(scope db.Scope, table, where string, args ...any) int {
		t.Helper()
		var n int
		err := e.db.Tx(ctx, scope, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" "+where, args...).Scan(&n)
		})
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		return n
	}

	// No tenant set: nothing is visible.
	for _, tbl := range tenantTables {
		if n := count(db.Scope{}, tbl, ""); n != 0 {
			t.Errorf("%s visible without tenant: %d rows", tbl, n)
		}
	}
	// Tenant B sees none of org A's rows, even when filtering for them.
	for _, tbl := range tenantTables {
		col := "org_id"
		if tbl == "organizations" {
			col = "id"
		}
		if n := count(db.Scope{OrgID: orgB}, tbl, "WHERE "+col+" = $1", orgA); n != 0 {
			t.Errorf("%s: tenant B sees %d rows of org A", tbl, n)
		}
		if tbl != "organization_invitations" && count(db.Scope{OrgID: orgA}, tbl, "WHERE "+col+" = $1", orgA) == 0 {
			t.Errorf("%s: tenant A cannot see its own rows", tbl)
		}
	}

	// Writes into another tenant are rejected by WITH CHECK.
	err = e.db.Tx(ctx, db.Scope{OrgID: orgB}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO organization_members (id, org_id, user_id, role_key)
			SELECT $1, $2, user_id, 'admin' FROM organization_members LIMIT 1`, db.NewID(), orgA)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("cross-tenant insert: %v", err)
	}
	// Updates cannot reach another tenant's rows (0 rows affected).
	var affected int64
	err = e.db.Tx(ctx, db.Scope{OrgID: orgB}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE organizations SET name = 'pwned' WHERE id = $1`, orgA)
		affected = tag.RowsAffected()
		return err
	})
	if err != nil || affected != 0 {
		t.Fatalf("cross-tenant update: affected=%d err=%v", affected, err)
	}
	// Scope settings do not leak to the next transaction on a pooled connection.
	if n := count(db.Scope{}, "organizations", ""); n != 0 {
		t.Fatalf("scope leaked across transactions: %d", n)
	}
}

// TestRouteTableRules guards the access declarations.
func TestRouteTableRules(t *testing.T) {
	e := newEnv(t)
	// Notifications are per user: the queries filter by the caller.
	selfAllowed := map[string]bool{"DELETE /orgs/{orgID}/members/{memberID}": true, "GET /orgs/{orgID}/notifications": true,
		"POST /orgs/{orgID}/notifications/read-all": true, "POST /orgs/{orgID}/notifications/{notificationID}/read": true}
	seen := map[string]bool{}
	for _, rt := range e.app.Routes() {
		key := rt.Method + " " + rt.Path
		if seen[key] {
			t.Errorf("duplicate route %s", key)
		}
		seen[key] = true
		if strings.HasPrefix(rt.Path, "/orgs/{orgID}") {
			if rt.Access == app.Tenant && rt.Perm == "" {
				t.Errorf("%s: tenant route without permission", key)
			}
			if rt.Access == app.TenantSelf && !selfAllowed[key] {
				t.Errorf("%s: TenantSelf must be explicitly allowlisted", key)
			}
			if rt.Access != app.Tenant && rt.Access != app.TenantSelf {
				t.Errorf("%s: org-scoped route must resolve the tenant", key)
			}
		}
		if rt.Access == app.Public && rt.Method == "GET" {
			t.Errorf("%s: unexpected public GET", key)
		}
	}
}

// TestSystemRoleCannotReadSecrets checks that the BYPASSRLS role only has
// the columns it needs: it can find tenants, never read tokens or keys.
func TestSystemRoleCannotReadSecrets(t *testing.T) {
	e := newEnv(t)
	e.requireSystem()
	ctx := context.Background()
	for _, q := range []string{
		"SELECT access_token_enc FROM shop_credentials",
		"SELECT refresh_token_enc FROM shop_credentials",
		"SELECT partner_key_enc FROM shopee_apps",
		"SELECT shop_name FROM shop_connections",
		"SELECT raw FROM shop_profiles",
		"SELECT output FROM workflow_steps",
		"SELECT input FROM workflow_runs",
		"SELECT diff_snapshot FROM workflow_approvals",
		"UPDATE workflow_steps SET status = 'PENDING'",
		"UPDATE shop_connections SET status = 'ACTIVE'",
	} {
		err := e.system.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: %v", q, err)
		}
	}
}

// insertBrowserTask writes a browser task and a screenshot row directly in
// the org's scope (the isolation tests only need the rows).
func insertBrowserTask(t *testing.T, e *env, org, run, step, shop uuid.UUID) (task, shot uuid.UUID) {
	t.Helper()
	task, shot = db.NewID(), db.NewID()
	err := e.db.Tx(context.Background(), db.Scope{OrgID: org}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO browser_tasks (id, org_id, shop_connection_id, run_id, step_id, task_key,
			capability_key, market, title) VALUES ($1, $2, $3, $4, $5, 'branding.banner_urls', 'shop.banner.upload', 'VN', 'Upload shop banners')`,
			task, org, shop, run, step); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO browser_task_screenshots (id, org_id, task_id, storage_key) VALUES ($1, $2, $3, 'x')`,
			shot, org, task)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return task, shot
}
