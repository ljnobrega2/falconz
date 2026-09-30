#!/usr/bin/env bash
# =============================================================================
# AUDIT CRON-sql-orphan-no-runner
# cron-preagendado-autocancel.sh — RUNNER da função SQL órfã
#   sz_cancel_preagendados_vencidos() (infra/postgres/schema-preagendado-autocancel.sql).
#
# A função existia (idempotente, não-destrutiva: só faz UPDATE status='cancelado'
# em pre_agendado vencido) mas NINGUÉM a chamava — o comentário do .sql só dizia
# "chamar via cron". Este wrapper é esse cron: chama a função e loga o nº de
# pedidos cancelados. Escopo infra (shell + psql), sem worker Go — sem dependência
# nova, verificável com bash -n.
#
# CONEXÃO: $DATABASE_URL ou POSTGRES_* de infra/docker/.env (igual db-migrate.sh).
#
# USO (manual):
#   bash infra/scripts/cron-preagendado-autocancel.sh
#
# USO (cron diário 02:00 UTC = 23:00 BRT, fora de pico — a própria função usa
#       horário de Brasília internamente p/ decidir o vencimento):
#   crontab -e:
#     0 2 * * *  /opt/senderzz/infra/scripts/cron-preagendado-autocancel.sh \
#       >> /var/log/senderzz-preagendado.log 2>&1
#
# IDEMPOTÊNCIA: rodar 2x no mesmo dia não recancela (já não estão 'pre_agendado').
#
# VALIDAÇÃO DE SINTAXE:  bash -n infra/scripts/cron-preagendado-autocancel.sh
# =============================================================================
set -euo pipefail
export LC_ALL=C

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="${ROOT}/infra/docker/.env"

command -v psql >/dev/null 2>&1 || {
    echo "ERRO: 'psql' não encontrado no PATH. Instale o cliente do PostgreSQL." >&2
    exit 1
}

# Resolve conexão — espelha db-migrate.sh / backup-postgres.sh.
if [ -z "${DATABASE_URL:-}" ]; then
    if [ -f "$ENV_FILE" ]; then
        # shellcheck disable=SC1090
        set -a; . "$ENV_FILE"; set +a
        DATABASE_URL="postgresql://${POSTGRES_USER:?ERRO: POSTGRES_USER não definido no .env}:${POSTGRES_PASSWORD:?ERRO: POSTGRES_PASSWORD não definido no .env}@${POSTGRES_HOST:-localhost}:${POSTGRES_PORT:-5432}/${POSTGRES_DB:?ERRO: POSTGRES_DB não definido no .env}?sslmode=disable"
    else
        echo "ERRO: \$DATABASE_URL não definido e $ENV_FILE não encontrado." >&2
        exit 1
    fi
fi
export DATABASE_URL

echo "→ [$(date -u +%Y-%m-%dT%H:%M:%SZ)] Executando sz_cancel_preagendados_vencidos()..."
# -tA: tuples-only/unaligned → captura só o nº retornado pela função.
# ON_ERROR_STOP=1: erro de SQL devolve exit != 0 (cron alerta).
CANCELADOS="$(psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tA \
    -c "SELECT sz_cancel_preagendados_vencidos();")"

echo "✓ [$(date -u +%Y-%m-%dT%H:%M:%SZ)] pre_agendado vencidos cancelados: ${CANCELADOS}"
