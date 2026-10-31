#!/usr/bin/env bash
# Logical backup of the application database.
#
#   pg_dump (custom format, compressed) | gpg (encrypted) > <dir>/<name>.dump.gpg
#   sha256 checksum next to it, optional upload, local retention cleanup.
#
# Connection: standard libpq variables (PGHOST, PGPORT, PGUSER, PGSSLMODE,
# PGPASSFILE / PGPASSWORD) so no password ever appears on a command line.
# The role must be able to read every row: tables use FORCE ROW LEVEL
# SECURITY, so it needs BYPASSRLS (see docs/deployment.md, "Backups").
#
# Required:
#   PGDATABASE                 database name (e.g. shopee_automation)
#   BACKUP_DIR                 local directory for backup files
#   one of:
#     BACKUP_GPG_RECIPIENT     encrypt to this public key (recommended: the
#                              backup host then cannot decrypt), or
#     BACKUP_PASSPHRASE_FILE   file holding a symmetric passphrase
# Optional:
#   BACKUP_PREFIX              file name prefix (default: $PGDATABASE)
#   BACKUP_RETENTION_DAYS      delete local backups older than this (default 14; 0 keeps all)
#   BACKUP_COMPRESS            pg_dump --compress value (default 6)
#   BACKUP_UPLOAD_CMD          shell command run as: $BACKUP_UPLOAD_CMD <dump> <sha256-file>
#                              e.g. 'aws s3 cp "$1" s3://bucket/db/ && aws s3 cp "$2" s3://bucket/db/'
#   PG_DUMP                    pg_dump command (default "pg_dump"); may be a wrapper such
#                              as "docker exec -i -e PGUSER=app_owner pg pg_dump"
#   GPG_HOMEDIR                gpg home (default: a private temp dir for symmetric mode)
#
# Prints the path of the encrypted dump on the last line of stdout.
set -euo pipefail

log() { printf '%s backup: %s\n' "$(date -u +%FT%TZ)" "$*" >&2; }
die() { log "error: $*"; exit 1; }

: "${PGDATABASE:?PGDATABASE is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"
prefix="${BACKUP_PREFIX:-$PGDATABASE}"
retention="${BACKUP_RETENTION_DAYS:-14}"
compress="${BACKUP_COMPRESS:-6}"
read -r -a pg_dump_cmd <<< "${PG_DUMP:-pg_dump}"

[[ "$retention" =~ ^[0-9]+$ ]] || die "BACKUP_RETENTION_DAYS must be a non-negative integer"
[[ "$prefix" =~ ^[A-Za-z0-9._-]+$ ]] || die "BACKUP_PREFIX may only contain letters, digits, . _ -"
command -v gpg >/dev/null || die "gpg is not installed"
command -v sha256sum >/dev/null || die "sha256sum is not installed"

tmp_home=""
partial=""
cleanup() {
  if [[ -n "$tmp_home" ]]; then rm -rf "$tmp_home"; fi
  if [[ -n "$partial" ]]; then rm -f "$partial"; fi
}
trap cleanup EXIT

gpg_args=(--batch --yes --no-tty --quiet)
if [[ -n "${BACKUP_GPG_RECIPIENT:-}" ]]; then
  [[ -n "${GPG_HOMEDIR:-}" ]] && gpg_args+=(--homedir "$GPG_HOMEDIR")
  gpg_args+=(--trust-model always --encrypt --recipient "$BACKUP_GPG_RECIPIENT")
elif [[ -n "${BACKUP_PASSPHRASE_FILE:-}" ]]; then
  [[ -r "$BACKUP_PASSPHRASE_FILE" ]] || die "cannot read BACKUP_PASSPHRASE_FILE"
  [[ -s "$BACKUP_PASSPHRASE_FILE" ]] || die "BACKUP_PASSPHRASE_FILE is empty"
  if [[ -n "${GPG_HOMEDIR:-}" ]]; then
    gpg_args+=(--homedir "$GPG_HOMEDIR")
  else
    tmp_home="$(mktemp -d)"
    chmod 700 "$tmp_home"
    gpg_args+=(--homedir "$tmp_home")
  fi
  # The passphrase is read from the file; only the file path is on the command line.
  gpg_args+=(--pinentry-mode loopback --passphrase-file "$BACKUP_PASSPHRASE_FILE"
             --symmetric --cipher-algo AES256 --s2k-digest-algo SHA512 --s2k-count 65011712)
else
  die "set BACKUP_GPG_RECIPIENT or BACKUP_PASSPHRASE_FILE"
fi

umask 077
mkdir -p "$BACKUP_DIR"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="$BACKUP_DIR/$prefix-$stamp.dump.gpg"
partial="$out.partial"

log "dumping database $PGDATABASE"
"${pg_dump_cmd[@]}" --format=custom --compress="$compress" --no-password \
  --serializable-deferrable --dbname="$PGDATABASE" \
  | gpg "${gpg_args[@]}" --output - > "$partial"
[[ -s "$partial" ]] || die "backup file is empty"
mv "$partial" "$out"
partial=""

( cd "$BACKUP_DIR" && sha256sum "$(basename "$out")" > "$(basename "$out").sha256" )
log "wrote $out ($(du -h "$out" | cut -f1)), checksum $(cut -d' ' -f1 "$out.sha256")"

if [[ -n "${BACKUP_UPLOAD_CMD:-}" ]]; then
  log "uploading"
  bash -c "$BACKUP_UPLOAD_CMD" upload "$out" "$out.sha256" || die "upload command failed"
fi

if (( retention > 0 )); then
  while IFS= read -r -d '' old; do
    log "retention: removing $(basename "$old")"
    rm -f "$old" "$old.sha256"
  done < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name "$prefix-*.dump.gpg" -mtime +"$retention" -print0)
fi

printf '%s\n' "$out"
