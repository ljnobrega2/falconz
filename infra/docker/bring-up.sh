#!/usr/bin/env bash
# ============================================================
# Sobe o stack mínimo do painel admin (Postgres + API + UI) e
# cria o primeiro admin a partir das credenciais do .env.
#
# Uso:  cd infra/docker && ./bring-up.sh
# Requer: Docker Desktop instalado e rodando.
# ============================================================
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v docker >/dev/null 2>&1; then
  echo "❌ Docker não instalado. Instale Docker Desktop: https://www.docker.com/products/docker-desktop"
  exit 1
fi
if [ ! -f .env ]; then
  echo "❌ .env ausente neste diretório."; exit 1
fi
set -a; . ./.env; set +a

ADMIN_PORT="${ADMIN_PORT:-8087}"
ADMIN_UI_PORT="${ADMIN_UI_PORT:-8089}"
BASE="http://localhost:${ADMIN_PORT}/wp-json/senderzz/v1/admin"

echo "▶ Subindo postgres + admin-service + admin-ui (build na 1ª vez, pode demorar)…"
docker compose up -d --build postgres admin-service admin-ui

echo "▶ Aguardando admin-service responder em :${ADMIN_PORT}…"
for i in $(seq 1 60); do
  if curl -fsS "${BASE}/onboarding/setup-status" >/dev/null 2>&1; then
    echo "  ✓ API no ar"; break
  fi
  sleep 2
  [ "$i" = "60" ] && { echo "❌ API não subiu a tempo. Veja: docker compose logs admin-service"; exit 1; }
done

echo "▶ Criando primeiro admin (${ADMIN_EMAIL})…"
RESP=$(curl -fsS -X POST "${BASE}/onboarding/setup/create-admin" \
  -H 'Content-Type: application/json' \
  -d "{\"nome\":\"Lucas\",\"email\":\"${ADMIN_EMAIL}\",\"senha\":\"${ADMIN_SENHA}\"}" || true)

if echo "$RESP" | grep -q '"already_setup"'; then
  echo "  ℹ Admin já existia — use suas credenciais do .env para logar."
elif echo "$RESP" | grep -q '"ok":true'; then
  echo "  ✓ Admin criado."
else
  echo "  ⚠ Resposta inesperada: $RESP"
fi

echo ""
echo "============================================================"
echo "  Painel:  http://localhost:${ADMIN_UI_PORT}/admin/"
echo "  Login:   ${ADMIN_EMAIL}"
echo "  Senha:   (a do .env)"
echo "============================================================"
echo "Parar tudo:  docker compose down"
echo "Logs:        docker compose logs -f admin-service"
