package app_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/testdb"
)

func TestCreateAndUpdateOrg(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("owner@example.com", "Owner")
	orgID := c.createOrg("Acme Shops", "acme")
	c.must(422, "POST", "/orgs", map[string]string{"name": "Dup", "slug": "acme"})
	c.must(422, "POST", "/orgs", map[string]string{"name": "Bad", "slug": "admin"})

	var list struct {
		Data []struct {
			Role        string   `json:"role"`
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	c.must(200, "GET", "/orgs", nil).json(t, &list)
	if len(list.Data) != 1 || list.Data[0].Role != "owner" || len(list.Data[0].Permissions) == 0 {
		t.Fatalf("list: %+v", list)
	}

	r := c.must(200, "GET", "/orgs/"+orgID, nil)
	etag := r.Header.Get("ETag")
	c.must(412, "PATCH", "/orgs/"+orgID, map[string]any{"name": "New"})
	c.must(412, "PATCH", "/orgs/"+orgID, map[string]any{"name": "New"}, "If-Match", `W/"1"`)
	r = c.must(200, "PATCH", "/orgs/"+orgID, map[string]any{"name": "Acme Vietnam", "settings": map[string]bool{"allow_self_approval": true}}, "If-Match", etag)
	var o struct {
		Data struct {
			Name     string `json:"name"`
			Settings struct {
				AllowSelfApproval bool `json:"allow_self_approval"`
			} `json:"settings"`
		} `json:"data"`
	}
	r.json(t, &o)
	if o.Data.Name != "Acme Vietnam" || !o.Data.Settings.AllowSelfApproval {
		t.Fatalf("update: %s", r.Body)
	}
	c.must(412, "PATCH", "/orgs/"+orgID, map[string]any{"name": "Stale"}, "If-Match", etag) // old etag
	c.must(400, "PATCH", "/orgs/"+orgID, map[string]any{"settings": map[string]bool{"unknown": true}}, "If-Match", r.Header.Get("ETag"))
}

func TestMembershipLifecycle(t *testing.T) {
	e := newEnv(t)
	owner := e.signUp("owner@example.com", "Owner")
	admin := e.signUp("admin@example.com", "Admin")
	op := e.signUp("op@example.com", "Operator")
	viewer := e.signUp("viewer@example.com", "Viewer")
	orgID := owner.createOrg("Acme", "acme")

	e.addMember(owner, orgID, admin, "admin@example.com", "admin")
	e.addMember(owner, orgID, op, "op@example.com", "operator")
	e.addMember(owner, orgID, viewer, "viewer@example.com", "viewer")
	ms := owner.members(orgID)
	if len(ms) != 4 {
		t.Fatalf("members: %+v", ms)
	}
	adminM, opM, viewerM, ownerM := findMember(ms, "admin@example.com"), findMember(ms, "op@example.com"),
		findMember(ms, "viewer@example.com"), findMember(ms, "owner@example.com")

	// Permissions by role.
	viewer.must(200, "GET", "/orgs/"+orgID+"/members", nil)
	viewer.must(403, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "x@example.com", "role": "viewer"})
	op.must(403, "GET", "/orgs/"+orgID+"/audit-logs", nil)
	admin.must(200, "GET", "/orgs/"+orgID+"/audit-logs", nil)
	admin.must(403, "DELETE", "/orgs/"+orgID, map[string]string{"confirm_slug": "acme"})

	// Role hierarchy.
	admin.must(200, "PATCH", "/orgs/"+orgID+"/members/"+opM.ID, map[string]string{"role": "viewer"})
	admin.must(403, "PATCH", "/orgs/"+orgID+"/members/"+ownerM.ID, map[string]string{"role": "viewer"})
	admin.must(403, "PATCH", "/orgs/"+orgID+"/members/"+adminM.ID, map[string]string{"role": "viewer"}) // self
	admin.must(422, "PATCH", "/orgs/"+orgID+"/members/"+viewerM.ID, map[string]string{"role": "owner"})
	op.must(403, "PATCH", "/orgs/"+orgID+"/members/"+viewerM.ID, map[string]string{"role": "admin"})
	owner.must(200, "PATCH", "/orgs/"+orgID+"/members/"+adminM.ID, map[string]string{"role": "operator"})
	owner.must(200, "PATCH", "/orgs/"+orgID+"/members/"+adminM.ID, map[string]string{"role": "admin"})

	// Invitations: duplicates, existing members, wrong recipient.
	admin.must(409, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "viewer@example.com", "role": "viewer"})
	admin.must(201, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "new@example.com", "role": "operator"})
	admin.must(409, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "new@example.com", "role": "viewer"})
	tok := e.tokenFromMail("new@example.com", "You are invited")
	stranger := e.signUp("stranger@example.com", "Stranger")
	if r := stranger.must(403, "POST", "/invitations/accept", map[string]string{"token": tok}); r.code(t) != "invitation_email_mismatch" {
		t.Fatalf("code %s", r.code(t))
	}
	stranger.must(410, "POST", "/invitations/accept", map[string]string{"token": "garbage"})
	newbie := e.signUp("new@example.com", "Newbie")
	newbie.must(200, "POST", "/invitations/accept", map[string]string{"token": tok})
	newbie.must(410, "POST", "/invitations/accept", map[string]string{"token": tok})

	// Revoke an invitation.
	var inv struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	admin.must(201, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "later@example.com", "role": "viewer"}).json(t, &inv)
	revokedTok := e.tokenFromMail("later@example.com", "You are invited")
	admin.must(204, "DELETE", "/orgs/"+orgID+"/invitations/"+inv.Data.ID, nil)
	admin.must(404, "DELETE", "/orgs/"+orgID+"/invitations/"+inv.Data.ID, nil)
	later := e.signUp("later@example.com", "Later")
	later.must(410, "POST", "/invitations/accept", map[string]string{"token": revokedTok})

	// Removing and leaving.
	viewer.must(403, "DELETE", "/orgs/"+orgID+"/members/"+opM.ID, nil)
	op.must(204, "DELETE", "/orgs/"+orgID+"/members/"+opM.ID, nil) // leave
	op.must(404, "GET", "/orgs/"+orgID, nil)
	admin.must(204, "DELETE", "/orgs/"+orgID+"/members/"+viewerM.ID, nil)
	owner.must(409, "DELETE", "/orgs/"+orgID+"/members/"+ownerM.ID, nil)

	// Ownership transfer.
	admin.must(403, "POST", "/orgs/"+orgID+"/transfer-ownership", map[string]string{"new_owner_user_id": adminM.UserID})
	owner.must(422, "POST", "/orgs/"+orgID+"/transfer-ownership", map[string]string{"new_owner_user_id": viewerM.UserID})
	owner.must(204, "POST", "/orgs/"+orgID+"/transfer-ownership", map[string]string{"new_owner_user_id": adminM.UserID})
	ms = admin.members(orgID)
	if findMember(ms, "admin@example.com").Role != "owner" || findMember(ms, "owner@example.com").Role != "admin" {
		t.Fatalf("after transfer: %+v", ms)
	}

	// Delete organization.
	admin.must(422, "DELETE", "/orgs/"+orgID, map[string]string{"confirm_slug": "wrong"})
	admin.must(204, "DELETE", "/orgs/"+orgID, map[string]string{"confirm_slug": "acme"})
	admin.must(404, "GET", "/orgs/"+orgID, nil)
}

func TestAuditTrail(t *testing.T) {
	e := newEnv(t)
	owner := e.signUp("owner@example.com", "Owner")
	orgID := owner.createOrg("Acme", "acme")
	owner.must(201, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": "x@example.com", "role": "viewer"})

	var out struct {
		Data []struct {
			Action    string  `json:"action"`
			RequestID *string `json:"request_id"`
			IP        *string `json:"ip"`
		} `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	owner.must(200, "GET", "/orgs/"+orgID+"/audit-logs?limit=1", nil).json(t, &out)
	if len(out.Data) != 1 || out.Data[0].Action != "invitation.created" || out.NextCursor == nil {
		t.Fatalf("page 1: %+v", out)
	}
	if out.Data[0].RequestID == nil || out.Data[0].IP == nil {
		t.Fatal("request metadata missing")
	}
	cursor := *out.NextCursor
	owner.must(200, "GET", "/orgs/"+orgID+"/audit-logs?limit=1&cursor="+cursor, nil).json(t, &out)
	if len(out.Data) != 1 || out.Data[0].Action != "org.created" {
		t.Fatalf("page 2: %+v", out)
	}
	owner.must(422, "GET", "/orgs/"+orgID+"/audit-logs?cursor=bad", nil)
	owner.must(422, "GET", "/orgs/"+orgID+"/audit-logs?limit=1000", nil)

	// Audit rows are append-only even for the owner role.
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testdb.OwnerURL(t, e.db))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `DELETE FROM audit_logs`); err == nil {
		t.Fatal("audit_logs delete should fail")
	}
	// The app role has no UPDATE/DELETE grant at all.
	err = e.db.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE audit_logs SET action = 'x'`)
		return err
	})
	if err == nil {
		t.Fatal("app role must not update audit_logs")
	}
}
