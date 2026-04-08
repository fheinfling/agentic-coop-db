#!/bin/sh
# deploy/backup/backup.sh — daily restic snapshot of the postgres data dir.
#
# Run inside the `backup` container in compose.cloud.yml. Reads:
#   RESTIC_REPOSITORY    s3:..., b2:..., sftp:..., or local path
#   RESTIC_PASSWORD_FILE path to a file containing the repo password
#
# The script:
#   1. inits the repo if it does not exist (idempotent)
#   2. uses pg_dump-style logical backup over the live cluster (preferred)
#      and falls back to a filesystem snapshot of /var/lib/postgresql/data
#      when pg_dump is not available
#   3. forgets snapshots older than the retention policy
#   4. exits non-zero on any failure (the cron loop will alert via stdout)

set -eu

: "${RESTIC_REPOSITORY:?must be set}"
: "${RESTIC_PASSWORD_FILE:?must be set}"

restic snapshots >/dev/null 2>&1 || {
  echo "[backup] initialising restic repo"
  restic init
}

# Try a logical pg_dump first via the postgres container's network alias.
# Falls back to a filesystem snapshot.
TMP_DUMP="/tmp/aicoldb.dump.gz"
if command -v pg_dump >/dev/null 2>&1; then
  PGPASSWORD="$(cat /run/secrets/postgres_password 2>/dev/null || echo '')" \
  pg_dump --format=custom --blobs --no-owner --compress=6 \
          --host=postgres --username=aicoldb_owner aicoldb \
          | gzip -c > "${TMP_DUMP}" || {
    echo "[backup] pg_dump failed; falling back to filesystem snapshot"
    rm -f "${TMP_DUMP}"
  }
fi

if [ -f "${TMP_DUMP}" ]; then
  restic backup --tag pg_dump "${TMP_DUMP}"
  rm -f "${TMP_DUMP}"
else
  restic backup --tag fs /var/lib/postgresql/data
fi

# Retention: keep 7 daily, 4 weekly, 6 monthly. Adjust as needed.
restic forget --tag pg_dump --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune
restic forget --tag fs       --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune

echo "[backup] done at $(date -u +%FT%TZ)"
