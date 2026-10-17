#!/bin/sh
set -eu

psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=app_password="$(cat /run/secrets/app_db_password)" <<'SQL'
CREATE ROLE app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE CONNECTION LIMIT 25 PASSWORD :'app_password';
ALTER DATABASE appdb OWNER TO app;
REVOKE CONNECT ON DATABASE appdb FROM PUBLIC;
GRANT CONNECT ON DATABASE appdb TO app;
SQL
