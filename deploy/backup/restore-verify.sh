#!/bin/sh
# deploy/backup/restore-verify.sh — weekly: restore the latest snapshot into
# a throwaway container, run a smoke SELECT, exit non-zero on any failure.
#
# Intended to be run by an external cron (or by a CI job). The compose
# stack does not run this on a schedule by default — give it its own
# `docker compose run --rm backup /backup/restore-verify.sh` invocation
# in your weekly maintenance window.

set -eu

: "${RESTIC_REPOSITORY:?must be set}"
: "${RESTIC_PASSWORD_FILE:?must be set}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "[verify] restoring latest pg_dump snapshot to ${WORK}"
restic restore latest --tag pg_dump --target "${WORK}"

DUMP="$(find "${WORK}" -type f -name 'aicoldb.dump.gz' -print -quit || true)"
if [ -z "${DUMP}" ]; then
  echo "[verify] no aicoldb.dump.gz in restored snapshot"
  exit 1
fi

# Spin up a throwaway postgres, load the dump, run a smoke SELECT.
echo "[verify] starting throwaway postgres"
PGDATA="${WORK}/pgdata"
mkdir -p "${PGDATA}"
docker run --rm -v "${PGDATA}:/var/lib/postgresql/data" \
  -e POSTGRES_PASSWORD=verify -e POSTGRES_DB=aicoldb \
  --name aicoldb-restore-verify \
  -d pgvector/pgvector:pg16 >/dev/null
sleep 5

gunzip -c "${DUMP}" | docker exec -i aicoldb-restore-verify pg_restore -U postgres -d aicoldb || {
  docker stop aicoldb-restore-verify >/dev/null
  exit 1
}

OK="$(docker exec aicoldb-restore-verify psql -U postgres -d aicoldb -tAc 'SELECT 1')"
docker stop aicoldb-restore-verify >/dev/null

if [ "${OK}" != "1" ]; then
  echo "[verify] smoke SELECT failed"
  exit 1
fi
echo "[verify] OK"
