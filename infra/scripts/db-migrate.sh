#!/usr/bin/env bash
# =============================================================================
# AUDIT INFRA-migration-schema-alphabetical-fragility
# db-migrate.sh — runner de migrações versionado para infra/postgres/*.sql.
#
# PROBLEMA (resolvido aqui):
#   - Aplicação de schema dependia de /docker-entrypoint-initdb.d (só roda na 1ª
#     criação do volume) OU de aplicar arquivos "à mão" em ordem alfabética frágil.
#   - Sem rastreamento → reaplicações cegas. Os arquivos base (020-070) começam com
#     `DROP TABLE ... CASCADE` e recriam do zero: reaplicar numa DB já populada
#     APAGA dados. Sem controle de "já aplicado" isso é destrutivo.
#
# SOLUÇÃO:
#   - Tabela `schema_migrations` registra cada arquivo aplicado (idempotente).
#   - Ordenação NUMÉRICA estável via `sort -V` (010 < 020 < ... < 100 < ... < 910;
#     seeds sem prefixo numérico vão por último). Resolve a fragilidade alfabética.
#   - Cada arquivo roda com `psql -v ON_ERROR_STOP=1 -f` (para no 1º erro) e só é
#     registrado em schema_migrations APÓS sucesso (apply→record atômico por arquivo).
#   - Arquivos já registrados são PULADOS. 2ª passada = 0 novos (idempotente).
#
# AFFORDANCE DE ADOÇÃO (BASELINE_THROUGH):
#   Numa DB EXISTENTE que já tem as tabelas base mas SEM schema_migrations, a 1ª
#   passada reaplicaria 020-070 (DROP CASCADE) e apagaria dados. Para adotar o
#   runner sem destruir nada, BASELINE_THROUGH=NNN marca como aplicados (REGISTRA
#   sem EXECUTAR) todos os arquivos com prefixo numérico <= NNN. Os demais (100+)
#   são genuinamente idempotentes (IF NOT EXISTS / DROP…IF EXISTS+ADD /
#   CREATE OR REPLACE / ON CONFLICT) e SÃO executados — no-op onde já aplicados,
#   fechando drift onde não. Numa DB NOVA, BASELINE_THROUGH é vazio (default) e
#   tudo roda do zero normalmente (DROP TABLE IF EXISTS = no-op em DB vazia).
#
# SEEDS:
#   - 900-seed-shipping-classes.sql  → BASELINE de dados (páginas dependem; ON
#     CONFLICT DO NOTHING). SEMPRE aplicado.
#   - 910-seed-dev-demo.sql, seed-*.sql (sem prefixo) → DEMO. Só aplicados com
#     APPLY_SEEDS=1 (fail-closed: em prod o deploy não seta → pulados).
#
# CONEXÃO:
#   Prioriza $DATABASE_URL. Sem ele, monta a partir de PG* / POSTGRES_* (fallback
#   localhost para uso local). Dentro do compose, $DATABASE_URL aponta p/ host
#   'postgres'.
#
# USO:
#   bash infra/scripts/db-migrate.sh
#   APPLY_SEEDS=1 bash infra/scripts/db-migrate.sh            # inclui seeds demo
#   BASELINE_THROUGH=070 bash infra/scripts/db-migrate.sh     # adoção em DB existente
#   DRY_RUN=1 bash infra/scripts/db-migrate.sh                # só lista o plano
#
# VALIDAÇÃO:  bash -n infra/scripts/db-migrate.sh
# =============================================================================
set -Eeuo pipefail

# ---------------------------------------------------------------------------
# Resolução de caminhos a partir do próprio script (não do cwd) — funciona
# tanto no container (/repo/infra/scripts → ../postgres) quanto localmente.
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_DIR="${SQL_DIR:-$(cd "${SCRIPT_DIR}/../postgres" && pwd)}"

# ---------------------------------------------------------------------------
# Flags de comportamento.
# ---------------------------------------------------------------------------
APPLY_SEEDS="${APPLY_SEEDS:-0}"          # 1 = aplica seeds demo (910 + seed-*)
BASELINE_THROUGH="${BASELINE_THROUGH:-}" # NNN = registra (sem executar) <= NNN
DRY_RUN="${DRY_RUN:-0}"                  # 1 = só imprime o plano, não toca a DB

# ---------------------------------------------------------------------------
# String de conexão. NUNCA logamos a senha.
# ---------------------------------------------------------------------------
if [ -z "${DATABASE_URL:-}" ]; then
    _pg_user="${POSTGRES_USER:-${PGUSER:-senderzz}}"
    _pg_pass="${POSTGRES_PASSWORD:-${PGPASSWORD:-}}"
    _pg_host="${PGHOST:-localhost}"
    _pg_port="${POSTGRES_PORT:-${PGPORT:-5432}}"
    _pg_db="${POSTGRES_DB:-${PGDATABASE:-senderzz}}"
    DATABASE_URL="postgresql://${_pg_user}:${_pg_pass}@${_pg_host}:${_pg_port}/${_pg_db}?sslmode=disable"
fi
export DATABASE_URL

# psql silencioso, sem prompt, para no 1º erro de SQL.
PSQL=(psql "$DATABASE_URL" -v ON_ERROR_STOP=1 --quiet --no-psqlrc -t -A)

log()  { printf '%s\n' "→ $*"; }
warn() { printf '%s\n' "[aviso] $*" >&2; }
die()  { printf '%s\n' "[ERRO] $*" >&2; exit 1; }

[ -d "$SQL_DIR" ] || die "diretório de SQL não encontrado: $SQL_DIR"

# ---------------------------------------------------------------------------
# Extrai o prefixo numérico de um nome de arquivo (ex: 070-affiliates.sql → 70).
# Arquivos sem prefixo numérico (seed-*.sql) retornam vazio.
# ---------------------------------------------------------------------------
num_prefix() {
    local base="$1" pref
    pref="${base%%-*}"
    [[ "$pref" =~ ^[0-9]+$ ]] && printf '%s' "$((10#$pref))" || printf ''
}

# Classifica um arquivo: 'baseline-seed' | 'demo-seed' | 'schema'.
classify() {
    local base="$1"
    case "$base" in
        900-seed-*)          printf 'baseline-seed' ;;  # dados base — sempre
        910-seed-*|seed-*)   printf 'demo-seed'     ;;  # demo — gated APPLY_SEEDS
        *)                   printf 'schema'        ;;
    esac
}

# ---------------------------------------------------------------------------
# Garante a tabela de controle.
# ---------------------------------------------------------------------------
ensure_table() {
    "${PSQL[@]}" >/dev/null <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    filename    TEXT        NOT NULL PRIMARY KEY,
    checksum    TEXT        NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    baselined   BOOLEAN     NOT NULL DEFAULT FALSE
);
SQL
}

# Retorna 0 se o arquivo já está registrado.
# NOTA: usamos variáveis psql (-v) com aspas :'var' (psql faz o quoting/escaping
# do literal com segurança — sem injeção). Dois detalhes importantes:
#   1. NÃO usar dollar-quoting ($$) — em bash $$ vira o PID do shell antes do psql.
#   2. psql só interpola :'var' quando o SQL chega via STDIN (here-string), NÃO via
#      -c "..." (que roda como string única sem expansão de variável). Por isso o
#      SQL é alimentado por stdin abaixo, não por -c.
is_applied() {
    local f="$1" out
    out="$("${PSQL[@]}" -v fn="$f" <<<"SELECT 1 FROM schema_migrations WHERE filename = :'fn' LIMIT 1;")"
    [ "$out" = "1" ]
}

# Registra um arquivo como aplicado (UPSERT idempotente).
record() {
    local f="$1" sum="$2" baselined="$3"
    "${PSQL[@]}" >/dev/null -v fn="$f" -v sum="$sum" -v bl="$baselined" <<<"INSERT INTO schema_migrations (filename, checksum, baselined)
         VALUES (:'fn', :'sum', :'bl'::boolean)
         ON CONFLICT (filename) DO NOTHING;"
}

checksum_of() {
    # sha256 do conteúdo (para auditoria; não bloqueia se a ferramenta faltar).
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        printf ''
    fi
}

# ---------------------------------------------------------------------------
# Monta a lista ordenada (sort -V = ordenação de versão = numérica estável).
# while-read em vez de mapfile p/ rodar também no bash 3.2 (macOS) — o container
# postgres:16 tem bash 5, mas o runner deve ser portável.
# ---------------------------------------------------------------------------
FILES=()
while IFS= read -r _f; do
    [ -n "$_f" ] && FILES+=("$_f")
done < <(cd "$SQL_DIR" && ls -1 *.sql 2>/dev/null | sort -V)
[ "${#FILES[@]}" -gt 0 ] || die "nenhum *.sql em $SQL_DIR"

log "runner: ${#FILES[@]} arquivo(s) .sql em $SQL_DIR (ordem sort -V)"
[ -n "$BASELINE_THROUGH" ] && log "runner: BASELINE_THROUGH=$BASELINE_THROUGH (registra sem executar prefixos <= $BASELINE_THROUGH)"
[ "$APPLY_SEEDS" = "1" ] && log "runner: APPLY_SEEDS=1 (seeds demo incluídos)" || log "runner: APPLY_SEEDS=0 (seeds demo pulados)"

if [ "$DRY_RUN" = "1" ]; then
    log "DRY_RUN=1 — plano (sem tocar a DB):"
    for f in "${FILES[@]}"; do
        kind="$(classify "$f")"; pref="$(num_prefix "$f")"
        action="run"
        if [ "$kind" = "demo-seed" ] && [ "$APPLY_SEEDS" != "1" ]; then action="skip(seed-demo)"
        elif [ -n "$BASELINE_THROUGH" ] && [ -n "$pref" ] && [ "$pref" -le "$((10#$BASELINE_THROUGH))" ]; then action="baseline(record-only)"
        fi
        printf '    %-40s %-14s %s\n' "$f" "[$kind]" "→ $action"
    done
    exit 0
fi

ensure_table

# ---------------------------------------------------------------------------
# GUARDA ANTI-DESTRUIÇÃO (fail-closed).
# Os arquivos base 020-070 começam com `DROP TABLE ... CASCADE` e recriam vazio.
# Numa DB JÁ POPULADA mas SEM tracking (ex.: volume antigo criado pelo método
# initdb.d que este runner substitui), uma 1ª execução SEM BASELINE_THROUGH
# reaplicaria esses DROPs e APAGARIA dados financeiros (tpc_carteira/transacoes).
# Abortamos esse caso exato:
#   - schema_migrations vazio (nunca adotado)  E
#   - BASELINE_THROUGH não informado            E
#   - uma tabela base sentinela já existe (DB populada por outro caminho)
# DB nova (sentinela ausente) → segue normal. DB já adotada (count>0) → segue.
# ---------------------------------------------------------------------------
mig_count="$("${PSQL[@]}" -c "SELECT count(*) FROM schema_migrations;")"
if [ "$mig_count" = "0" ] && [ -z "$BASELINE_THROUGH" ]; then
    sentinel="$("${PSQL[@]}" -c "SELECT (to_regclass('public.tpc_carteira') IS NOT NULL)::int;")"
    if [ "$sentinel" = "1" ]; then
        die "DB já populada (tpc_carteira existe) mas sem schema_migrations. \
Os schemas base (020-070) fazem DROP TABLE CASCADE e APAGARIAM dados. \
Adote o tracking UMA vez sem destruir: BASELINE_THROUGH=360 bash infra/scripts/db-migrate.sh \
(ou o maior prefixo já aplicado). Só então rode normalmente."
    fi
fi

applied_now=0
baselined_now=0
skipped=0

for f in "${FILES[@]}"; do
    kind="$(classify "$f")"
    pref="$(num_prefix "$f")"
    path="$SQL_DIR/$f"

    # Seeds demo: só com APPLY_SEEDS=1 (fail-closed em prod).
    if [ "$kind" = "demo-seed" ] && [ "$APPLY_SEEDS" != "1" ]; then
        skipped=$((skipped + 1)); continue
    fi

    # Já registrado → pula (idempotência principal).
    if is_applied "$f"; then
        skipped=$((skipped + 1)); continue
    fi

    sum="$(checksum_of "$path")"

    # Baseline: registra SEM executar (adoção em DB já populada).
    if [ -n "$BASELINE_THROUGH" ] && [ -n "$pref" ] && [ "$pref" -le "$((10#$BASELINE_THROUGH))" ]; then
        record "$f" "$sum" TRUE
        baselined_now=$((baselined_now + 1))
        log "baseline (record-only): $f"
        continue
    fi

    # Aplica de fato. ON_ERROR_STOP=1 → sai != 0 no 1º erro de SQL.
    # Captura o EXIT REAL do psql (sem pipe — pipe mascararia o exit). NÃO registra
    # se falhar. stdout do arquivo vai p/ /dev/null (alguns .sql têm SELECT/INSERT
    # que imprimem linhas; isso poluiria a saída de progresso do runner). stderr
    # (NOTICE/ERROR) é preservado p/ diagnóstico.
    if "${PSQL[@]}" -f "$path" >/dev/null; then
        record "$f" "$sum" FALSE
        applied_now=$((applied_now + 1))
        log "aplicado: $f"
    else
        die "falha ao aplicar $f (psql exit != 0). schema_migrations NÃO atualizado para este arquivo."
    fi
done

total="$("${PSQL[@]}" -c "SELECT count(*) FROM schema_migrations;")"
log "concluído: ${applied_now} novo(s) aplicado(s), ${baselined_now} baseline(s), ${skipped} pulado(s). Total em schema_migrations: ${total}."
exit 0
