// Package shopee is the anti-corruption layer for the Shopee Open Platform
// v2 API. It signs requests, applies rate limits and retries, and turns
// Shopee responses into typed results or normalized *Error values. Nothing
// outside this package, shops and the step executors sees Shopee payloads.
//
// Secrets (partner key, access tokens, signs) travel only in the outgoing
// request. They are never logged, never put into errors and never attached
// to traces: errors and spans carry the API path, not the URL.
package shopee

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/shopee/signer"
)

const maxResponseBytes = 10 << 20

// App is a Shopee partner app as the client needs it.
type App struct {
	ID         uuid.UUID
	Host       string // API host, e.g. https://partner.shopeemobile.com
	PartnerID  int64
	PartnerKey []byte
}

// Request describes one API call.
type Request struct {
	Method      string // GET or POST
	Path        string // e.g. /api/v2/shop/get_shop_info
	Kind        signer.Kind
	AccessToken string
	TargetID    int64 // shop_id (Shop) or merchant_id (Merchant)
	Query       url.Values
	Body        any // JSON body for POST
	// Idempotent marks calls that are safe to repeat after an unclear
	// failure (reads). Writes and single-use token exchanges are not.
	Idempotent bool
}

// Limits are outbound request budgets per second. Zero disables a limit.
type Limits struct {
	PerApp  int
	PerShop int
}

// Client calls the Shopee API.
type Client struct {
	HTTP        *http.Client
	Clock       clock.Clock
	Limiter     ratelimit.Limiter // optional
	Limits      Limits
	MaxAttempts int                                              // default 4
	Sleep       func(ctx context.Context, d time.Duration) error // tests replace it
}

// envelope holds the fields every Shopee response carries.
type envelope struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Warning   any    `json:"warning"`
}

// Meta describes a successful call.
type Meta struct {
	RequestID string
	Warning   string
}

var tracer = otel.Tracer("shopee-automation/shopee")

// Do performs req and decodes the full JSON response into out (may be nil).
func (c *Client) Do(ctx context.Context, app App, req Request, out any) (Meta, error) {
	ctx, span := tracer.Start(ctx, "shopee "+req.Path)
	defer span.End()
	span.SetAttributes(attribute.String("shopee.path", req.Path), attribute.Int64("shopee.partner_id", app.PartnerID))

	var body []byte
	if req.Body != nil {
		b, err := json.Marshal(req.Body)
		if err != nil {
			return Meta{}, fmt.Errorf("shopee: encode body: %w", err)
		}
		body = b
	}

	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 4
	}
	var lastErr *Error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := c.sleep(ctx, backoff(attempt)); err != nil {
				break
			}
		}
		if err := c.wait(ctx, app, req); err != nil {
			return Meta{}, err
		}
		meta, err := c.once(ctx, app, req, body, out)
		if err == nil {
			span.SetAttributes(attribute.String("shopee.request_id", meta.RequestID))
			return meta, nil
		}
		lastErr = err
		span.SetAttributes(attribute.Int("shopee.attempts", attempt))
		if !retryable(err, req.Idempotent) {
			break
		}
	}
	span.SetStatus(codes.Error, lastErr.Code)
	span.SetAttributes(attribute.String("shopee.error", lastErr.Code), attribute.String("shopee.request_id", lastErr.RequestID))
	return Meta{}, lastErr
}

// sentErr marks transport failures that happened after the request may have
// reached Shopee.
type sentErr struct{ error }

func (c *Client) once(ctx context.Context, app App, req Request, body []byte, out any) (Meta, *Error) {
	fail := func(kind Kind, code string, status int, cause error) *Error {
		return &Error{Kind: kind, Code: code, HTTPStatus: status, Path: req.Path, cause: cause}
	}

	// Sign on every attempt: the timestamp is only valid for 5 minutes.
	ts := c.Clock.Now().Unix()
	q := url.Values{}
	for k, v := range req.Query {
		q[k] = v
	}
	q.Set("partner_id", strconv.FormatInt(app.PartnerID, 10))
	q.Set("timestamp", strconv.FormatInt(ts, 10))
	switch req.Kind {
	case signer.Shop:
		q.Set("access_token", req.AccessToken)
		q.Set("shop_id", strconv.FormatInt(req.TargetID, 10))
	case signer.Merchant:
		q.Set("access_token", req.AccessToken)
		q.Set("merchant_id", strconv.FormatInt(req.TargetID, 10))
	}
	q.Set("sign", signer.Sign(app.PartnerKey, app.PartnerID, req.Path, ts, req.Kind, req.AccessToken, req.TargetID))

	full := strings.TrimRight(app.Host, "/") + req.Path + "?" + q.Encode()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.Method, full, rd)
	if err != nil {
		return Meta{}, fail(KindUnknown, "bad_request", 0, errors.New("cannot build request"))
	}
	if body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	hr.Header.Set("Accept", "application/json")

	res, err := c.HTTP.Do(hr)
	if err != nil {
		cause := scrub(err)
		if beforeSend(err) {
			return Meta{}, fail(KindTransient, "network", 0, cause)
		}
		return Meta{}, fail(KindTransient, "network", 0, sentErr{cause})
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return Meta{}, fail(KindTransient, "network", res.StatusCode, sentErr{scrub(err)})
	}
	if len(raw) > maxResponseBytes {
		return Meta{}, fail(KindUnknown, "response_too_large", res.StatusCode, nil)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		kind := classify("", res.StatusCode)
		if kind == KindUnknown {
			kind = KindTransient
		}
		return Meta{}, fail(kind, "invalid_response", res.StatusCode, sentErr{errors.New("response is not JSON")})
	}
	if env.Error != "" || res.StatusCode >= 400 {
		code := env.Error
		if code == "" {
			code = "http_" + strconv.Itoa(res.StatusCode)
		}
		e := fail(classify(code, res.StatusCode), code, res.StatusCode, nil)
		e.Message = truncate(env.Message, 500)
		e.RequestID = env.RequestID
		return Meta{}, e
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e := fail(KindUnknown, "invalid_response", res.StatusCode, errors.New("unexpected response shape"))
			e.RequestID = env.RequestID
			return Meta{}, e
		}
	}
	return Meta{RequestID: env.RequestID, Warning: warningString(env.Warning)}, nil
}

// retryable decides whether another attempt is safe.
func retryable(e *Error, idempotent bool) bool {
	var s sentErr
	sent := errors.As(e.cause, &s)
	switch {
	case e.Kind == KindRateLimited && e.Code != "error_limit":
		return true // rejected before processing; the daily quota will not reset soon
	case e.Code == "network" && !sent:
		return true // never reached Shopee
	case idempotent && e.Kind == KindTransient:
		return true
	case !idempotent && e.Kind == KindTransient:
		// Shopee may have applied the write. Make it explicit for the caller.
		if sent || e.HTTPStatus >= 500 || e.Code == "error_network" || e.Code == "error_server" ||
			e.Code == "error_system_busy" || e.Code == "error_inner" {
			e.Kind = KindAmbiguous
		}
	}
	return false
}

// wait blocks until the per-app and per-shop budgets allow one more call.
// Limiter failures fail open: availability matters more than a local budget.
func (c *Client) wait(ctx context.Context, app App, req Request) error {
	if c.Limiter == nil {
		return nil
	}
	keys := []struct {
		key   string
		limit int
	}{{"shopee:app:" + app.ID.String(), c.Limits.PerApp}}
	if req.TargetID != 0 {
		keys = append(keys, struct {
			key   string
			limit int
		}{"shopee:shop:" + app.ID.String() + ":" + strconv.FormatInt(req.TargetID, 10), c.Limits.PerShop})
	}
	for _, k := range keys {
		if k.limit <= 0 {
			continue
		}
		for {
			ok, retryIn, err := c.Limiter.Allow(ctx, k.key, k.limit, time.Second)
			if err != nil || ok {
				break
			}
			if err := c.sleep(ctx, retryIn); err != nil {
				return &Error{Kind: KindTransient, Code: "cancelled", Path: req.Path, cause: err}
			}
		}
	}
	return nil
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// backoff is exponential with full jitter: 0..min(5s, 250ms*2^(n-2)).
func backoff(attempt int) time.Duration {
	ceiling := 250 * time.Millisecond << (attempt - 2)
	if ceiling > 5*time.Second || ceiling <= 0 {
		ceiling = 5 * time.Second
	}
	return time.Duration(rand.Int64N(int64(ceiling) + 1)) // #nosec G404 -- jitter, not security
}

// beforeSend reports transport errors that happen before any byte of the
// request can reach the server (DNS, connection refused, dial timeout).
func beforeSend(err error) bool {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// scrub drops the URL (which holds the access token and sign) from
// net/http errors and keeps only the underlying reason.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	msg := err.Error()
	if strings.Contains(msg, "access_token") || strings.Contains(msg, "sign=") {
		return errors.New("transport error")
	}
	return err
}

func warningString(w any) string {
	switch v := w.(type) {
	case nil:
		return ""
	case string:
		return truncate(v, 500)
	default:
		b, _ := json.Marshal(v)
		return truncate(string(b), 500)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Config is the part of the process configuration the client needs.
type Config interface {
	ShopeeSettings() (timeout time.Duration, perApp, perShop int)
}

// NewClient builds a production client.
func NewClient(cfg Config, limiter ratelimit.Limiter) *Client {
	timeout, perApp, perShop := cfg.ShopeeSettings()
	return &Client{
		HTTP:    &http.Client{Timeout: timeout},
		Clock:   clock.Real{},
		Limiter: limiter,
		Limits:  Limits{PerApp: perApp, PerShop: perShop},
	}
}
