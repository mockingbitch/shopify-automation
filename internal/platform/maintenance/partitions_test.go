package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/testdb"
)

func TestEnsurePartitions(t *testing.T) {
	d := testdb.New(t)
	sys := testdb.System(t, d)
	ctx := context.Background()
	future := time.Now().AddDate(2, 0, 0) // beyond the partitions made by the migration
	for range 2 {                         // idempotent
		if err := EnsurePartitions(ctx, sys, future, 1); err != nil {
			t.Fatal(err)
		}
	}
	name := "_" + future.Format("200601")
	for _, table := range PartitionedTables {
		var n int
		err := d.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relname = $1`, table+name).Scan(&n)
		})
		if err != nil || n != 1 {
			t.Fatalf("%s%s: %d %v", table, name, n, err)
		}
	}
	// The runtime role cannot create partitions.
	err := d.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT app_ensure_monthly_partition('audit_logs', now()::date)`)
		return err
	})
	if err == nil {
		t.Fatal("app_rw may create partitions")
	}
}
