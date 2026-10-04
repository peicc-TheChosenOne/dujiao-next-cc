#!/bin/bash
set -euo pipefail
umask 077

base=/opt/dujiao-next
backup_root="$base/backups"
cd "$base"
exec 9>/run/lock/dujiao-next-backup.lock
flock -n 9 || exit 0
mkdir -p "$backup_root"
tmpdir=$(mktemp -d "$backup_root/.tmp.XXXXXX")
trap 'rm -rf -- "$tmpdir"' EXIT

docker compose exec -T postgres pg_dump -U postgres -d dujiao_next \
  --format=custom --no-owner --no-privileges > "$tmpdir/postgres.dump"
docker compose exec -T redis sh -c \
  'REDISCLI_AUTH="$(cat /run/secrets/redis_password)" redis-cli --rdb /tmp/dujiao-next-backup.rdb >/dev/null'
docker compose cp redis:/tmp/dujiao-next-backup.rdb "$tmpdir/redis.rdb" >/dev/null
docker compose exec -T redis rm -f /tmp/dujiao-next-backup.rdb

tar -czf "$tmpdir/backup.tar.gz" \
  -C "$tmpdir" postgres.dump redis.rdb \
  -C "$base" .env compose.yml postgres-init.sh redis.conf nginx.conf renew-cert.sh config secrets tls data/uploads
archive="$backup_root/dujiao-next-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
mv "$tmpdir/backup.tar.gz" "$archive"
find "$backup_root" -maxdepth 1 -type f -name 'dujiao-next-*.tar.gz' -mtime +14 -delete
printf 'Backup completed: %s\n' "$archive"
