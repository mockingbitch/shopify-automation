package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
)

func TestRegisterVerifyLoginLogout(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("alice@example.com", "Alice")

	var s struct {
		Data struct {
			User struct {
				Email         string `json:"email"`
				EmailVerified bool   `json:"email_verified"`
			} `json:"user"`
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	c.must(200, "GET", "/auth/session", nil).json(t, &s)
	if s.Data.User.Email != "alice@example.com" || !s.Data.User.EmailVerified || s.Data.CSRFToken != c.csrf {
		t.Fatalf("session: %+v", s.Data)
	}
	if strings.Contains(string(c.do("GET", "/auth/session", nil).Body), "password") {
		t.Fatal("session response must not mention password data")
	}

	c.must(204, "POST", "/auth/logout", nil)
	c.must(401, "GET", "/auth/session", nil)
}

func TestRegisterDoesNotRevealExistingEmail(t *testing.T) {
	e := newEnv(t)
	e.signUp("bob@example.com", "Bob")
	c := e.client()
	r := c.must(202, "POST", "/auth/register", map[string]string{"email": "BOB@example.com", "password": testPassword, "display_name": "Other"})
	if !strings.Contains(string(r.Body), "check_email") {
		t.Fatalf("body: %s", r.Body)
	}
	m, _ := e.mail.Last("bob@example.com")
	if m.Subject != "Bạn đã có tài khoản" {
		t.Fatalf("expected notice email, got %q", m.Subject)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	r := c.must(422, "POST", "/auth/register", map[string]string{"email": "nope", "password": "short", "display_name": ""})
	var p struct {
		Errors []struct{ Field string } `json:"errors"`
	}
	r.json(t, &p)
	if len(p.Errors) != 3 {
		t.Fatalf("errors: %s", r.Body)
	}
	c.must(400, "POST", "/auth/register", map[string]any{"email": "a@b.co", "password": testPassword, "display_name": "A", "is_platform_admin": true})
}

func TestLoginFailuresAndRateLimit(t *testing.T) {
	e := newEnv(t)
	e.signUp("carol@example.com", "Carol")
	c := e.client()
	r := c.must(401, "POST", "/auth/login", map[string]string{"email": "carol@example.com", "password": "wrong password here"})
	unknown := c.must(401, "POST", "/auth/login", map[string]string{"email": "nobody@example.com", "password": "wrong password here"})
	if string(r.Body[:40]) != string(unknown.Body[:40]) {
		t.Fatalf("unknown and wrong-password responses differ:\n%s\n%s", r.Body, unknown.Body)
	}
	for i := 0; i < 4; i++ {
		c.do("POST", "/auth/login", map[string]string{"email": "carol@example.com", "password": "wrong password here"})
	}
	r = c.must(429, "POST", "/auth/login", map[string]string{"email": "carol@example.com", "password": testPassword})
	if r.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestSessionExpiry(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("dave@example.com", "Dave")
	e.clock.Advance(11 * time.Hour)
	c.must(200, "GET", "/me", nil) // refreshes idle expiry
	e.clock.Advance(11 * time.Hour)
	c.must(200, "GET", "/me", nil)
	e.clock.Advance(13 * time.Hour) // idle timeout
	c.must(401, "GET", "/me", nil)
}

func TestCSRFAndOrigin(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("erin@example.com", "Erin")
	token := c.csrf
	c.csrf = ""
	if r := c.must(403, "POST", "/orgs", map[string]string{"name": "X", "slug": "xorg"}); r.code(t) != "csrf_failed" {
		t.Fatalf("code %s", r.code(t))
	}
	c.csrf = "forged"
	c.must(403, "POST", "/orgs", map[string]string{"name": "X", "slug": "xorg"})
	c.csrf = token
	c.must(403, "POST", "/orgs", map[string]string{"name": "X", "slug": "xorg"}, "Origin", "https://evil.example")
	c.must(201, "POST", "/orgs", map[string]string{"name": "X", "slug": "xorg"}, "Origin", "http://app.test")
	c.must(415, "POST", "/orgs", nil, "Content-Type", "text/plain")
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("frank@example.com", "Frank")
	anon := e.client()
	anon.must(202, "POST", "/auth/password/forgot", map[string]string{"email": "frank@example.com"})
	anon.must(202, "POST", "/auth/password/forgot", map[string]string{"email": "ghost@example.com"}) // no leak
	tok := e.tokenFromMail("frank@example.com", "Đặt lại")
	anon.must(422, "POST", "/auth/password/reset", map[string]string{"token": tok, "new_password": "short"})
	anon.must(204, "POST", "/auth/password/reset", map[string]string{"token": tok, "new_password": "a brand new passphrase"})
	anon.must(410, "POST", "/auth/password/reset", map[string]string{"token": tok, "new_password": "another new passphrase"})
	c.must(401, "GET", "/me", nil)
	c2 := e.client()
	c2.must(401, "POST", "/auth/login", map[string]string{"email": "frank@example.com", "password": testPassword})
	c2.login("frank@example.com", "a brand new passphrase")
}

func TestLoginRotatesSession(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("gina@example.com", "Gina")
	old := c.csrf
	c.login("gina@example.com", testPassword)
	if c.csrf == old {
		t.Fatal("csrf token should change with the new session")
	}
	var revoked int
	err := e.db.Tx(context.Background(), db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM user_sessions WHERE revoked_at IS NOT NULL`).Scan(&revoked)
	})
	if err != nil || revoked != 1 {
		t.Fatalf("revoked sessions = %d, err %v", revoked, err)
	}
}

func TestUpdateMe(t *testing.T) {
	e := newEnv(t)
	c := e.signUp("hank@example.com", "Hank")
	var out struct {
		Data struct {
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	c.must(200, "PATCH", "/me", map[string]string{"display_name": "  Hank H.  "}).json(t, &out)
	if out.Data.DisplayName != "Hank H." {
		t.Fatalf("got %q", out.Data.DisplayName)
	}
	c.must(422, "PATCH", "/me", map[string]string{"display_name": ""})
}

func TestResendVerification(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.must(202, "POST", "/auth/register", map[string]string{"email": "late@a.example", "password": testPassword, "display_name": "Late"})
	c.login("late@a.example", testPassword)
	e.client().must(401, "POST", "/auth/verify-email/resend", nil)

	c.must(202, "POST", "/auth/verify-email/resend", nil)
	tok := e.tokenFromMail("late@a.example", "Xác minh")
	c.must(204, "POST", "/auth/verify-email", map[string]string{"token": tok})
	var me struct {
		Data struct {
			EmailVerified bool `json:"email_verified"`
		} `json:"data"`
	}
	c.must(200, "GET", "/me", nil).json(t, &me)
	if !me.Data.EmailVerified {
		t.Fatal("not verified with the resent link")
	}

	// Verified users get the same answer and no email; the limit still applies.
	before := len(e.mail.Sent)
	c.must(202, "POST", "/auth/verify-email/resend", nil)
	if len(e.mail.Sent) != before {
		t.Fatal("email sent to a verified user")
	}
	c.must(202, "POST", "/auth/verify-email/resend", nil)
	c.must(429, "POST", "/auth/verify-email/resend", nil)
}
