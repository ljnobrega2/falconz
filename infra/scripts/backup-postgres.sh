#!/usr/bin/env bash
# =============================================================================
# AUDIT INFRA-postgres-backup-absent (CRIT)
# backup-postgres.sh — BACKUP automatizado do Postgres da FALKZ/Senderzz.
#
# Sem este script o banco financeiro (tpc_carteira, tpc_transacoes,
# senderzz_revenue, sz_orders...) NÃO tinha disaster recovery: RPO/RTO indefinido.
# Este backup é o piso de recuperação (RPO alvo = 1 dia com cron diário; reduza a
# janela aumentando a frequência do cron). PITR via WAL archiving é passo separado
# (ver "PRÓXIMOS PASSOS" no rodapé) — este script entrega o dump lógico diário.
#
# O QUE FAZ:
#   1. Resolve a conexão ($DATABASE_URL ou POSTGRES_* de infra/docker/.env,
#      igual ao scripts/db-migrate.sh — mesma convenção, sem surpresa).
#   2. pg_dump -Fc (formato custom, comprimido, restaurável seletivamente).
#   3. Grava em $BACKUP_DIR/senderzz-YYYYmmdd-HHMMSS.dump (UTC no nome).
#   4. Gera checksum .sha256 ao lado (integridade verificável no restore).
#   5. Retenção: apaga dumps + checksums com mais de $RETENTION_DAYS dias.
#   6. (Opcional) Upload p/ S3 se $S3_BUCKET definido — gated por env, roda sem AWS.
#   7. Fail-loud: qualquer erro aborta com exit != 0 (cron manda e-mail/alerta).
#
# VARIÁVEIS DE AMBIENTE:
#   DATABASE_URL      string de conexão completa (tem prioridade). Opcional.
#   BACKUP_DIR        diretório de destino. Default: /var/backups/senderzz/postgres
#   RETENTION_DAYS    dias de retenção local. Default: 30
#   S3_BUCKET         bucket p/ upload (ex.: s3://meu-bucket/senderzz/pg). Opcional.
#   S3_SSE            algoritmo de criptografia server-side do S3. Default: AES256
#   PGDUMP_BIN        caminho do pg_dump. Default: pg_dump no PATH.
#
# USO (manual):
#   bash infra/scripts/backup-postgres.sh
#
# USO (cron diário 02:00 UTC = 23:00 BRT, fora de pico — RPO ~1 dia):
#   crontab -e  e adicione (ajuste o caminho do repo e o destino):
#     0 2 * * *  BACKUP_DIR=/var/backups/senderzz/postgres RETENTION_DAYS=30 \
#       /opt/senderzz/infra/scripts/backup-postgres.sh >> /var/log/senderzz-backup.log 2>&1
#   Com upload S3 (criptografado, fora da VPS = sobrevive a perda do host):
#     0 2 * * *  S3_BUCKET=s3://SEU-BUCKET/senderzz/pg BACKUP_DIR=/var/backups/senderzz/postgres \
#       /opt/senderzz/infra/scripts/backup-postgres.sh >> /var/log/senderzz-backup.log 2>&1
#
# RESTAURAR (disaster recovery — RTO depende do tamanho do dump):
#   1. Conferir integridade:  sha256sum -c senderzz-AAAAmmdd-HHMMSS.dump.sha256
#   2. Restaurar em DB limpo: pg_restore --clean --if-exists --no-owner \
#        -d "postgresql://USER:PASS@HOST:5432/senderzz" senderzz-AAAAmmdd-HHMMSS.dump
#   (dump -Fc permite restore seletivo de uma tabela com  -t tabela)
#
# VALIDAÇÃO DE SINTAXE (sem executar):  bash -n infra/scripts/backup-postgres.sh
# =============================================================================
set -euo pipefail

# Locale fixo p/ datas/sort determinísticos (mesma postura do db-migrate.sh).
export LC_ALL=C

# ---------------------------------------------------------------------------
# 0. Caminhos do repo + defaults
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="${ROOT}/infra/docker/.env"

BACKUP_DIR="${BACKUP_DIR:-/var/backups/senderzz/postgres}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
S3_SSE="${S3_SSE:-AES256}"
PGDUMP_BIN="${PGDUMP_BIN:-pg_dump}"

# ---------------------------------------------------------------------------
# 1. Pré-requisitos
# ---------------------------------------------------------------------------
command -v "$PGDUMP_BIN" >/dev/null 2>&1 || {
    echo "ERRO: '$PGDUMP_BIN' não encontrado no PATH. Instale o cliente do PostgreSQL." >&2
    exit 1
}
# sha256sum (Linux) ou shasum (macOS) — resolve qual usar.
if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
else
    echo "ERRO: nem 'sha256sum' nem 'shasum' encontrados — não dá p/ gerar checksum." >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# 2. Resolve a string de conexão ($DATABASE_URL) — espelha o db-migrate.sh
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
# 3. Destino + nome do arquivo (timestamp UTC determinístico)
# ---------------------------------------------------------------------------
mkdir -p "$BACKUP_DIR" || {
    echo "ERRO: não consegui criar/acessar BACKUP_DIR=$BACKUP_DIR" >&2
    exit 1
}
STAMP="$(date -u +%Y%m%d-%H%M%S)"
DUMP_FILE="${BACKUP_DIR}/senderzz-${STAMP}.dump"
SHA_FILE="${DUMP_FILE}.sha256"

# ---------------------------------------------------------------------------
# 4. pg_dump -Fc (custom/comprimido). Em arquivo temporário primeiro: só
#    renomeia p/ o nome final se o dump terminou OK — nunca deixa dump truncado
#    com nome "válido" (a retenção e o restore confiariam num arquivo quebrado).
# ---------------------------------------------------------------------------
TMP_FILE="${DUMP_FILE}.partial"
echo "→ Gerando dump: $DUMP_FILE"
if ! "$PGDUMP_BIN" --format=custom --no-owner --no-privileges \
        --file="$TMP_FILE" "$DATABASE_URL"; then
    echo "ERRO: pg_dump falhou — backup ABORTADO." >&2
    rm -f "$TMP_FILE"
    exit 1
fi
mv -f "$TMP_FILE" "$DUMP_FILE"

# ---------------------------------------------------------------------------
# 5. Checksum (integridade verificável no restore)
# ---------------------------------------------------------------------------
( cd "$BACKUP_DIR" && $SHA_CMD "$(basename "$DUMP_FILE")" > "$SHA_FILE" )
echo "→ Checksum: $SHA_FILE"

DUMP_BYTES="$(wc -c < "$DUMP_FILE" | tr -d ' ')"
echo "✓ Dump concluído: $(basename "$DUMP_FILE") (${DUMP_BYTES} bytes)"

# ---------------------------------------------------------------------------
# 6. (Opcional) Upload S3 criptografado — só se $S3_BUCKET definido e 'aws' existe.
# ---------------------------------------------------------------------------
if [ -n "${S3_BUCKET:-}" ]; then
    if command -v aws >/dev/null 2>&1; then
        echo "→ Enviando p/ S3: ${S3_BUCKET%/}/"
        aws s3 cp "$DUMP_FILE" "${S3_BUCKET%/}/$(basename "$DUMP_FILE")" \
            --sse "$S3_SSE"
        aws s3 cp "$SHA_FILE"  "${S3_BUCKET%/}/$(basename "$SHA_FILE")" \
            --sse "$S3_SSE"
        echo "✓ Upload S3 concluído (sse=$S3_SSE)."
    else
        # Fail-loud: o operador PEDIU S3 mas 'aws' não está disponível → não silenciar.
        echo "ERRO: S3_BUCKET definido mas 'aws' não está no PATH. Backup local OK, upload FALHOU." >&2
        exit 1
    fi
else
    echo "→ S3_BUCKET não definido — pulando upload (backup só local em $BACKUP_DIR)."
fi

# ---------------------------------------------------------------------------
# 7. Retenção: remove dumps + checksums com mais de $RETENTION_DAYS dias.
#    Só mexe nos artefatos deste script (padrão senderzz-*.dump*) — nunca varre
#    o diretório inteiro às cegas.
# ---------------------------------------------------------------------------
echo "→ Aplicando retenção: removendo backups com mais de ${RETENTION_DAYS} dias em $BACKUP_DIR"
removidos=0
while IFS= read -r -d '' old; do
    rm -f "$old"
    echo "  • removido (expirado): $(basename "$old")"
    removidos=$((removidos + 1))
done < <(find "$BACKUP_DIR" -maxdepth 1 -type f \
            \( -name 'senderzz-*.dump' -o -name 'senderzz-*.dump.sha256' \) \
            -mtime "+${RETENTION_DAYS}" -print0)

echo ""
echo "✓ Backup OK. Arquivo: $DUMP_FILE | Expirados removidos: $removidos | Retenção: ${RETENTION_DAYS}d"

# =============================================================================
# PRÓXIMOS PASSOS (não cobertos por este script — backlog de DR):
#   • WAL archiving p/ PITR (archive_command + base backup) → RPO ~minutos.
#   • Réplica em outra AZ/host (streaming replication / hot standby).
#   • Restore-test semanal automático em staging + verify-migration.sh.
#   • SLA documentado: RPO/RTO no RUNBOOK.md.
# =============================================================================
