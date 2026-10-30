package shopee_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shopee/shopeefake"
	"shopee-automation/internal/shopee/signer"
)

var partnerKey = []byte("test-partner-key-not-a-real-secret")

func setup(t *testing.T) (*shopeefake.Server, *shopee.Client, shopee.App, *clock.Fake) {
	t.Helper()
	fake := shopeefake.New(1001, partnerKey)
	t.Cleanup(fake.Close)
	clk := clock.NewFake(time.Now())
	fake.Now = clk.Now
	c := &shopee.Client{HTTP: &http.Client{Timeout: 2 * time.Second}, Clock: clk,
		Sleep: func(context.Context, time.Duration) error { return nil }}
	app := shopee.App{ID: uuid.New(), Host: fake.URL, PartnerID: 1001, PartnerKey: partnerKey}
	return fake, c, app, clk
}

func TestConnectRefreshAndRead(t *testing.T) {
	fake, c, app, _ := setup(t)
	fake.AddShop(shopeefake.Shop{ID: 42, Name: "Test Shop", Region: "VN", Logo: "https://cf.shopee.vn/file/x", Description: "hello"})
	ctx := context.Background()

	code := fake.Authorize(42)
	tok, err := c.GetAccessToken(ctx, app, code, 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetAccessToken(ctx, app, code, 42, 0); !shopee.IsKind(err, shopee.KindAuth) {
		t.Fatalf("reused code: %v", err)
	}

	info, err := c.GetShopInfo(ctx, app, tok.AccessToken, 42)
	if err != nil || info.ShopName != "Test Shop" || info.Region != "VN" || info.Status != "NORMAL" || len(info.Raw) == 0 {
		t.Fatalf("shop info: %+v %v", info, err)
	}
	p, err := c.GetProfile(ctx, app, tok.AccessToken, 42)
	if err != nil || p.Description != "hello" || p.ShopLogo == "" {
		t.Fatalf("profile: %+v %v", p, err)
	}

	nt, err := c.RefreshAccessToken(ctx, app, tok.RefreshToken, 42)
	if err != nil || nt.AccessToken == tok.AccessToken {
		t.Fatalf("refresh: %+v %v", nt, err)
	}
	_, err = c.RefreshAccessToken(ctx, app, tok.RefreshToken, 42)
	if !shopee.IsKind(err, shopee.KindAuthorizationLost) {
		t.Fatalf("reused refresh token: %v", err)
	}
}

func TestMainAccountTokensRefreshPerShop(t *testing.T) {
	fake, c, app, _ := setup(t)
	fake.AddShop(shopeefake.Shop{ID: 1, Name: "A", Region: "VN"})
	fake.AddShop(shopeefake.Shop{ID: 2, Name: "B", Region: "TH"})
	ctx := context.Background()
	tok, err := c.GetAccessToken(ctx, app, fake.AuthorizeMain(77, 1, 2), 0, 77)
	if err != nil || len(tok.ShopIDList) != 2 {
		t.Fatalf("main account: %+v %v", tok, err)
	}
	for _, id := range []int64{1, 2} {
		if _, err := c.GetShopInfo(ctx, app, tok.AccessToken, id); err != nil {
			t.Fatalf("shared token for shop %d: %v", id, err)
		}
		if _, err := c.RefreshAccessToken(ctx, app, tok.RefreshToken, id); err != nil {
			t.Fatalf("refresh shop %d: %v", id, err)
		}
	}
}

func TestErrorsAreNormalizedAndCarryNoSecrets(t *testing.T) {
	fake, c, app, _ := setup(t)
	fake.AddShop(shopeefake.Shop{ID: 42, Name: "S", Region: "VN"})
	ctx := context.Background()

	_, err := c.GetShopInfo(ctx, app, "wrong-token-value", 42)
	e, ok := shopee.AsError(err)
	if !ok || e.Kind != shopee.KindAuth || e.Code != "invalid_acceess_token" || e.RequestID == "" {
		t.Fatalf("error: %#v", err)
	}
	if strings.Contains(err.Error(), "wrong-token-value") || strings.Contains(err.Error(), "sign") {
		t.Fatalf("error leaks secrets: %v", err)
	}

	bad := app
	bad.PartnerKey = []byte("wrong")
	if _, err := c.GetAccessToken(ctx, bad, "x", 42, 0); !shopee.IsKind(err, shopee.KindAppConfig) {
		t.Fatalf("wrong key: %v", err)
	}
}

func TestReadsRetryAndResign(t *testing.T) {
	fake, c, app, clk := setup(t)
	fake.AddShop(shopeefake.Shop{ID: 42, Name: "S", Region: "VN"})
	ctx := context.Background()
	tok, _ := c.GetAccessToken(ctx, app, fake.Authorize(42), 42, 0)

	// The fake rejects stale timestamps, so a retry that reused the first
	// signature would fail after the clock moved past 5 minutes.
	c.Sleep = func(context.Context, time.Duration) error { clk.Advance(6 * time.Minute); return nil }
	fake.FailNext(shopee.PathGetShopInfo, 500, "error_server", "Something wrong. Please try later.")
	if _, err := c.GetShopInfo(ctx, app, tok.AccessToken, 42); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := fake.Calls(shopee.PathGetShopInfo); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

func TestWritesAreNotRetriedAfterAmbiguousFailure(t *testing.T) {
	fake, c, app, _ := setup(t)
	fake.AddShop(shopeefake.Shop{ID: 42, Name: "S", Region: "VN"})
	ctx := context.Background()
	tok, _ := c.GetAccessToken(ctx, app, fake.Authorize(42), 42, 0)

	fake.FailNext(shopee.PathRefreshAccessToken, 500, "error_server", "error server")
	_, err := c.RefreshAccessToken(ctx, app, tok.RefreshToken, 42)
	if !shopee.IsKind(err, shopee.KindAmbiguous) || fake.Calls(shopee.PathRefreshAccessToken) != 1 {
		t.Fatalf("5xx on write: %v, calls=%d", err, fake.Calls(shopee.PathRefreshAccessToken))
	}

	c.HTTP.Timeout = 200 * time.Millisecond
	fake.HangNext(shopee.PathRefreshAccessToken)
	_, err = c.RefreshAccessToken(ctx, app, tok.RefreshToken, 42)
	if !shopee.IsKind(err, shopee.KindAmbiguous) || fake.Calls(shopee.PathRefreshAccessToken) != 2 {
		t.Fatalf("timeout on write: %v, calls=%d", err, fake.Calls(shopee.PathRefreshAccessToken))
	}

	// Throttling rejections are safe to retry, even for writes.
	c.HTTP.Timeout = 2 * time.Second
	fake.FailNext(shopee.PathRefreshAccessToken, 429, "error_rate_limit", "Too many requests.")
	if _, err := c.RefreshAccessToken(ctx, app, tok.RefreshToken, 42); err != nil {
		t.Fatalf("after throttle: %v", err)
	}
}

func TestConnectionRefusedIsRetriedForWrites(t *testing.T) {
	_, c, app, _ := setup(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	app.Host = srv.URL
	srv.Close() // nothing listens any more
	c.MaxAttempts = 3
	_, err := c.RefreshAccessToken(context.Background(), app, "rt", 1)
	e, ok := shopee.AsError(err)
	if !ok || e.Kind != shopee.KindTransient || e.Code != "network" {
		t.Fatalf("refused: %#v", err)
	}
	if strings.Contains(err.Error(), "refresh_token") || strings.Contains(err.Error(), "sign=") {
		t.Fatalf("leaks url: %v", err)
	}
}

func TestRateLimitWaits(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"error":"","message":"","request_id":"r"}`)
	}))
	defer srv.Close()
	var waited time.Duration
	c := &shopee.Client{HTTP: srv.Client(), Clock: clock.Real{}, Limiter: &ratelimit.Memory{},
		Limits: shopee.Limits{PerApp: 2},
		Sleep: func(_ context.Context, d time.Duration) error {
			waited += d
			time.Sleep(d)
			return nil
		}}
	app := shopee.App{ID: uuid.New(), Host: srv.URL, PartnerID: 1, PartnerKey: []byte("k")}
	for range 3 {
		if _, err := c.Do(context.Background(), app, shopee.Request{Method: "GET", Path: "/x", Kind: signer.Public, Idempotent: true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 3 || waited == 0 {
		t.Fatalf("hits=%d waited=%v", hits.Load(), waited)
	}
}

func TestAuthURLAndPush(t *testing.T) {
	u := shopee.AuthURL("https://open.sandbox.test-stable.shopee.com/", 1001, "https://app.example/api/v1/oauth/shopee/callback", "st.ate")
	want := "https://open.sandbox.test-stable.shopee.com/auth?auth_type=seller&partner_id=1001&redirect_uri=https%3A%2F%2Fapp.example%2Fapi%2Fv1%2Foauth%2Fshopee%2Fcallback&response_type=code&state=st.ate"
	if u != want {
		t.Fatalf("auth url\n got %s\nwant %s", u, want)
	}

	body := []byte(`{"shop_id": 123, "code": 1, "success": 1, "timestamp": 1470198856}`)
	cb := "https://app.example/api/v1/webhooks/shopee/abc"
	sig := signer.HMAC(partnerKey, cb+"|"+string(body))
	if !shopee.VerifyPush(partnerKey, cb, body, sig) {
		t.Fatal("valid push rejected")
	}
	for name, ok := range map[string]bool{
		"other url":  shopee.VerifyPush(partnerKey, cb+"x", body, sig),
		"other body": shopee.VerifyPush(partnerKey, cb, append(body, ' '), sig),
		"other key":  shopee.VerifyPush([]byte("k"), cb, body, sig),
		"empty":      shopee.VerifyPush(partnerKey, cb, body, ""),
	} {
		if ok {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestAccessTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := map[int64]time.Duration{
		14400:                   4 * time.Hour,
		13859:                   13859 * time.Second,
		0:                       4 * time.Hour,
		1_700_000_000 + 3600:    time.Hour, // Unix timestamp form, as in one sample
		1_700_000_000 + 999_999: 4 * time.Hour,
	}
	for in, want := range cases {
		if got := (shopee.Tokens{ExpireIn: in}).AccessTTL(now); got != want {
			t.Errorf("AccessTTL(%d) = %v, want %v", in, got, want)
		}
	}
}
