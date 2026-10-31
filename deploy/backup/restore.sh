#!/usr/bin/env bash
# Restores an encrypted backup made by backup.sh into a target database.
#
#   restore.sh [--force] [--no-owner] --target-db NAME FILE.dump.gpg
#
# The checksum file (FILE.dump.gpg.sha256) is verified first when present
# (required unless --skip-checksum). The plaintext dump is streamed from gpg
# into pg_restore and never written to disk.
#
# Refuses to restore into a database that already contains tables unless
# --force is given (then existing objects are dropped first: --clean).
#
# Connection: libpq variables (PGHOST, PGPORT, PGUSER, PGSSLMODE, PGPASSFILE).
# The target database must exist, the roles app_owner, app_rw and app_system
# must exist in the target cluster (grants reference them), and the restoring
# role needs BYPASSRLS because tables use FORCE ROW LEVEL SECURITY.
#
# Decryption: BACKUP_PASSPHRASE_FILE (symmetric) or the private key in
# GPG_HOMEDIR / the default keyring (public-key mode).
# Optional wrappers: PSQL (default "psql"), PG_RESTORE (default "pg_restore").
set -euo pipefail

log() { printf '%s restore: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
die() { log "error: $*"; exit 1; }
usage() { sed -n '2,4p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

force=0 no_owner=0 skip_checksum=0 target="" file=""
while (( $# > 0 )); do
  case "$1" in
    --force) force=1 ;;
    --no-owner) no_owner=1 ;;
    --skip-checksum) skip_checksum=1 ;;
    --target-db) shift; target="${1:-}" ;;
    --target-db=*) target="${1#*=}" ;;
    -h|--help) usage ;;
    -*) die "unknown option $1" ;;
    *) [[ -z "$file" ]] || die "only one backup file may be given"; file="$1" ;;
  esac
  shift
done
[[ -n "$target" && -n "$file" ]] || usage
[[ "$target" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "target database name must be a plain identifier"
[[ -r "$file" ]] || die "cannot read $file"

read -r -a psql_cmd <<< "${PSQL:-psql}"
read -r -a pg_restore_cmd <<< "${PG_RESTORE:-pg_restore}"

if [[ -f "$file.sha256" ]]; then
  log "verifying checksum"
  ( cd "$(dirname "$file")" && sha256sum --check --quiet "$(basename "$file").sha256" ) || die "checksum mismatch"
elif (( skip_checksum == 0 )); then
  die "$file.sha256 not found (use --skip-checksum to restore anyway)"
fi

tables="$("${psql_cmd[@]}" -X -A -t -v ON_ERROR_STOP=1 --no-password --dbname="$target" -c \
  "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema')
     AND n.nspname NOT LIKE 'pg_toast%'")" || die "cannot connect to database $target"
tables="${tables//[[:space:]]/}"
if [[ "$tables" != "0" ]]; then
  (( force == 1 )) || die "database $target already has $tables tables; refusing without --force"
  log "warning: --force given, existing objects in $target will be dropped"
fi

tmp_home=""
cleanup() { if [[ -n "$tmp_home" ]]; then rm -rf "$tmp_home"; fi; }
trap cleanup EXIT

gpg_args=(--batch --yes --no-tty --quiet --decrypt)
if [[ -n "${BACKUP_PASSPHRASE_FILE:-}" ]]; then
  [[ -r "$BACKUP_PASSPHRASE_FILE" ]] || die "cannot read BACKUP_PASSPHRASE_FILE"
  if [[ -n "${GPG_HOMEDIR:-}" ]]; then
    gpg_args+=(--homedir "$GPG_HOMEDIR")
  else
    tmp_home="$(mktemp -d)"; chmod 700 "$tmp_home"
    gpg_args+=(--homedir "$tmp_home")
  fi
  gpg_args+=(--pinentry-mode loopback --passphrase-file "$BACKUP_PASSPHRASE_FILE")
elif [[ -n "${GPG_HOMEDIR:-}" ]]; then
  gpg_args+=(--homedir "$GPG_HOMEDIR")
fi

restore_args=(--exit-on-error --single-transaction --no-password --dbname="$target")
(( force == 1 )) && restore_args+=(--clean --if-exists)
(( no_owner == 1 )) && restore_args+=(--no-owner)

log "restoring $(basename "$file") into $target"
gpg "${gpg_args[@]}" "$file" | "${pg_restore_cmd[@]}" "${restore_args[@]}"
"${psql_cmd[@]}" -X -q -v ON_ERROR_STOP=1 --no-password --dbname="$target" -c "ANALYZE" >/dev/null
log "done"
