package identity

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/identity/identitydb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/securetoken"
	"shopee-automation/internal/platform/totp"
)

const (
	mfaTable          = "user_mfa"
	challengeTTL      = 5 * time.Minute
	maxChallengeTries = 5
	recoveryCodes     = 10
	issuer            = "Shopee Shop Automation"
)

var (
	errMFAUnavailable = apperr.Unavailable("mfa_unavailable", "Máy chủ này chưa được cấu hình xác minh hai bước.")
	errBadCode        = apperr.Invalid(apperr.FieldError{Field: "code", Code: "invalid_code", Message: "Mã không đúng hoặc đã được dùng."})
	errChallenge      = apperr.Unauthenticated("Lượt đăng nhập đã hết hạn. Hãy đăng nhập lại.")
)

func (s *Service) mfaEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.DB.Tx(ctx, db.Scope{UserID: userID}, func(tx pgx.Tx) error {
		m, err := identitydb.New(tx).GetMFA(ctx, userID)
		if db.IsNoRows(err) {
			return nil
		}
		ok = err == nil && m.ConfirmedAt != nil
		return err
	})
	return ok, err
}

// startChallenge issues the second login step.
func (s *Service) startChallenge(ctx context.Context, u identitydb.User) (Session, error) {
	token, hash := NewToken()
	exp := s.Clock.Now().Add(challengeTTL)
	err := s.DB.Tx(ctx, db.Scope{UserID: u.ID}, func(tx pgx.Tx) error {
		return identitydb.New(tx).InsertChallenge(ctx, identitydb.InsertChallengeParams{ID: db.NewID(), UserID: u.ID, TokenHash: hash, ExpiresAt: exp})
	})
	if err != nil {
		return Session{}, apperr.Internal(err)
	}
	return Session{MFAChallenge: token, ExpiresAt: exp}, nil
}

func (s *Service) secret(m identitydb.UserMfa) ([]byte, error) {
	if s.Keys == nil {
		return nil, errMFAUnavailable
	}
	row, err := s.Keys.OpenRow(mfaTable, m.UserID.String(), envelope.WrappedKey{KEKID: m.KekID, DEK: m.DekWrapped})
	if err != nil {
		return nil, err
	}
	return row.Open("secret_enc", m.SecretEnc)
}

// checkCode verifies a TOTP code (or, when allowRecovery, a recovery code)
// for a user whose MFA row is locked, and records it as used.
func (s *Service) checkCode(ctx context.Context, q *identitydb.Queries, m identitydb.UserMfa, code string, allowRecovery bool) (bool, error) {
	code = strings.TrimSpace(code)
	if allowRecovery && len(normalizeRecovery(code)) == 10 {
		n, err := q.UseRecoveryCode(ctx, identitydb.UseRecoveryCodeParams{Now: ptr(s.Clock.Now()), UserID: m.UserID,
			CodeHash: securetoken.Hash(normalizeRecovery(code))})
		return n == 1, err
	}
	sec, err := s.secret(m)
	if err != nil {
		return false, err
	}
	step, ok := totp.Verify(sec, code, s.Clock.Now(), m.LastUsedStep)
	if !ok {
		return false, nil
	}
	return true, q.SetMFAStep(ctx, identitydb.SetMFAStepParams{Step: step, UserID: m.UserID})
}

func ptr[T any](v T) *T { return &v }

// CompleteMFALogin exchanges a challenge and a code for a session.
func (s *Service) CompleteMFALogin(ctx context.Context, challenge, code string, previousSessionID uuid.UUID) (Session, error) {
	if challenge == "" || len(challenge) > 128 {
		return Session{}, errChallenge
	}
	now := s.Clock.Now()
	var user identitydb.User
	var accepted bool
	err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		c, err := q.LockChallenge(ctx, HashToken(challenge))
		if db.IsNoRows(err) {
			return errChallenge
		}
		if err != nil {
			return err
		}
		if c.UsedAt != nil || !now.Before(c.ExpiresAt) || c.Attempts >= maxChallengeTries {
			return errChallenge
		}
		if err := q.ChallengeAttempt(ctx, c.ID); err != nil {
			return err
		}
		m, err := q.LockMFA(ctx, c.UserID)
		if err != nil || m.ConfirmedAt == nil {
			return errChallenge
		}
		ok, err := s.checkCode(ctx, q, m, code, true)
		if err != nil {
			return err
		}
		if !ok {
			// Commit the attempt count; the caller gets errBadCode.
			return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &c.UserID, Action: "auth.mfa_failed", Outcome: audit.Failure})
		}
		accepted = true
		if err := q.UseChallenge(ctx, identitydb.UseChallengeParams{Now: &now, ID: c.ID}); err != nil {
			return err
		}
		user = identitydb.User{ID: c.UserID, Email: c.Email, DisplayName: c.DisplayName, IsPlatformAdmin: c.IsPlatformAdmin, EmailVerifiedAt: c.EmailVerifiedAt}
		return nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Session{}, err
		}
		return Session{}, apperr.Internal(err)
	}
	if !accepted {
		return Session{}, errBadCode
	}
	full, err := s.GetUser(ctx, user.ID)
	if err != nil {
		return Session{}, err
	}
	user.CreatedAt = full.CreatedAt
	return s.createSession(ctx, user, previousSessionID, true, "auth.login")
}

// MFASetup is shown once while enrolling.
type MFASetup struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

// SetupMFA creates (or replaces) an unconfirmed secret.
func (s *Service) SetupMFA(ctx context.Context, p authctx.Principal) (MFASetup, error) {
	if s.Keys == nil {
		return MFASetup{}, errMFAUnavailable
	}
	if p.MFAEnabled {
		return MFASetup{}, apperr.Conflict("mfa_enabled", "Xác minh hai bước đang bật. Hãy tắt trước khi đổi ứng dụng xác thực.")
	}
	sec := totp.NewSecret()
	row, wrapped, err := s.Keys.NewRow(mfaTable, p.UserID.String())
	if err != nil {
		return MFASetup{}, apperr.Internal(err)
	}
	enc, err := row.Seal("secret_enc", sec)
	if err != nil {
		return MFASetup{}, apperr.Internal(err)
	}
	if err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		return identitydb.New(tx).UpsertPendingMFA(ctx, identitydb.UpsertPendingMFAParams{UserID: p.UserID, KekID: wrapped.KEKID,
			DekWrapped: wrapped.DEK, SecretEnc: enc})
	}); err != nil {
		return MFASetup{}, apperr.Internal(err)
	}
	return MFASetup{Secret: totp.Encode(sec), URI: totp.URI(sec, issuer, p.Email)}, nil
}

// ConfirmMFA turns two-step verification on with a first code and returns
// the recovery codes (shown once).
func (s *Service) ConfirmMFA(ctx context.Context, p authctx.Principal, code string) ([]string, error) {
	var codes []string
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		m, err := q.LockMFA(ctx, p.UserID)
		if db.IsNoRows(err) {
			return apperr.Conflict("mfa_not_started", "Hãy bắt đầu thiết lập trước.")
		}
		if err != nil {
			return err
		}
		if m.ConfirmedAt != nil {
			return apperr.Conflict("mfa_enabled", "Xác minh hai bước đã được bật.")
		}
		sec, err := s.secret(m)
		if err != nil {
			return err
		}
		step, ok := totp.Verify(sec, code, s.Clock.Now(), 0)
		if !ok {
			return errBadCode
		}
		if err := q.ConfirmMFA(ctx, identitydb.ConfirmMFAParams{Now: ptr(s.Clock.Now()), Step: step, UserID: p.UserID}); err != nil {
			return err
		}
		if codes, err = s.newRecoveryCodes(ctx, q, p.UserID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "auth.mfa_enabled", TargetType: "user", TargetID: &p.UserID})
	})
	return codes, wrapErr(err)
}

func normalizeRecovery(c string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

func (s *Service) newRecoveryCodes(ctx context.Context, q *identitydb.Queries, userID uuid.UUID) ([]string, error) {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}
	const alphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"
	codes := make([]string, 0, recoveryCodes)
	for range recoveryCodes {
		b := securetoken.Bytes(10)
		raw := make([]byte, 10)
		for i, x := range b {
			raw[i] = alphabet[int(x)%len(alphabet)]
		}
		code := string(raw[:5]) + "-" + string(raw[5:])
		if err := q.InsertRecoveryCode(ctx, identitydb.InsertRecoveryCodeParams{ID: db.NewID(), UserID: userID,
			CodeHash: securetoken.Hash(normalizeRecovery(code))}); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// DisableMFA turns two-step verification off. It needs the password and a
// current code (or a recovery code), and signs out other sessions.
func (s *Service) DisableMFA(ctx context.Context, p authctx.Principal, password, code string) error {
	if err := s.checkPassword(ctx, p.UserID, password); err != nil {
		return err
	}
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		m, err := q.LockMFA(ctx, p.UserID)
		if db.IsNoRows(err) || (err == nil && m.ConfirmedAt == nil) {
			return apperr.Conflict("mfa_disabled", "Xác minh hai bước chưa được bật.")
		}
		if err != nil {
			return err
		}
		ok, err := s.checkCode(ctx, q, m, code, true)
		if err != nil {
			return err
		}
		if !ok {
			return errBadCode
		}
		if err := q.DeleteMFA(ctx, p.UserID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, p.UserID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "auth.mfa_disabled", TargetType: "user", TargetID: &p.UserID})
	})
	return wrapErr(err)
}

// RegenerateRecoveryCodes replaces the recovery codes (needs a current code).
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p authctx.Principal, code string) ([]string, error) {
	var codes []string
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		m, err := q.LockMFA(ctx, p.UserID)
		if db.IsNoRows(err) || (err == nil && m.ConfirmedAt == nil) {
			return apperr.Conflict("mfa_disabled", "Xác minh hai bước chưa được bật.")
		}
		if err != nil {
			return err
		}
		ok, err := s.checkCode(ctx, q, m, code, false)
		if err != nil {
			return err
		}
		if !ok {
			return errBadCode
		}
		if codes, err = s.newRecoveryCodes(ctx, q, p.UserID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "auth.recovery_codes_regenerated", TargetType: "user", TargetID: &p.UserID})
	})
	return codes, wrapErr(err)
}

// MFAStatus describes the caller's two-step verification.
type MFAStatus struct {
	Enabled           bool  `json:"enabled"`
	RecoveryCodesLeft int64 `json:"recovery_codes_left"`
	Available         bool  `json:"available"`
}

// Status returns the caller's MFA status.
func (s *Service) MFAStatusOf(ctx context.Context, p authctx.Principal) (MFAStatus, error) {
	out := MFAStatus{Enabled: p.MFAEnabled, Available: s.Keys != nil}
	if !p.MFAEnabled {
		return out, nil
	}
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		var err error
		out.RecoveryCodesLeft, err = identitydb.New(tx).CountRecoveryCodes(ctx, p.UserID)
		return err
	})
	return out, wrapErr(err)
}

func (s *Service) checkPassword(ctx context.Context, userID uuid.UUID, password string) error {
	var u identitydb.User
	err := s.DB.Tx(ctx, db.Scope{UserID: userID}, func(tx pgx.Tx) error {
		var err error
		u, err = identitydb.New(tx).GetUserByID(ctx, userID)
		return err
	})
	if err != nil {
		return wrapErr(err)
	}
	if ok, _, _ := s.Hasher.Verify(password, u.PasswordHash); !ok {
		return apperr.Invalid(apperr.FieldError{Field: "password", Code: "invalid", Message: "Mật khẩu không đúng."})
	}
	return nil
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	return apperr.Internal(err)
}
