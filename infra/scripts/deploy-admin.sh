#!/usr/bin/env bash
# Deploy falk-admin (Go API) + admin-ui (React SPA) no VPS.
# Uso: bash infra/scripts/deploy-admin.sh
# Pré-requisito: ssh-agent com chave para o VPS configurado.
#
# AUDIT-2026-07-31: reescrito de vez — apontava pro stack morto
# (/opt/senderzz + docker-compose.yml + app.senderzz.com.br, WordPress
# ainda no meio). O stack real em produção é /opt/falk + docker-compose.falk.yml
# + app.falklog.com.br, 100% Go, sem WordPress. Alinhado com o deploy manual
# já validado (rsync + docker compose build/up + migrate-runner via docker run).

set -euo pipefail

VPS_HOST="${VPS_HOST:-93.127.141.6}"
VPS_USER="${VPS_USER:-administrator}"
VPS_DIR="${VPS_DIR:-/opt/falk}"
VPS_SSH_PORT="${VPS_SSH_PORT:-10037}"

# AUDIT INFRA-deploy-script-ssh-no-host-key-check: NÃO usar StrictHostKeyChecking=no
# (aceita QUALQUER host key → superfície de MITM). Default = 'accept-new' (TOFU:
# aceita na 1ª conexão, REJEITA se a key mudar depois). Em produção, exporte
# SSH_STRICT_HOST_KEY=yes e pré-popule ~/.ssh/known_hosts com a fingerprint do VPS
# (verificada por SHA256) p/ rejeitar até a 1ª key desconhecida.
SSH_STRICT_HOST_KEY="${SSH_STRICT_HOST_KEY:-accept-new}"
SSH_OPTS="-p ${VPS_SSH_PORT} -o StrictHostKeyChecking=${SSH_STRICT_HOST_KEY}"
SSH="ssh ${SSH_OPTS} ${VPS_USER}@${VPS_HOST}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "==> [1/5] Sincronizando código admin Go para VPS..."
rsync -az --delete \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/go/admin/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/go/admin/"

echo "==> [2/5] Sincronizando admin-ui para VPS..."
rsync -az --delete \
  -e "ssh ${SSH_OPTS}" \
  --exclude node_modules \
  --exclude dist \
  "${ROOT}/admin-ui/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/admin-ui/"

echo "==> [3/5] Sincronizando docker-compose.falk.yml + infra/postgres..."
rsync -az \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/docker/docker-compose.falk.yml" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/docker/docker-compose.falk.yml"

# seed-safety: EXCLUI seeds de demo do push (rsync ignora .gitignore) — demo data
# nunca vai p/ prod. (Mesmo se fossem, db-migrate.sh os pula sem APPLY_SEEDS=1.)
rsync -az --delete \
  --exclude='*seed-dev-demo.sql' --exclude='seed-demo-*.sql' --exclude='seed-cod-wallet-*.sql' --exclude='910-seed-dev-demo.sql' \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/postgres/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/postgres/"

rsync -az \
  -e "ssh ${SSH_OPTS}" \
  "${ROOT}/infra/docker/migrate-entrypoint.sh" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/docker/migrate-entrypoint.sh"

echo "==> [4/5] Build e recreate no VPS (docker-compose.falk.yml)..."
$SSH bash -s <<REMOTE
set -euo pipefail
cd ${VPS_DIR}/infra/docker

# Garante que ADMIN_JWT_SECRET está no .env
if ! grep -q "ADMIN_JWT_SECRET" .env 2>/dev/null; then
  SECRET=\$(openssl rand -hex 32)
  echo "ADMIN_JWT_SECRET=\${SECRET}" >> .env
  echo "[aviso] ADMIN_JWT_SECRET gerado e adicionado ao .env"
fi

docker compose -f docker-compose.falk.yml build admin-service admin-ui
docker compose -f docker-compose.falk.yml up -d --force-recreate --no-deps admin-service admin-ui

# force-recreate troca o IP Docker do admin-service. O nginx mantém o IP
# resolvido anteriormente até recarregar a configuração, causando HTTP 502.
docker exec falk-gateway nginx -t
docker exec falk-gateway nginx -s reload

echo "Aguardando admin-service..."
for i in \$(seq 1 20); do
  if docker exec falk-gateway sh -c 'wget -qO- http://falk-admin:8087/healthz' > /dev/null 2>&1; then
    echo "admin-service UP"
    break
  fi
  sleep 3
done
REMOTE

echo "==> [5/5] Aplicando TODAS as migrações no Postgres (migrate-runner idempotente)..."
# AUDIT INFRA-migration-schema-alphabetical-fragility: roda o migrate-runner como
# container efêmero na rede falk_falk-net — aplica TODOS os NNN-*.sql na ordem
# numérica, versionado em schema_migrations (pula os já feitos). Idêntico ao
# processo manual já validado nesta sessão.
$SSH bash -s <<REMOTE
set -euo pipefail
cd ${VPS_DIR}
source infra/docker/.env
docker run --rm --network falk_falk-net \
  -e DATABASE_URL="postgresql://\${POSTGRES_USER:-falk}:\${POSTGRES_PASSWORD}@falk-postgres:5432/\${POSTGRES_DB:-falk}?sslmode=disable" \
  -v ${VPS_DIR}:/repo:ro \
  postgres:16 bash /repo/infra/docker/migrate-entrypoint.sh
REMOTE

echo ""
echo "============================================================"
echo "Deploy concluído!"
echo ""
echo "Verifique:"
echo "  https://app.falklog.com.br/admin/"
echo "============================================================"
