#!/usr/bin/env bash
# tunnel-keepalive.sh — mantém os 3 cloudflared quick-tunnels vivos (dono dormindo).
# quick-tunnel morre após horas (loop "control stream failure": processo vivo, para
# de servir). Este loop detecta tunnel REALMENTE morto e restarta só aquele, gravando
# a URL CORRENTE em CURRENT-URLS.txt (URL rotaciona no restart → o arquivo estável é a
# fonte de verdade pro dono pegar a URL viva: `cat /tmp/senderzz-dev/CURRENT-URLS.txt`).
#
# CONSERVADOR (não churna tunnel saudável): 3 tentativas de curl por check + DEBOUNCE
# de 2 ciclos consecutivos mortos antes de restartar + GRACE de 1 ciclo pós-restart.
#
# Uso: nohup bash scripts/tunnel-keepalive.sh > /tmp/senderzz-dev/keepalive.log 2>&1 &
# Bounded: ~6h (180 ciclos × 120s).
set -u
LOG=/tmp/senderzz-dev
OUT="$LOG/CURRENT-URLS.txt"
mkdir -p "$LOG"

# port|logfile|healthpath|nome
TUN=( "5173|tunnel|/admin/|admin" "5174|portal-tunnel|/portal/|portal" "5175|checkout-tunnel|/|checkout" )
declare -A FAILS=( [5173]=0 [5174]=0 [5175]=0 )
declare -A GRACE=( [5173]=0 [5174]=0 [5175]=0 )

url_of() { grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG/$1.log" 2>/dev/null | tail -1; }

# saudável se algum dos 3 curls der 200/302
healthy() {
  local u="$1" path="$2" code
  for _ in 1 2 3; do
    code=$(curl -s -m8 -o /dev/null -w '%{http_code}' "${u}${path}" 2>/dev/null)
    { [ "$code" = "200" ] || [ "$code" = "302" ]; } && return 0
    sleep 2
  done
  return 1
}

restart_one() {
  local port="$1" logf="$2"
  pkill -f "cloudflared tunnel --url http://localhost:$port" 2>/dev/null
  sleep 2
  nohup cloudflared tunnel --url "http://localhost:$port" > "$LOG/$logf.log" 2>&1 &
  for _ in $(seq 1 20); do [ -n "$(url_of "$logf")" ] && break; sleep 2; done
}

for cycle in $(seq 1 180); do
  : > "$OUT"
  echo "# URLs vivas FALK LOG — atualizado a cada 120s por tunnel-keepalive.sh" >> "$OUT"
  for t in "${TUN[@]}"; do
    IFS='|' read -r port logf path name <<< "$t"
    u="$(url_of "$logf")"
    if [ "${GRACE[$port]}" -gt 0 ]; then
      GRACE[$port]=$(( GRACE[$port] - 1 ))          # pós-restart: pula 1 ciclo (edge aquecendo)
    elif [ -z "$u" ] || ! healthy "$u" "$path"; then
      FAILS[$port]=$(( FAILS[$port] + 1 ))
      if [ "${FAILS[$port]}" -ge 2 ]; then          # debounce: 2 falhas consecutivas
        echo "[keepalive] :$port morto 2× — restartando" >&2
        restart_one "$port" "$logf"; u="$(url_of "$logf")"
        FAILS[$port]=0; GRACE[$port]=1
      fi
    else
      FAILS[$port]=0
    fi
    echo "$name: ${u}${path}" >> "$OUT"
  done
  sleep 120
done
