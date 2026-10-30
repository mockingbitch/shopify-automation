package app_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// testBannerRecipe is a fixture recipe. Its origin and selectors are made
// up: no real Seller Centre recipe ships with the code.
const testBannerRecipe = `{
  "capability": "shop.banner.upload",
  "version": 1,
  "market": "VN",
  "start_url": "https://seller.example.test/decoration",
  "allowed_origins": ["https://seller.example.test"],
  "inputs": {"banner_images": {"type": "files", "required": true}},
  "steps": [
    {"assert_shop_id": {"target": {"selector": "[data-shop-id]"}, "pattern": "Shop ID: (\\d+)"}},
    {"click": {"role": "button", "name_any": ["Edit"]}},
    {"upload": {"target": {"selector": "input[type=file]"}, "files": "{{input.banner_images}}"}},
    {"click": {"role": "button", "name_any": ["Save"]}},
    {"expect": {"text_any": ["Saved"]}}
  ],
  "stop_on": [{"url_matches": "/login|/verify"}]
}`

// ext calls the extension API like the extension does: an Origin header,
// a bearer token, no cookies.
func (e *env) ext(token, method, path string, body any, hdr ...string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", testExtensionOrigin)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	if method != http.MethodOptions && req.Header.Get("Origin") == testExtensionOrigin && strings.HasPrefix(path, "/ext/") {
		validateResponse(e.t, req, res.StatusCode, res.Header, b)
	}
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}

func (e *env) extMust(status int, token, method, path string, body any) resp {
	e.t.Helper()
	r := e.ext(token, method, path, body)
	if r.Status != status {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.Status, status, r.Body)
	}
	return r
}

type pairedResp struct {
	Data struct {
		Token string `json:"token"`
		Org   struct {
			Name string `json:"name"`
		} `json:"org"`
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	} `json:"data"`
}

// pair creates a pairing code as c and redeems it like the extension.
func (e *env) pair(c *client, org string) (token, deviceID string) {
	e.t.Helper()
	var code struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	c.must(201, "POST", "/orgs/"+org+"/extension/pairing-codes", nil).json(e.t, &code)
	var p pairedResp
	e.extMust(201, "", "POST", "/ext/pair", map[string]string{"code": strings.ToLower(code.Data.Code), "device_name": "Chrome on test"}).json(e.t, &p)
	return p.Data.Token, p.Data.Device.ID
}

// enableRecipe adds the fixture recipe, marks it verified and enables it.
func enableRecipe(t *testing.T, admin *client, raw string) string {
	t.Helper()
	var r struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	admin.must(201, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(raw), "notes": "fixture"}).json(t, &r)
	admin.must(200, "PATCH", "/admin/browser-recipes/"+r.Data.ID, map[string]any{"verified": true, "enabled": true})
	return r.Data.ID
}

type extTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Shop   struct {
		ShopeeShopID string `json:"shopee_shop_id"`
	} `json:"shop"`
	Preview       map[string]any `json:"preview"`
	ScreenshotIDs []string       `json:"screenshot_ids"`
	StepLog       []any          `json:"step_log"`
}

func (e *env) extTasks(token string) []extTask {
	e.t.Helper()
	var l struct {
		Data []extTask `json:"data"`
	}
	e.extMust(200, token, "GET", "/ext/tasks", nil).json(e.t, &l)
	return l.Data
}

type claimResp struct {
	Data struct {
		LeaseToken string `json:"lease_token"`
		Recipe     struct {
			JSON      string `json:"json"`
			Signature string `json:"signature"`
			KeyID     string `json:"key_id"`
		} `json:"recipe"`
		Inputs map[string]struct {
			Type  string `json:"type"`
			Files []struct {
				URL string `json:"url"`
			} `json:"files"`
		} `json:"inputs"`
		Task struct {
			ShopeeShopID string `json:"shopee_shop_id"`
		} `json:"task"`
	} `json:"data"`
}

// bannerSetup connects a shop, enables the fixture recipe and returns the
// pieces of a run whose banner change waits in the extension queue.
type bannerEnv struct {
	e        *env
	owner    *client
	admin    *client
	org      string
	shop     string
	token    string
	runID    string
	recipeID string
}

func newBannerEnv(t *testing.T, shopeeID int64, sections map[string]any, selected []string) *bannerEnv {
	images := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(testPNG(30, 10)) }))
	t.Cleanup(images.Close)
	e := newEnv(t)
	e.requireSystem()
	e.app.Products.Fetcher.Client = images.Client()
	admin, _, _ := e.setupApp()
	setupShopForWorkflow(e, shopeeID, "Shop")
	owner := e.signUp("owner@a.example", "Owner").noRedirect()
	org := owner.createOrg("Org A", "org-a")
	shop := e.connectShop(owner, org, shopeeID)
	b := &bannerEnv{e: e, owner: owner, admin: admin, org: org, shop: shop}
	b.recipeID = enableRecipe(t, admin, testBannerRecipe)
	b.token, _ = e.pair(owner, org)

	if sections == nil {
		sections = map[string]any{"branding": map[string]any{"banner_urls": []string{images.URL + "/banner.png"}}}
	} else if br, ok := sections["branding"].(map[string]any); ok {
		br["banner_urls"] = []string{images.URL + "/banner.png"}
	}
	tpl, v := publishTemplate(t, owner, org, sections)
	var a applyResp
	owner.must(202, "POST", "/orgs/"+org+"/templates/"+tpl+"/versions/"+v+"/apply", map[string]any{"shop_ids": []string{shop}}).json(t, &a)
	b.runID = a.Data.RunIDs[0]
	rv := owner.runView(org, b.runID)
	if rv.Data.PendingApproval == nil {
		t.Fatalf("no approval: %+v", rv.Data.Run)
	}
	if c := byPath(rv.Data.PendingApproval.Diff.Changes)["branding.banner_urls"]; c.Mode != "BROWSER" {
		t.Fatalf("banner should run in the browser: %+v", c)
	}
	owner.must(200, "POST", "/orgs/"+org+"/approvals/"+rv.Data.PendingApproval.ID+"/decision",
		map[string]any{"decision": "APPROVE", "selected_change_paths": selected})
	return b
}

func TestExtensionBannerTaskEndToEnd(t *testing.T) {
	b := newBannerEnv(t, 831, nil, []string{"branding.banner_urls"})
	e, owner, org := b.e, b.owner, b.org

	v := owner.runView(org, b.runID)
	_, status, out := v.step("apply_branding")
	if v.Data.Run.Status != "WAITING_USER" || status != "WAITING_USER" || !strings.Contains(string(out), "browser_task_id") {
		t.Fatalf("waiting for the extension: %s %s %s", v.Data.Run.Status, status, out)
	}
	tasks := e.extTasks(b.token)
	if len(tasks) != 1 || tasks[0].Status != "QUEUED" || tasks[0].Shop.ShopeeShopID != "831" {
		t.Fatalf("tasks: %+v", tasks)
	}
	id := tasks[0].ID

	var c claimResp
	e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).json(t, &c)
	pub := ed25519.NewKeyFromSeed(testRecipeSeed).Public().(ed25519.PublicKey)
	sig, _ := base64.StdEncoding.DecodeString(c.Data.Recipe.Signature)
	var compact bytes.Buffer
	_ = json.Compact(&compact, []byte(testBannerRecipe))
	if c.Data.Recipe.KeyID != "test1" || !ed25519.Verify(pub, []byte(c.Data.Recipe.JSON), sig) || c.Data.Recipe.JSON != compact.String() {
		t.Fatalf("recipe signature: %+v", c.Data.Recipe)
	}
	if in := c.Data.Inputs["banner_images"]; in.Type != "files" || len(in.Files) != 1 || !strings.HasPrefix(in.Files[0].URL, "https://storage.test/") {
		t.Fatalf("inputs: %+v", c.Data.Inputs)
	}
	if c.Data.Task.ShopeeShopID != "831" {
		t.Fatalf("task: %+v", c.Data.Task)
	}
	if code := e.extMust(409, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).code(t); code != "task_unavailable" {
		t.Fatalf("second claim: %s", code)
	}

	lease := c.Data.LeaseToken
	e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/heartbeat", map[string]any{"lease_token": lease, "state": "WAITING_USER", "message": "Seller is verifying"})
	if code := e.extMust(409, b.token, "POST", "/ext/tasks/"+id+"/heartbeat", map[string]any{"lease_token": "00000000-0000-0000-0000-000000000001", "state": "RUNNING"}).code(t); code != "lease_lost" {
		t.Fatalf("stale lease: %s", code)
	}
	e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/heartbeat", map[string]any{"lease_token": lease, "state": "RUNNING"})

	// Screenshot (multipart).
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	_, _ = fw.Write(testPNG(20, 20))
	_ = mw.WriteField("lease_token", lease)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/ext/tasks/"+id+"/screenshots", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Origin", testExtensionOrigin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sb, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	validateResponse(t, req, res.StatusCode, res.Header, sb)
	if res.StatusCode != 201 {
		t.Fatalf("screenshot: %d %s", res.StatusCode, sb)
	}

	steps := []map[string]any{{"index": 0, "action": "assert_shop_id", "outcome": "ok", "duration_ms": 120},
		{"index": 1, "action": "click", "outcome": "ok", "duration_ms": 900}}
	e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/result", map[string]any{"lease_token": lease, "status": "SUCCEEDED", "steps": steps})
	if code := e.extMust(409, b.token, "POST", "/ext/tasks/"+id+"/result", map[string]any{"lease_token": lease, "status": "FAILED"}).code(t); code != "lease_lost" {
		t.Fatalf("result twice: %s", code)
	}
	if len(e.queue.Errors) > 0 {
		t.Fatalf("queue errors: %v", e.queue.Errors)
	}

	// The step completed by itself; the run finished.
	v = owner.runView(org, b.runID)
	_, status, out = v.step("apply_branding")
	if v.Data.Run.Status != "COMPLETED" || status != "SUCCEEDED" || !strings.Contains(string(out), "browser_extension") {
		t.Fatalf("after result: %s %s %s", v.Data.Run.Status, status, out)
	}
	var dt struct {
		Data []extTask `json:"data"`
	}
	owner.must(200, "GET", "/orgs/"+org+"/workflows/"+b.runID+"/browser-tasks", nil).json(t, &dt)
	if len(dt.Data) != 1 || dt.Data[0].Status != "SUCCEEDED" || len(dt.Data[0].StepLog) != 2 || len(dt.Data[0].ScreenshotIDs) != 1 {
		t.Fatalf("dashboard view: %+v", dt.Data)
	}
	shot := owner.must(200, "GET", "/orgs/"+org+"/browser-tasks/"+id+"/screenshots/"+dt.Data[0].ScreenshotIDs[0], nil)
	if !strings.Contains(string(shot.Body), "storage.test") {
		t.Fatalf("screenshot url: %s", shot.Body)
	}
	if len(e.extTasks(b.token)) != 0 {
		t.Fatal("finished task still listed")
	}
	var recipes struct {
		Data []struct {
			SuccessCount int `json:"success_count"`
		} `json:"data"`
	}
	b.admin.must(200, "GET", "/admin/browser-recipes", nil).json(t, &recipes)
	if recipes.Data[0].SuccessCount != 1 {
		t.Fatalf("recipe stats: %+v", recipes)
	}
}

// A step with a manual task besides the browser task waits for the user
// even after the extension succeeded.
func TestExtensionMixedTasks(t *testing.T) {
	b := newBannerEnv(t, 832, map[string]any{
		"branding":  map[string]any{},
		"warehouse": map[string]any{"instructions": "Use the HCM warehouse"},
	}, []string{"branding.banner_urls", "warehouse.instructions"})
	e, owner, org := b.e, b.owner, b.org
	tasks := e.extTasks(b.token)
	if len(tasks) != 1 {
		t.Fatalf("only the banner is a browser task: %+v", tasks)
	}
	var c claimResp
	e.extMust(200, b.token, "POST", "/ext/tasks/"+tasks[0].ID+"/claim", nil).json(t, &c)
	e.extMust(204, b.token, "POST", "/ext/tasks/"+tasks[0].ID+"/result", map[string]any{"lease_token": c.Data.LeaseToken, "status": "SUCCEEDED"})
	v := owner.runView(org, b.runID)
	stepID, status, _ := v.step("apply_branding")
	if v.Data.Run.Status != "WAITING_USER" || status != "WAITING_USER" {
		t.Fatalf("manual task still open: %s %s", v.Data.Run.Status, status)
	}
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+b.runID+"/steps/"+stepID+"/complete", map[string]any{"note": "Warehouse done"})
	if v = owner.runView(org, b.runID); v.Data.Run.Status != "COMPLETED" {
		t.Fatalf("final: %s", v.Data.Run.Status)
	}
}

// Failures disable the recipe after three in a row; claims then stop, and
// finishing the step by hand cancels the task.
func TestExtensionFailuresAndKillSwitch(t *testing.T) {
	b := newBannerEnv(t, 833, nil, []string{"branding.banner_urls"})
	e, owner, org := b.e, b.owner, b.org
	id := e.extTasks(b.token)[0].ID
	// A seller pressing Stop does not count against the recipe.
	var stop claimResp
	e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).json(t, &stop)
	e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/result", map[string]any{"lease_token": stop.Data.LeaseToken, "status": "FAILED",
		"error_code": "stopped_by_user"})
	owner.must(204, "POST", "/orgs/"+org+"/browser-tasks/"+id+"/requeue", nil)
	for i := 0; i < 3; i++ {
		var c claimResp
		e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).json(t, &c)
		e.extMust(204, b.token, "POST", "/ext/tasks/"+id+"/result", map[string]any{"lease_token": c.Data.LeaseToken, "status": "FAILED",
			"error_code": "target_not_found", "message": "Edit button not found",
			"steps": []map[string]any{{"index": 1, "action": "click", "outcome": "failed", "duration_ms": 15000, "error": "not found"}}})
		if i == 0 {
			// A failed task is not listed until someone queues it again.
			if len(e.extTasks(b.token)) != 0 {
				t.Fatal("failed task listed")
			}
			e.ext(b.token, "POST", "/ext/tasks/"+id+"/claim", nil)
		}
		owner.must(204, "POST", "/orgs/"+org+"/browser-tasks/"+id+"/requeue", nil)
	}
	var recipes struct {
		Data []struct {
			Enabled        bool    `json:"enabled"`
			DisabledReason *string `json:"disabled_reason"`
			FailureCount   int     `json:"failure_count"`
		} `json:"data"`
	}
	b.admin.must(200, "GET", "/admin/browser-recipes", nil).json(t, &recipes)
	if recipes.Data[0].Enabled || recipes.Data[0].FailureCount != 3 || recipes.Data[0].DisabledReason == nil || !strings.Contains(*recipes.Data[0].DisabledReason, "3 failures") {
		t.Fatalf("auto-disable: %+v", recipes.Data[0])
	}
	if code := e.extMust(409, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).code(t); code != "recipe_disabled" {
		t.Fatalf("kill switch: %s", code)
	}
	// The capability falls back to a manual task for new runs.
	var caps struct {
		Data []struct {
			Key    string `json:"key"`
			Mode   string `json:"mode"`
			Reason string `json:"reason"`
		} `json:"data"`
	}
	owner.must(200, "GET", "/orgs/"+org+"/shops/"+b.shop+"/capabilities", nil).json(t, &caps)
	for _, c := range caps.Data {
		if c.Key == "shop.banner.upload" && (c.Mode != "MANUAL" || c.Reason != "browser_recipe_unavailable") {
			t.Fatalf("capability: %+v", c)
		}
	}
	// The seller does it by hand; the queued task is cancelled.
	stepID, _, _ := owner.runView(org, b.runID).step("apply_branding")
	owner.must(202, "POST", "/orgs/"+org+"/workflows/"+b.runID+"/steps/"+stepID+"/complete", map[string]any{})
	if len(e.extTasks(b.token)) != 0 {
		t.Fatal("task of a finished step still listed")
	}
	var dt struct {
		Data []extTask `json:"data"`
	}
	owner.must(200, "GET", "/orgs/"+org+"/workflows/"+b.runID+"/browser-tasks", nil).json(t, &dt)
	if dt.Data[0].Status != "CANCELLED" {
		t.Fatalf("task: %+v", dt.Data[0])
	}
	if code := owner.must(409, "POST", "/orgs/"+org+"/browser-tasks/"+id+"/requeue", nil).code(t); code != "not_retryable" {
		t.Fatalf("requeue after the step: %s", code)
	}
}

func TestExtensionPairingAndAuth(t *testing.T) {
	e := newEnv(t)
	e.requireSystem()
	owner := e.signUp("owner@a.example", "Owner")
	org := owner.createOrg("Org A", "org-a")
	op := e.signUp("op@a.example", "Op")
	e.addMember(owner, org, op, "op@a.example", "operator")
	viewer := e.signUp("viewer@a.example", "Viewer")
	e.addMember(owner, org, viewer, "viewer@a.example", "viewer")

	viewer.must(403, "POST", "/orgs/"+org+"/extension/pairing-codes", nil)
	var code struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	op.must(201, "POST", "/orgs/"+org+"/extension/pairing-codes", nil).json(t, &code)
	if r := e.ext("", "POST", "/ext/pair", map[string]string{"code": "AAAAA-BBBBB"}); r.Status != 422 || !strings.Contains(string(r.Body), "invalid_code") {
		t.Fatalf("wrong code: %d %s", r.Status, r.Body)
	}
	var p pairedResp
	e.extMust(201, "", "POST", "/ext/pair", map[string]string{"code": code.Data.Code, "device_name": "Edge", "extension_version": "0.1.0"}).json(t, &p)
	if p.Data.Org.Name != "Org A" || len(p.Data.Token) < 40 {
		t.Fatalf("paired: %+v", p)
	}
	if r := e.ext("", "POST", "/ext/pair", map[string]string{"code": code.Data.Code}); r.Status != 422 {
		t.Fatalf("code reused: %d", r.Status)
	}
	// Expired codes.
	op.must(201, "POST", "/orgs/"+org+"/extension/pairing-codes", nil).json(t, &code)
	e.clock.Advance(7 * time.Minute)
	if r := e.ext("", "POST", "/ext/pair", map[string]string{"code": code.Data.Code}); r.Status != 422 {
		t.Fatalf("expired code: %d", r.Status)
	}

	tok := p.Data.Token
	e.extMust(200, tok, "GET", "/ext/me", nil)
	if code := e.extMust(401, "", "GET", "/ext/me", nil).code(t); code != "device_unauthorized" {
		t.Fatalf("no token: %s", code)
	}
	e.extMust(401, "not-a-real-token-but-long-enough-xxxxxxxx", "GET", "/ext/tasks", nil)

	// Rotation invalidates the old token.
	var rot pairedResp
	e.extMust(200, tok, "POST", "/ext/token/rotate", nil).json(t, &rot)
	e.extMust(401, tok, "GET", "/ext/me", nil)
	tok = rot.Data.Token
	e.extMust(200, tok, "GET", "/ext/me", nil)

	// Losing workflows.run (role change) stops the device.
	ms := owner.members(org)
	owner.must(200, "PATCH", "/orgs/"+org+"/members/"+findMember(ms, "op@a.example").ID, map[string]string{"role": "viewer"})
	e.extMust(401, tok, "GET", "/ext/me", nil)
	owner.must(200, "PATCH", "/orgs/"+org+"/members/"+findMember(ms, "op@a.example").ID, map[string]string{"role": "operator"})
	e.extMust(200, tok, "GET", "/ext/me", nil)

	// Devices list; the viewer cannot revoke someone else's device.
	var devs struct {
		Data []struct {
			ID        string `json:"id"`
			UserEmail string `json:"user_email"`
		} `json:"data"`
	}
	owner.must(200, "GET", "/orgs/"+org+"/extension/devices", nil).json(t, &devs)
	if len(devs.Data) != 1 || devs.Data[0].UserEmail != "op@a.example" {
		t.Fatalf("devices: %+v", devs)
	}
	viewer.must(403, "DELETE", "/orgs/"+org+"/extension/devices/"+devs.Data[0].ID, nil)
	owner.must(204, "DELETE", "/orgs/"+org+"/extension/devices/"+devs.Data[0].ID, nil)
	e.extMust(401, tok, "GET", "/ext/me", nil)

	// Self-unpair.
	tok2, _ := e.pair(op, org)
	e.extMust(204, tok2, "DELETE", "/ext/device", nil)
	e.extMust(401, tok2, "GET", "/ext/me", nil)

	// Sliding expiry: unused for 31 days, the token is dead.
	tok3, _ := e.pair(op, org)
	e.clock.Advance(31 * 24 * time.Hour)
	e.extMust(401, tok3, "GET", "/ext/me", nil)

	// CORS: preflight from the extension; other origins are refused.
	pre := e.ext("", "OPTIONS", "/ext/tasks", nil, "Access-Control-Request-Method", "GET")
	if pre.Status != 204 || pre.Header.Get("Access-Control-Allow-Origin") != testExtensionOrigin ||
		!strings.Contains(pre.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", pre.Status, pre.Header)
	}
	if r := e.ext(tok3, "GET", "/ext/me", nil, "Origin", "https://evil.example"); r.Status != 403 {
		t.Fatalf("foreign origin: %d", r.Status)
	}
	// The session API does not accept device tokens, and the extension API
	// does not accept session cookies.
	if r := e.ext(tok3, "GET", "/orgs/"+org+"/shops", nil); r.Status == 200 {
		t.Fatal("device token accepted by the session API")
	}
}

func TestBrowserRecipeAdmin(t *testing.T) {
	e := newEnv(t)
	admin, _, _ := e.setupApp()
	user := e.signUp("user@a.example", "User")
	user.must(404, "GET", "/admin/browser-recipes", nil)

	bad := strings.Replace(testBannerRecipe, `{"assert_shop_id": {"target": {"selector": "[data-shop-id]"}, "pattern": "Shop ID: (\\d+)"}},`, "", 1)
	if r := admin.must(422, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(bad)}); !strings.Contains(string(r.Body), "shop_check_missing") {
		t.Fatalf("missing shop check: %s", r.Body)
	}
	api := strings.Replace(testBannerRecipe, "shop.banner.upload", "shop.info.read", 1)
	if r := admin.must(422, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(api)}); !strings.Contains(string(r.Body), "not_browser") {
		t.Fatalf("api capability: %s", r.Body)
	}
	v2 := strings.Replace(testBannerRecipe, `"version": 1`, `"version": 2`, 1)
	admin.must(422, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(v2)})

	var r1 struct {
		Data struct {
			ID     string          `json:"id"`
			Recipe json.RawMessage `json:"recipe"`
		} `json:"data"`
	}
	admin.must(201, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(testBannerRecipe)}).json(t, &r1)
	if code := admin.must(409, "PATCH", "/admin/browser-recipes/"+r1.Data.ID, map[string]any{"enabled": true}).code(t); code != "not_verified" {
		t.Fatalf("enable unverified: %s", code)
	}
	admin.must(200, "PATCH", "/admin/browser-recipes/"+r1.Data.ID, map[string]any{"verified": true, "enabled": true})
	var r2 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	admin.must(201, "POST", "/admin/browser-recipes", map[string]any{"recipe": json.RawMessage(v2)}).json(t, &r2)
	admin.must(200, "PATCH", "/admin/browser-recipes/"+r2.Data.ID, map[string]any{"verified": true, "enabled": true})
	var list struct {
		Data []struct {
			Version        int     `json:"version"`
			Enabled        bool    `json:"enabled"`
			DisabledReason *string `json:"disabled_reason"`
		} `json:"data"`
	}
	admin.must(200, "GET", "/admin/browser-recipes", nil).json(t, &list)
	if len(list.Data) != 2 || !list.Data[0].Enabled || list.Data[1].Enabled || !strings.Contains(*list.Data[1].DisabledReason, "replaced by version 2") {
		t.Fatalf("one enabled version: %+v", list.Data)
	}
}

// Another organization's device sees nothing of org A and cannot act on
// its tasks.
func TestExtensionIsolation(t *testing.T) {
	b := newBannerEnv(t, 834, nil, []string{"branding.banner_urls"})
	e := b.e
	id := e.extTasks(b.token)[0].ID
	bob := e.signUp("bob@b.example", "Bob")
	orgB := bob.createOrg("Org B", "org-b")
	tokB, _ := e.pair(bob, orgB)
	if len(e.extTasks(tokB)) != 0 {
		t.Fatal("org B device lists org A tasks")
	}
	e.extMust(404, tokB, "POST", "/ext/tasks/"+id+"/claim", nil)
	var c claimResp
	e.extMust(200, b.token, "POST", "/ext/tasks/"+id+"/claim", nil).json(t, &c)
	e.extMust(404, tokB, "POST", "/ext/tasks/"+id+"/result", map[string]any{"lease_token": c.Data.LeaseToken, "status": "FAILED"})
	// Heartbeats answer lease_lost for any task the device does not hold,
	// existing or not, so nothing is revealed.
	e.extMust(409, tokB, "POST", "/ext/tasks/"+id+"/heartbeat", map[string]any{"lease_token": c.Data.LeaseToken, "state": "RUNNING"})
	e.extMust(409, tokB, "POST", "/ext/tasks/"+uuid.NewString()+"/heartbeat", map[string]any{"lease_token": c.Data.LeaseToken, "state": "RUNNING"})
	// Org B's dashboard cannot see org A's run tasks or revoke its device.
	bob.must(404, "GET", "/orgs/"+orgB+"/browser-tasks/"+id+"/screenshots/"+id, nil)
}
