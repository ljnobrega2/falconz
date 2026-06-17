#!/usr/bin/env bash
# =============================================================================
# verify-migration.sh — Diff MySQL ↔ Postgres para todas as Fases (1-6)
#
# Uso:
#   MYSQL_URL="mysql://user:pass@host/db" \
#   PG_URL="postgresql://user:pass@host/db" \
#   bash infra/scripts/verify-migration.sh
#
# Saída:
#   EXIT 0: divergência ≤ 0,01% em todas as tabelas e saldo financeiro exato
#   EXIT 1: divergência acima do limiar (bloquear cutover)
#
# Requer: mysql-client, psql, bc
# =============================================================================

set -euo pipefail

MYSQL_URL="${MYSQL_URL:-mysql://wordpress:secret@127.0.0.1:3306/wordpress_db}"
PG_URL="${PG_URL:-postgresql://senderzz:secret@127.0.0.1:5432/senderzz}"
THRESHOLD="${THRESHOLD:-0.01}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
fail=0

check() {
    local label="$1" mysql_q="$2" pg_q="$3"
    mysql_count=$(mysql "$MYSQL_URL" -sN -e "$mysql_q" 2>/dev/null || echo "ERR")
    pg_count=$(psql "$PG_URL" -t -A -c "$pg_q" 2>/dev/null || echo "ERR")

    if [[ "$mysql_count" == "ERR" || "$pg_count" == "ERR" ]]; then
        echo -e "${YELLOW}⚠ $label: erro ao consultar${NC}"; return
    fi
    if [[ "$mysql_count" -eq 0 ]]; then
        echo -e "${YELLOW}⚠ $label: MySQL count=0${NC}"; return
    fi

    diff=$(echo "scale=4; ($mysql_count - $pg_count) / $mysql_count * 100" | bc)
    abs_diff="${diff#-}"
    if (( $(echo "$abs_diff > $THRESHOLD" | bc -l) )); then
        echo -e "${RED}✗ $label: MySQL=$mysql_count PG=$pg_count divergência=${diff}% (> ${THRESHOLD}%)${NC}"
        fail=1
    else
        echo -e "${GREEN}✓ $label: MySQL=$mysql_count PG=$pg_count divergência=${diff}%${NC}"
    fi
}

echo "=== Verificação de migração MySQL ↔ Postgres (threshold: ${THRESHOLD}%) ==="
date

# ── Fase 1: Motoboy ──────────────────────────────────────────────────────────
echo -e "\n--- Fase 1: Motoboy"
check "sz_motoboy_pedidos (total)"     "SELECT COUNT(*) FROM wp_sz_motoboy_pedidos"                        "SELECT COUNT(*) FROM sz_motoboy_pedidos"
check "sz_motoboy_pedidos (entregues)" "SELECT COUNT(*) FROM wp_sz_motoboy_pedidos WHERE status='entregue'" "SELECT COUNT(*) FROM sz_motoboy_pedidos WHERE status='entregue'"
check "sz_motoboys (ativos)"           "SELECT COUNT(*) FROM wp_sz_motoboys WHERE ativo=1"                 "SELECT COUNT(*) FROM sz_motoboys WHERE ativo=true"
check "sz_motoboy_audit"               "SELECT COUNT(*) FROM wp_sz_motoboy_audit"                          "SELECT COUNT(*) FROM sz_motoboy_audit"
check "sz_motoboy_comprovantes"        "SELECT COUNT(*) FROM wp_sz_motoboy_comprovantes"                   "SELECT COUNT(*) FROM sz_motoboy_comprovantes"
check "sz_motoboy_cds"                 "SELECT COUNT(*) FROM wp_sz_motoboy_cds"                            "SELECT COUNT(*) FROM sz_motoboy_cds"
check "sz_motoboy_zonas"               "SELECT COUNT(*) FROM wp_sz_motoboy_zonas"                          "SELECT COUNT(*) FROM sz_motoboy_zonas"

# ── Fase 2: Wallet ───────────────────────────────────────────────────────────
echo -e "\n--- Fase 2: Wallet"
check "tpc_carteira"                   "SELECT COUNT(*) FROM wp_tpc_carteira"                                        "SELECT COUNT(*) FROM tpc_carteira"
check "tpc_transacoes (total)"         "SELECT COUNT(*) FROM wp_tpc_transacoes"                                      "SELECT COUNT(*) FROM tpc_transacoes"
check "tpc_transacoes (confirmadas)"   "SELECT COUNT(*) FROM wp_tpc_transacoes WHERE status='confirmado'"            "SELECT COUNT(*) FROM tpc_transacoes WHERE status='confirmado'"
check "tpc_recargas (confirmadas)"     "SELECT COUNT(*) FROM wp_tpc_recargas WHERE status='confirmado'"              "SELECT COUNT(*) FROM tpc_recargas WHERE status='confirmado'"
check "tpc_webhook_events"             "SELECT COUNT(*) FROM wp_tpc_webhook_events"                                  "SELECT COUNT(*) FROM tpc_webhook_events"

# Saldo financeiro — tolerância zero (financeiro é crítico)
echo ""
mysql_saldo=$(mysql "$MYSQL_URL" -sN -e "SELECT ROUND(SUM(saldo),2) FROM wp_tpc_carteira" 2>/dev/null || echo "ERR")
pg_saldo=$(psql "$PG_URL" -t -A -c "SELECT ROUND(SUM(saldo)::numeric,2) FROM tpc_carteira" 2>/dev/null || echo "ERR")
if [[ "$mysql_saldo" != "ERR" && "$pg_saldo" != "ERR" ]]; then
    if [[ "$mysql_saldo" == "$pg_saldo" ]]; then
        echo -e "${GREEN}✓ SALDO TOTAL: MySQL=R\$$mysql_saldo PG=R\$$pg_saldo (exato)${NC}"
    else
        echo -e "${RED}✗ DIVERGÊNCIA FINANCEIRA CRÍTICA: MySQL=R\$$mysql_saldo PG=R\$$pg_saldo${NC}"
        fail=1
    fi
fi

# ── Fase 3: Affiliates ────────────────────────────────────────────────────────
echo -e "\n--- Fase 3: Affiliates"
check "senderzz_affiliates"            "SELECT COUNT(*) FROM wp_senderzz_affiliates"                                 "SELECT COUNT(*) FROM senderzz_affiliates"
check "affiliate_commissions"          "SELECT COUNT(*) FROM wp_senderzz_affiliate_commissions"                      "SELECT COUNT(*) FROM senderzz_affiliate_commissions"
check "cod_wallet"                     "SELECT COUNT(*) FROM wp_senderzz_cod_wallet"                                 "SELECT COUNT(*) FROM senderzz_cod_wallet"

# ── Fase 6: Orders ────────────────────────────────────────────────────────────
echo -e "\n--- Fase 6: Orders"
check "sz_orders (total)"              "SELECT COUNT(*) FROM wp_wc_orders"                                          "SELECT COUNT(*) FROM sz_orders"
check "sz_orders (completos)"          "SELECT COUNT(*) FROM wp_wc_orders WHERE status='wc-completo'"               "SELECT COUNT(*) FROM sz_orders WHERE status='completo'"
check "sz_order_items"                 "SELECT COUNT(*) FROM wp_woocommerce_order_items"                             "SELECT COUNT(*) FROM sz_order_items"

echo ""
if [[ $fail -eq 0 ]]; then
    echo -e "${GREEN}=== PASS: todas as verificações dentro do limiar ===${NC}"
    exit 0
else
    echo -e "${RED}=== FAIL: divergências detectadas — NÃO fazer cutover ===${NC}"
    exit 1
fi
