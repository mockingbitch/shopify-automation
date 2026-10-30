// Package testdb creates an isolated, migrated PostgreSQL database for
// integration tests and connects to it as the runtime role app_rw, so tests
// exercise the same RLS rules as production.
//
// Configure with:
//
//	TEST_DATABASE_OWNER_URL  postgres://app_owner:...@host:port/postgres  (can CREATE DATABASE)
//	TEST_DATABASE_RW_URL     postgres://app_rw:...@host:port/postgres     (database name is replaced)
//	TEST_DATABASE_SYSTEM_URL postgres://app_system:...@host:port/postgres (optional; see System)
//
// Tests are skipped when the variables are unset, unless TEST_REQUIRE_DB=1.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
)

// New returns a handle to a fresh database. It is dropped when the test ends.
func New(t testing.TB) *db.DB {
	t.Helper()
	ownerURL := os.Getenv("TEST_DATABASE_OWNER_URL")
	rwURL := os.Getenv("TEST_DATABASE_RW_URL")
	if ownerURL == "" || rwURL == "" {
		if os.Getenv("TEST_REQUIRE_DB") == "1" {
			t.Fatal("TEST_DATABASE_OWNER_URL and TEST_DATABASE_RW_URL are required")
		}
		t.Skip("integration test: set TEST_DATABASE_OWNER_URL and TEST_DATABASE_RW_URL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "t_" + hex.EncodeToString(b)

	admin, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatalf("connect owner: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create database: %v", err)
	}
	_ = admin.Close(ctx)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, ownerURL)
		if err != nil {
			t.Logf("cleanup connect: %v", err)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
	})

	if err := db.Migrate(ctx, withDB(t, ownerURL, name)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	d, err := db.Open(ctx, withDB(t, rwURL, name))
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// OwnerURL returns a connection URL for the schema owner on the database
// behind d. Useful for assertions that must bypass RLS.
func OwnerURL(t testing.TB, d *db.DB) string {
	t.Helper()
	return withDB(t, os.Getenv("TEST_DATABASE_OWNER_URL"), d.Pool.Config().ConnConfig.Database)
}

func withDB(t testing.TB, raw, name string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// System opens the app_system (BYPASSRLS) role on the database behind d.
// It skips the test when TEST_DATABASE_SYSTEM_URL is unset, unless
// TEST_REQUIRE_DB=1.
func System(t testing.TB, d *db.DB) *db.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_SYSTEM_URL")
	if raw == "" {
		if os.Getenv("TEST_REQUIRE_DB") == "1" {
			t.Fatal("TEST_DATABASE_SYSTEM_URL is required")
		}
		t.Skip("integration test: set TEST_DATABASE_SYSTEM_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := db.Open(ctx, withDB(t, raw, d.Pool.Config().ConnConfig.Database))
	if err != nil {
		t.Fatalf("open system: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}
