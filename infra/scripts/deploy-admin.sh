#!/usr/bin/env bash
# Deploy senderzz-admin (Go API) + admin-ui (React SPA) no VPS.
# Uso: bash infra/scripts/deploy-admin.sh
# Pré-requisito: ssh-agent com chave para o VPS configurado.

set -euo pipefail

VPS_HOST="${VPS_HOST:-93.127.141.6}"
VPS_USER="${VPS_USER:-root}"
VPS_DIR="${VPS_DIR:-/opt/senderzz}"
SSH="ssh -o StrictHostKeyChecking=no ${VPS_USER}@${VPS_HOST}"
SCP="scp -o StrictHostKeyChecking=no"
RSYNC="rsync -az --delete -e 'ssh -o StrictHostKeyChecking=no'"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "==> [1/5] Sincronizando código admin Go para VPS..."
rsync -az --delete \
  -e "ssh -o StrictHostKeyChecking=no" \
  "${ROOT}/go/admin/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/go/admin/"

echo "==> [2/5] Sincronizando admin-ui para VPS..."
rsync -az --delete \
  -e "ssh -o StrictHostKeyChecking=no" \
  --exclude node_modules \
  --exclude dist \
  "${ROOT}/admin-ui/" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/admin-ui/"

echo "==> [3/5] Sincronizando docker-compose + infra + nginx..."
rsync -az \
  -e "ssh -o StrictHostKeyChecking=no" \
  "${ROOT}/infra/docker/docker-compose.yml" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/docker/docker-compose.yml"

rsync -az \
  -e "ssh -o StrictHostKeyChecking=no" \
  "${ROOT}/infra/postgres/schema-admin.sql" \
  "${VPS_USER}@${VPS_HOST}:${VPS_DIR}/infra/postgres/schema-admin.sql"

rsync -az \
  -e "ssh -o StrictHostKeyChecking=no" \
  "${ROOT}/infra/nginx/senderzz-gateway.conf" \
  "${VPS_USER}@${VPS_HOST}:/etc/nginx/conf.d/senderzz-gateway.conf"

$SSH bash -s <<'NGINXTEST'
set -euo pipefail
nginx -t && nginx -s reload
echo "[ok] nginx reloaded"
NGINXTEST

echo "==> [4/5] Build e start no VPS..."
$SSH bash -s <<'REMOTE'
set -euo pipefail
cd /opt/senderzz/infra/docker

# Garante que ADMIN_JWT_SECRET está no .env
if ! grep -q "ADMIN_JWT_SECRET" .env 2>/dev/null; then
  SECRET=$(openssl rand -hex 32)
  echo "ADMIN_JWT_SECRET=${SECRET}" >> .env
  echo "[aviso] ADMIN_JWT_SECRET gerado e adicionado ao .env"
fi

# Build + start dos dois serviços
docker compose build --no-cache admin-service admin-ui
docker compose up -d admin-service admin-ui

# Aguarda admin-service estar healthy
echo "Aguardando admin-service..."
for i in $(seq 1 20); do
  if curl -sf http://localhost:8087/healthz > /dev/null; then
    echo "admin-service UP"
    break
  fi
  sleep 3
done
REMOTE

echo "==> [5/5] Aplicando schema-admin no Postgres..."
$SSH bash -s <<'REMOTE'
set -euo pipefail
cd /opt/senderzz/infra/docker
source .env 2>/dev/null || true

docker compose exec -T postgres psql \
  -U "${POSTGRES_USER:-senderzz}" \
  -d "${POSTGRES_DB:-senderzz}" \
  < /opt/senderzz/infra/postgres/schema-admin.sql

echo "Schema-admin aplicado."
REMOTE

echo ""
echo "============================================================"
echo "Deploy concluído!"
echo ""
echo "Verifique:"
echo "  curl https://app.senderzz.com.br/health/admin"
echo "  https://app.senderzz.com.br/admin/"
echo ""
echo "Para criar o primeiro admin:"
echo "  curl -X POST https://app.senderzz.com.br/wp-json/senderzz/v1/admin/onboarding/setup/create-admin \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"email\":\"SEU_EMAIL\",\"senha\":\"SUA_SENHA\",\"nome\":\"Admin Senderzz\"}'"
echo "============================================================"
