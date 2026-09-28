#!/bin/sh
set -eu

: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${TGSTRM_DATABASE_PASSWORD:?TGSTRM_DATABASE_PASSWORD is required}"

# The image runs this only for an empty data directory. TgStrm receives a
# database-owner role, never the cluster superuser credential.
PGPASSWORD="$POSTGRES_PASSWORD" psql \
  --username "$POSTGRES_USER" \
  --dbname postgres \
  --set ON_ERROR_STOP=1 \
  --set app_password="$TGSTRM_DATABASE_PASSWORD" \
  --set app_database="$POSTGRES_DB" <<'SQL'
CREATE ROLE tgstrm LOGIN PASSWORD :'app_password';
ALTER DATABASE :"app_database" OWNER TO tgstrm;
SQL
