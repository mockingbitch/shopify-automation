#!/usr/bin/env bash
# Restore drill against the development stack (deploy/docker-compose.yml):
#   1. back up the dev database with backup.sh (encrypted, throwaway passphrase)
#   2. restore it with restore.sh into a scratch database "restore_drill"
#   3. compare exact row counts of every table, plus schema object counts
#   4. drop the scratch database and the temporary files
# The dev database is only read. Exit status is non-zero on any mismatch.
#
# Optional env: DRILL_SOURCE_DB (default shopee_automation), DRILL_DB_USER
# (default app_owner), COMPOSE_FILE_PATH (default deploy/docker-compose.yml).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
compose_file="${COMPOSE_FILE_PATH:-$repo/deploy/docker-compose.yml}"
source_db="${DRILL_SOURCE_DB:-shopee_automation}"
db_user="${DRILL_DB_USER:-app_owner}"
scratch_db="restore_drill"

log() { printf '%s drill: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
die() { log "error: $*"; exit 1; }

[[ "$source_db" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "bad DRILL_SOURCE_DB"
[[ "$source_db" != "$scratch_db" ]] || die "source and scratch database must differ"

# Tools run inside the postgres container over its local socket.
exec_pg="docker compose -f $compose_file exec -T -e PGUSER=$db_user postgres"
read -r -a pg <<< "$exec_pg"
psql_in() { local db="$1"; shift; "${pg[@]}" psql -X -A -t -q -v ON_ERROR_STOP=1 --dbname="$db" "$@"; }

"${pg[@]}" pg_isready -q || die "dev postgres is not running (make up)"

work="$(mktemp -d)"
created_scratch=0
cleanup() {
  if (( created_scratch == 1 )); then
    psql_in postgres -c "DROP DATABASE IF EXISTS $scratch_db WITH (FORCE)" >/dev/null 2>&1 \
      && log "dropped scratch database $scratch_db" || log "warning: could not drop $scratch_db"
  fi
  rm -rf "$work"
}
trap cleanup EXIT

# Exact row count of every ordinary table and leaf partition (partitioned
# parents are skipped so rows are not counted twice).
counts_sql="SELECT n.nspname || '.' || c.relname || '|' ||
  (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', n.nspname, c.relname), false, true, '')))[1]::text
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'
ORDER BY 1"
schema_sql="SELECT 'tables|'    || count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname='public'
UNION ALL SELECT 'indexes|'   || count(*) FROM pg_indexes WHERE schemaname='public'
UNION ALL SELECT 'policies|'  || count(*) FROM pg_policies WHERE schemaname='public'
UNION ALL SELECT 'functions|' || count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'
UNION ALL SELECT 'rls_forced|'|| count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relforcerowsecurity
UNION ALL SELECT 'grants|'    || count(*) FROM information_schema.role_table_grants WHERE table_schema='public'"

if [[ -n "$(psql_in postgres -c "SELECT 1 FROM pg_database WHERE datname='$scratch_db'")" ]]; then
  die "database $scratch_db already exists; drop it first (it is only for drills)"
fi

log "counting rows in $source_db"
psql_in "$source_db" -c "$counts_sql" > "$work/source_before.txt"
psql_in "$source_db" -c "$schema_sql" > "$work/source_schema.txt"

head -c 48 /dev/urandom | base64 > "$work/passphrase"
chmod 600 "$work/passphrase"

start=$(date +%s)
dump="$(PGDATABASE="$source_db" BACKUP_DIR="$work/backups" BACKUP_PASSPHRASE_FILE="$work/passphrase" \
  BACKUP_RETENTION_DAYS=0 PG_DUMP="$exec_pg pg_dump" "$here/backup.sh" | tail -n 1)"
backup_secs=$(( $(date +%s) - start ))
psql_in "$source_db" -c "$counts_sql" > "$work/source_after.txt"

psql_in postgres -c "CREATE DATABASE $scratch_db OWNER $db_user" >/dev/null
created_scratch=1

start=$(date +%s)
BACKUP_PASSPHRASE_FILE="$work/passphrase" PSQL="$exec_pg psql" PG_RESTORE="$exec_pg pg_restore" \
  "$here/restore.sh" --target-db "$scratch_db" "$dump"
restore_secs=$(( $(date +%s) - start ))

psql_in "$scratch_db" -c "$counts_sql" > "$work/restored.txt"
psql_in "$scratch_db" -c "$schema_sql" > "$work/restored_schema.txt"

# Report.
changed="no"
cmp -s "$work/source_before.txt" "$work/source_after.txt" || changed="YES (writes happened; counts may differ)"
echo
echo "Restore drill report ($(date -u +%FT%TZ))"
echo "  source database : $source_db"
echo "  backup file     : $(basename "$dump") ($(du -h "$dump" | cut -f1), encrypted)"
echo "  backup time     : ${backup_secs}s   restore time: ${restore_secs}s"
echo "  source changed during backup: $changed"
echo
printf '  %-48s %12s %12s  %s\n' TABLE SOURCE RESTORED RESULT
mismatch=0 total_tables=0 total_rows=0
while IFS='|' read -r table src; do
  restored="$(awk -F'|' -v t="$table" '$1 == t { print $2 }' "$work/restored.txt")"
  [[ -n "$restored" ]] || restored="missing"
  result=ok
  if [[ "$restored" != "$src" ]]; then result=MISMATCH; mismatch=$((mismatch + 1)); fi
  total_tables=$((total_tables + 1)); total_rows=$((total_rows + src))
  printf '  %-48s %12s %12s  %s\n' "$table" "$src" "$restored" "$result"
done < "$work/source_after.txt"
extra="$(cut -d'|' -f1 "$work/restored.txt" | grep -v -x -F -f <(cut -d'|' -f1 "$work/source_after.txt") || true)"
if [[ -n "$extra" ]]; then
  echo "  tables only in the restore: $extra"; mismatch=$((mismatch + 1))
fi
echo
echo "  schema objects (source -> restored):"
schema_ok=yes
while IFS='|' read -r kind n; do
  r="$(awk -F'|' -v k="$kind" '$1 == k { print $2 }' "$work/restored_schema.txt")"
  [[ "$r" == "$n" ]] || schema_ok=no
  printf '    %-12s %6s -> %6s\n' "$kind" "$n" "$r"
done < "$work/source_schema.txt"
echo
echo "  tables compared : $total_tables, rows in source: $total_rows"
if (( mismatch == 0 )) && [[ "$schema_ok" == yes ]]; then
  echo "  RESULT          : PASS (all row counts and schema object counts match)"
else
  echo "  RESULT          : FAIL ($mismatch table mismatches, schema match: $schema_ok)"
  exit 1
fi
