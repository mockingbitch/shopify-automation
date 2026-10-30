package capability

import (
	"testing"

	"github.com/google/uuid"
)

func ptr[T any](v T) *T { return &v }

func TestResolve(t *testing.T) {
	app := uuid.New()
	other := uuid.New()
	api := Definition{Key: "shop.info.read", DefaultMode: ModeAPI, Enabled: true, Verification: DocVerified}
	browser := Definition{Key: "shop.banner.upload", DefaultMode: ModeBrowser, FallbackMode: ModeManual, Enabled: true, RequiresApproval: true}
	browserNoFallback := Definition{Key: "x.browser", DefaultMode: ModeBrowser, Enabled: true}
	regional := Definition{Key: "r", DefaultMode: ModeAPI, Enabled: true, SupportedRegions: []string{"VN"}}
	typed := Definition{Key: "t", DefaultMode: ModeAPI, Enabled: true, RequiredAppTypes: []string{"ERP System"}}
	verified := Definition{Key: "v", DefaultMode: ModeAPI, Enabled: true, Verification: SandboxVerified}
	unsupported := Definition{Key: "u", DefaultMode: ModeUnsupported, Enabled: true}
	off := Definition{Key: "off", DefaultMode: ModeAPI, Enabled: false}

	sandbox := Context{AppID: app, AppType: "ERP System", AppEnvironment: "sandbox", Region: "VN", ConnectionStatus: "ACTIVE"}
	with := func(f func(*Context)) Context { c := sandbox; f(&c); return c }

	cases := []struct {
		name    string
		def     Definition
		ovs     []Override
		ctx     Context
		mode    Mode
		enabled bool
		reason  string
	}{
		{"api in sandbox", api, nil, sandbox, ModeAPI, true, ReasonOK},
		{"api in production needs sandbox verification", api, nil, with(func(c *Context) { c.AppEnvironment = "production" }), ModeAPI, false, ReasonNotSandboxVerified},
		{"verified api in production", verified, nil, with(func(c *Context) { c.AppEnvironment = "production" }), ModeAPI, true, ReasonOK},
		{"shop not active", api, nil, with(func(c *Context) { c.ConnectionStatus = "RECONNECT_REQUIRED" }), ModeAPI, false, ReasonShopNotActive},
		{"browser without recipe falls back to manual", browser, nil, with(func(c *Context) { c.ExtensionAvailable = true }), ModeManual, true, ReasonNoRecipe},
		{"browser recipe but no extension", browser, nil, with(func(c *Context) { c.BrowserReady = map[string]bool{"shop.banner.upload": true} }), ModeManual, true, ReasonExtensionUnavailable},
		{"browser with extension and recipe", browser, nil, with(func(c *Context) {
			c.ExtensionAvailable, c.BrowserReady = true, map[string]bool{"shop.banner.upload": true}
		}), ModeBrowser, true, ReasonOK},
		{"browser without fallback", browserNoFallback, nil, with(func(c *Context) { c.BrowserReady = map[string]bool{"x.browser": true} }), ModeBrowser, false, ReasonExtensionUnavailable},
		{"region allowed", regional, nil, sandbox, ModeAPI, true, ReasonOK},
		{"region not allowed", regional, nil, with(func(c *Context) { c.Region = "TH" }), ModeAPI, false, ReasonRegion},
		{"app type allowed", typed, nil, sandbox, ModeAPI, true, ReasonOK},
		{"app type not allowed", typed, nil, with(func(c *Context) { c.AppType = "Customer Service" }), ModeAPI, false, ReasonAppType},
		{"unsupported", unsupported, nil, sandbox, ModeUnsupported, false, ReasonUnsupported},
		{"disabled", off, nil, sandbox, ModeAPI, false, ReasonDisabled},
		{"global kill switch", api, []Override{{CapabilityKey: "shop.info.read", Enabled: ptr(false), Reason: "incident"}}, sandbox, ModeAPI, false, "incident"},
		{"override for other app ignored", api, []Override{{CapabilityKey: "shop.info.read", AppID: &other, Enabled: ptr(false), Reason: "x"}}, sandbox, ModeAPI, true, ReasonOK},
		{"override for other key ignored", api, []Override{{CapabilityKey: "nope", Enabled: ptr(false), Reason: "x"}}, sandbox, ModeAPI, true, ReasonOK},
		{"region override beats global", api, []Override{
			{CapabilityKey: "shop.info.read", Region: ptr("VN"), Enabled: ptr(true), Reason: "vn ok"},
			{CapabilityKey: "shop.info.read", Enabled: ptr(false), Reason: "global off"},
		}, sandbox, ModeAPI, true, ReasonOK},
		{"mode override to manual", api, []Override{{CapabilityKey: "shop.info.read", AppID: &app, Mode: ptr(ModeManual), Reason: "api broken"}}, sandbox, ModeManual, true, ReasonOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Resolve([]Definition{c.def}, c.ovs, c.ctx)
			if len(got) != 1 {
				t.Fatalf("got %d results", len(got))
			}
			r := got[0]
			if r.Mode != c.mode || r.Enabled != c.enabled || r.Reason != c.reason {
				t.Fatalf("got mode=%s enabled=%v reason=%q, want %s %v %q", r.Mode, r.Enabled, r.Reason, c.mode, c.enabled, c.reason)
			}
		})
	}
}
