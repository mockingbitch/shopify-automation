package app_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/app"
	"shopee-automation/internal/browser"
	"shopee-automation/internal/identity"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/config"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/platform/testdb"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shopee/shopeefake"
)

type env struct {
	t      *testing.T
	srv    *httptest.Server
	app    *app.App
	db     *db.DB
	mail   *mailer.Memory
	clock  *clock.Fake
	limits *ratelimit.Memory
	queue  *queue.Inline
	store  *storage.Memory
	fake   *shopeefake.Server
	system *db.DB
	kek    []byte // the test KEK ("t1")
}

// testExtensionOrigin is the extension origin allowed by CORS in tests.
const testExtensionOrigin = "chrome-extension://abcdefghijklmnopabcdefghijklmnop"

// testRecipeSeed is the Ed25519 seed that signs recipes in tests.
var testRecipeSeed = []byte("recipe-signing-seed-for-tests-32")

// Test partner app credentials for the in-process Shopee fake.
const testPartnerID = 2001887

var testPartnerKey = []byte("fake-partner-key-for-tests-only-0123456789")

func newEnv(t *testing.T, opts ...func(*config.Config)) *env {
	t.Helper()
	d := testdb.New(t)
	e := &env{t: t, db: d, mail: &mailer.Memory{}, clock: clock.NewFake(time.Now()), limits: &ratelimit.Memory{}, queue: &queue.Inline{}, store: &storage.Memory{}}
	if os.Getenv("TEST_DATABASE_SYSTEM_URL") != "" {
		e.system = testdb.System(t, d)
	}
	e.fake = shopeefake.New(testPartnerID, testPartnerKey)
	e.fake.Now = e.clock.Now
	t.Cleanup(e.fake.Close)
	kek := make([]byte, 32)
	_, _ = rand.Read(kek)
	e.kek = kek
	keys, err := envelope.NewKeyring("t1", map[string][]byte{"t1": kek})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse("http://app.test")
	cfg := config.Config{
		Env: "test", PublicBaseURL: base, CookieSecure: false,
		SessionIdleTTL: 12 * time.Hour, SessionAbsoluteTTL: 7 * 24 * time.Hour,
		ExtensionOrigins: []string{testExtensionOrigin},
	}
	for _, o := range opts {
		o(&cfg)
	}
	signer, err := browser.NewSigner("test1", base64.StdEncoding.EncodeToString(testRecipeSeed))
	if err != nil {
		t.Fatal(err)
	}
	e.app = app.New(app.Deps{
		Signer: signer,
		Config: cfg, DB: d, SystemDB: e.system, Limiter: e.limits, Mailer: e.mail, Clock: e.clock,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hasher: identity.Hasher{Params: identity.Argon2Params{MemoryKiB: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}},
		Keys:   keys,
		Shopee: &shopee.Client{HTTP: &http.Client{Timeout: 5 * time.Second}, Clock: e.clock,
			Sleep: func(context.Context, time.Duration) error { return nil }},
		Queue: e.queue,
		Store: e.store, Fetcher: media.NewTestFetcher(),
	})
	e.queue.Handlers = map[string]queue.Handler{
		queue.TypeShopSync:       e.app.Shops.HandleSync,
		queue.TypeTokenRefresh:   e.app.Shops.HandleRefresh,
		queue.TypeWebhookProcess: e.app.Shops.HandleWebhook,
		queue.TypeStepExecute:    e.app.Engine.HandleExecute,
		queue.TypeImport:         e.app.Products.HandleImport,
		queue.TypeMailSend:       e.app.Outbox.HandleSend,
	}
	e.srv = httptest.NewServer(e.app.Handler())
	t.Cleanup(e.srv.Close)
	return e
}

// client is a browser-like API client with a cookie jar and CSRF token.
type client struct {
	e    *env
	http *http.Client
	csrf string
	// extra headers for every request
	header http.Header
	// skipContract disables OpenAPI response validation (for deliberately
	// malformed requests to undocumented paths).
	skipContract bool
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}, header: http.Header{}}
}

type resp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

func (r resp) code(t *testing.T) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body, &p)
	return p.Code
}

// noRedirect stops the client at 3xx responses so tests can inspect them.
func (c *client) noRedirect() *client {
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// requireSystem skips tests that need the app_system role when it is not
// configured (always configured in CI).
func (e *env) requireSystem() {
	e.t.Helper()
	if e.system == nil {
		e.system = testdb.System(e.t, e.db) // skips or fails per TEST_REQUIRE_DB
	}
}

// promote makes the user a platform admin (directly in the database).
func (e *env) promote(email string) {
	e.t.Helper()
	ctx := context.Background()
	conn, err := pgxConnect(ctx, testdb.OwnerURL(e.t, e.db))
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `UPDATE users SET is_platform_admin = true WHERE email = $1`, email); err != nil {
		e.t.Fatal(err)
	}
}

func (c *client) do(method, path string, body any, hdr ...string) resp {
	c.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.e.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.e.srv.URL+"/api/v1"+path, rd)
	if err != nil {
		c.e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range c.header {
		req.Header[k] = v
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if !c.skipContract {
		validateResponse(c.e.t, req, res.StatusCode, res.Header, b)
	}
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}

func (c *client) must(status int, method, path string, body any, hdr ...string) resp {
	c.e.t.Helper()
	r := c.do(method, path, body, hdr...)
	if r.Status != status {
		c.e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.Status, status, r.Body)
	}
	return r
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9._%-]+)`)

func (e *env) tokenFromMail(to, subjectPrefix string) string {
	e.t.Helper()
	m, ok := e.mail.Last(to)
	if !ok || !strings.HasPrefix(m.Subject, subjectPrefix) {
		e.t.Fatalf("no %q email to %s (last: %+v)", subjectPrefix, to, m)
	}
	match := tokenRe.FindStringSubmatch(m.Text)
	if match == nil {
		e.t.Fatalf("no token in email: %s", m.Text)
	}
	tok, _ := url.QueryUnescape(match[1])
	return tok
}

const testPassword = "correct horse battery staple"

// signUp registers, verifies and signs in a new user.
func (e *env) signUp(email, name string) *client {
	e.t.Helper()
	c := e.client()
	c.must(202, "POST", "/auth/register", map[string]string{"email": email, "password": testPassword, "display_name": name})
	tok := e.tokenFromMail(email, "Xác minh")
	c.must(204, "POST", "/auth/verify-email", map[string]string{"token": tok})
	c.login(email, testPassword)
	return c
}

func (c *client) login(email, password string) {
	c.e.t.Helper()
	r := c.must(200, "POST", "/auth/login", map[string]string{"email": email, "password": password})
	var out struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	r.json(c.e.t, &out)
	c.csrf = out.Data.CSRFToken
}

type orgResp struct {
	Data struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"data"`
}

func (c *client) createOrg(name, slug string) string {
	c.e.t.Helper()
	var o orgResp
	c.must(201, "POST", "/orgs", map[string]string{"name": name, "slug": slug}).json(c.e.t, &o)
	return o.Data.ID
}

type member struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

func (c *client) members(orgID string) []member {
	c.e.t.Helper()
	var out struct {
		Data []member `json:"data"`
	}
	c.must(200, "GET", "/orgs/"+orgID+"/members", nil).json(c.e.t, &out)
	return out.Data
}

func findMember(ms []member, email string) member {
	for _, m := range ms {
		if m.Email == email {
			return m
		}
	}
	return member{}
}

// addMember invites email with role into orgID (as owner) and accepts as invitee.
func (e *env) addMember(owner *client, orgID string, invitee *client, email, role string) {
	e.t.Helper()
	owner.must(201, "POST", "/orgs/"+orgID+"/invitations", map[string]string{"email": email, "role": role})
	tok := e.tokenFromMail(email, "Bạn được mời")
	invitee.must(200, "POST", "/invitations/accept", map[string]string{"token": tok})
}

var pgxConnect = pgx.Connect
