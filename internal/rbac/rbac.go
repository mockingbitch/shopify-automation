// Package rbac defines organization roles, permissions and the rules for
// granting roles. The same catalog is seeded into the database by migration
// 00001; a test keeps the two in sync.
package rbac

import "slices"

// Role is an organization role.
type Role string

const (
	Owner    Role = "owner"
	Admin    Role = "admin"
	Operator Role = "operator"
	Viewer   Role = "viewer"
)

// Permission is a capability checked by the API.
type Permission string

const (
	OrgRead          Permission = "org.read"
	OrgUpdate        Permission = "org.update"
	OrgDelete        Permission = "org.delete"
	OrgTransfer      Permission = "org.transfer"
	MembersRead      Permission = "members.read"
	MembersManage    Permission = "members.manage"
	ShopsRead        Permission = "shops.read"
	ShopsConnect     Permission = "shops.connect"
	ShopsDisconnect  Permission = "shops.disconnect"
	ShopsSync        Permission = "shops.sync"
	TemplatesRead    Permission = "templates.read"
	TemplatesWrite   Permission = "templates.write"
	TemplatesPublish Permission = "templates.publish"
	ProductsRead     Permission = "products.read"
	ProductsWrite    Permission = "products.write"
	ImportsWrite     Permission = "imports.write"
	WorkflowsRead    Permission = "workflows.read"
	WorkflowsRun     Permission = "workflows.run"
	WorkflowsCancel  Permission = "workflows.cancel"
	WorkflowsRetry   Permission = "workflows.retry"
	ApprovalsDecide  Permission = "approvals.decide"
	AuditRead        Permission = "audit.read"
)

// All lists every permission.
var All = []Permission{
	OrgRead, OrgUpdate, OrgDelete, OrgTransfer, MembersRead, MembersManage,
	ShopsRead, ShopsConnect, ShopsDisconnect, ShopsSync,
	TemplatesRead, TemplatesWrite, TemplatesPublish,
	ProductsRead, ProductsWrite, ImportsWrite,
	WorkflowsRead, WorkflowsRun, WorkflowsCancel, WorkflowsRetry, ApprovalsDecide,
	AuditRead,
}

var rank = map[Role]int{Viewer: 10, Operator: 20, Admin: 30, Owner: 40}

var grants = func() map[Role][]Permission {
	viewer := []Permission{OrgRead, MembersRead, ShopsRead, TemplatesRead, ProductsRead, WorkflowsRead}
	operator := append(slices.Clone(viewer),
		ShopsSync, TemplatesWrite, ProductsWrite, ImportsWrite,
		WorkflowsRun, WorkflowsCancel, WorkflowsRetry, ApprovalsDecide)
	var admin []Permission
	for _, p := range All {
		if p != OrgDelete && p != OrgTransfer {
			admin = append(admin, p)
		}
	}
	return map[Role][]Permission{
		Viewer:   viewer,
		Operator: operator,
		Admin:    admin,
		Owner:    slices.Clone(All),
	}
}()

// Valid reports whether r is a known role.
func (r Role) Valid() bool { _, ok := rank[r]; return ok }

// Rank orders roles; higher is more privileged.
func (r Role) Rank() int { return rank[r] }

// Has reports whether role r grants permission p.
func Has(r Role, p Permission) bool { return slices.Contains(grants[r], p) }

// Permissions returns the permissions granted to r, sorted.
func Permissions(r Role) []Permission {
	out := slices.Clone(grants[r])
	slices.Sort(out)
	return out
}

// Grants returns a copy of the full role → permissions table.
func Grants() map[Role][]Permission {
	out := make(map[Role][]Permission, len(grants))
	for r := range grants {
		out[r] = Permissions(r)
	}
	return out
}

// CanAssign reports whether an actor with role actor may give role target to
// someone. Ownership is only transferred, never assigned.
func CanAssign(actor, target Role) bool {
	if !target.Valid() || target == Owner {
		return false
	}
	return Has(actor, MembersManage) && target.Rank() <= actor.Rank()
}

// CanManageMember reports whether actor may change or remove a member who
// currently holds role member. Owners manage everyone but themselves are only
// changed through ownership transfer; admins manage operators and viewers.
func CanManageMember(actor, member Role) bool {
	if !Has(actor, MembersManage) || member == Owner {
		return false
	}
	if actor == Owner {
		return true
	}
	return member.Rank() < actor.Rank()
}

// RolesWith returns the roles granted p, as strings for SQL.
func RolesWith(p Permission) []string {
	var out []string
	for r, ps := range grants {
		if slices.Contains(ps, p) {
			out = append(out, string(r))
		}
	}
	slices.Sort(out)
	return out
}
