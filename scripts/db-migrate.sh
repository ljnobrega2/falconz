#!/usr/bin/env bash
# =============================================================================
# Senderzz — MIGRATION RUNNER idempotente para infra/postgres/*.sql
#
# Aplica TODOS os arquivos infra/postgres/*.sql em ordem ALFABÉTICA
# por PREFIXO NUMÉRICO NNN-nome.sql (010 bases, 100 fixes, 300 revenue, 900 seeds).
# Rastreia o que já foi aplicado na tabela schema_migrations(filename, applied_at)
# e PULA os arquivos já registrados. Rodar 2x não erra (idempotente).
#
# NÃO aplica nada dentro de infra/postgres/tests/ (usa só o glob de topo).
#
# Conexão:
#   - Usa $DATABASE_URL se já estiver definido no ambiente.
#   - Caso contrário, monta a URL a partir de POSTGRES_* lidos de
#     infra/docker/.env (host padrão = localhost, igual ao dev-local.sh).
#
# Uso:
#   chmod +x scripts/db-migrate.sh     # (rodar uma vez para tornar executável)
#   scripts/db-migrate.sh              # aplica as migrações pendentes
#
# Validação de sintaxe (sem executar):
#   bash -n scripts/db-migrate.sh
# =============================================================================
set -euo pipefail

# Locale fixo para que a ordenação "alfabética" seja determinística e bata
# com o que você vê no `ls` (independente do locale do shell que chama).
export LC_ALL=C

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PGDIR="$ROOT/infra/postgres"
ENV_FILE="$ROOT/infra/docker/.env"

# ---------------------------------------------------------------------------
# 1. Pré-requisitos
# ---------------------------------------------------------------------------
command -v psql >/dev/null 2>&1 || {
    echo "ERRO: 'psql' não encontrado no PATH. Instale o cliente do PostgreSQL." >&2
    exit 1
}
[ -d "$PGDIR" ] || {
    echo "ERRO: diretório de migrações não existe: $PGDIR" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# 2. Resolve a string de conexão ($DATABASE_URL)
# ---------------------------------------------------------------------------
if [ -z "${DATABASE_URL:-}" ]; then
    if [ -f "$ENV_FILE" ]; then
        echo "→ \$DATABASE_URL ausente — montando a partir de $ENV_FILE"
        # shellcheck disable=SC1090
        set -a; . "$ENV_FILE"; set +a
        DATABASE_URL="postgresql://${POSTGRES_USER:?ERRO: POSTGRES_USER não definido no .env}:${POSTGRES_PASSWORD:?ERRO: POSTGRES_PASSWORD não definido no .env}@${POSTGRES_HOST:-localhost}:${POSTGRES_PORT:-5432}/${POSTGRES_DB:?ERRO: POSTGRES_DB não definido no .env}?sslmode=disable"
    else
        echo "ERRO: \$DATABASE_URL não definido e $ENV_FILE não encontrado." >&2
        exit 1
    fi
fi
export DATABASE_URL

# ---------------------------------------------------------------------------
# 3. Garante a tabela de controle (idempotente — IF NOT EXISTS)
# ---------------------------------------------------------------------------
echo "→ Garantindo tabela de controle schema_migrations..."
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    filename   text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
SQL

# ---------------------------------------------------------------------------
# 4. Coleta os arquivos *.sql do topo (o glob NÃO entra em tests/ — de graça)
# ---------------------------------------------------------------------------
shopt -s nullglob
FILES=( "$PGDIR"/*.sql )
shopt -u nullglob

if [ "${#FILES[@]}" -eq 0 ]; then
    echo "Nenhum arquivo .sql encontrado em $PGDIR — nada a fazer."
    exit 0
fi

# Ordena alfabeticamente (= ordem de aplicação).
# AUDIT INFRA-migration-schema-alphabetical-fragility: a ordem alfabética NÃO era
# cronológica/dependente — "schema-fixes-v460.sql" sorteava ANTES de "schema-orders.sql"
# e dava ALTER em sz_orders antes da tabela existir (falha comprovada em DB novo).
# CONVENÇÃO atual: PREFIXO NUMÉRICO explícito "NNN-nome.sql". Faixas:
#   010-090  bases (admin, wallet, motoboy, orders, portal, labels, affiliates)
#   100-299  fixes/extensões por versão (ALTERs, colunas, tabelas derivadas)
#   300-399  revenue (triggers/ledger — dependem de orders + affiliate_transactions)
#   900-999  seeds (dependem de tudo)
# Ao adicionar schema: escolha o prefixo pela DEPENDÊNCIA (não pela data). Gaps de 10
# permitem inserir no meio sem renomear. NUNCA volte ao padrão "schema-*-vN.sql".
IFS=$'\n' FILES=( $(printf '%s\n' "${FILES[@]}" | sort) )
unset IFS

# ---------------------------------------------------------------------------
# 5. Aplica cada migração pendente
# ---------------------------------------------------------------------------
aplicadas=0
puladas=0

for file in "${FILES[@]}"; do
    base="$(basename "$file")"

    # AUDIT INFRA-postgres-backup-absent/seed-safety: SEEDS DE DEMO nunca em produção.
    # Fail-closed: arquivos de dados de DEMO (seed-dev-demo, seed-demo-*) só são
    # aplicados se APPLY_SEEDS=1 (o migrate-runner do compose seta isso em DEV).
    # Deploy de prod e CI NÃO setam → demo data jamais entra no banco financeiro.
    # NOTA: 900-seed-shipping-classes NÃO é demo (config baseline que prod precisa)
    # — por isso o filtro casa só os nomes de demo, não a palavra "seed".
    case "$base" in
        seed-dev-demo.sql|9*-seed-dev-demo.sql|seed-demo-*.sql)
            if [ "${APPLY_SEEDS:-0}" != "1" ]; then
                echo "  • pulando (seed de demo, APPLY_SEEDS!=1): $base"
                puladas=$((puladas + 1))
                continue
            fi
            ;;
        # AUDIT-2026-06-22: arquivos com prefixo "_" são snapshots/backups manuais,
        # NÃO migrações versionadas. Ex.: _backup_view_financeiro_v2.sql tenta
        # recriar uma view dropando colunas e quebra o runner ("cannot drop columns
        # from view"). Convenção: "_" = ignorar.
        _*)
            echo "  • pulando (backup/snapshot manual, prefixo _): $base"
            puladas=$((puladas + 1))
            continue
            ;;
    esac

    # Já aplicada? (consulta tuples-only/unaligned; resultado vazio = pendente)
    ja="$(psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tA \
        -c "SELECT 1 FROM schema_migrations WHERE filename = '$base';")"

    if [ -n "$ja" ]; then
        echo "  • já aplicada, pulando: $base"
        puladas=$((puladas + 1))
        continue
    fi

    echo "  ▸ aplicando: $base"
    # Arquivo + registro na MESMA transação: se o SQL falhar, NADA é commitado
    # e a migração NÃO fica marcada como aplicada (re-run reaproveita do zero).
    # ON_ERROR_STOP=1 faz o psql retornar != 0 em qualquer erro de SQL (sem ele
    # o psql sairia 0 e o 'set -e' não pegaria a falha → registro mentiroso).
    psql "$DATABASE_URL" --single-transaction -v ON_ERROR_STOP=1 -q <<SQL
\i $file
INSERT INTO schema_migrations (filename) VALUES ('$base');
SQL

    aplicadas=$((aplicadas + 1))
done

# ---------------------------------------------------------------------------
# 6. Resumo
# ---------------------------------------------------------------------------
echo ""
echo "✓ Concluído. Aplicadas: $aplicadas | Já aplicadas (puladas): $puladas | Total: ${#FILES[@]}"
