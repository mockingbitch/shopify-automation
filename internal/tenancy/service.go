// Package tenancy owns organizations, membership, invitations and the
// resolution of the tenant for each request.
package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/securetoken"
	"shopee-automation/internal/platform/validate"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/tenancy/tenancydb"
)

const invitationTTL = 7 * 24 * time.Hour

// Service implements tenancy use cases.
type Service struct {
	DB      *db.DB
	Clock   clock.Clock
	Audit   audit.Recorder
	Mailer  mailer.Mailer
	Log     *slog.Logger
	BaseURL *url.URL
}

// Settings are the organization settings users may change. Unknown keys are
// rejected so that settings stay a typed contract.
type Settings struct {
	AllowSelfApproval bool `json:"allow_self_approval"`
	// RequireMFA makes two-step verification mandatory for owners and
	// admins of the organization.
	RequireMFA bool `json:"require_mfa"`
}

// Org is the API view of an organization.
type Org struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Plan      string    `json:"plan"`
	Settings  Settings  `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ETag returns the entity tag used for optimistic concurrency.
func (o Org) ETag() string { return fmt.Sprintf(`W/"%d"`, o.UpdatedAt.UnixMicro()) }

func toOrg(o tenancydb.Organization) Org {
	var s Settings
	_ = json.Unmarshal(o.Settings, &s) // stored settings are written by this package only
	return Org{ID: o.ID, Name: o.Name, Slug: o.Slug, Plan: o.Plan, Settings: s,
		CreatedAt: o.CreatedAt.UTC(), UpdatedAt: o.UpdatedAt.UTC()}
}

// Membership is an organization the caller belongs to, with their role.
type Membership struct {
	Org         Org               `json:"organization"`
	Role        rbac.Role         `json:"role"`
	Permissions []rbac.Permission `json:"permissions"`
}

// Member is the API view of an organization member.
type Member struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        rbac.Role `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

// Invitation is the API view of an open invitation.
type Invitation struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      rbac.Role `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) tx(ctx context.Context, scope db.Scope, fn func(q *tenancydb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, scope, func(tx pgx.Tx) error { return fn(tenancydb.New(tx), tx) })
}

// CreateOrgInput is the request to create an organization.
type CreateOrgInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// CreateOrg creates an organization owned by the caller.
func (s *Service) CreateOrg(ctx context.Context, p authctx.Principal, in CreateOrgInput) (Org, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	var v validate.Errors
	v.Length("name", in.Name, 1, 100)
	v.Slug("slug", in.Slug)
	if err := v.Err(); err != nil {
		return Org{}, err
	}
	orgID := db.NewID()
	var org tenancydb.Organization
	err := s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		var err error
		org, err = q.CreateOrganization(ctx, tenancydb.CreateOrganizationParams{
			ID: orgID, Name: in.Name, Slug: in.Slug, CreatedBy: p.UserID})
		if err != nil {
			return err
		}
		m, err := q.CreateMember(ctx, tenancydb.CreateMemberParams{
			ID: db.NewID(), OrgID: orgID, UserID: p.UserID, RoleKey: string(rbac.Owner)})
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &orgID, ActorUserID: &p.UserID,
			Action: "org.created", TargetType: "organization", TargetID: &orgID,
			Metadata: map[string]any{"slug": in.Slug, "owner_member_id": m.ID.String()}})
	})
	if db.UniqueViolation(err) == "organizations_slug_uk" {
		return Org{}, apperr.Invalid(apperr.FieldError{Field: "slug", Code: "taken", Message: "Tên này đã có người dùng. Hãy chọn tên khác."})
	}
	if err != nil {
		return Org{}, apperr.Internal(err)
	}
	return toOrg(org), nil
}

// ListMemberships returns the organizations userID belongs to.
func (s *Service) ListMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	var rows []tenancydb.ListOrganizationsForUserRow
	err := s.tx(ctx, db.Scope{UserID: userID}, func(q *tenancydb.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListOrganizationsForUser(ctx, userID)
		return err
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]Membership, 0, len(rows))
	for _, r := range rows {
		role := rbac.Role(r.RoleKey)
		out = append(out, Membership{Org: toOrg(r.Organization), Role: role, Permissions: rbac.Permissions(role)})
	}
	return out, nil
}

var errOrgNotFound = apperr.NotFound("Không tìm thấy tổ chức.")

// ResolveTenant verifies that userID is a member of orgID. Non-members get a
// not-found error so that organization IDs cannot be probed.
func (s *Service) ResolveTenant(ctx context.Context, userID, orgID uuid.UUID) (authctx.Tenant, error) {
	var t authctx.Tenant
	err := s.tx(ctx, db.Scope{OrgID: orgID, UserID: userID}, func(q *tenancydb.Queries, _ pgx.Tx) error {
		m, err := q.GetMembership(ctx, tenancydb.GetMembershipParams{OrgID: orgID, UserID: userID})
		if err != nil {
			return err
		}
		o, err := q.GetOrganization(ctx, orgID)
		if err != nil {
			return err
		}
		t = authctx.Tenant{OrgID: orgID, MemberID: m.ID, Role: rbac.Role(m.RoleKey), MFARequired: toOrg(o).Settings.RequireMFA}
		return nil
	})
	if db.IsNoRows(err) {
		return authctx.Tenant{}, errOrgNotFound
	}
	if err != nil {
		return authctx.Tenant{}, apperr.Internal(err)
	}
	return t, nil
}

// GetOrg returns the tenant's organization.
func (s *Service) GetOrg(ctx context.Context, t authctx.Tenant) (Org, error) {
	var o tenancydb.Organization
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *tenancydb.Queries, _ pgx.Tx) error {
		var err error
		o, err = q.GetOrganization(ctx, t.OrgID)
		return err
	})
	if db.IsNoRows(err) {
		return Org{}, errOrgNotFound
	}
	if err != nil {
		return Org{}, apperr.Internal(err)
	}
	return toOrg(o), nil
}

// UpdateOrgInput holds optional changes.
type UpdateOrgInput struct {
	Name     *string   `json:"name"`
	Settings *Settings `json:"settings"`
}

// UpdateOrg applies changes if ifMatch equals the current ETag.
func (s *Service) UpdateOrg(ctx context.Context, p authctx.Principal, t authctx.Tenant, in UpdateOrgInput, ifMatch string) (Org, error) {
	var v validate.Errors
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		v.Length("name", n, 1, 100)
	}
	if err := v.Err(); err != nil {
		return Org{}, err
	}
	if in.Settings != nil && in.Settings.RequireMFA && !p.MFAEnabled {
		return Org{}, apperr.Invalid(apperr.FieldError{Field: "settings.require_mfa", Code: "mfa_required",
			Message: "Hãy bật xác minh hai bước cho tài khoản của bạn trước."})
	}
	if ifMatch == "" {
		return Org{}, apperr.PreconditionFailed("Thiếu header If-Match. Hãy tải lại trang và thử lại.")
	}
	var settings []byte
	if in.Settings != nil {
		settings, _ = json.Marshal(in.Settings)
	}
	var out tenancydb.Organization
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		cur, err := q.GetOrganization(ctx, t.OrgID)
		if err != nil {
			return err
		}
		if toOrg(cur).ETag() != ifMatch {
			return apperr.PreconditionFailed("Tổ chức vừa được người khác thay đổi. Hãy tải lại trang và thử lại.")
		}
		out, err = q.UpdateOrganization(ctx, tenancydb.UpdateOrganizationParams{
			ID: t.OrgID, Name: in.Name, Settings: settings, ExpectedUpdatedAt: cur.UpdatedAt})
		if db.IsNoRows(err) {
			return apperr.PreconditionFailed("Tổ chức vừa được người khác thay đổi. Hãy tải lại trang và thử lại.")
		}
		if err != nil {
			return err
		}
		changed := map[string]any{}
		if in.Name != nil {
			changed["name"] = *in.Name
		}
		if in.Settings != nil {
			changed["settings"] = in.Settings
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "org.updated", TargetType: "organization", TargetID: &t.OrgID, Metadata: changed})
	})
	if err != nil {
		return Org{}, wrap(err, errOrgNotFound)
	}
	return toOrg(out), nil
}

// DeleteOrg soft-deletes the organization after the caller confirms its slug.
func (s *Service) DeleteOrg(ctx context.Context, p authctx.Principal, t authctx.Tenant, confirmSlug string) error {
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		o, err := q.GetOrganization(ctx, t.OrgID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(o.Slug, strings.TrimSpace(confirmSlug)) {
			return apperr.Invalid(apperr.FieldError{Field: "confirm_slug", Code: "mismatch",
				Message: "Hãy nhập đúng mã định danh (slug) của tổ chức để xác nhận."})
		}
		if _, err := q.SoftDeleteOrganization(ctx, t.OrgID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "org.deleted", TargetType: "organization", TargetID: &t.OrgID})
	})
	return wrap(err, errOrgNotFound)
}

var errMemberNotFound = apperr.NotFound("Không tìm thấy thành viên.")

// ListMembers returns all members of the tenant.
func (s *Service) ListMembers(ctx context.Context, t authctx.Tenant) ([]Member, error) {
	var rows []tenancydb.ListMembersRow
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *tenancydb.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListMembers(ctx, t.OrgID)
		return err
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, Member{ID: r.ID, UserID: r.UserID, Email: r.Email, DisplayName: r.DisplayName,
			Role: rbac.Role(r.RoleKey), JoinedAt: r.CreatedAt.UTC()})
	}
	return out, nil
}

// ChangeRole sets a member's role, enforcing the role hierarchy.
func (s *Service) ChangeRole(ctx context.Context, p authctx.Principal, t authctx.Tenant, memberID uuid.UUID, role rbac.Role) (Member, error) {
	if !role.Valid() {
		return Member{}, apperr.Invalid(apperr.FieldError{Field: "role", Code: "invalid", Message: "Vai trò không hợp lệ."})
	}
	if role == rbac.Owner {
		return Member{}, apperr.Invalid(apperr.FieldError{Field: "role", Code: "owner_requires_transfer",
			Message: "Hãy dùng chức năng chuyển quyền sở hữu để đặt một người làm Chủ sở hữu."})
	}
	var out tenancydb.OrganizationMember
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		m, err := q.GetMemberForUpdate(ctx, tenancydb.GetMemberForUpdateParams{OrgID: t.OrgID, ID: memberID})
		if err != nil {
			return err
		}
		cur := rbac.Role(m.RoleKey)
		if m.ID == t.MemberID {
			return apperr.Forbidden("cannot_change_own_role", "Bạn không thể tự đổi vai trò của mình.")
		}
		if !rbac.CanManageMember(t.Role, cur) || !rbac.CanAssign(t.Role, role) {
			return apperr.Forbidden("role_hierarchy", "Bạn không có quyền cấp hoặc đổi vai trò này.")
		}
		if cur == role {
			out = m
			return nil
		}
		out, err = q.UpdateMemberRole(ctx, tenancydb.UpdateMemberRoleParams{OrgID: t.OrgID, ID: memberID, RoleKey: string(role)})
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "member.role_changed", TargetType: "member", TargetID: &memberID,
			Metadata: map[string]any{"from": string(cur), "to": string(role), "user_id": m.UserID.String()}})
	})
	if err != nil {
		return Member{}, wrap(err, errMemberNotFound)
	}
	return s.memberByID(ctx, t, out.ID)
}

// RemoveMember removes a member. Members may always remove themselves
// (leave), except the owner, who must transfer ownership first.
func (s *Service) RemoveMember(ctx context.Context, p authctx.Principal, t authctx.Tenant, memberID uuid.UUID) error {
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		m, err := q.GetMemberForUpdate(ctx, tenancydb.GetMemberForUpdateParams{OrgID: t.OrgID, ID: memberID})
		if err != nil {
			return err
		}
		cur := rbac.Role(m.RoleKey)
		self := m.ID == t.MemberID
		switch {
		case cur == rbac.Owner:
			return apperr.Conflict("owner_cannot_leave", "Hãy chuyển quyền sở hữu trước khi gỡ Chủ sở hữu.")
		case self:
			// leaving is always allowed for non-owners
		case !rbac.CanManageMember(t.Role, cur):
			return apperr.Forbidden("role_hierarchy", "Bạn không có quyền gỡ thành viên này.")
		}
		if _, err := q.DeleteMember(ctx, tenancydb.DeleteMemberParams{OrgID: t.OrgID, ID: memberID}); err != nil {
			return err
		}
		action := "member.removed"
		if self {
			action = "member.left"
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: action, TargetType: "member", TargetID: &memberID,
			Metadata: map[string]any{"user_id": m.UserID.String(), "role": string(cur)}})
	})
	return wrap(err, errMemberNotFound)
}

// TransferOwnership makes another member the owner; the current owner becomes admin.
func (s *Service) TransferOwnership(ctx context.Context, p authctx.Principal, t authctx.Tenant, newOwnerUserID uuid.UUID) error {
	if t.Role != rbac.Owner {
		return apperr.Forbidden("forbidden", "Chỉ Chủ sở hữu mới có thể chuyển quyền sở hữu.")
	}
	if newOwnerUserID == p.UserID {
		return apperr.Invalid(apperr.FieldError{Field: "new_owner_user_id", Code: "self", Message: "Bạn đã là Chủ sở hữu."})
	}
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		cur, err := q.GetMemberByUserForUpdate(ctx, tenancydb.GetMemberByUserForUpdateParams{OrgID: t.OrgID, UserID: p.UserID})
		if err != nil {
			return err
		}
		next, err := q.GetMemberByUserForUpdate(ctx, tenancydb.GetMemberByUserForUpdateParams{OrgID: t.OrgID, UserID: newOwnerUserID})
		if db.IsNoRows(err) {
			return apperr.Invalid(apperr.FieldError{Field: "new_owner_user_id", Code: "not_member",
				Message: "Chủ sở hữu mới phải là thành viên của tổ chức."})
		}
		if err != nil {
			return err
		}
		if cur.RoleKey != string(rbac.Owner) {
			return apperr.Forbidden("forbidden", "Chỉ Chủ sở hữu mới có thể chuyển quyền sở hữu.")
		}
		// Demote first: a partial unique index allows only one owner.
		if _, err := q.UpdateMemberRole(ctx, tenancydb.UpdateMemberRoleParams{OrgID: t.OrgID, ID: cur.ID, RoleKey: string(rbac.Admin)}); err != nil {
			return err
		}
		if _, err := q.UpdateMemberRole(ctx, tenancydb.UpdateMemberRoleParams{OrgID: t.OrgID, ID: next.ID, RoleKey: string(rbac.Owner)}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "org.ownership_transferred", TargetType: "organization", TargetID: &t.OrgID,
			Metadata: map[string]any{"from_user_id": p.UserID.String(), "to_user_id": newOwnerUserID.String()}})
	})
	return wrap(err, errMemberNotFound)
}

// InviteInput is the request to invite a member.
type InviteInput struct {
	Email string    `json:"email"`
	Role  rbac.Role `json:"role"`
}

// Invite creates an invitation and emails it.
func (s *Service) Invite(ctx context.Context, p authctx.Principal, t authctx.Tenant, in InviteInput) (Invitation, error) {
	in.Email = validate.NormalizeEmail(in.Email)
	var v validate.Errors
	v.Email("email", in.Email)
	if !in.Role.Valid() || in.Role == rbac.Owner {
		v.Add("role", "invalid", "Hãy chọn Quản trị viên, Nhân viên vận hành hoặc Người xem.")
	}
	if err := v.Err(); err != nil {
		return Invitation{}, err
	}
	if !rbac.CanAssign(t.Role, in.Role) {
		return Invitation{}, apperr.Forbidden("role_hierarchy", "Bạn không có quyền mời người khác với vai trò này.")
	}
	token, hash := invitationToken(t.OrgID)
	now := s.Clock.Now()
	var inv tenancydb.OrganizationInvitation
	var orgName string
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		o, err := q.GetOrganization(ctx, t.OrgID)
		if err != nil {
			return err
		}
		orgName = o.Name
		member, err := q.IsMemberByEmail(ctx, tenancydb.IsMemberByEmailParams{OrgID: t.OrgID, Email: in.Email})
		if err != nil {
			return err
		}
		if member {
			return apperr.Conflict("already_member", "Người này đã là thành viên.")
		}
		// An expired open invitation must not block a new one.
		if err := q.ExpireStaleInvitation(ctx, tenancydb.ExpireStaleInvitationParams{OrgID: t.OrgID, Email: in.Email, Now: now}); err != nil {
			return err
		}
		inv, err = q.CreateInvitation(ctx, tenancydb.CreateInvitationParams{
			ID: db.NewID(), OrgID: t.OrgID, Email: in.Email, RoleKey: string(in.Role), TokenHash: hash,
			InvitedBy: p.UserID, ExpiresAt: now.Add(invitationTTL)})
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "invitation.created", TargetType: "invitation", TargetID: &inv.ID,
			Metadata: map[string]any{"email": in.Email, "role": string(in.Role)}})
	})
	if db.UniqueViolation(err) == "organization_invitations_open_uk" {
		return Invitation{}, apperr.Conflict("already_invited", "Email này đã có một lời mời đang chờ chấp nhận.")
	}
	if err != nil {
		return Invitation{}, wrap(err, errOrgNotFound)
	}
	u := *s.BaseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/invitations/accept"
	u.RawQuery = url.Values{"token": {token}}.Encode()
	if err := s.Mailer.Send(ctx, mailer.Message{To: in.Email, Subject: "Bạn được mời tham gia " + orgName,
		Text: "Xin chào,\n\n" + p.DisplayName + " đã mời bạn tham gia tổ chức " + orgName + " với vai trò " + roleLabel(in.Role) + ".\n\n" +
			"Hãy mở đường dẫn dưới đây trong vòng 7 ngày để chấp nhận lời mời:\n\n" + u.String() +
			"\n\nNếu bạn không biết người mời, bạn có thể bỏ qua email này."}); err != nil {
		s.Log.ErrorContext(ctx, "send invitation email", slog.String("error", err.Error()))
	}
	return toInvitation(inv), nil
}

func toInvitation(i tenancydb.OrganizationInvitation) Invitation {
	return Invitation{ID: i.ID, Email: i.Email, Role: rbac.Role(i.RoleKey), ExpiresAt: i.ExpiresAt.UTC(), CreatedAt: i.CreatedAt.UTC()}
}

// ListInvitations returns open invitations.
func (s *Service) ListInvitations(ctx context.Context, t authctx.Tenant) ([]Invitation, error) {
	var rows []tenancydb.OrganizationInvitation
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *tenancydb.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListOpenInvitations(ctx, t.OrgID)
		return err
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]Invitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInvitation(r))
	}
	return out, nil
}

// RevokeInvitation cancels an open invitation.
func (s *Service) RevokeInvitation(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		n, err := q.RevokeInvitation(ctx, tenancydb.RevokeInvitationParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("Không tìm thấy lời mời.")
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID,
			Action: "invitation.revoked", TargetType: "invitation", TargetID: &id})
	})
	return wrap(err, nil)
}

var errInvitationInvalid = apperr.Gone("invitation_invalid", "Lời mời này không hợp lệ hoặc đã hết hạn.")

// AcceptInvitation adds the caller to the inviting organization. The
// caller's email must match the invited email.
func (s *Service) AcceptInvitation(ctx context.Context, p authctx.Principal, token string) (Membership, error) {
	orgID, ok := parseInvitationToken(token)
	if !ok {
		return Membership{}, errInvitationInvalid
	}
	now := s.Clock.Now()
	var role rbac.Role
	var org tenancydb.Organization
	err := s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(q *tenancydb.Queries, tx pgx.Tx) error {
		inv, err := q.GetInvitationByTokenHashForUpdate(ctx, tenancydb.GetInvitationByTokenHashForUpdateParams{
			OrgID: orgID, TokenHash: securetoken.Hash(token)})
		if err != nil {
			return err
		}
		if inv.AcceptedAt != nil || inv.RevokedAt != nil || !now.Before(inv.ExpiresAt) {
			return errInvitationInvalid
		}
		if !strings.EqualFold(inv.Email, p.Email) {
			return apperr.Forbidden("invitation_email_mismatch",
				"Lời mời này được gửi tới một địa chỉ email khác. Hãy đăng nhập bằng địa chỉ đó để chấp nhận.")
		}
		if org, err = q.GetOrganization(ctx, orgID); err != nil {
			return err
		}
		m, err := q.CreateMember(ctx, tenancydb.CreateMemberParams{ID: db.NewID(), OrgID: orgID,
			UserID: p.UserID, RoleKey: inv.RoleKey, InvitedBy: &inv.InvitedBy})
		if err != nil {
			return err
		}
		role = rbac.Role(inv.RoleKey)
		if err := q.MarkInvitationAccepted(ctx, tenancydb.MarkInvitationAcceptedParams{
			Now: now, AcceptedBy: p.UserID, OrgID: orgID, ID: inv.ID}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &orgID, ActorUserID: &p.UserID,
			Action: "invitation.accepted", TargetType: "member", TargetID: &m.ID,
			Metadata: map[string]any{"invitation_id": inv.ID.String(), "role": inv.RoleKey}})
	})
	if db.UniqueViolation(err) == "organization_members_org_user_uk" {
		return Membership{}, apperr.Conflict("already_member", "Bạn đã là thành viên của tổ chức này.")
	}
	if err != nil {
		return Membership{}, wrap(err, errInvitationInvalid)
	}
	return Membership{Org: toOrg(org), Role: role, Permissions: rbac.Permissions(role)}, nil
}

func (s *Service) memberByID(ctx context.Context, t authctx.Tenant, id uuid.UUID) (Member, error) {
	members, err := s.ListMembers(ctx, t)
	if err != nil {
		return Member{}, err
	}
	for _, m := range members {
		if m.ID == id {
			return m, nil
		}
	}
	return Member{}, errMemberNotFound
}

// wrap maps "no rows" to notFound and unknown errors to internal errors,
// passing application errors through.
func wrap(err error, notFound *apperr.Error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if db.IsNoRows(err) && notFound != nil {
		return notFound
	}
	return apperr.Internal(err)
}

// Invitation tokens embed the organization ID so the tenant scope can be set
// before the lookup: "<org uuid hex>.<random>".
func invitationToken(orgID uuid.UUID) (string, []byte) {
	random, _ := securetoken.New()
	token := strings.ReplaceAll(orgID.String(), "-", "") + "." + random
	return token, securetoken.Hash(token)
}

func parseInvitationToken(token string) (uuid.UUID, bool) {
	orgPart, rest, ok := strings.Cut(token, ".")
	if !ok || len(orgPart) != 32 || len(rest) < 32 || len(token) > 128 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(orgPart)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// roleLabel is the Vietnamese name of a role, used in emails.
func roleLabel(r rbac.Role) string {
	switch r {
	case rbac.Owner:
		return "Chủ sở hữu"
	case rbac.Admin:
		return "Quản trị viên"
	case rbac.Operator:
		return "Nhân viên vận hành"
	case rbac.Viewer:
		return "Người xem"
	}
	return string(r)
}
