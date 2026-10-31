package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/storage"
)

// PurgeOrganizations permanently removes organizations deleted longer ago
// than retention: their stored files (orgs/<id>/...) first, then the row,
// which cascades to every tenant table. Audit logs are kept on purpose
// (they have no foreign key and follow their own retention). It runs on the
// system pool and handles at most 20 organizations per call.
func PurgeOrganizations(ctx context.Context, system *db.DB, store storage.Store, now time.Time, retention time.Duration, log *slog.Logger) (int, error) {
	var ids []uuid.UUID
	err := system.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM organizations WHERE deleted_at IS NOT NULL AND deleted_at < $1 ORDER BY deleted_at LIMIT 20`,
			now.Add(-retention))
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, id := range ids {
		files := 0
		if store != nil {
			if files, err = store.DeletePrefix(ctx, "orgs/"+id.String()+"/"); err != nil {
				return purged, fmt.Errorf("purge %s: files: %w", id, err)
			}
		}
		err := system.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
			// Partitioned, without a foreign key: not reached by the cascade.
			if _, err := tx.Exec(ctx, `DELETE FROM workflow_events WHERE org_id = $1`, id); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1 AND deleted_at IS NOT NULL`, id)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO audit_logs (id, actor_type, action, target_type, target_id, outcome, metadata, created_at)
				VALUES ($1, 'system', 'organization.purged', 'organization', $2, 'success', jsonb_build_object('files', $3::int), $4)`,
				db.NewID(), id, files, now)
			return err
		})
		if err != nil {
			return purged, fmt.Errorf("purge %s: %w", id, err)
		}
		purged++
		log.InfoContext(ctx, "organization purged", slog.String("org_id", id.String()), slog.Int("files", files))
	}
	return purged, nil
}
