package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"shopee-automation/db/migrations"
)

// Migrate applies all pending migrations. ownerURL must connect as the schema
// owner (app_owner), never as a runtime role.
func Migrate(ctx context.Context, ownerURL string) error {
	return runGoose(ctx, ownerURL, func(p *goose.Provider) error {
		_, err := p.Up(ctx)
		return err
	})
}

// MigrateDownAll rolls back every migration. Used by CI to verify down scripts.
func MigrateDownAll(ctx context.Context, ownerURL string) error {
	return runGoose(ctx, ownerURL, func(p *goose.Provider) error {
		_, err := p.DownTo(ctx, 0)
		return err
	})
}

func runGoose(ctx context.Context, url string, fn func(*goose.Provider) error) error {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, conn, migrations.FS)
	if err != nil {
		return fmt.Errorf("init goose: %w", err)
	}
	if err := fn(p); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
