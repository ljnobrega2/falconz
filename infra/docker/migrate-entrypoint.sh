#!/usr/bin/env bash
# =============================================================================
# AUDIT INFRA-docker-compose-schema-divergence
# migrate-entrypoint.sh — entrypoint do serviço 'migrate-runner' do compose.
#
# PROBLEMA: o docker-compose montava só 7 dos ~29 .sql via /docker-entrypoint-initdb.d
# (drift dev vs prod) E initdb.d só roda na 1ª criação do volume (nunca em restart /
# volume existente). RESULTADO: schemas novos (revenue, fixes-vNNN, stock...) nunca
# eram aplicados pelo compose. Solução: parar de depender do initdb.d e rodar o
# MIGRATION RUNNER versionado (scripts/db-migrate.sh) num serviço dedicado que:
#   • aplica TODOS os .sql em ordem (rastreado em schema_migrations, idempotente),
#   • SAI 0 ao terminar (bloqueando os serviços Go via depends_on:
#     condition: service_completed_successfully),
#   • host do DB DENTRO da rede compose = 'postgres' (NÃO localhost).
#
# Imagem: postgres:16 (Debian — tem bash + psql; o db-migrate.sh usa arrays bash,
# que a variante -alpine/busybox NÃO suporta).
# =============================================================================
set -euo pipefail

# DATABASE_URL é injetado pelo compose apontando p/ o host de serviço 'postgres'.
# db-migrate.sh prioriza $DATABASE_URL (não cai no fallback localhost do .env).
: "${DATABASE_URL:?ERRO: DATABASE_URL não definido (deveria vir do compose).}"

echo "→ migrate-runner: aguardando Postgres ficar pronto..."
# O depends_on já espera service_healthy, mas reforçamos com pg_isready (barato e
# evita corrida se o healthcheck e a 1ª conexão divergirem por milissegundos).
for i in $(seq 1 30); do
    if pg_isready -d "$DATABASE_URL" >/dev/null 2>&1; then
        echo "→ migrate-runner: Postgres pronto."
        break
    fi
    [ "$i" -eq 30 ] && { echo "ERRO: Postgres não respondeu a tempo." >&2; exit 1; }
    sleep 2
done

echo "→ migrate-runner: aplicando migrações via infra/scripts/db-migrate.sh"
# AUDIT INFRA-migration-schema-alphabetical-fragility: o repo é montado em /repo
# (compose: ../../:/repo:ro), então o runner está em /repo/infra/scripts/db-migrate.sh
# (NÃO /repo/scripts/...). Caminho anterior estava errado → runner não era encontrado.
exec bash /repo/infra/scripts/db-migrate.sh
