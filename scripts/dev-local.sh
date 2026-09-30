#!/usr/bin/env bash
# =============================================================================
# Senderzz — stack DEV local SEM WordPress (admin + portal Go + React + tunnel)
# AUDIT-2026-06-18: roda sem Docker (hypervisor quebrado no Mac). Usa Postgres
# nativo (brew), serviços Go via `go run` e Vite dev, exposto por cloudflared.
#
# Uso:
#   scripts/dev-local.sh up      # sobe tudo + imprime URL do tunnel
#   scripts/dev-local.sh status  # health de cada peça + URL atual
#   scripts/dev-local.sh url     # só a URL do tunnel
#   scripts/dev-local.sh down     # derruba go/vite/tunnel (postgres fica)
# =============================================================================
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG=/tmp/senderzz-dev
mkdir -p "$LOG"
export PATH="/usr/local/bin:/usr/local/opt/postgresql@16/bin:$PATH"
export GOFLAGS=-mod=mod

load_env() { set -a; . "$ROOT/infra/docker/.env"; set +a
  export DATABASE_URL="postgresql://senderzz:${POSTGRES_PASSWORD}@localhost:5432/senderzz?sslmode=disable"; }

url() { grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG/tunnel.log" 2>/dev/null | tail -1; }

up() {
  load_env
  # 1. Postgres (brew service)
  pg_isready -h localhost -p 5432 >/dev/null 2>&1 || brew services start postgresql@16 >/dev/null 2>&1
  for i in $(seq 1 20); do pg_isready -h localhost -p 5432 >/dev/null 2>&1 && break; sleep 1; done
  # 2. go/admin :8087
  if ! curl -s -m2 http://localhost:8087/healthz >/dev/null 2>&1; then
    ( cd "$ROOT/go/admin" && PORT=8087 APP_BASE_URL=http://localhost:5173 \
      nohup go run ./cmd/server > "$LOG/admin_svc.log" 2>&1 & )
    for i in $(seq 1 30); do curl -s -m2 http://localhost:8087/healthz >/dev/null 2>&1 && break; sleep 1; done
  fi
  # 2b. go/portal :8085 (API do painel usuário)
  if ! curl -s -m2 http://localhost:8085/health >/dev/null 2>&1; then
    ( cd "$ROOT/go/portal" && PORT=8085 \
      nohup go run ./cmd/server > "$LOG/portal_svc.log" 2>&1 & )
    for i in $(seq 1 30); do curl -s -m2 http://localhost:8085/health >/dev/null 2>&1 && break; sleep 1; done
  fi
  # 2c. go/cron (runner de jobs idempotentes; sem porta → pidfile)
  if ! kill -0 "$(cat "$LOG/cron.pid" 2>/dev/null)" 2>/dev/null; then
    ( cd "$ROOT/go/cron" && nohup go run ./cmd/server > "$LOG/cron_svc.log" 2>&1 & echo $! > "$LOG/cron.pid" )
  fi
  # 3. Vite :5173 (SPA admin + proxy /wp-json -> :8087)
  if ! curl -s -m2 -o /dev/null http://localhost:5173/admin/ 2>/dev/null; then
    ( cd "$ROOT/admin-ui" && nohup npm run dev -- --port 5173 --strictPort > "$LOG/vite.log" 2>&1 & )
    for i in $(seq 1 20); do curl -s -m2 -o /dev/null http://localhost:5173/admin/ 2>/dev/null && break; sleep 1; done
  fi
  # 4. cloudflared tunnel
  if [ -z "$(url)" ] || ! pgrep -f 'cloudflared tunnel' >/dev/null; then
    pkill -f 'cloudflared tunnel' 2>/dev/null; sleep 1
    nohup cloudflared tunnel --url http://localhost:5173 > "$LOG/tunnel.log" 2>&1 &
    for i in $(seq 1 25); do [ -n "$(url)" ] && break; sleep 1; done
  fi
  status
}

status() {
  echo "Postgres : $(pg_isready -h localhost -p 5432 2>/dev/null || echo DOWN)"
  echo "go/admin : $(curl -s -m3 http://localhost:8087/healthz 2>/dev/null || echo DOWN)"
  echo "go/portal: $(curl -s -m3 http://localhost:8085/health 2>/dev/null || echo DOWN)"
  echo "go/cron  : $(kill -0 "$(cat "$LOG/cron.pid" 2>/dev/null)" 2>/dev/null && echo 'rodando' || echo DOWN)"
  echo "Vite     : $(curl -s -m3 -o /dev/null -w '%{http_code}' http://localhost:5173/admin/ 2>/dev/null || echo DOWN)"
  local u; u="$(url)"
  echo "Tunnel   : ${u:-DOWN}"
  [ -n "$u" ] && echo "PAINEL   : ${u}/admin/"
}

down() { pkill -f 'cloudflared tunnel' 2>/dev/null; pkill -f 'go/admin' 2>/dev/null
  pkill -f 'exe/server' 2>/dev/null; pkill -f 'vite' 2>/dev/null; echo "derrubado (postgres mantido)"; }

case "${1:-up}" in
  up) up ;; status) status ;; url) url ;; down) down ;;
  *) echo "uso: $0 {up|status|url|down}"; exit 1 ;;
esac
