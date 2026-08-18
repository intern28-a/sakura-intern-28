#!/usr/bin/env bash
set -euo pipefail

: "${DB_HOST:?DB_HOST is required}"
: "${DB_PORT:?DB_PORT is required}"
: "${DB_USER:?DB_USER is required}"
: "${DB_NAME:?DB_NAME is required}"
: "${DB_PASSWORD:?DB_PASSWORD is required}"

MIGRATIONS_DIR="${MIGRATIONS_DIR:-/opt/sakuravel/migrations}"

db() {
  MYSQL_PWD="${DB_PASSWORD}" mariadb \
    --protocol=tcp \
    --host="${DB_HOST}" \
    --port="${DB_PORT}" \
    --user="${DB_USER}" \
    --database="${DB_NAME}" \
    "$@"
}

db -e '
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(255) NOT NULL PRIMARY KEY,
  applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);'

for migration in "${MIGRATIONS_DIR}"/*.sql; do
  version="$(basename "${migration}")"

  if db --batch --skip-column-names \
      -e "SELECT version FROM schema_migrations WHERE version = '${version}'" \
      | grep -Fxq "${version}"; then
    echo "skip ${version}"
    continue
  fi

  echo "apply ${version}"
  db < "${migration}"
  db -e "INSERT INTO schema_migrations (version) VALUES ('${version}')"
done
