#!/usr/bin/env bash
# =============================================================================
# AUDIT INFRA-admin-only-deploy-path
# deploy-service.sh — deploy GENÉRICO de qualquer serviço Go no VPS.
#
# PROBLEMA: só existia deploy-admin.sh (cobria admin + admin-ui). Os outros 6
# serviços (motoboy, wallet, affiliates, labels, portal, orders) não tinham
# automação de deploy. Este script generaliza o mesmo pipeline para qualquer um.
#
# USO:
#   bash infra/scripts/deploy-service.sh <serviço>
#   ex.: bash infra/scripts/deploy-service.sh motoboy
#        bash infra/scripts/deploy-service.sh wallet
#   serviços válidos: admin motoboy wallet affiliates labels portal orders
#
# O QUE FAZ:
#   1. rsync de go/<serviço>/ p/ VPS.
#   2. rsync do docker-compose.yml + schemas + nginx (infra compartilhada).
#   3. docker compose build --no-cache <serviço>-service + up -d.
#   4. health check no /health (ou /healthz p/ admin) do serviço.
#
# SSH: AUDIT INFRA-deploy-script-ssh-no-host-key-check — default accept-new (TOFU).
#   Produção: SSH_STRICT_HOST_KEY=yes + known_hosts pré-populado.
#
# VALIDAÇÃO:  bash -n infra/scripts/deploy-service.sh
# =============================================================================
set -euo pipefail

SERVICE="${1:-}"
VALID="admin motoboy wallet affiliates labels portal orders"
case " $VALID " in
    *" $SERVICE "*) ;;  # ok
    *)
        echo "ERRO: serviço inválido: '${SERVICE:-<vazio>}'." >&2
        echo "Uso: bash infra/scripts/deploy-service.sh <serviço>" >&2
        echo "Válidos: $VALID" >&2
        exit 1
        ;;
esac

VPS_HOST="${VPS_HOST:-93.127.141.6}"
VPS_USER="${VPS_USER:-root}"
VPS_DIR="${VPS_DIR:-/opt/senderzz}"

# AUDIT INFRA-deploy-script-ssh-no-host-key-check
# NUNCA StrictHostKeyChecking=no (aceita QUALQUER host key → MITM). Default aqui =
# 'accept-new' (TOFU: aceita na 1ª conexão, REJEITA se a key mudar depois).
#
# PRODUÇÃO (verificação forte via known_hosts):
#   1. Capture e VERIFIQUE a fingerprint do VPS por canal confiável (console do
#      provedor), comparando o SHA256:
#         ssh-keyscan -t ed25519 "$VPS_HOST" | ssh-keygen -lf -
#   2. Após conferir, fixe a chave em ~/.ssh/known_hosts:
#         ssh-keyscan -t ed25519 "$VPS_HOST" >> ~/.ssh/known_hosts
#   3. Rode o deploy com checagem estrita (rejeita até a 1ª key desconhecida):
#         SSH_STRICT_HOST_KEY=yes bash infra/scripts/deploy-service.sh <serviço>
SSH_STRICT_HOST_KEY="${SSH_STRICT_HOST_KEY:-accept-new}"
SSH_OPTS="-o StrictHostKeyChecking=${SSH_STRICT_HOST_KEY}"
SSH="ssh ${SSH_OPTS} ${VPS_USER}@${VPS_HOST}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

COMPOSE_SVC="${SERVICE}-service"
# admin usa /healthz; os demais expõem /health (ver cmd/server de cada serviço).
HEALTH_PATH="/health"; [ "$SERVICE" = "admin" ] && HEALTH_PATH="/healthz"
# Porta interna por serviço (igual ao docker-compose.yml).
case "$SERVICE" in
    motoboy)    PORT=8080 ;;
    wallet)     PORT=8081 ;;
    affiliates) PORT=8083 ;;
    labels)     PORT=8084 ;;
    portal)     PORT=8085 ;;
    orders)     PORT=8086 ;;
    admin)      PORT=8087 ;;
esac

echo "==> [1/4] Sincronizando go/${SERVICE} para VPS..."
rsync -az --delete \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/go/${SERVICE}/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/go/${SERVICE}/"

echo "==> [2/4] Sincronizando infra compartilhada (compose + schemas + nginx)..."
rsync -az -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/docker/docker-compose.yml" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/docker/docker-compose.yml"
# seed-safety: EXCLUI seeds de demo do push (rsync ignora .gitignore) — demo data
# nunca vai p/ prod. (Mesmo se fossem, db-migrate.sh os pula sem APPLY_SEEDS=1.)
rsync -az --delete \
  --exclude='*seed-dev-demo.sql' --exclude='seed-demo-*.sql' --exclude='seed-cod-wallet-*.sql' --exclude='910-seed-dev-demo.sql' \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/postgres/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/postgres/"
rsync -az -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/docker/migrate-entrypoint.sh" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/docker/migrate-entrypoint.sh"

echo "==> [3/4] Build + start de ${COMPOSE_SVC} no VPS..."
# ADOPT_BASELINE: na 1ª adoção do runner numa DB JÁ POPULADA sem schema_migrations
# (volume antigo do método initdb.d), exporte ADOPT_BASELINE=360 (ou o maior prefixo
# já aplicado) p/ registrar sem reaplicar os DROP CASCADE base. Vazio = execução
# normal (DB nova ou já adotada). A guarda do db-migrate.sh aborta o caso perigoso.
ADOPT_BASELINE="${ADOPT_BASELINE:-}"
$SSH bash -s "$COMPOSE_SVC" "$PORT" "$HEALTH_PATH" "$ADOPT_BASELINE" <<'REMOTE'
set -euo pipefail
COMPOSE_SVC="$1"; PORT="$2"; HEALTH_PATH="$3"; ADOPT_BASELINE="${4:-}"
cd /opt/senderzz/infra/docker

# Aplica migrações pendentes antes (migrate-runner é idempotente / exit 0).
docker compose up -d postgres
docker compose run --rm -e BASELINE_THROUGH="${ADOPT_BASELINE}" migrate-runner \
  || { echo "[ERRO] migrate-runner falhou"; exit 1; }

docker compose build --no-cache "$COMPOSE_SVC"
docker compose up -d "$COMPOSE_SVC"

echo "==> [4/4] Health check ${COMPOSE_SVC} em ${HEALTH_PATH}..."
for i in $(seq 1 20); do
  if curl -sf "http://localhost:${PORT}${HEALTH_PATH}" > /dev/null; then
    echo "[ok] ${COMPOSE_SVC} UP (porta ${PORT})"
    exit 0
  fi
  sleep 3
done
echo "[ERRO] ${COMPOSE_SVC} não respondeu ao health check em ${PORT}${HEALTH_PATH}" >&2
exit 1
REMOTE

echo ""
echo "============================================================"
echo "Deploy de ${SERVICE} concluído."
echo "============================================================"
