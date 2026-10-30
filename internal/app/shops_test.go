package app_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/shopee/shopeefake"
	"shopee-automation/internal/shopee/signer"
)

type appResp struct {
	Data struct {
		ID              string  `json:"id"`
		PartnerID       string  `json:"partner_id"`
		PartnerKeySet   bool    `json:"partner_key_set"`
		PushCallbackURL *string `json:"push_callback_url"`
		Enabled         bool    `json:"enabled"`
	} `json:"data"`
}

// setupApp registers the fake Shopee as a sandbox partner app.
func (e *env) setupApp() (admin *client, appID string, pushURL string) {
	e.t.Helper()
	admin = e.signUp("root@platform.example", "Root")
	e.promote("root@platform.example")
	var a appResp
	admin.must(201, "POST", "/admin/shopee-apps", map[string]any{
		"name": "Sandbox", "environment": "sandbox", "partner_id": strconv.Itoa(testPartnerID),
		"partner_key": string(testPartnerKey), "app_type": "ERP System",
		"api_host": e.fake.URL, "auth_host": e.fake.URL,
	}).json(e.t, &a)
	return admin, a.Data.ID, *a.Data.PushCallbackURL
}

type shopDetail struct {
	Data struct {
		Shop struct {
			ID                     string  `json:"id"`
			ShopeeShopID           string  `json:"shopee_shop_id"`
			ShopName               *string `json:"shop_name"`
			Region                 *string `json:"region"`
			Status                 string  `json:"connection_status"`
			AuthorizationExpiresAt *string `json:"authorization_expires_at"`
		} `json:"shop"`
		Profile *struct {
			Description *string `json:"description"`
			Status      *string `json:"status"`
		} `json:"profile"`
		Capabilities []struct {
			Key     string `json:"key"`
			Mode    string `json:"mode"`
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
		} `json:"capabilities"`
		History []struct {
			Event  string `json:"event"`
			Source string `json:"source"`
		} `json:"history"`
	} `json:"data"`
}

// startConnect calls the connect endpoint and returns the state from the URL.
func startConnect(t *testing.T, c *client, orgID string) string {
	t.Helper()
	var out struct {
		Data struct {
			AuthorizationURL string `json:"authorization_url"`
		} `json:"data"`
	}
	c.must(200, "POST", "/orgs/"+orgID+"/shops/connect", map[string]any{}).json(t, &out)
	u, err := url.Parse(out.Data.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/auth" || q.Get("partner_id") != strconv.Itoa(testPartnerID) || q.Get("auth_type") != "seller" ||
		q.Get("response_type") != "code" || q.Get("redirect_uri") != "http://app.test/api/v1/oauth/shopee/callback" || q.Get("state") == "" {
		t.Fatalf("authorization url: %s", out.Data.AuthorizationURL)
	}
	return q.Get("state")
}

// callback simulates Shopee redirecting the seller back; returns Location.
func callback(t *testing.T, c *client, q url.Values) *url.URL {
	t.Helper()
	r := c.do("GET", "/oauth/shopee/callback?"+q.Encode(), nil)
	if r.Status != 302 {
		t.Fatalf("callback: status %d: %s", r.Status, r.Body)
	}
	loc, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func (e *env) connectShop(c *client, orgID string, shopID int64) string {
	e.t.Helper()
	state := startConnect(e.t, c, orgID)
	loc := callback(e.t, c, url.Values{"code": {e.fake.Authorize(shopID)}, "shop_id": {strconv.FormatInt(shopID, 10)}, "state": {state}})
	parts := strings.Split(loc.Path, "/")
	if loc.Query().Get("connected") != "1" || len(parts) != 5 || parts[3] != "shops" {
		e.t.Fatalf("connect redirect: %s", loc)
	}
	return parts[4]
}

func TestAdminShopeeApps(t *testing.T) {
	e := newEnv(t)
	user := e.signUp("user@a.example", "User")
	user.must(404, "GET", "/admin/shopee-apps", nil)
	user.must(404, "POST", "/admin/shopee-apps", map[string]any{"name": "x"})

	admin, appID, pushURL := e.setupApp()
	if !strings.HasSuffix(pushURL, "/api/v1/webhooks/shopee/"+appID) {
		t.Fatalf("default push url: %s", pushURL)
	}
	r := admin.must(200, "GET", "/admin/shopee-apps", nil)
	if strings.Contains(string(r.Body), string(testPartnerKey)) || !strings.Contains(string(r.Body), `"partner_key_set":true`) {
		t.Fatalf("app list leaks or misses key flag: %s", r.Body)
	}
	if code := admin.must(409, "POST", "/admin/shopee-apps", map[string]any{
		"name": "Dup", "environment": "sandbox", "partner_id": strconv.Itoa(testPartnerID),
		"partner_key": string(testPartnerKey), "app_type": "ERP System",
	}).code(t); code != "app_exists" {
		t.Fatalf("duplicate: %s", code)
	}
	admin.must(422, "POST", "/admin/shopee-apps", map[string]any{
		"name": "Bad", "environment": "staging", "partner_id": "-1", "partner_key": "short", "app_type": "",
		"api_host": "ftp://x",
	})
	var a appResp
	admin.must(200, "PATCH", "/admin/shopee-apps/"+appID, map[string]any{"enabled": false}).json(t, &a)
	if a.Data.Enabled {
		t.Fatal("not disabled")
	}

	// With no enabled app, connecting explains what is missing.
	org := user.createOrg("Org", "org-u")
	if code := user.must(424, "POST", "/orgs/"+org+"/shops/connect", map[string]any{}).code(t); code != "no_shopee_app" {
		t.Fatalf("no app: %s", code)
	}
}

func TestConnectShopFlow(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 501, Name: "Alice Store", Region: "VN", Description: "Hello"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")

	id := e.connectShop(alice, org, 501)

	// The inline queue ran the sync triggered by the connection.
	var d shopDetail
	r := alice.must(200, "GET", "/orgs/"+org+"/shops/"+id, nil)
	r.json(t, &d)
	if strings.Contains(strings.ToLower(string(r.Body)), "token") {
		t.Fatalf("shop detail mentions tokens: %s", r.Body)
	}
	s := d.Data.Shop
	if s.Status != "ACTIVE" || s.ShopeeShopID != "501" || s.ShopName == nil || *s.ShopName != "Alice Store" ||
		s.Region == nil || *s.Region != "VN" || s.AuthorizationExpiresAt == nil {
		t.Fatalf("shop: %+v", s)
	}
	if d.Data.Profile == nil || *d.Data.Profile.Description != "Hello" || *d.Data.Profile.Status != "NORMAL" {
		t.Fatalf("profile: %+v", d.Data.Profile)
	}
	caps := map[string]struct {
		mode    string
		enabled bool
		reason  string
	}{}
	for _, c := range d.Data.Capabilities {
		caps[c.Key] = struct {
			mode    string
			enabled bool
			reason  string
		}{c.Mode, c.Enabled, c.Reason}
	}
	if c := caps["shop.info.read"]; c.mode != "API" || !c.enabled {
		t.Errorf("shop.info.read: %+v", c)
	}
	if c := caps["shop.banner.upload"]; c.mode != "MANUAL" || !c.enabled || c.reason != "browser_extension_unavailable" {
		t.Errorf("shop.banner.upload: %+v", c)
	}
	if c := caps["product.batch_create"]; c.enabled || c.reason != "not_supported" {
		t.Errorf("product.batch_create: %+v", c)
	}
	if len(d.Data.History) == 0 || d.Data.History[len(d.Data.History)-1].Event != "GRANTED" {
		t.Errorf("history: %+v", d.Data.History)
	}

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/orgs/"+org+"/shops?status=ACTIVE&q=alice", nil).json(t, &list)
	if len(list.Data) != 1 || list.Data[0].ID != id {
		t.Fatalf("list: %+v", list)
	}
	alice.must(200, "GET", "/orgs/"+org+"/shops?q=501", nil).json(t, &list)
	if len(list.Data) != 1 {
		t.Fatalf("search by shop id: %+v", list)
	}
	alice.must(422, "GET", "/orgs/"+org+"/shops?status=BOGUS", nil)

	// Sync on demand is queued and deduplicated within a minute.
	before := e.queue.Count(queue.TypeShopSync)
	alice.must(202, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)
	alice.must(202, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)
	if n := e.queue.Count(queue.TypeShopSync) - before; n != 2 || len(e.queue.Errors) != 0 {
		t.Fatalf("sync enqueued %d, errors %v", n, e.queue.Errors)
	}
	if n := e.fake.Calls("/api/v2/shop/get_shop_info"); n != 2 { // connect + one deduplicated manual sync
		t.Fatalf("get_shop_info calls = %d, want 2", n)
	}

	// Disconnect removes tokens and profile; the connection stays as history.
	alice.must(422, "DELETE", "/orgs/"+org+"/shops/"+id, map[string]any{"confirm": false})
	alice.must(204, "DELETE", "/orgs/"+org+"/shops/"+id, map[string]any{"confirm": true})
	alice.must(204, "DELETE", "/orgs/"+org+"/shops/"+id, map[string]any{"confirm": true})
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+id, nil).json(t, &d)
	if d.Data.Shop.Status != "DISCONNECTED" || d.Data.Profile != nil {
		t.Fatalf("after disconnect: %+v", d.Data)
	}
	if code := alice.must(424, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil).code(t); code != "shop_reconnect_required" {
		t.Fatalf("sync after disconnect: %s", code)
	}
	if _, err := e.app.Shops.AccessFor(context.Background(), uuid.MustParse(org), uuid.MustParse(id)); err == nil {
		t.Fatal("tokens still usable after disconnect")
	}

	// Connecting the same shop again reuses the row.
	if again := e.connectShop(alice, org, 501); again != id {
		t.Fatalf("reconnect created a new row: %s vs %s", again, id)
	}
}

func TestCallbackRejectsBadState(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 7, Name: "S", Region: "VN"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	bob := e.signUp("bob@b.example", "Bob").noRedirect()
	e.addMember(alice, org, bob, "bob@b.example", "admin")

	errCode := func(loc *url.URL) string { return loc.Query().Get("connect_error") }
	q := func(state string) url.Values {
		return url.Values{"code": {e.fake.Authorize(7)}, "shop_id": {"7"}, "state": {state}}
	}

	// Garbage state.
	if c := errCode(callback(t, alice, q("nonsense"))); c != "invalid_state" {
		t.Fatalf("garbage: %s", c)
	}
	// A state started by Alice cannot be finished by Bob (same org, other user).
	state := startConnect(t, alice, org)
	if c := errCode(callback(t, bob, q(state))); c != "invalid_state" {
		t.Fatalf("other user: %s", c)
	}
	// Single use.
	if loc := callback(t, alice, q(state)); loc.Query().Get("connected") != "1" {
		t.Fatalf("first use: %s", loc)
	}
	if c := errCode(callback(t, alice, q(state))); c != "state_used" {
		t.Fatalf("replay: %s", c)
	}
	// Expiry after 10 minutes.
	state = startConnect(t, alice, org)
	e.clock.Advance(11 * time.Minute)
	if c := errCode(callback(t, alice, q(state))); c != "state_expired" {
		t.Fatalf("expired: %s", c)
	}
	// Seller denied or Shopee sent no code.
	state = startConnect(t, alice, org)
	if c := errCode(callback(t, alice, url.Values{"state": {state}})); c != "authorization_denied" {
		t.Fatalf("denied: %s", c)
	}
	// Invalid code: the exchange fails cleanly.
	state = startConnect(t, alice, org)
	if c := errCode(callback(t, alice, url.Values{"code": {"bogus"}, "shop_id": {"7"}, "state": {state}})); c != "token_exchange_failed" {
		t.Fatalf("bad code: %s", c)
	}
	// Without a session the browser is sent to the login page, state intact.
	anon := e.client().noRedirect()
	state = startConnect(t, alice, org)
	loc := callback(t, anon, q(state))
	if loc.Path != "/login" || !strings.HasPrefix(loc.Query().Get("next"), "/api/v1/oauth/shopee/callback?") {
		t.Fatalf("anonymous: %s", loc)
	}
	// Losing the permission between start and finish is rejected.
	carol := e.signUp("carol@c.example", "Carol").noRedirect()
	e.addMember(alice, org, carol, "carol@c.example", "admin")
	state = startConnect(t, carol, org)
	m := findMember(alice.members(org), "carol@c.example")
	alice.must(200, "PATCH", "/orgs/"+org+"/members/"+m.ID, map[string]string{"role": "viewer"})
	if c := errCode(callback(t, carol, q(state))); c != "permission_denied" {
		t.Fatalf("demoted: %s", c)
	}
}

func TestShopBelongsToOneOrg(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 9, Name: "Shared", Region: "TH"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	orgA := alice.createOrg("Org A", "org-a")
	bob := e.signUp("bob@b.example", "Bob").noRedirect()
	orgB := bob.createOrg("Org B", "org-b")

	idA := e.connectShop(alice, orgA, 9)
	state := startConnect(t, bob, orgB)
	loc := callback(t, bob, url.Values{"code": {e.fake.Authorize(9)}, "shop_id": {"9"}, "state": {state}})
	if loc.Query().Get("connect_error") != "shop_connected_elsewhere" || strings.Contains(loc.String(), orgA) {
		t.Fatalf("second org: %s", loc)
	}
	alice.must(204, "DELETE", "/orgs/"+orgA+"/shops/"+idA, map[string]any{"confirm": true})
	e.connectShop(bob, orgB, 9)
}

func TestMainAccountSelection(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 11, Name: "VN shop", Region: "VN"})
	e.fake.AddShop(shopeefake.Shop{ID: 12, Name: "TH shop", Region: "TH"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")

	state := startConnect(t, alice, org)
	loc := callback(t, alice, url.Values{"code": {e.fake.AuthorizeMain(77, 11, 12)}, "main_account_id": {"77"}, "state": {state}})
	if loc.Path != "/orgs/"+org+"/shops/select" || loc.Query().Get("state") == "" {
		t.Fatalf("main account redirect: %s", loc)
	}
	sel := loc.Query().Get("state")
	var got struct {
		Data struct {
			Shops []struct {
				ShopeeShopID string  `json:"shopee_shop_id"`
				ShopName     *string `json:"shop_name"`
			} `json:"shops"`
		} `json:"data"`
	}
	alice.must(200, "GET", "/orgs/"+org+"/shops/connect/"+sel, nil).json(t, &got)
	if len(got.Data.Shops) != 2 || got.Data.Shops[0].ShopName == nil || *got.Data.Shops[0].ShopName != "VN shop" {
		t.Fatalf("selection: %+v", got)
	}

	bob := e.signUp("bob@b.example", "Bob")
	e.addMember(alice, org, bob, "bob@b.example", "admin")
	bob.must(404, "GET", "/orgs/"+org+"/shops/connect/"+sel, nil) // bound to Alice

	alice.must(422, "POST", "/orgs/"+org+"/shops/connect/"+sel+"/select", map[string]any{"shopee_shop_ids": []string{"99"}})
	var shops struct {
		Data []struct {
			ID           string `json:"id"`
			ShopeeShopID string `json:"shopee_shop_id"`
		} `json:"data"`
	}
	alice.must(201, "POST", "/orgs/"+org+"/shops/connect/"+sel+"/select", map[string]any{"shopee_shop_ids": []string{"12"}}).json(t, &shops)
	if len(shops.Data) != 1 || shops.Data[0].ShopeeShopID != "12" {
		t.Fatalf("selected: %+v", shops)
	}
	alice.must(404, "POST", "/orgs/"+org+"/shops/connect/"+sel+"/select", map[string]any{"shopee_shop_ids": []string{"11"}})

	// The shared main-account token works, and the shop refreshes on its own.
	var d shopDetail
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+shops.Data[0].ID, nil).json(t, &d)
	if d.Data.Shop.ShopName == nil || *d.Data.Shop.ShopName != "TH shop" {
		t.Fatalf("synced: %+v", d.Data.Shop)
	}
	e.clock.Advance(4 * time.Hour)
	if _, err := e.app.Shops.AccessFor(context.Background(), uuid.MustParse(org), uuid.MustParse(shops.Data[0].ID)); err != nil {
		t.Fatalf("refresh main-account shop: %v", err)
	}
}

func TestConcurrentRefreshCallsShopeeOnce(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 21, Name: "S", Region: "VN"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	id := e.connectShop(alice, org, 21)
	orgID, connID := uuid.MustParse(org), uuid.MustParse(id)

	e.clock.Advance(3*time.Hour + 58*time.Minute) // inside the 5-minute refresh window
	e.fake.RefreshDelay = 50 * time.Millisecond   // widen the race

	var wg sync.WaitGroup
	tokens := make([]string, 50)
	errs := make([]error, 50)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc, err := e.app.Shops.AccessFor(context.Background(), orgID, connID)
			tokens[i], errs[i] = acc.AccessToken, err
		}()
	}
	wg.Wait()
	for i := range 50 {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if tokens[i] != tokens[0] {
			t.Fatalf("caller %d got a different token", i)
		}
	}
	if n := e.fake.Calls("/api/v2/auth/access_token/get"); n != 1 {
		t.Fatalf("upstream refreshes = %d, want 1", n)
	}
}

func TestRefreshRejectedNeedsReconnect(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 31, Name: "S", Region: "VN"})
	e.fake.AddShop(shopeefake.Shop{ID: 32, Name: "Other", Region: "VN"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	id := e.connectShop(alice, org, 31)

	e.fake.RevokeTokens(31)
	e.clock.Advance(4 * time.Hour)
	e.queue.ResetIDs()
	alice.must(202, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)
	var d shopDetail
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+id, nil).json(t, &d)
	if d.Data.Shop.Status != "RECONNECT_REQUIRED" {
		t.Fatalf("status: %s", d.Data.Shop.Status)
	}
	alice.must(424, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)

	// Reconnecting with another shop is refused; with the same shop it works.
	var rc struct {
		Data struct {
			AuthorizationURL string `json:"authorization_url"`
		} `json:"data"`
	}
	alice.must(200, "POST", "/orgs/"+org+"/shops/"+id+"/reconnect", nil).json(t, &rc)
	u, _ := url.Parse(rc.Data.AuthorizationURL)
	loc := callback(t, alice, url.Values{"code": {e.fake.Authorize(32)}, "shop_id": {"32"}, "state": {u.Query().Get("state")}})
	if loc.Query().Get("connect_error") != "shop_mismatch" {
		t.Fatalf("mismatch: %s", loc)
	}
	alice.must(200, "POST", "/orgs/"+org+"/shops/"+id+"/reconnect", nil).json(t, &rc)
	u, _ = url.Parse(rc.Data.AuthorizationURL)
	loc = callback(t, alice, url.Values{"code": {e.fake.Authorize(31)}, "shop_id": {"31"}, "state": {u.Query().Get("state")}})
	if !strings.HasSuffix(loc.Path, "/shops/"+id) {
		t.Fatalf("reconnect: %s", loc)
	}
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+id, nil).json(t, &d)
	if d.Data.Shop.Status != "ACTIVE" {
		t.Fatalf("after reconnect: %s", d.Data.Shop.Status)
	}
}

func TestScheduledRefresh(t *testing.T) {
	e := newEnv(t)
	e.requireSystem()
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 41, Name: "S", Region: "VN"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	e.connectShop(alice, org, 41)

	ctx := context.Background()
	if n, err := e.app.Shops.EnqueueDueRefreshes(ctx); err != nil || n != 0 {
		t.Fatalf("fresh token enqueued: %d %v", n, err)
	}
	e.clock.Advance(3*time.Hour + 40*time.Minute)
	if n, err := e.app.Shops.EnqueueDueRefreshes(ctx); err != nil || n != 1 {
		t.Fatalf("due: %d %v", n, err)
	}
	if n := e.fake.Calls("/api/v2/auth/access_token/get"); n != 1 || len(e.queue.Errors) != 0 {
		t.Fatalf("refreshes = %d, errors %v", n, e.queue.Errors)
	}
}

func signPush(url string, body string) string {
	return signer.HMAC(testPartnerKey, url+"|"+body)
}

func TestWebhooks(t *testing.T) {
	e := newEnv(t)
	e.requireSystem()
	_, appID, pushURL := e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 22000000, Name: "VN", Region: "VN"})
	e.fake.AddShop(shopeefake.Shop{ID: 51, Name: "Soon", Region: "VN"})
	alice := e.signUp("alice@a.example", "Alice").noRedirect()
	org := alice.createOrg("Org A", "org-a")
	revoked := e.connectShop(alice, org, 22000000)
	expiring := e.connectShop(alice, org, 51)

	anon := e.client()
	post := func(body, sig string) resp {
		return anon.do("POST", "/webhooks/shopee/"+appID, rawJSON(body), "Authorization", sig)
	}

	// Documented sample of code 2 (note "shopid").
	body := `{"data":{"authorize_type":"user cancel shop authorization","shopid":22000000,"success":1,"extra":"shop id 22000000 (VN) has been cancelled its authorization"},"code":2,"partner_id":` + strconv.Itoa(testPartnerID) + `,"timestamp":1653394175}`
	if r := post(body, "deadbeef"); r.Status != 401 {
		t.Fatalf("bad signature: %d", r.Status)
	}
	if r := post(body, signPush(pushURL+"x", body)); r.Status != 401 {
		t.Fatalf("signature over another url: %d", r.Status)
	}
	if r := anon.do("POST", "/webhooks/shopee/"+uuid.NewString(), rawJSON(body), "Authorization", signPush(pushURL, body)); r.Status != 404 {
		t.Fatalf("unknown app: %d", r.Status)
	}
	r := post(body, signPush(pushURL, body))
	if r.Status != 204 || len(r.Body) != 0 {
		t.Fatalf("valid push: %d %q", r.Status, r.Body)
	}
	if r := post(body, signPush(pushURL, body)); r.Status != 204 {
		t.Fatalf("duplicate push: %d", r.Status)
	}
	if n := e.queue.Count(queue.TypeWebhookProcess); n != 1 || len(e.queue.Errors) != 0 {
		t.Fatalf("processed %d, errors %v", n, e.queue.Errors)
	}
	var d shopDetail
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+revoked, nil).json(t, &d)
	if d.Data.Shop.Status != "REVOKED" || d.Data.History[0].Event != "REVOKED" || d.Data.History[0].Source != "push" {
		t.Fatalf("after code 2: %+v %+v", d.Data.Shop, d.Data.History)
	}

	// Code 12 (expiring within 7 days), documented sample shape.
	body = `{"code":12,"timestamp":1568606634,"data":{"merchant_expire_soon":[],"shop_expire_soon":[51],"user_expire_soon":[],"expire_before":` +
		strconv.FormatInt(e.clock.Now().Add(5*24*time.Hour).Unix(), 10) + `,"page_no":1,"total_page":1}}`
	if r := post(body, signPush(pushURL, body)); r.Status != 204 {
		t.Fatalf("code 12: %d", r.Status)
	}
	alice.must(200, "GET", "/orgs/"+org+"/shops/"+expiring, nil).json(t, &d)
	if d.Data.Shop.Status != "ACTIVE" || d.Data.History[0].Event != "EXPIRING" {
		t.Fatalf("after code 12: %+v %+v", d.Data.Shop, d.Data.History)
	}

	// Unhandled codes are stored and ignored.
	body = `{"code":3,"shop_id":51,"timestamp":1,"data":{"ordersn":"X"}}`
	if r := post(body, signPush(pushURL, body)); r.Status != 204 {
		t.Fatalf("code 3: %d", r.Status)
	}
}

func TestShopPermissions(t *testing.T) {
	e := newEnv(t)
	e.setupApp()
	e.fake.AddShop(shopeefake.Shop{ID: 61, Name: "S", Region: "VN"})
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org", "org-p")
	id := e.connectShop(owner, org, 61)
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")
	viewer := e.signUp("viewer@a.example", "Viewer")
	e.addMember(owner, org, viewer, "viewer@a.example", "viewer")

	viewer.must(200, "GET", "/orgs/"+org+"/shops", nil)
	viewer.must(200, "GET", "/orgs/"+org+"/shops/"+id+"/capabilities", nil)
	viewer.must(403, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)
	viewer.must(403, "POST", "/orgs/"+org+"/shops/connect", map[string]any{})
	op.must(202, "POST", "/orgs/"+org+"/shops/"+id+"/sync", nil)
	op.must(403, "POST", "/orgs/"+org+"/shops/connect", map[string]any{})
	op.must(403, "POST", "/orgs/"+org+"/shops/"+id+"/reconnect", nil)
	op.must(403, "DELETE", "/orgs/"+org+"/shops/"+id, map[string]any{"confirm": true})
}

// rawJSON sends body unchanged (it is already compact JSON), so the push
// signature computed over it stays valid.
func rawJSON(body string) json.RawMessage { return json.RawMessage(body) }
