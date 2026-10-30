// Package maintenance holds housekeeping jobs run by the scheduler.
package maintenance

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
)

// PartitionedTables are range-partitioned by month.
var PartitionedTables = []string{"audit_logs", "workflow_events"}

// EnsurePartitions creates monthly partitions from this month up to
// monthsAhead months in the future. It runs on the app_system pool, the only
// role allowed to call app_ensure_monthly_partition. Idempotent.
func EnsurePartitions(ctx context.Context, system *db.DB, now time.Time, monthsAhead int) error {
	return system.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		for _, table := range PartitionedTables {
			for m := 0; m <= monthsAhead; m++ {
				if _, err := tx.Exec(ctx, `SELECT app_ensure_monthly_partition($1::regclass, $2::date)`, table, first.AddDate(0, m, 0)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
