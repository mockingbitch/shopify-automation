package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelDebug)
	log.Info("call",
		slog.String("access_token", "AT-secret"),
		slog.String("Refresh_Token", "RT-secret"),
		slog.String("device_token", "DT-secret"),
		slog.Group("req", slog.String("authorization", "Bearer x"), slog.String("path", "/ok")),
		slog.String("shop_id", "123"),
	)
	out := buf.String()
	for _, secret := range []string{"AT-secret", "RT-secret", "DT-secret", "Bearer x"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"shop_id":"123"`) || !strings.Contains(out, `"path":"/ok"`) {
		t.Errorf("non-sensitive values missing: %s", out)
	}
}

func TestRedactMapNested(t *testing.T) {
	in := map[string]any{
		"email":    "a@b.c",
		"password": "pw",
		"nested":   map[string]any{"partner_key": "pk", "ok": 1},
		"list":     []any{map[string]any{"refresh_token": "rt"}, "plain"},
	}
	out := RedactMap(in)
	if out["password"] != Redacted || out["email"] != "a@b.c" {
		t.Fatalf("top level: %v", out)
	}
	if out["nested"].(map[string]any)["partner_key"] != Redacted || out["nested"].(map[string]any)["ok"] != 1 {
		t.Fatalf("nested: %v", out["nested"])
	}
	if out["list"].([]any)[0].(map[string]any)["refresh_token"] != Redacted {
		t.Fatalf("list: %v", out["list"])
	}
	if in["password"] != "pw" {
		t.Fatal("input must not be mutated")
	}
}
