#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/closerai}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"
TIMESTAMP="$(date +"%Y%m%d_%H%M%S")"
FILENAME="closerai_db_${TIMESTAMP}.sql.gz"

mkdir -p "$BACKUP_DIR"
echo "[$(date)] Starting database backup..."
docker compose -f deploy/docker-compose.yml --env-file .env exec -T postgres \
  pg_dump -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-closer_ai}" --clean --if-exists \
  | gzip > "${BACKUP_DIR}/${FILENAME}"
echo "[$(date)] Backup saved: ${BACKUP_DIR}/${FILENAME}"
find "$BACKUP_DIR" -type f -name "closerai_db_*.sql.gz" -mtime "+${RETENTION_DAYS}" -delete
