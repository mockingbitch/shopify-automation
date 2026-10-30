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

	ids := map[string]string{"{memberID}": aMember, "{invitationID}": inv.Data.ID, "{shopID}": aShop, "{stateID}": aSelection,
		"{templateID}": aTpl, "{versionID}": aVersion, "{sectionKey}": "description"}
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
			r := bob.do(rt.Method, path, body, "If-Match", `W/"0"`)
			if r.Status != 404 && (target.org != orgB || r.Status != 403) {
				t.Errorf("%s (%s): status %d, want 404: %s", key, target.name, r.Status, r.Body)
			}
			if strings.Contains(string(r.Body), "Org A") || strings.Contains(string(r.Body), "alice@a.example") ||
				strings.Contains(string(r.Body), "1001") || strings.Contains(string(r.Body), "Org A secret") {
				t.Errorf("%s (%s): response leaks org A data: %s", key, target.name, r.Body)
			}
			checked++
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
	alice.createTemplate(orgA.String(), "Org A template", nil)

	tenantTables := []string{"organizations", "organization_members", "organization_invitations", "audit_logs",
		"shop_connections", "shop_credentials", "shop_authorizations", "shop_profiles", "oauth_states",
		"shop_templates", "shop_template_versions", "shop_template_sections"}
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
	err := e.db.Tx(ctx, db.Scope{OrgID: orgB}, func(tx pgx.Tx) error {
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
	selfAllowed := map[string]bool{"DELETE /orgs/{orgID}/members/{memberID}": true}
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
