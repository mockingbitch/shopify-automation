package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		"DATABASE_URL":    "postgres://x",
		"REDIS_URL":       "redis://x",
		"ENCRYPTION_KEYS": "k1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"S3_ENDPOINT":     "localhost:8333",
		"S3_ACCESS_KEY":   "a",
		"S3_SECRET_KEY":   "b",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "development" || c.HTTPAddr != ":8080" || !c.CookieSecure {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.SessionIdleTTL != 12*time.Hour || c.SessionAbsoluteTTL != 7*24*time.Hour {
		t.Fatalf("unexpected session TTLs: %v %v", c.SessionIdleTTL, c.SessionAbsoluteTTL)
	}
}

func TestLoadRequiredAndInvalid(t *testing.T) {
	m := map[string]string{
		"APP_ENV":          "prod",
		"LOG_LEVEL":        "loud",
		"COOKIE_SECURE":    "maybe",
		"PUBLIC_BASE_URL":  "not a url",
		"SESSION_IDLE_TTL": "-1h",
	}
	_, err := Load(env(m))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"DATABASE_URL", "REDIS_URL", "APP_ENV", "LOG_LEVEL", "COOKIE_SECURE", "PUBLIC_BASE_URL", "SESSION_IDLE_TTL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestLoadProductionRules(t *testing.T) {
	m := base()
	m["APP_ENV"] = "production"
	m["COOKIE_SECURE"] = "false"
	m["PUBLIC_BASE_URL"] = "http://example.com"
	_, err := Load(env(m))
	if err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE") || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected production errors, got %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "db")
	if err := os.WriteFile(p, []byte("postgres://from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := base()
	delete(m, "DATABASE_URL")
	m["DATABASE_URL_FILE"] = p
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://from-file" {
		t.Fatalf("got %q", c.DatabaseURL)
	}
}
