package browser

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/browser/browserdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/securetoken"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/rbac"
)

const (
	pairingTTL = 6 * time.Minute
	deviceTTL  = 30 * 24 * time.Hour
	// touchEvery limits writes for the sliding expiry.
	touchEvery = time.Hour
	// codeAlphabet avoids look-alike characters (no I, L, O, U, 0, 1).
	codeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"
	codeLen      = 10
)

// StepCompleter finishes a workflow step whose tasks the extension did.
type StepCompleter interface {
	// succeeded lists the step's browser tasks that have succeeded.
	CompleteFromExtension(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID, stepID uuid.UUID, succeeded []uuid.UUID) error
}

// Service is the browser extension backend.
type Service struct {
	DB *db.DB
	// System is the app_system pool, used only to find the organization
	// of a pairing code or device token.
	System  *db.DB
	Clock   clock.Clock
	Audit   audit.Recorder
	Store   storage.Store
	Fetcher *media.Fetcher
	Signer  *Signer
	Steps   StepCompleter
	Log     *slog.Logger
	// AutoDisableAfter disables a recipe after this many failures in a row.
	AutoDisableAfter int
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, orgID, userID uuid.UUID, fn func(q *browserdb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, db.Scope{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error { return fn(browserdb.New(tx), tx) })
}

// Device is an authenticated extension.
type Device struct {
	ID     uuid.UUID
	OrgID  uuid.UUID
	UserID uuid.UUID
	Name   string
	Role   rbac.Role
}

// Principal returns the device's user as a principal for engine actions.
func (d Device) Principal() authctx.Principal { return authctx.Principal{UserID: d.UserID} }

// Tenant returns the device's organization scope.
func (d Device) Tenant() authctx.Tenant { return authctx.Tenant{OrgID: d.OrgID, Role: d.Role} }

// PairingCode is shown once in the dashboard.
type PairingCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

func newCode() string {
	b := securetoken.Bytes(codeLen)
	var sb strings.Builder
	for i, x := range b {
		if i == codeLen/2 {
			sb.WriteByte('-')
		}
		// 256 % 30 bias is negligible for a 6-minute single-use code with
		// rate-limited redemption.
		sb.WriteByte(codeAlphabet[int(x)%len(codeAlphabet)])
	}
	return sb.String()
}

// normalizeCode accepts lower case and a missing or extra dash.
func normalizeCode(c string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(c) {
		if strings.ContainsRune(codeAlphabet, r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// CreatePairingCode issues a one-time code for the caller in this org.
func (s *Service) CreatePairingCode(ctx context.Context, p authctx.Principal, t authctx.Tenant) (PairingCode, error) {
	code := newCode()
	out := PairingCode{Code: code, ExpiresAt: s.now().Add(pairingTTL)}
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		if err := q.InsertPairingCode(ctx, browserdb.InsertPairingCodeParams{ID: db.NewID(), OrgID: t.OrgID, UserID: p.UserID,
			CodeHash: securetoken.Hash(normalizeCode(code)), ExpiresAt: out.ExpiresAt}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "extension.pairing_code_created",
			TargetType: "organization", TargetID: &t.OrgID})
	})
	if err != nil {
		return PairingCode{}, apperr.Internal(err)
	}
	return out, nil
}

// PairInput comes from the extension.
type PairInput struct {
	Code             string `json:"code"`
	DeviceName       string `json:"device_name"`
	ExtensionVersion string `json:"extension_version"`
}

// DeviceInfo describes a paired device.
type DeviceInfo struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	ExtensionVersion string     `json:"extension_version"`
	UserID           uuid.UUID  `json:"user_id"`
	UserEmail        string     `json:"user_email,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
}

// Paired is returned to the extension once; the token is never shown again.
type Paired struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	Device    DeviceInfo `json:"device"`
	Org       OrgInfo    `json:"org"`
	User      UserInfo   `json:"user"`
}

// OrgInfo names the organization of a device.
type OrgInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// UserInfo names the user of a device.
type UserInfo struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
}

var errInvalidCode = apperr.Invalid(apperr.FieldError{Field: "code", Code: "invalid_code", Message: "This code is wrong, used or expired. Create a new one in the dashboard."})

func clean(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

// Pair redeems a code and creates a device.
func (s *Service) Pair(ctx context.Context, in PairInput) (Paired, error) {
	code := normalizeCode(in.Code)
	if len(code) != codeLen {
		return Paired{}, errInvalidCode
	}
	name := clean(in.DeviceName, 100)
	if name == "" {
		name = "Browser"
	}
	if s.System == nil {
		return Paired{}, errNoSystem
	}
	var found browserdb.SystemFindPairingCodeRow
	err := s.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		found, err = browserdb.New(tx).SystemFindPairingCode(ctx, securetoken.Hash(code))
		return err
	})
	if db.IsNoRows(err) {
		return Paired{}, errInvalidCode
	}
	if err != nil {
		return Paired{}, apperr.Internal(err)
	}
	now := s.now()
	token, hash := securetoken.New()
	var out Paired
	err = s.tx(ctx, found.OrgID, uuid.Nil, func(q *browserdb.Queries, tx pgx.Tx) error {
		pc, err := q.LockPairingCode(ctx, browserdb.LockPairingCodeParams{OrgID: found.OrgID, ID: found.ID})
		if err != nil {
			return err
		}
		if pc.UsedAt != nil || !now.Before(pc.ExpiresAt) {
			return errInvalidCode
		}
		role, err := q.MemberRole(ctx, browserdb.MemberRoleParams{OrgID: pc.OrgID, UserID: pc.UserID})
		if err != nil || !rbac.Has(rbac.Role(role), rbac.WorkflowsRun) {
			return errInvalidCode
		}
		if err := q.UsePairingCode(ctx, browserdb.UsePairingCodeParams{UsedAt: &now, OrgID: pc.OrgID, ID: pc.ID}); err != nil {
			return err
		}
		d, err := q.InsertDevice(ctx, browserdb.InsertDeviceParams{ID: db.NewID(), OrgID: pc.OrgID, UserID: pc.UserID, Name: name,
			ExtensionVersion: clean(in.ExtensionVersion, 40), TokenHash: hash, ExpiresAt: now.Add(deviceTTL), LastUsedAt: &now})
		if err != nil {
			return err
		}
		info, err := q.DeviceContext(ctx, browserdb.DeviceContextParams{OrgID: pc.OrgID, UserID: pc.UserID})
		if err != nil {
			return err
		}
		out = Paired{Token: token, ExpiresAt: d.ExpiresAt, Device: toDevice(d, info.Email),
			Org: OrgInfo{ID: pc.OrgID, Name: info.OrgName}, User: UserInfo{ID: pc.UserID, Email: info.Email}}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &pc.OrgID, ActorUserID: &pc.UserID, Action: "extension.device_paired",
			TargetType: "extension_device", TargetID: &d.ID, Metadata: map[string]any{"name": name}})
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Paired{}, err
		}
		return Paired{}, apperr.Internal(err)
	}
	return out, nil
}

func toDevice(d browserdb.ExtensionDevice, email string) DeviceInfo {
	return DeviceInfo{ID: d.ID, Name: d.Name, ExtensionVersion: d.ExtensionVersion, UserID: d.UserID, UserEmail: email,
		CreatedAt: d.CreatedAt.UTC(), LastUsedAt: d.LastUsedAt, ExpiresAt: d.ExpiresAt.UTC()}
}

var errNoSystem = apperr.Unavailable("extension_unavailable", "The browser extension API is not configured on this server.")

// ErrDeviceUnauthorized is returned for any token that cannot be used.
var ErrDeviceUnauthorized = &apperr.Error{Kind: apperr.KindUnauthenticated, Code: "device_unauthorized",
	Message: "This browser is not paired any more. Pair it again from the dashboard."}

// Authenticate resolves a bearer token. The device must be unrevoked and
// unexpired, and its user must still be a member allowed to run workflows.
func (s *Service) Authenticate(ctx context.Context, token, version string) (Device, error) {
	if len(token) < 20 || len(token) > 100 {
		return Device{}, ErrDeviceUnauthorized
	}
	if s.System == nil {
		return Device{}, errNoSystem
	}
	hash := securetoken.Hash(token)
	var found browserdb.SystemFindDeviceRow
	err := s.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		found, err = browserdb.New(tx).SystemFindDevice(ctx, hash)
		return err
	})
	if db.IsNoRows(err) {
		return Device{}, ErrDeviceUnauthorized
	}
	if err != nil {
		return Device{}, apperr.Internal(err)
	}
	now := s.now()
	var dev Device
	err = s.tx(ctx, found.OrgID, uuid.Nil, func(q *browserdb.Queries, _ pgx.Tx) error {
		d, err := q.GetDevice(ctx, browserdb.GetDeviceParams{OrgID: found.OrgID, ID: found.ID})
		if err != nil {
			return err
		}
		if d.RevokedAt != nil || !now.Before(d.ExpiresAt) || string(d.TokenHash) != string(hash) {
			return ErrDeviceUnauthorized
		}
		role, err := q.MemberRole(ctx, browserdb.MemberRoleParams{OrgID: d.OrgID, UserID: d.UserID})
		if db.IsNoRows(err) || (err == nil && !rbac.Has(rbac.Role(role), rbac.WorkflowsRun)) {
			return ErrDeviceUnauthorized
		}
		if err != nil {
			return err
		}
		dev = Device{ID: d.ID, OrgID: d.OrgID, UserID: d.UserID, Name: d.Name, Role: rbac.Role(role)}
		version = clean(version, 40)
		if d.LastUsedAt == nil || now.Sub(*d.LastUsedAt) > touchEvery || (version != "" && version != d.ExtensionVersion) {
			if version == "" {
				version = d.ExtensionVersion
			}
			return q.TouchDevice(ctx, browserdb.TouchDeviceParams{LastUsedAt: &now, ExpiresAt: now.Add(deviceTTL),
				ExtensionVersion: version, OrgID: d.OrgID, ID: d.ID})
		}
		return nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Device{}, err
		}
		return Device{}, apperr.Internal(err)
	}
	return dev, nil
}

// Me describes the device to the extension.
type Me struct {
	Device DeviceInfo `json:"device"`
	Org    OrgInfo    `json:"org"`
	User   UserInfo   `json:"user"`
}

// Me returns the device, org and user.
func (s *Service) Me(ctx context.Context, dev Device) (Me, error) {
	var out Me
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		d, err := q.GetDevice(ctx, browserdb.GetDeviceParams{OrgID: dev.OrgID, ID: dev.ID})
		if err != nil {
			return err
		}
		info, err := q.DeviceContext(ctx, browserdb.DeviceContextParams{OrgID: dev.OrgID, UserID: dev.UserID})
		if err != nil {
			return err
		}
		out = Me{Device: toDevice(d, info.Email), Org: OrgInfo{ID: dev.OrgID, Name: info.OrgName}, User: UserInfo{ID: dev.UserID, Email: info.Email}}
		return nil
	})
	return out, wrapInternal(err)
}

// Rotate replaces the device token.
func (s *Service) Rotate(ctx context.Context, dev Device) (Paired, error) {
	token, hash := securetoken.New()
	now := s.now()
	if err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		return q.RotateDeviceToken(ctx, browserdb.RotateDeviceTokenParams{TokenHash: hash, ExpiresAt: now.Add(deviceTTL), LastUsedAt: &now,
			OrgID: dev.OrgID, ID: dev.ID})
	}); err != nil {
		return Paired{}, apperr.Internal(err)
	}
	me, err := s.Me(ctx, dev)
	if err != nil {
		return Paired{}, err
	}
	return Paired{Token: token, ExpiresAt: me.Device.ExpiresAt, Device: me.Device, Org: me.Org, User: me.User}, nil
}

// Revoke unpairs a device. Members may revoke their own devices; admins
// (members.manage) any device of the organization.
func (s *Service) Revoke(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	now := s.now()
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		d, err := q.GetDevice(ctx, browserdb.GetDeviceParams{OrgID: t.OrgID, ID: id})
		if err != nil || d.RevokedAt != nil {
			return apperr.NotFound("Device not found.")
		}
		if d.UserID != p.UserID && !t.Can(rbac.MembersManage) {
			return apperr.Forbidden("forbidden", "You can only remove your own browsers.")
		}
		if _, err := q.RevokeDevice(ctx, browserdb.RevokeDeviceParams{RevokedAt: &now, RevokedBy: &p.UserID, OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "extension.device_revoked",
			TargetType: "extension_device", TargetID: &id})
	})
	return wrapInternal(err)
}

// ListDevices lists the organization's active devices.
func (s *Service) ListDevices(ctx context.Context, p authctx.Principal, t authctx.Tenant) ([]DeviceInfo, error) {
	out := []DeviceInfo{}
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListDevices(ctx, browserdb.ListDevicesParams{OrgID: t.OrgID, Now: s.now()})
		for _, r := range rows {
			out = append(out, DeviceInfo{ID: r.ID, Name: r.Name, ExtensionVersion: r.ExtensionVersion, UserID: r.UserID, UserEmail: r.UserEmail,
				CreatedAt: r.CreatedAt.UTC(), LastUsedAt: r.LastUsedAt, ExpiresAt: r.ExpiresAt.UTC()})
		}
		return err
	})
	return out, wrapInternal(err)
}

func wrapInternal(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if db.IsNoRows(err) {
		return apperr.NotFound("Not found.")
	}
	return apperr.Internal(err)
}
