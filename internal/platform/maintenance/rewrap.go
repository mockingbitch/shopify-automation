package maintenance

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
)

// sealedTables lists every table with an envelope-wrapped data key: the
// AAD table name, the row-ID column, and the key columns.
var sealedTables = []struct {
	table, idCol, kekCol, dekCol string
}{
	{"shopee_apps", "id", "kek_id", "dek_wrapped"},
	{"shop_credentials", "shop_connection_id", "kek_id", "dek_wrapped"},
	{"oauth_states", "id", "pending_kek_id", "pending_dek_wrapped"},
	{"email_outbox", "id", "kek_id", "dek_wrapped"},
	{"user_mfa", "user_id", "kek_id", "dek_wrapped"},
}

// RewrapKeys re-seals every data key that is not under the active KEK
// (rotation step 2; see docs/deployment.md). Encrypted columns are not
// touched. It runs on the system pool and returns the rows changed per
// table. Rows whose KEK is missing from the keyring are reported as errors.
func RewrapKeys(ctx context.Context, system *db.DB, keys *envelope.Keyring) (map[string]int, error) {
	out := map[string]int{}
	active := keys.ActiveID()
	for _, t := range sealedTables {
		for {
			type row struct {
				id      string
				kek     string
				wrapped []byte
			}
			var batch []row
			err := system.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
				// #nosec G201 -- identifiers come from the constant table above
				q := fmt.Sprintf(`SELECT %s::text, %s, %s FROM %s WHERE %s IS NOT NULL AND %s <> $1 LIMIT 500 FOR UPDATE`,
					t.idCol, t.kekCol, t.dekCol, t.table, t.kekCol, t.kekCol)
				rows, err := tx.Query(ctx, q, active)
				if err != nil {
					return err
				}
				for rows.Next() {
					var r row
					if err := rows.Scan(&r.id, &r.kek, &r.wrapped); err != nil {
						return err
					}
					batch = append(batch, r)
				}
				if err := rows.Err(); err != nil {
					return err
				}
				// #nosec G201 -- identifiers come from the constant table above
				upd := fmt.Sprintf(`UPDATE %s SET %s = $1, %s = $2 WHERE %s::text = $3`, t.table, t.kekCol, t.dekCol, t.idCol)
				for _, r := range batch {
					w, err := keys.Rewrap(t.table, r.id, envelope.WrappedKey{KEKID: r.kek, DEK: r.wrapped})
					if err != nil {
						return fmt.Errorf("%s %s: %w (is key %q still in ENCRYPTION_KEYS?)", t.table, r.id, err, r.kek)
					}
					if _, err := tx.Exec(ctx, upd, w.KEKID, w.DEK, r.id); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return out, err
			}
			out[t.table] += len(batch)
			if len(batch) < 500 {
				break
			}
		}
	}
	return out, nil
}
