// Package authctx carries the authenticated principal and the resolved tenant
// through context.Context. It holds data only; identity and tenancy own the
// rules that produce it.
package authctx

import (
	"context"

	"github.com/google/uuid"

	"shopee-automation/internal/rbac"
)

// Principal is the authenticated user of a request.
type Principal struct {
	UserID          uuid.UUID
	SessionID       uuid.UUID
	Email           string
	DisplayName     string
	EmailVerified   bool
	IsPlatformAdmin bool
	// MFAEnabled is true when the user has confirmed two-step verification.
	MFAEnabled bool
	CSRFToken  string
}

// Tenant is the organization a request operates on, resolved from the URL
// path and verified against the caller's membership.
type Tenant struct {
	OrgID    uuid.UUID
	MemberID uuid.UUID
	Role     rbac.Role
	// MFARequired is the organization's require_mfa setting.
	MFARequired bool
}

// Can reports whether the tenant member has permission p.
func (t Tenant) Can(p rbac.Permission) bool { return rbac.Has(t.Role, p) }

type principalKey struct{}
type tenantKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal, if authenticated.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithTenant stores t in ctx.
func WithTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, tenantKey{}, t)
}

// TenantFrom returns the tenant, if resolved.
func TenantFrom(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(tenantKey{}).(Tenant)
	return t, ok
}

// MustPrincipal returns the principal or panics. Use only behind the session
// middleware, where absence is a programming error.
func MustPrincipal(ctx context.Context) Principal {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		panic("authctx: no principal in context (route is missing session middleware)")
	}
	return p
}

// MustTenant returns the tenant or panics. Use only behind the tenant middleware.
func MustTenant(ctx context.Context) Tenant {
	t, ok := TenantFrom(ctx)
	if !ok {
		panic("authctx: no tenant in context (route is missing tenant middleware)")
	}
	return t
}
