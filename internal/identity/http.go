package identity

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/reqmeta"
	"shopee-automation/internal/platform/validate"
)

// MembershipLister returns the caller's organization memberships for the
// session bootstrap response. Implemented by the tenancy module.
type MembershipLister func(ctx context.Context, userID uuid.UUID) (any, error)

// Handler serves authentication endpoints.
type Handler struct {
	Svc          *Service
	Limiter      ratelimit.Limiter
	Log          *slog.Logger
	CookieSecure bool
	Memberships  MembershipLister
}

// CookieName returns the session cookie name. With Secure cookies the
// __Host- prefix pins the cookie to this exact host and path.
func (h Handler) CookieName() string {
	if h.CookieSecure {
		return "__Host-sid"
	}
	return "sid"
}

// CSRFHeader carries the CSRF token on unsafe requests.
const CSRFHeader = "X-CSRF-Token"

func (h Handler) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	// Secure is configurable only so that local development works over plain
	// HTTP; config.Load rejects COOKIE_SECURE=false in production.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // see comment above
		Name: h.CookieName(), Value: token, Path: "/", Expires: expires,
		MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h Handler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: h.CookieName(), Value: "", Path: "/", MaxAge: -1, //nolint:gosec // see setCookie

		HttpOnly: true, Secure: h.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// limit applies a rate limit. Redis failures fail open (availability) but
// are logged.
func (h Handler) limit(r *http.Request, key string, n int, window time.Duration) error {
	ok, retry, err := h.Limiter.Allow(r.Context(), key, n, window)
	if err != nil {
		h.Log.WarnContext(r.Context(), "rate limiter unavailable", slog.String("error", err.Error()))
		return nil
	}
	if !ok {
		e := apperr.RateLimited(fmt.Sprintf("Too many attempts. Try again in %d seconds.", int(retry.Seconds())+1))
		return e
	}
	return nil
}

func clientKey(r *http.Request) string {
	if ip := reqmeta.From(r.Context()).IP; ip != nil {
		return ip.String()
	}
	return "unknown"
}

// Register handles POST /auth/register.
func (h Handler) Register(w http.ResponseWriter, r *http.Request) error {
	var in RegisterInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.limit(r, "register:ip:"+clientKey(r), 10, time.Hour); err != nil {
		return err
	}
	if err := h.Svc.Register(r.Context(), in); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[map[string]string]{Data: map[string]string{
		"status": "check_email"}})
}

// ResendVerification handles POST /auth/verify-email/resend.
func (h Handler) ResendVerification(w http.ResponseWriter, r *http.Request) error {
	p := authctx.MustPrincipal(r.Context())
	if err := h.limit(r, "verify-resend:user:"+p.UserID.String(), 3, time.Hour); err != nil {
		return err
	}
	if err := h.Svc.ResendVerification(r.Context(), p); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[map[string]string]{Data: map[string]string{
		"status": "check_email"}})
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionView struct {
	User        User   `json:"user"`
	CSRFToken   string `json:"csrf_token"`
	Memberships any    `json:"memberships,omitempty"`
}

// Login handles POST /auth/login.
func (h Handler) Login(w http.ResponseWriter, r *http.Request) error {
	var in loginInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	email := validate.NormalizeEmail(in.Email)
	if err := h.limit(r, "login:ip:"+clientKey(r), 30, time.Minute); err != nil {
		return err
	}
	if err := h.limit(r, "login:ipemail:"+clientKey(r)+":"+email, 5, time.Minute); err != nil {
		return err
	}
	var prev uuid.UUID
	if p, ok := authctx.PrincipalFrom(r.Context()); ok {
		prev = p.SessionID
	}
	sess, err := h.Svc.Login(r.Context(), email, in.Password, prev)
	if err != nil {
		return err
	}
	h.setCookie(w, sess.Token, sess.ExpiresAt)
	return httpx.JSON(w, http.StatusOK, httpx.Data[sessionView]{Data: sessionView{
		User: sess.User, CSRFToken: sess.Principal.CSRFToken}})
}

// Logout handles POST /auth/logout.
func (h Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Logout(r.Context(), authctx.MustPrincipal(r.Context())); err != nil {
		return err
	}
	h.clearCookie(w)
	return httpx.NoContent(w)
}

// Session handles GET /auth/session: the dashboard bootstrap call.
func (h Handler) Session(w http.ResponseWriter, r *http.Request) error {
	p := authctx.MustPrincipal(r.Context())
	u, err := h.Svc.GetUser(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	view := sessionView{User: u, CSRFToken: p.CSRFToken}
	if h.Memberships != nil {
		if view.Memberships, err = h.Memberships(r.Context(), p.UserID); err != nil {
			return err
		}
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[sessionView]{Data: view})
}

type tokenInput struct {
	Token string `json:"token"`
}

// VerifyEmail handles POST /auth/verify-email.
func (h Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) error {
	var in tokenInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.limit(r, "verify:ip:"+clientKey(r), 20, time.Hour); err != nil {
		return err
	}
	if err := h.Svc.VerifyEmail(r.Context(), in.Token); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

type emailInput struct {
	Email string `json:"email"`
}

// ForgotPassword handles POST /auth/password/forgot.
func (h Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) error {
	var in emailInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.limit(r, "forgot:ip:"+clientKey(r), 10, time.Hour); err != nil {
		return err
	}
	if err := h.limit(r, "forgot:email:"+validate.NormalizeEmail(in.Email), 3, time.Hour); err != nil {
		return err
	}
	if err := h.Svc.ForgotPassword(r.Context(), in.Email); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[map[string]string]{Data: map[string]string{"status": "check_email"}})
}

type resetInput struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ResetPassword handles POST /auth/password/reset.
func (h Handler) ResetPassword(w http.ResponseWriter, r *http.Request) error {
	var in resetInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.limit(r, "reset:ip:"+clientKey(r), 20, time.Hour); err != nil {
		return err
	}
	if err := h.Svc.ResetPassword(r.Context(), in.Token, in.NewPassword); err != nil {
		return err
	}
	h.clearCookie(w)
	return httpx.NoContent(w)
}

// Me handles GET /me.
func (h Handler) Me(w http.ResponseWriter, r *http.Request) error {
	u, err := h.Svc.GetUser(r.Context(), authctx.MustPrincipal(r.Context()).UserID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[User]{Data: u})
}

type updateMeInput struct {
	DisplayName *string `json:"display_name"`
}

// UpdateMe handles PATCH /me.
func (h Handler) UpdateMe(w http.ResponseWriter, r *http.Request) error {
	var in updateMeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p := authctx.MustPrincipal(r.Context())
	if in.DisplayName == nil {
		u, err := h.Svc.GetUser(r.Context(), p.UserID)
		if err != nil {
			return err
		}
		return httpx.JSON(w, http.StatusOK, httpx.Data[User]{Data: u})
	}
	u, err := h.Svc.UpdateProfile(r.Context(), p, *in.DisplayName)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[User]{Data: u})
}

// LoadSession is middleware that authenticates the session cookie when
// present. Invalid cookies are cleared; the request continues anonymously.
func (h Handler) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(h.CookieName())
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		p, err := h.Svc.Authenticate(r.Context(), c.Value)
		if err != nil {
			if !apperr.IsKind(err, apperr.KindUnauthenticated) {
				httpx.Adapter{Log: h.Log}.WriteError(w, r, err)
				return
			}
			h.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(authctx.WithPrincipal(r.Context(), p)))
	})
}

// RequireSession rejects anonymous requests and enforces the CSRF token on
// unsafe methods.
func (h Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := httpx.Adapter{Log: h.Log}
		p, ok := authctx.PrincipalFrom(r.Context())
		if !ok {
			a.WriteError(w, r, apperr.Unauthenticated("Sign in to continue."))
			return
		}
		if !httpx.IsSafeMethod(r.Method) {
			got := r.Header.Get(CSRFHeader)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRFToken)) != 1 {
				a.WriteError(w, r, apperr.Forbidden("csrf_failed", "Missing or invalid CSRF token."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
