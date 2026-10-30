package rbac

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/testdb"
	"shopee-automation/internal/rbac/rbacdb"
)

func TestRoleHierarchy(t *testing.T) {
	// Every permission of a lower role is held by every higher role.
	order := []Role{Viewer, Operator, Admin, Owner}
	for i := 1; i < len(order); i++ {
		for _, p := range Permissions(order[i-1]) {
			if !Has(order[i], p) {
				t.Errorf("%s lacks %s held by %s", order[i], p, order[i-1])
			}
		}
	}
	if Has(Admin, OrgDelete) || Has(Admin, OrgTransfer) || !Has(Owner, OrgDelete) {
		t.Error("owner-only permissions wrong")
	}
	if Has(Viewer, WorkflowsRun) || Has(Operator, MembersManage) || Has(Operator, AuditRead) {
		t.Error("lower roles have too much")
	}
}

func TestCanAssign(t *testing.T) {
	cases := []struct {
		actor, target Role
		want          bool
	}{
		{Owner, Admin, true}, {Owner, Owner, false}, {Owner, Viewer, true},
		{Admin, Admin, true}, {Admin, Operator, true}, {Admin, Owner, false},
		{Operator, Viewer, false}, {Viewer, Viewer, false}, {Admin, "superuser", false},
	}
	for _, c := range cases {
		if got := CanAssign(c.actor, c.target); got != c.want {
			t.Errorf("CanAssign(%s,%s)=%v", c.actor, c.target, got)
		}
	}
}

func TestCanManageMember(t *testing.T) {
	cases := []struct {
		actor, member Role
		want          bool
	}{
		{Owner, Admin, true}, {Owner, Owner, false},
		{Admin, Admin, false}, {Admin, Operator, true}, {Admin, Owner, false},
		{Operator, Viewer, false},
	}
	for _, c := range cases {
		if got := CanManageMember(c.actor, c.member); got != c.want {
			t.Errorf("CanManageMember(%s,%s)=%v", c.actor, c.member, got)
		}
	}
}

// TestCatalogMatchesDatabase keeps the Go catalog and the migration seed in sync.
func TestCatalogMatchesDatabase(t *testing.T) {
	d := testdb.New(t)
	ctx := context.Background()
	var rows []rbacdb.RolePermission
	var roles []rbacdb.Role
	err := d.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := rbacdb.New(tx)
		var err error
		if rows, err = q.ListRolePermissions(ctx); err != nil {
			return err
		}
		roles, err = q.ListRoles(ctx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if Role(r.Key).Rank() != int(r.Rank) {
			t.Errorf("rank of %s: db %d, go %d", r.Key, r.Rank, Role(r.Key).Rank())
		}
	}
	got := map[Role][]Permission{}
	for _, r := range rows {
		got[Role(r.RoleKey)] = append(got[Role(r.RoleKey)], Permission(r.PermissionKey))
	}
	want := Grants()
	for _, role := range []Role{Owner, Admin, Operator, Viewer} {
		g := got[role]
		slices.Sort(g)
		if !slices.Equal(g, want[role]) {
			t.Errorf("%s:\n db %v\n go %v", role, g, want[role])
		}
	}
}
