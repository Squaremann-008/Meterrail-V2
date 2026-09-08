#!/usr/bin/env bash
#
# Takes a compressed logical backup of the Postgres database and uploads it to
# Cloudflare R2, keeping a short local history as well.
#
# Driven by deploy/systemd/meterrail-backup.timer, or run by hand:
#   ./deploy/scripts/backup-db.sh
#
# Neon and Supabase both provide point-in-time recovery; this exists so there is
# also a copy in storage you control, restorable without the provider.

set -euo pipefail

APP_DIR="${APP_DIR:-/opt/meterrail}"
ENV_FILE="${ENV_FILE:-${APP_DIR}/.env.production}"
BACKUP_DIR="${BACKUP_DIR:-${APP_DIR}/backups}"
RETAIN_DAYS="${RETAIN_DAYS:-7}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

[[ -f "$ENV_FILE" ]] || die "environment file not found: ${ENV_FILE}"

# shellcheck disable=SC1090
set -a; source "$ENV_FILE"; set +a

[[ -n "${DATABASE_URL:-}" ]] || die "DATABASE_URL is not set"

mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
ARCHIVE="${BACKUP_DIR}/meterrail-${TIMESTAMP}.dump.gz"

log "Dumping the database"
# pg_dump runs in a container so the host needs no Postgres client installed,
# and the version always matches the server.
docker run --rm -i postgres:17-alpine \
    pg_dump --format=custom --no-owner --no-acl "$DATABASE_URL" \
    | gzip -9 > "$ARCHIVE"

SIZE="$(du -h "$ARCHIVE" | cut -f1)"
log "Wrote ${ARCHIVE} (${SIZE})"

# An empty or near-empty dump usually means pg_dump failed silently mid-stream.
MIN_BYTES=1024
ACTUAL_BYTES="$(stat -c%s "$ARCHIVE" 2>/dev/null || stat -f%z "$ARCHIVE")"
[[ "$ACTUAL_BYTES" -gt "$MIN_BYTES" ]] || die "backup is suspiciously small (${ACTUAL_BYTES} bytes)"

if [[ -n "${R2_BUCKET:-}" && -n "${R2_ACCESS_KEY_ID:-}" ]]; then
    log "Uploading to R2"
    R2_ENDPOINT_RESOLVED="${R2_ENDPOINT:-https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com}"

    docker run --rm \
        -e AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
        -e AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
        -e AWS_DEFAULT_REGION=auto \
        -v "${BACKUP_DIR}:/backups:ro" \
        amazon/aws-cli:latest \
        s3 cp "/backups/$(basename "$ARCHIVE")" \
              "s3://${R2_BUCKET}/backups/$(basename "$ARCHIVE")" \
              --endpoint-url "$R2_ENDPOINT_RESOLVED"
else
    log "R2 is not configured; keeping the local copy only"
fi

log "Pruning local backups older than ${RETAIN_DAYS} days"
find "$BACKUP_DIR" -name 'meterrail-*.dump.gz' -type f -mtime "+${RETAIN_DAYS}" -delete

log "Backup complete"
