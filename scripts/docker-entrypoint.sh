#!/bin/sh
# Ensure the SQLite data directory is writable by the service user, then drop
# privileges. Named Docker volumes are often root-owned on first create.
set -eu

DATA_DIR="$(dirname "${MDS_DB_PATH:-/data/micro-device-status.db}")"
mkdir -p "$DATA_DIR"

if [ "$(id -u)" = "0" ]; then
  chown -R mds:mds "$DATA_DIR"
  exec su-exec mds:mds "$@"
fi

exec "$@"
