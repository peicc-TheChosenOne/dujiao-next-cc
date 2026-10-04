#!/bin/sh
set -eu

psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=app_password="$(cat /run/secrets/app_db_password)" <<'SQL'
CREATE ROLE dujiao LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE CONNECTION LIMIT 25 PASSWORD :'app_password';
ALTER DATABASE dujiao_next OWNER TO dujiao;
REVOKE CONNECT ON DATABASE dujiao_next FROM PUBLIC;
GRANT CONNECT ON DATABASE dujiao_next TO dujiao;
SQL
