// Package config loads and validates process configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds settings shared by all processes.
type Config struct {
	Env           string
	HTTPAddr      string
	PublicBaseURL *url.URL

	DatabaseURL       string
	DatabaseSystemURL string

	RedisURL string

	SMTPAddr string
	SMTPFrom string

	OTLPEndpoint string
	LogLevel     slog.Level

	CookieSecure       bool
	SessionIdleTTL     time.Duration
	SessionAbsoluteTTL time.Duration

	// EncryptionKeys is "id:base64key[,id:base64key]" (32-byte keys) used to
	// wrap per-row data keys; EncryptionActiveKey selects the key for new data.
	EncryptionKeys      string
	EncryptionActiveKey string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string
	S3UseSSL    bool

	ShopeeHTTPTimeout time.Duration
	ShopeeRatePerApp  int
	ShopeeRatePerShop int

	// TrustProxyHeaders enables X-Forwarded-For for client IPs. Only set it
	// when the server is reachable exclusively through the reverse proxy.
	TrustProxyHeaders bool

	// RecipeSigningKey is the base64 32-byte Ed25519 seed that signs browser
	// recipes; RecipeSigningKeyID names it for the extension. Empty: the
	// extension cannot claim tasks.
	RecipeSigningKey   string
	RecipeSigningKeyID string
	// ExtensionOrigins are the browser extension origins allowed by CORS
	// (chrome-extension://<id>), comma separated.
	ExtensionOrigins []string
}

// IsProduction reports whether the process runs in production.
func (c Config) IsProduction() bool { return c.Env == "production" }

// Getenv looks up an environment variable. Tests pass a map-backed version.
type Getenv func(key string) string

// Load reads configuration. Every variable may instead be provided as a file
// path in KEY_FILE (Docker/Kubernetes secrets).
func Load(getenv Getenv) (Config, error) {
	l := loader{getenv: getenv}
	c := Config{
		Env:                l.str("APP_ENV", "development"),
		HTTPAddr:           l.str("HTTP_ADDR", ":8080"),
		DatabaseURL:        l.required("DATABASE_URL"),
		DatabaseSystemURL:  l.str("DATABASE_SYSTEM_URL", ""),
		RedisURL:           l.required("REDIS_URL"),
		SMTPAddr:           l.str("SMTP_ADDR", ""),
		SMTPFrom:           l.str("SMTP_FROM", "no-reply@localhost"),
		OTLPEndpoint:       l.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CookieSecure:       l.boolean("COOKIE_SECURE", true),
		SessionIdleTTL:     l.duration("SESSION_IDLE_TTL", 12*time.Hour),
		SessionAbsoluteTTL: l.duration("SESSION_ABSOLUTE_TTL", 7*24*time.Hour),
		TrustProxyHeaders:  l.boolean("TRUST_PROXY_HEADERS", false),

		EncryptionKeys:      l.required("ENCRYPTION_KEYS"),
		EncryptionActiveKey: l.str("ENCRYPTION_ACTIVE_KEY", ""),
		ShopeeHTTPTimeout:   l.duration("SHOPEE_HTTP_TIMEOUT", 15*time.Second),
		S3Endpoint:          l.required("S3_ENDPOINT"),
		S3AccessKey:         l.required("S3_ACCESS_KEY"),
		S3SecretKey:         l.required("S3_SECRET_KEY"),
		S3Bucket:            l.str("S3_BUCKET", "shopee-automation"),
		S3Region:            l.str("S3_REGION", "us-east-1"),
		S3UseSSL:            l.boolean("S3_USE_SSL", true),
		ShopeeRatePerApp:    l.integer("SHOPEE_RATE_PER_APP", 50),
		ShopeeRatePerShop:   l.integer("SHOPEE_RATE_PER_SHOP", 5),
	}

	switch c.Env {
	case "development", "test", "staging", "production":
	default:
		l.fail("APP_ENV", "must be one of development, test, staging, production")
	}

	if raw := l.str("PUBLIC_BASE_URL", "http://localhost:8080"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			l.fail("PUBLIC_BASE_URL", "must be an absolute http(s) URL")
		} else {
			c.PublicBaseURL = u
		}
	}

	switch strings.ToLower(l.str("LOG_LEVEL", "info")) {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		l.fail("LOG_LEVEL", "must be debug, info, warn or error")
	}

	c.RecipeSigningKey = l.str("RECIPE_SIGNING_KEY", "")
	c.RecipeSigningKeyID = l.str("RECIPE_SIGNING_KEY_ID", "k1")
	for _, o := range strings.Split(l.str("EXTENSION_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			if !strings.HasPrefix(o, "chrome-extension://") && c.IsProduction() {
				l.fail("EXTENSION_ORIGINS", "must list chrome-extension:// origins in production")
			}
			c.ExtensionOrigins = append(c.ExtensionOrigins, o)
		}
	}

	if c.SessionIdleTTL > c.SessionAbsoluteTTL {
		l.fail("SESSION_IDLE_TTL", "must not exceed SESSION_ABSOLUTE_TTL")
	}
	if c.IsProduction() {
		if !c.CookieSecure {
			l.fail("COOKIE_SECURE", "must be true in production")
		}
		if !c.S3UseSSL {
			l.fail("S3_USE_SSL", "must be true in production")
		}
		if c.PublicBaseURL != nil && c.PublicBaseURL.Scheme != "https" {
			l.fail("PUBLIC_BASE_URL", "must use https in production")
		}
	}

	if len(l.errs) > 0 {
		return Config{}, errors.Join(l.errs...)
	}
	return c, nil
}

type loader struct {
	getenv Getenv
	errs   []error
}

func (l *loader) fail(key, msg string) {
	l.errs = append(l.errs, fmt.Errorf("config %s: %s", key, msg))
}

func (l *loader) lookup(key string) string {
	if v := l.getenv(key); v != "" {
		return v
	}
	if path := l.getenv(key + "_FILE"); path != "" {
		b, err := os.ReadFile(path) // #nosec G304 -- path comes from operator configuration
		if err != nil {
			l.fail(key+"_FILE", "cannot read file")
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return ""
}

func (l *loader) str(key, def string) string {
	if v := l.lookup(key); v != "" {
		return v
	}
	return def
}

func (l *loader) required(key string) string {
	v := l.lookup(key)
	if v == "" {
		l.fail(key, "is required")
	}
	return v
}

func (l *loader) boolean(key string, def bool) bool {
	v := l.lookup(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, "must be a boolean")
		return def
	}
	return b
}

func (l *loader) integer(key string, def int) int {
	v := l.lookup(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		l.fail(key, "must be a non-negative integer")
		return def
	}
	return n
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.lookup(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		l.fail(key, "must be a positive duration like 12h")
		return def
	}
	return d
}

// ShopeeSettings returns the outbound Shopee client settings.
func (c Config) ShopeeSettings() (time.Duration, int, int) {
	return c.ShopeeHTTPTimeout, c.ShopeeRatePerApp, c.ShopeeRatePerShop
}
