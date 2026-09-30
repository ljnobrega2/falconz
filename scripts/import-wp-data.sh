#!/usr/bin/env bash
# =============================================================================
# import-wp-data.sh — Backfill ÚNICO dos dados reais do WordPress para o Postgres
# dos serviços Go (sem WP). Carrega um dump MySQL do WP num MariaDB local e roda
# o migrador Python (infra/scripts/migrate-wp-to-pg.py).
#
# AUDIT-2026-06-18. Docker indisponível → MariaDB nativo (brew).
# Uso:
#   scripts/import-wp-data.sh "/caminho/para/dump-wp.sql"
# =============================================================================
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DUMP="${1:?Informe o caminho do dump .sql do WordPress}"
DB="${MYSQL_DB:-u904932976_YNCRk}"
export PATH="/usr/local/opt/mariadb/bin:/usr/local/opt/postgresql@16/bin:/usr/local/bin:$PATH"
set -a; . "$ROOT/infra/docker/.env"; set +a

echo "▸ MariaDB up"
mysqladmin ping -h 127.0.0.1 2>/dev/null | grep -q alive || brew services start mariadb
for i in $(seq 1 25); do mysqladmin ping -h 127.0.0.1 2>/dev/null | grep -q alive && break; sleep 1; done

echo "▸ Usuário TCP 'sz' + DB $DB"
mysql <<SQL
CREATE USER IF NOT EXISTS 'sz'@'127.0.0.1' IDENTIFIED BY 'sz';
CREATE USER IF NOT EXISTS 'sz'@'localhost' IDENTIFIED BY 'sz';
GRANT ALL PRIVILEGES ON *.* TO 'sz'@'127.0.0.1';
GRANT ALL PRIVILEGES ON *.* TO 'sz'@'localhost';
DROP DATABASE IF EXISTS $DB; CREATE DATABASE $DB CHARACTER SET utf8mb4;
FLUSH PRIVILEGES;
SQL

echo "▸ Carrega dump (1x, --force pula tabelas de outros plugins)"
mysql --force "$DB" < "$DUMP" 2>/tmp/senderzz-dev/wpload.err || true
echo "  erros pulados: $(grep -c ERROR /tmp/senderzz-dev/wpload.err 2>/dev/null || echo 0)"

echo "▸ venv Python + deps"
[ -d /tmp/senderzz-dev/venv ] || python3 -m venv /tmp/senderzz-dev/venv
/tmp/senderzz-dev/venv/bin/pip install -q pymysql psycopg2-binary 2>/dev/null || true

echo "▸ Migra MySQL → Postgres"
MYSQL_DB="$DB" POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
  /tmp/senderzz-dev/venv/bin/python "$ROOT/infra/scripts/migrate-wp-to-pg.py"

echo "✓ Import concluído."
