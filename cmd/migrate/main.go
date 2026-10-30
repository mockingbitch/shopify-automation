// Command migrate applies database migrations as the schema owner.
//
//	migrate up        apply pending migrations (default)
//	migrate down-all  roll back everything (CI only)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/logx"
)

func main() {
	log := logx.New(os.Stdout, slog.LevelInfo).With(slog.String("service", "migrate"))
	url := os.Getenv("DATABASE_MIGRATE_URL")
	if url == "" {
		log.Error("DATABASE_MIGRATE_URL is required")
		os.Exit(2)
	}
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var err error
	switch cmd {
	case "up":
		err = db.Migrate(ctx, url)
	case "down-all":
		if os.Getenv("APP_ENV") == "production" {
			err = fmt.Errorf("down-all is disabled in production")
			break
		}
		err = db.MigrateDownAll(ctx, url)
	default:
		err = fmt.Errorf("unknown command %q (use up or down-all)", cmd)
	}
	if err != nil {
		log.Error("migration failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	log.Info("migration finished", slog.String("command", cmd))
}
