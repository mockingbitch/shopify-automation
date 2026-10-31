package tenancy

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/rbac"
)

// OrgIDParam is the chi URL parameter for the organization ID.
const OrgIDParam = "orgID"

// Handler serves organization, member and invitation endpoints.
type Handler struct {
	Svc *Service
	Log *slog.Logger
}

// ResolveTenant is middleware for /orgs/{orgID}/... routes. It requires a
// principal and verifies membership; the org ID from the path is the only
// tenant input that is ever trusted, and only after this check.
func (h Handler) ResolveTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := httpx.Adapter{Log: h.Log}
		p := authctx.MustPrincipal(r.Context())
		orgID, err := uuid.Parse(chi.URLParam(r, OrgIDParam))
		if err != nil {
			a.WriteError(w, r, errOrgNotFound)
			return
		}
		t, err := h.Svc.ResolveTenant(r.Context(), p.UserID, orgID)
		if err != nil {
			a.WriteError(w, r, err)
			return
		}
		if t.MFARequired && (t.Role == rbac.Owner || t.Role == rbac.Admin) && !p.MFAEnabled {
			a.WriteError(w, r, apperr.Forbidden("mfa_required",
				"This organization requires two-step verification for owners and admins. Turn it on in your account settings."))
			return
		}
		next.ServeHTTP(w, r.WithContext(authctx.WithTenant(r.Context(), t)))
	})
}

// Require returns middleware that checks a tenant permission.
func Require(log *slog.Logger, perm rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !authctx.MustTenant(r.Context()).Can(perm) {
				httpx.Adapter{Log: log}.WriteError(w, r,
					apperr.Forbidden("forbidden", "You do not have permission to do this."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func pathID(r *http.Request, name string, notFound error) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, notFound
	}
	return id, nil
}

// CreateOrg handles POST /orgs.
func (h Handler) CreateOrg(w http.ResponseWriter, r *http.Request) error {
	var in CreateOrgInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	o, err := h.Svc.CreateOrg(r.Context(), authctx.MustPrincipal(r.Context()), in)
	if err != nil {
		return err
	}
	w.Header().Set("ETag", o.ETag())
	w.Header().Set("Location", "/api/v1/orgs/"+o.ID.String())
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Org]{Data: o})
}

// ListOrgs handles GET /orgs.
func (h Handler) ListOrgs(w http.ResponseWriter, r *http.Request) error {
	ms, err := h.Svc.ListMemberships(r.Context(), authctx.MustPrincipal(r.Context()).UserID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Membership]{Data: ms})
}

// GetOrg handles GET /orgs/{orgID}.
func (h Handler) GetOrg(w http.ResponseWriter, r *http.Request) error {
	o, err := h.Svc.GetOrg(r.Context(), authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", o.ETag())
	return httpx.JSON(w, http.StatusOK, httpx.Data[Org]{Data: o})
}

// UpdateOrg handles PATCH /orgs/{orgID}.
func (h Handler) UpdateOrg(w http.ResponseWriter, r *http.Request) error {
	var in UpdateOrgInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	o, err := h.Svc.UpdateOrg(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()),
		in, r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", o.ETag())
	return httpx.JSON(w, http.StatusOK, httpx.Data[Org]{Data: o})
}

type deleteOrgInput struct {
	ConfirmSlug string `json:"confirm_slug"`
}

// DeleteOrg handles DELETE /orgs/{orgID}.
func (h Handler) DeleteOrg(w http.ResponseWriter, r *http.Request) error {
	var in deleteOrgInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.Svc.DeleteOrg(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), in.ConfirmSlug); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

type transferInput struct {
	NewOwnerUserID uuid.UUID `json:"new_owner_user_id"`
}

// TransferOwnership handles POST /orgs/{orgID}/transfer-ownership.
func (h Handler) TransferOwnership(w http.ResponseWriter, r *http.Request) error {
	var in transferInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.Svc.TransferOwnership(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), in.NewOwnerUserID); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// ListMembers handles GET /orgs/{orgID}/members.
func (h Handler) ListMembers(w http.ResponseWriter, r *http.Request) error {
	ms, err := h.Svc.ListMembers(r.Context(), authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Member]{Data: ms})
}

type changeRoleInput struct {
	Role rbac.Role `json:"role"`
}

// ChangeRole handles PATCH /orgs/{orgID}/members/{memberID}.
func (h Handler) ChangeRole(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "memberID", errMemberNotFound)
	if err != nil {
		return err
	}
	var in changeRoleInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	m, err := h.Svc.ChangeRole(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, in.Role)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Member]{Data: m})
}

// RemoveMember handles DELETE /orgs/{orgID}/members/{memberID}. Members may
// remove themselves without members.manage; other checks are in the service.
func (h Handler) RemoveMember(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "memberID", errMemberNotFound)
	if err != nil {
		return err
	}
	t := authctx.MustTenant(r.Context())
	if id != t.MemberID && !t.Can(rbac.MembersManage) {
		return apperr.Forbidden("forbidden", "You do not have permission to do this.")
	}
	if err := h.Svc.RemoveMember(r.Context(), authctx.MustPrincipal(r.Context()), t, id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Invite handles POST /orgs/{orgID}/invitations.
func (h Handler) Invite(w http.ResponseWriter, r *http.Request) error {
	var in InviteInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	inv, err := h.Svc.Invite(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Invitation]{Data: inv})
}

// ListInvitations handles GET /orgs/{orgID}/invitations.
func (h Handler) ListInvitations(w http.ResponseWriter, r *http.Request) error {
	invs, err := h.Svc.ListInvitations(r.Context(), authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Invitation]{Data: invs})
}

// RevokeInvitation handles DELETE /orgs/{orgID}/invitations/{invitationID}.
func (h Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "invitationID", apperr.NotFound("Invitation not found."))
	if err != nil {
		return err
	}
	if err := h.Svc.RevokeInvitation(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

type acceptInput struct {
	Token string `json:"token"`
}

// AcceptInvitation handles POST /invitations/accept.
func (h Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) error {
	var in acceptInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	m, err := h.Svc.AcceptInvitation(r.Context(), authctx.MustPrincipal(r.Context()), in.Token)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Membership]{Data: m})
}
