#!/usr/bin/env bash
set -euo pipefail
umask 077
STAKRAF_BACKUP_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$STAKRAF_BACKUP_ROOT"
STAKRAF_BACKUP_COMPOSE="${STAKRAF_COMPOSE_FILE:-docker-compose.production.yml}"
STAKRAF_BACKUP_DIRECTORY="${1:-$STAKRAF_BACKUP_ROOT/backups}"
mkdir -p "$STAKRAF_BACKUP_DIRECTORY"
STAKRAF_BACKUP_TARGET="$(mktemp "$STAKRAF_BACKUP_DIRECTORY/stakraf-$(date -u +%Y%m%dT%H%M%SZ).XXXXXX.dump")"
docker compose -f "$STAKRAF_BACKUP_COMPOSE" exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$STAKRAF_BACKUP_TARGET"
docker compose -f "$STAKRAF_BACKUP_COMPOSE" exec -T postgres pg_restore --list < "$STAKRAF_BACKUP_TARGET" >/dev/null
echo "Verified PostgreSQL archive: $STAKRAF_BACKUP_TARGET"
echo 'Store an encrypted copy off this server. Back up private .env and agent state separately.'
