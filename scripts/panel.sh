#!/usr/bin/env bash
# =============================================================================
# panel.sh — dump TEXTO de qualquer endpoint do admin (sem screenshot/visão).
# Mais exato que OCR p/ dados de tabela/valores. Loga e imprime JSON formatado.
#
# Uso:
#   scripts/panel.sh /affiliates/commissions
#   scripts/panel.sh "/cod-livro/orders?from=2026-01-01&to=2026-06-30"
#   scripts/panel.sh /wallet/carteiras
#   scripts/panel.sh /orders/motoboy
# =============================================================================
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EP="${1:?uso: scripts/panel.sh /endpoint  (ex: /affiliates/commissions)}"
export PATH="/usr/local/bin:$PATH"
set -a; . "$ROOT/infra/docker/.env"; set +a
B=http://localhost:8087/wp-json/senderzz/v1/admin
TOK=$(curl -s -m8 -X POST -H 'Content-Type: application/json' "$B/login" \
  -d "{\"email\":\"${ADMIN_EMAIL}\",\"senha\":\"${ADMIN_SENHA}\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
[ -z "$TOK" ] && { echo "login falhou — go/admin no ar? (scripts/dev-local.sh status)"; exit 1; }
curl -s -m15 -H "Authorization: Bearer $TOK" "${B}${EP}" | python3 -m json.tool 2>/dev/null \
  || curl -s -m15 -H "Authorization: Bearer $TOK" "${B}${EP}"
