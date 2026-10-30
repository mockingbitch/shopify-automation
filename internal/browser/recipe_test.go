package browser

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestRecipe is a fixture recipe for tests. The origin and selectors are
// made up: no real Seller Centre recipe ships with the code.
const TestRecipe = `{
  "capability": "shop.banner.upload",
  "version": 1,
  "market": "VN",
  "start_url": "https://seller.example.test/decoration",
  "allowed_origins": ["https://seller.example.test"],
  "inputs": {"banner_images": {"type": "files", "required": true}},
  "steps": [
    {"wait_for": {"role": "heading", "name_any": ["Decoration"]}, "timeout_ms": 15000},
    {"assert_shop_id": {"target": {"selector": "[data-shop-id]"}, "pattern": "Shop ID: (\\d+)"}},
    {"click": {"role": "button", "name_any": ["Edit"]}},
    {"upload": {"target": {"selector": "input[type=file]"}, "files": "{{input.banner_images}}"}},
    {"click": {"role": "button", "name_any": ["Save"]}},
    {"expect": {"text_any": ["Saved"]}, "timeout_ms": 20000}
  ],
  "verify": [{"expect": {"target": {"selector": "[data-banner] img"}}}],
  "stop_on": [{"url_matches": "/login|/verify"}, {"text_any": ["Verify it's you"]}]
}`

func mutate(t *testing.T, f func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(TestRecipe), &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	b, _ := json.Marshal(m)
	return b
}

func steps(m map[string]any) []any { return m["steps"].([]any) }

func TestValidateRecipe(t *testing.T) {
	if r, errs := ValidateRecipe([]byte(TestRecipe)); errs != nil || r.Capability != "shop.banner.upload" || len(r.Steps) != 6 {
		t.Fatalf("valid recipe: %+v %v", r, errs)
	}
	cases := []struct {
		name, code string
		raw        []byte
	}{
		{"not json", "invalid_json", []byte("{")},
		{"unknown action", "invalid", mutate(t, func(m map[string]any) {
			m["steps"] = append(steps(m), map[string]any{"eval": map[string]any{"code": "alert(1)"}})
		})},
		{"two actions in one step", "invalid", mutate(t, func(m map[string]any) {
			steps(m)[0] = map[string]any{"click": map[string]any{"role": "button"}, "fill": map[string]any{"target": map[string]any{"selector": "x"}, "value": "y"}}
		})},
		{"write before shop check", "shop_check_missing", mutate(t, func(m map[string]any) {
			s := steps(m)
			s[1], s[2] = s[2], s[1]
		})},
		{"no shop check", "shop_check_missing", mutate(t, func(m map[string]any) {
			s := steps(m)
			m["steps"] = append(s[:1], s[2:]...)
		})},
		{"http origin", "invalid", mutate(t, func(m map[string]any) { m["allowed_origins"] = []string{"http://seller.example.test"} })},
		{"start url elsewhere", "origin_not_allowed", mutate(t, func(m map[string]any) { m["start_url"] = "https://evil.example/x" })},
		{"goto elsewhere", "origin_not_allowed", mutate(t, func(m map[string]any) {
			m["steps"] = append([]any{map[string]any{"goto": map[string]any{"url": "https://evil.example/"}}}, steps(m)...)
		})},
		{"template in goto host", "origin_not_allowed", mutate(t, func(m map[string]any) {
			m["inputs"].(map[string]any)["host"] = map[string]any{"type": "text"}
			m["steps"] = append([]any{map[string]any{"goto": map[string]any{"url": "https://{{input.host}}/x"}}}, steps(m)...)
		})},
		{"undeclared input", "undeclared_input", mutate(t, func(m map[string]any) {
			steps(m)[3] = map[string]any{"upload": map[string]any{"target": map[string]any{"selector": "input"}, "files": "{{input.logo}}"}}
		})},
		{"files input used as text", "input_type", mutate(t, func(m map[string]any) {
			steps(m)[3] = map[string]any{"fill": map[string]any{"target": map[string]any{"selector": "input"}, "value": "{{input.banner_images}}"}}
		})},
		{"unknown template", "bad_template", mutate(t, func(m map[string]any) {
			steps(m)[3] = map[string]any{"fill": map[string]any{"target": map[string]any{"selector": "input"}, "value": "{{document.cookie}}"}}
		})},
		{"pattern without group", "bad_pattern", mutate(t, func(m map[string]any) {
			steps(m)[1] = map[string]any{"assert_shop_id": map[string]any{"target": map[string]any{"selector": "x"}, "pattern": `Shop ID: \d+`}}
		})},
		{"template in a selector", "template_not_allowed", mutate(t, func(m map[string]any) {
			steps(m)[2] = map[string]any{"click": map[string]any{"selector": "#{{input.banner_images}}"}}
		})},
		{"template in start_url", "template_not_allowed", mutate(t, func(m map[string]any) {
			m["start_url"] = "https://seller.example.test/{{task.shopee_shop_id}}"
		})},
		{"write in verify", "write_in_verify", mutate(t, func(m map[string]any) {
			m["verify"] = []any{map[string]any{"click": map[string]any{"role": "button"}}}
		})},
		{"no stop_on", "invalid", mutate(t, func(m map[string]any) { m["stop_on"] = []any{} })},
		{"unknown market", "invalid", mutate(t, func(m map[string]any) { m["market"] = "US" })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := ValidateRecipe(c.raw)
			found := false
			for _, e := range errs {
				if e.Code == c.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s, got %+v", c.code, errs)
			}
		})
	}
}

func TestSigner(t *testing.T) {
	seed := make([]byte, 32)
	s, err := NewSigner("k1", base64.StdEncoding.EncodeToString(seed))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(TestRecipe)
	sig, _ := base64.StdEncoding.DecodeString(s.Sign(msg))
	pub, _ := base64.StdEncoding.DecodeString(s.PublicKey())
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("signature does not verify")
	}
	tampered := []byte(strings.Replace(TestRecipe, "Save", "Delete", 1))
	if ed25519.Verify(pub, tampered, sig) {
		t.Fatal("tampered recipe verified")
	}
	if _, err := NewSigner("k1", "short"); err == nil {
		t.Fatal("bad seed accepted")
	}
	if _, err := NewSigner("", base64.StdEncoding.EncodeToString(seed)); err == nil {
		t.Fatal("empty key id accepted")
	}
}

func TestPairingCode(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		c := newCode()
		if len(c) != 11 || c[5] != '-' || seen[c] {
			t.Fatalf("code %q", c)
		}
		seen[c] = true
		if normalizeCode(strings.ToLower(strings.ReplaceAll(c, "-", " "))) != strings.ReplaceAll(c, "-", "") {
			t.Fatalf("normalize %q", c)
		}
	}
}

func TestRecipeFault(t *testing.T) {
	code := func(s string) *string { return &s }
	for _, c := range []string{"stopped_by_user", "shop_mismatch", "tab_closed", "input_fetch_failed"} {
		if recipeFault(code(c)) {
			t.Errorf("%s should not count against the recipe", c)
		}
	}
	for _, c := range []string{"target_not_found", "expect_failed", "ambiguous_target"} {
		if !recipeFault(code(c)) {
			t.Errorf("%s should count against the recipe", c)
		}
	}
	if !recipeFault(nil) {
		t.Error("unknown failures count")
	}
}
