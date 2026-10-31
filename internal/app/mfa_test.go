package app_test

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"shopee-automation/internal/platform/config"
	"shopee-automation/internal/platform/totp"
)

// enableMFA enrolls c's user and returns the secret and recovery codes.
func enableMFA(c *client) ([]byte, []string) {
	c.e.t.Helper()
	var setup struct {
		Data struct {
			Secret string `json:"secret"`
			URI    string `json:"otpauth_uri"`
		} `json:"data"`
	}
	c.must(200, "POST", "/auth/mfa/setup", nil).json(c.e.t, &setup)
	if !strings.HasPrefix(setup.Data.URI, "otpauth://totp/") {
		c.e.t.Fatalf("uri: %s", setup.Data.URI)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Data.Secret)
	if err != nil {
		c.e.t.Fatal(err)
	}
	c.must(422, "POST", "/auth/mfa/confirm", map[string]string{"code": "000000"})
	var codes struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	c.must(200, "POST", "/auth/mfa/confirm", map[string]string{"code": totp.Code(secret, totp.Step(c.e.clock.Now()))}).json(c.e.t, &codes)
	if len(codes.Data.RecoveryCodes) != 10 {
		c.e.t.Fatalf("recovery codes: %v", codes.Data.RecoveryCodes)
	}
	return secret, codes.Data.RecoveryCodes
}

type loginResp struct {
	Data struct {
		MFARequired bool   `json:"mfa_required"`
		Challenge   string `json:"challenge"`
		CSRFToken   string `json:"csrf_token"`
		User        struct {
			MFAEnabled bool `json:"mfa_enabled"`
		} `json:"user"`
	} `json:"data"`
}

func TestMFALogin(t *testing.T) {
	e := newEnv(t)
	e.limits.Now = e.clock.Now // login limits follow the test clock
	c := e.signUp("mfa@a.example", "Mfa").noRedirect()
	secret, recovery := enableMFA(c)
	c.must(409, "POST", "/auth/mfa/setup", nil)
	var st struct {
		Data struct {
			Enabled bool `json:"enabled"`
			Left    int  `json:"recovery_codes_left"`
		} `json:"data"`
	}
	c.must(200, "GET", "/auth/mfa", nil).json(t, &st)
	if !st.Data.Enabled || st.Data.Left != 10 {
		t.Fatalf("status: %+v", st)
	}

	// Password alone no longer signs in.
	fresh := e.client()
	var lr loginResp
	fresh.must(200, "POST", "/auth/login", map[string]string{"email": "mfa@a.example", "password": testPassword}).json(t, &lr)
	if !lr.Data.MFARequired || lr.Data.Challenge == "" || lr.Data.CSRFToken != "" {
		t.Fatalf("login: %+v", lr)
	}
	fresh.must(401, "GET", "/auth/session", nil)
	fresh.must(422, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": "123456"})
	e.clock.Advance(totp.Period)
	code := totp.Code(secret, totp.Step(e.clock.Now()))
	var ok loginResp
	fresh.must(200, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": code}).json(t, &ok)
	if ok.Data.CSRFToken == "" || !ok.Data.User.MFAEnabled {
		t.Fatalf("second step: %+v", ok)
	}
	fresh.csrf = ok.Data.CSRFToken
	fresh.must(200, "GET", "/auth/session", nil)
	// The challenge is single use, and the same code cannot be replayed.
	fresh.must(401, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": code})
	again := e.client()
	again.must(200, "POST", "/auth/login", map[string]string{"email": "mfa@a.example", "password": testPassword}).json(t, &lr)
	again.must(422, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": code})

	// A recovery code works once.
	again.must(200, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": strings.ToLower(recovery[0])})
	third := e.client()
	third.must(200, "POST", "/auth/login", map[string]string{"email": "mfa@a.example", "password": testPassword}).json(t, &lr)
	third.must(422, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": recovery[0]})

	// Five wrong attempts end the challenge.
	for i := 0; i < 4; i++ {
		third.must(422, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": "000000"})
	}
	e.clock.Advance(totp.Period)
	third.must(401, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": totp.Code(secret, totp.Step(e.clock.Now()))})

	// Challenges expire.
	e.clock.Advance(time.Minute) // past the login rate-limit window
	third.must(200, "POST", "/auth/login", map[string]string{"email": "mfa@a.example", "password": testPassword}).json(t, &lr)
	e.clock.Advance(6 * time.Minute)
	third.must(401, "POST", "/auth/login/mfa", map[string]string{"challenge": lr.Data.Challenge, "code": totp.Code(secret, totp.Step(e.clock.Now()))})

	// New recovery codes need a fresh authenticator code; turning MFA off
	// needs the password and a code.
	e.clock.Advance(totp.Period)
	var rc struct {
		Data struct {
			RecoveryCodes []string `json:"recovery_codes"`
		} `json:"data"`
	}
	fresh.must(200, "POST", "/auth/mfa/recovery-codes", map[string]string{"code": totp.Code(secret, totp.Step(e.clock.Now()))}).json(t, &rc)
	if len(rc.Data.RecoveryCodes) != 10 || rc.Data.RecoveryCodes[0] == recovery[1] {
		t.Fatalf("regenerated: %v", rc.Data.RecoveryCodes)
	}
	fresh.must(422, "POST", "/auth/mfa/disable", map[string]string{"password": "wrong password here", "code": rc.Data.RecoveryCodes[0]})
	fresh.must(204, "POST", "/auth/mfa/disable", map[string]string{"password": testPassword, "code": rc.Data.RecoveryCodes[0]})
	e.clock.Advance(time.Minute)
	plain := e.client()
	var pl loginResp
	plain.must(200, "POST", "/auth/login", map[string]string{"email": "mfa@a.example", "password": testPassword}).json(t, &pl)
	if pl.Data.MFARequired || pl.Data.CSRFToken == "" {
		t.Fatalf("after disable: %+v", pl)
	}
}

func TestOrgRequiresMFA(t *testing.T) {
	e := newEnv(t)
	owner := e.signUp("owner@a.example", "Owner")
	org := owner.createOrg("Org A", "org-a")
	adm := e.signUp("admin@a.example", "Admin")
	e.addMember(owner, org, adm, "admin@a.example", "admin")
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")

	turnOn := func(c *client, status int) resp {
		r := c.must(200, "GET", "/orgs/"+org, nil)
		return c.must(status, "PATCH", "/orgs/"+org, map[string]any{"settings": map[string]any{"allow_self_approval": false, "require_mfa": true}},
			"If-Match", r.Header.Get("ETag"))
	}
	if r := turnOn(owner, 422); !strings.Contains(string(r.Body), "mfa_required") {
		t.Fatalf("enable without own MFA: %s", r.Body)
	}
	enableMFA(owner)
	turnOn(owner, 200)

	// The admin without MFA is blocked from the organization; the operator
	// is not. Account settings stay reachable.
	if code := adm.must(403, "GET", "/orgs/"+org+"/shops", nil).code(t); code != "mfa_required" {
		t.Fatalf("admin: %s", code)
	}
	op.must(200, "GET", "/orgs/"+org+"/shops", nil)
	adm.must(200, "GET", "/auth/mfa", nil)
	enableMFA(adm)
	adm.must(200, "GET", "/orgs/"+org+"/shops", nil)
}

// With REQUIRE_ADMIN_MFA, platform admin routes need two-step verification.
func TestAdminRequiresMFA(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RequireAdminMFA = true })
	admin := e.signUp("root@platform.example", "Root")
	e.promote("root@platform.example")
	if code := admin.must(403, "GET", "/admin/shopee-apps", nil).code(t); code != "mfa_required" {
		t.Fatalf("admin without MFA: %s", code)
	}
	user := e.signUp("user@a.example", "User")
	user.must(404, "GET", "/admin/shopee-apps", nil) // still hidden from non-admins
	enableMFA(admin)
	admin.must(200, "GET", "/admin/shopee-apps", nil)
}
