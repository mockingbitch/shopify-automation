package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shopee-automation/internal/platform/apperr"
)

func adapter() Adapter { return Adapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))} }

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type %q", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteErrorMapsKinds(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{apperr.NotFound("x"), 404, "not_found"},
		{apperr.Invalid(apperr.FieldError{Field: "a", Code: "required"}), 422, "validation_failed"},
		{apperr.Conflict("dup", "x"), 409, "dup"},
		{errors.New("db exploded: password=hunter2"), 500, "internal"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		adapter().WriteError(rec, httptest.NewRequest("GET", "/", nil), c.err)
		p := decodeProblem(t, rec)
		if rec.Code != c.status || p.Code != c.code {
			t.Errorf("%v: got %d %s", c.err, rec.Code, p.Code)
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Error("internal error details leaked to client")
		}
	}
}

func TestDecodeStrict(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name, ct, body string
		status         int
	}{
		{"ok", "application/json", `{"name":"a"}`, 0},
		{"charset ok", "application/json; charset=utf-8", `{"name":"a"}`, 0},
		{"wrong type header", "text/plain", `{"name":"a"}`, 415},
		{"unknown field", "application/json", `{"name":"a","org_id":"x"}`, 400},
		{"trailing", "application/json", `{"name":"a"}{"name":"b"}`, 400},
		{"empty", "application/json", ``, 400},
		{"bad json", "application/json", `{"name":`, 400},
		{"wrong field type", "application/json", `{"name":1}`, 422},
		{"too big", "application/json", `{"name":"` + strings.Repeat("a", MaxJSONBody) + `"}`, 413},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(c.body))
			r.Header.Set("Content-Type", c.ct)
			rec := httptest.NewRecorder()
			var v in
			err := Decode(rec, r, &v)
			if c.status == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			adapter().WriteError(rec, r, err)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d (%v)", rec.Code, c.status, err)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	h := SameOrigin(adapter(), "https://app.example.com")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	cases := []struct {
		method, origin, referer string
		want                    int
	}{
		{"POST", "https://app.example.com", "", 204},
		{"POST", "https://evil.example", "", 403},
		{"POST", "", "https://evil.example/page", 403},
		{"POST", "", "https://app.example.com/x?y", 204},
		{"POST", "", "", 204}, // non-browser clients; CSRF token still required for session routes
		{"GET", "https://evil.example", "", 204},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "/", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.referer != "" {
			r.Header.Set("Referer", c.referer)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%+v: got %d", c, rec.Code)
		}
	}
}

func TestRequestMetaRejectsMalformedID(t *testing.T) {
	var got string
	h := RequestMeta(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = w.Header().Get("X-Request-ID")
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "bad id\n<script>")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got == "" || strings.ContainsAny(got, " \n<") {
		t.Fatalf("request id not replaced: %q", got)
	}
}
