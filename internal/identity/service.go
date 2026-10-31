// Package identity owns users, password authentication, sessions and the
// one-time tokens for email verification and password reset.
package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/identity/identitydb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/reqmeta"
	"shopee-automation/internal/platform/validate"
)

const (
	verifyEmailTTL   = 48 * time.Hour
	passwordResetTTL = time.Hour
	// touchInterval limits session row updates to one per minute per session.
	touchInterval = time.Minute
)

// Service implements identity use cases.
type Service struct {
	DB     *db.DB
	Clock  clock.Clock
	Mailer mailer.Mailer
	Audit  audit.Recorder
	Log    *slog.Logger
	Hasher Hasher
	// Keys seals TOTP secrets; nil disables two-step verification.
	Keys *envelope.Keyring
	// Pwned checks new passwords against known breaches; nil skips it.
	Pwned       PwnedChecker
	BaseURL     *url.URL
	IdleTTL     time.Duration
	AbsoluteTTL time.Duration

	dummyOnce sync.Once
	dummyHash string
}

// User is the public view of a user.
type User struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	EmailVerified   bool       `json:"email_verified"`
	IsPlatformAdmin bool       `json:"is_platform_admin"`
	MFAEnabled      bool       `json:"mfa_enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	EmailVerifiedAt *time.Time `json:"-"`
}

func toUser(u identitydb.User) User {
	return User{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, EmailVerified: u.EmailVerifiedAt != nil,
		IsPlatformAdmin: u.IsPlatformAdmin, CreatedAt: u.CreatedAt, EmailVerifiedAt: u.EmailVerifiedAt,
	}
}

// RegisterInput is the sign-up request.
type RegisterInput struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// Register creates an account and emails a verification link. When the email
// is already registered it sends a notice to that address instead and returns
// success, so the response never reveals which emails have accounts.
func (s *Service) Register(ctx context.Context, in RegisterInput) error {
	in.Email = validate.NormalizeEmail(in.Email)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	var v validate.Errors
	v.Email("email", in.Email)
	v.Password("password", in.Password)
	v.Length("display_name", in.DisplayName, 1, 100)
	if err := v.Err(); err != nil {
		return err
	}
	if err := s.checkBreached(ctx, in.Password); err != nil {
		return err
	}
	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return apperr.Internal(err)
	}

	var verifyToken string
	exists := false
	err = s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		if _, err := q.GetUserByEmail(ctx, in.Email); err == nil {
			exists = true
			return nil
		} else if !db.IsNoRows(err) {
			return err
		}
		u, err := q.CreateUser(ctx, identitydb.CreateUserParams{
			ID: db.NewID(), Email: in.Email, PasswordHash: hash, DisplayName: in.DisplayName,
		})
		if err != nil {
			return err
		}
		if verifyToken, err = s.issueToken(ctx, q, u.ID, "verify_email", verifyEmailTTL); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &u.ID, Action: "user.registered",
			TargetType: "user", TargetID: &u.ID})
	})
	if db.UniqueViolation(err) == "users_email_uk" {
		exists, err = true, nil // concurrent sign-up with the same email
	}
	if err != nil {
		return apperr.Internal(err)
	}

	if exists {
		s.send(ctx, mailer.Message{To: in.Email, Subject: "You already have an account",
			Text: "Someone tried to create an account with this email address, which is already registered.\n\n" +
				"If this was you, sign in or reset your password at " + s.link("/forgot-password", nil) + "\n\n" +
				"If it was not you, you can ignore this email."})
		return nil
	}
	s.send(ctx, mailer.Message{To: in.Email, Subject: "Verify your email address",
		Text: "Confirm your email address by opening this link within 48 hours:\n\n" +
			s.link("/verify-email", url.Values{"token": {verifyToken}})})
	return nil
}

// ResendVerification sends a new verification link to a signed-in user whose
// email is not verified yet. Verified users get no email; the response is the
// same either way.
func (s *Service) ResendVerification(ctx context.Context, p authctx.Principal) error {
	var to, token string
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		u, err := q.GetUserByID(ctx, p.UserID)
		if err != nil {
			return err
		}
		if u.EmailVerifiedAt != nil {
			return nil
		}
		if token, err = s.issueToken(ctx, q, u.ID, "verify_email", verifyEmailTTL); err != nil {
			return err
		}
		to = u.Email
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &u.ID, Action: "user.verification_resent",
			TargetType: "user", TargetID: &u.ID})
	})
	if err != nil {
		return apperr.Internal(err)
	}
	if token != "" {
		s.send(ctx, mailer.Message{To: to, Subject: "Verify your email address",
			Text: "Confirm your email address by opening this link within 48 hours:\n\n" +
				s.link("/verify-email", url.Values{"token": {token}})})
	}
	return nil
}

// Session is the result of a successful login. When the user has
// two-step verification, MFAChallenge is set instead of Token and the
// login continues with CompleteMFALogin.
type Session struct {
	Token        string
	MFAChallenge string
	ExpiresAt    time.Time
	Principal    authctx.Principal
	User         User
}

var errBadCredentials = apperr.Unauthenticated("Email or password is incorrect.")

// Login verifies credentials and creates a session. previousSessionID, if
// set, is revoked (session rotation on login).
func (s *Service) Login(ctx context.Context, email, password string, previousSessionID uuid.UUID) (Session, error) {
	email = validate.NormalizeEmail(email)
	if email == "" || password == "" || len(password) > 128 {
		return Session{}, errBadCredentials
	}

	var u identitydb.User
	var found bool
	err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		u, err = identitydb.New(tx).GetUserByEmail(ctx, email)
		if db.IsNoRows(err) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return Session{}, apperr.Internal(err)
	}

	encoded := u.PasswordHash
	if !found {
		encoded = s.dummy() // keep timing similar for unknown emails
	}
	ok, needsRehash, verr := s.Hasher.Verify(password, encoded)
	if verr != nil && found {
		s.Log.ErrorContext(ctx, "stored password hash is malformed", slog.String("user_id", u.ID.String()))
	}
	if !found || !ok {
		s.recordLoginFailure(ctx, u, found, email)
		return Session{}, errBadCredentials
	}

	if needsRehash {
		if h, err := s.Hasher.Hash(password); err == nil {
			if err := s.DB.Tx(ctx, db.Scope{UserID: u.ID}, func(tx pgx.Tx) error {
				return identitydb.New(tx).UpdatePasswordHash(ctx, identitydb.UpdatePasswordHashParams{ID: u.ID, PasswordHash: h})
			}); err != nil {
				return Session{}, apperr.Internal(err)
			}
		}
	}
	mfa, err := s.mfaEnabled(ctx, u.ID)
	if err != nil {
		return Session{}, apperr.Internal(err)
	}
	if mfa {
		return s.startChallenge(ctx, u)
	}
	return s.createSession(ctx, u, previousSessionID, false, "auth.login")
}

// createSession issues a session for an authenticated user.
func (s *Service) createSession(ctx context.Context, u identitydb.User, previousSessionID uuid.UUID, mfa bool, action string) (Session, error) {
	now := s.Clock.Now()
	token, tokenHash := NewToken()
	csrf := randomBytes(32)
	sess := Session{Token: token, ExpiresAt: now.Add(s.AbsoluteTTL), User: toUser(u)}
	sess.User.MFAEnabled = mfa
	sessionID := db.NewID()
	m := reqmeta.From(ctx)
	err := s.DB.Tx(ctx, db.Scope{UserID: u.ID}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		if previousSessionID != uuid.Nil {
			if err := q.RevokeSession(ctx, previousSessionID); err != nil {
				return err
			}
		}
		var ua *string
		if m.UserAgent != "" {
			ua = &m.UserAgent
		}
		if err := q.CreateSession(ctx, identitydb.CreateSessionParams{
			ID: sessionID, UserID: u.ID, TokenHash: tokenHash, CsrfSecret: csrf, Ip: m.IP, UserAgent: ua,
			IdleExpiresAt: now.Add(s.IdleTTL), AbsoluteExpiresAt: sess.ExpiresAt,
		}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &u.ID, Action: action,
			TargetType: "session", TargetID: &sessionID, Metadata: map[string]any{"mfa": mfa}})
	})
	if err != nil {
		return Session{}, apperr.Internal(err)
	}
	sess.Principal = principalFor(u, sessionID, csrf, mfa)
	return sess, nil
}

func (s *Service) recordLoginFailure(ctx context.Context, u identitydb.User, found bool, email string) {
	e := audit.Entry{Action: "auth.login", Outcome: audit.Failure, Metadata: map[string]any{"email": email}}
	if found {
		e.ActorUserID, e.ActorType = &u.ID, audit.ActorUser
	}
	if err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error { return s.Audit.Record(ctx, tx, e) }); err != nil {
		s.Log.ErrorContext(ctx, "audit login failure", slog.String("error", err.Error()))
	}
}

func principalFor(u identitydb.User, sessionID uuid.UUID, csrf []byte, mfa bool) authctx.Principal {
	return authctx.Principal{
		UserID: u.ID, SessionID: sessionID, Email: u.Email, DisplayName: u.DisplayName,
		EmailVerified: u.EmailVerifiedAt != nil, IsPlatformAdmin: u.IsPlatformAdmin, MFAEnabled: mfa,
		CSRFToken: base64.RawURLEncoding.EncodeToString(csrf),
	}
}

// Authenticate resolves a session token. It returns an unauthenticated error
// for unknown, expired or revoked sessions.
func (s *Service) Authenticate(ctx context.Context, token string) (authctx.Principal, error) {
	if token == "" || len(token) > 128 {
		return authctx.Principal{}, apperr.Unauthenticated("Sign in to continue.")
	}
	now := s.Clock.Now()
	var p authctx.Principal
	err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		row, err := q.GetActiveSessionByTokenHash(ctx, identitydb.GetActiveSessionByTokenHashParams{
			TokenHash: HashToken(token), Now: now})
		if err != nil {
			return err
		}
		if now.Sub(row.UserSession.LastSeenAt) >= touchInterval {
			if err := q.TouchSession(ctx, identitydb.TouchSessionParams{
				ID: row.UserSession.ID, Now: now, IdleExpiresAt: now.Add(s.IdleTTL)}); err != nil {
				return err
			}
		}
		p = principalFor(row.User, row.UserSession.ID, row.UserSession.CsrfSecret, row.MfaEnabled)
		return nil
	})
	if db.IsNoRows(err) {
		return authctx.Principal{}, apperr.Unauthenticated("Your session has expired. Sign in again.")
	}
	if err != nil {
		return authctx.Principal{}, apperr.Internal(err)
	}
	return p, nil
}

// Logout revokes the current session.
func (s *Service) Logout(ctx context.Context, p authctx.Principal) error {
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		if err := identitydb.New(tx).RevokeSession(ctx, p.SessionID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "auth.logout",
			TargetType: "session", TargetID: &p.SessionID})
	})
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}

var errInvalidToken = apperr.Gone("token_invalid", "This link is invalid or has expired.")

// VerifyEmail consumes an email verification token.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		userID, err := q.ConsumeUserToken(ctx, identitydb.ConsumeUserTokenParams{
			TokenHash: HashToken(token), Purpose: "verify_email", Now: s.Clock.Now()})
		if err != nil {
			return err
		}
		if err := q.MarkEmailVerified(ctx, userID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &userID, Action: "user.email_verified",
			TargetType: "user", TargetID: &userID})
	})
	if db.IsNoRows(err) {
		return errInvalidToken
	}
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ForgotPassword emails a reset link if the account exists. It always
// succeeds from the caller's point of view.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = validate.NormalizeEmail(email)
	var v validate.Errors
	if !v.Email("email", email) {
		return v.Err()
	}
	var token string
	err := s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		u, err := q.GetUserByEmail(ctx, email)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.InvalidateUserTokens(ctx, identitydb.InvalidateUserTokensParams{UserID: u.ID, Purpose: "password_reset"}); err != nil {
			return err
		}
		if token, err = s.issueToken(ctx, q, u.ID, "password_reset", passwordResetTTL); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &u.ID, Action: "auth.password_reset_requested",
			TargetType: "user", TargetID: &u.ID})
	})
	if err != nil {
		return apperr.Internal(err)
	}
	if token != "" {
		s.send(ctx, mailer.Message{To: email, Subject: "Reset your password",
			Text: "Open this link within 1 hour to choose a new password:\n\n" +
				s.link("/reset-password", url.Values{"token": {token}}) +
				"\n\nIf you did not ask for this, you can ignore this email."})
	}
	return nil
}

// ResetPassword sets a new password using a reset token and signs out every
// session of the user.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	var v validate.Errors
	if !v.Password("new_password", newPassword) {
		return v.Err()
	}
	if err := s.checkBreached(ctx, newPassword); err != nil {
		if ae, ok := apperr.As(err); ok && len(ae.Fields) == 1 {
			ae.Fields[0].Field = "new_password"
		}
		return err
	}
	hash, err := s.Hasher.Hash(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	err = s.DB.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		userID, err := q.ConsumeUserToken(ctx, identitydb.ConsumeUserTokenParams{
			TokenHash: HashToken(token), Purpose: "password_reset", Now: s.Clock.Now()})
		if err != nil {
			return err
		}
		if err := q.UpdatePasswordHash(ctx, identitydb.UpdatePasswordHashParams{ID: userID, PasswordHash: hash}); err != nil {
			return err
		}
		// Receiving the email proves ownership of the address.
		if err := q.MarkEmailVerified(ctx, userID); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, userID); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &userID, Action: "auth.password_reset",
			TargetType: "user", TargetID: &userID})
	})
	if db.IsNoRows(err) {
		return errInvalidToken
	}
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// GetUser returns the user by ID.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	var u identitydb.User
	err := s.DB.Tx(ctx, db.Scope{UserID: id}, func(tx pgx.Tx) error {
		var err error
		u, err = identitydb.New(tx).GetUserByID(ctx, id)
		return err
	})
	if db.IsNoRows(err) {
		return User{}, apperr.NotFound("User not found.")
	}
	if err != nil {
		return User{}, apperr.Internal(err)
	}
	return toUser(u), nil
}

// UpdateProfile changes the display name of the principal.
func (s *Service) UpdateProfile(ctx context.Context, p authctx.Principal, displayName string) (User, error) {
	displayName = strings.TrimSpace(displayName)
	var v validate.Errors
	if !v.Length("display_name", displayName, 1, 100) {
		return User{}, v.Err()
	}
	var u identitydb.User
	err := s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error {
		var err error
		u, err = identitydb.New(tx).UpdateDisplayName(ctx, identitydb.UpdateDisplayNameParams{ID: p.UserID, DisplayName: displayName})
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "user.profile_updated",
			TargetType: "user", TargetID: &p.UserID})
	})
	if err != nil {
		return User{}, apperr.Internal(err)
	}
	return toUser(u), nil
}

func (s *Service) issueToken(ctx context.Context, q *identitydb.Queries, userID uuid.UUID, purpose string, ttl time.Duration) (string, error) {
	token, hash := NewToken()
	err := q.CreateUserToken(ctx, identitydb.CreateUserTokenParams{
		ID: db.NewID(), UserID: userID, Purpose: purpose, TokenHash: hash, ExpiresAt: s.Clock.Now().Add(ttl)})
	if err != nil {
		return "", fmt.Errorf("issue %s token: %w", purpose, err)
	}
	return token, nil
}

// send delivers email after the transaction committed. Delivery failures are
// logged, not returned: the account state is already correct and the user can
// request the email again. (Phase 4 moves this to a retried background job.)
func (s *Service) send(ctx context.Context, m mailer.Message) {
	if err := s.Mailer.Send(ctx, m); err != nil {
		s.Log.ErrorContext(ctx, "send email", slog.String("subject", m.Subject), slog.String("error", err.Error()))
	}
}

func (s *Service) link(path string, q url.Values) string {
	u := *s.BaseURL
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Service) dummy() string {
	s.dummyOnce.Do(func() {
		h, err := s.Hasher.Hash("dummy password for timing")
		if err != nil {
			panic(errors.New("identity: cannot create dummy hash"))
		}
		s.dummyHash = h
	})
	return s.dummyHash
}

// checkBreached rejects passwords found in known breaches. The range API
// only sees a 5-character hash prefix. If it cannot be reached, the
// password is accepted (availability) and the failure is logged.
func (s *Service) checkBreached(ctx context.Context, password string) error {
	if s.Pwned == nil {
		return nil
	}
	bad, err := s.Pwned.Breached(ctx, password)
	if err != nil {
		s.Log.WarnContext(ctx, "breached-password check unavailable", slog.String("error", err.Error()))
		return nil
	}
	if bad {
		return apperr.Invalid(apperr.FieldError{Field: "password", Code: "breached",
			Message: "This password appeared in a data breach. Choose a different one."})
	}
	return nil
}
