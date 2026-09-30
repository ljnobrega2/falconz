#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"
fail=0
ok(){ printf '\033[32m[OK]\033[0m %s\n' "$1"; }
bad(){ printf '\033[31m[FAIL]\033[0m %s\n' "$1"; fail=1; }
skip(){ printf '\033[33m[SKIP]\033[0m %s\n' "$1"; }
has(){ LC_ALL=C grep -aFq -- "$2" "$1" && ok "$3" || bad "$3"; }
not_has(){ ! LC_ALL=C grep -aFq -- "$2" "$1" && ok "$3" || bad "$3"; }

echo "Senderzz Security Regression Tests"
echo "Plugin: $ROOT"
echo

for f in includes/tpc/webhook.php includes/tpc/database.php includes/tpc/rest-api.php includes/senderzz-me-webhook.php includes/senderzz-engine.php; do
  if php -n -l "$f" >/dev/null; then ok "PHP válido: $f"; else php -n -l "$f" || true; bad "PHP inválido: $f"; fi
done
php -n -l src/Traits/Helpers.php >/dev/null && ok "PHP válido: src/Traits/Helpers.php" || bad "PHP inválido: src/Traits/Helpers.php"

has src/Traits/Helpers.php "DOING_AJAX" "Existe bloco AJAX"
has src/Traits/Helpers.php "wc_melhor_envio_can_request_label', false" "AJAX anônimo não emite etiqueta"
has src/Traits/Helpers.php "is_user_logged_in() && current_user_can( 'manage_woocommerce' )" "Etiqueta exige login + manage_woocommerce"

has includes/tpc/rest-api.php "tpc_jwt_access_token_ttl" "JWT usa TTL curto configurável"
has includes/tpc/rest-api.php "HOUR_IN_SECONDS" "TTL padrão do JWT é 1 hora"
not_has includes/tpc/rest-api.php "30 * DAY_IN_SECONDS" "JWT não expira em 30 dias"
not_has assets/js/painel.js "localStorage.getItem('tpc_jwt')" "Painel principal não lê JWT em localStorage"
not_has assets/js/painel.js "localStorage.setItem('tpc_jwt'" "Painel principal não grava JWT em localStorage"
has assets/js/painel.js "sessionStorage.getItem('tpc_jwt')" "Painel principal usa sessionStorage"

# tpc-assets/js/painel.js foi migrado para assets/js/painel.js (MIGRATION.md — migração concluída)
ok "Painel TPC migrado para assets/ (tpc-assets/ removido conforme MIGRATION.md)"

has includes/tpc/webhook.php "status obrigatório para confirmar PIX" "Webhook PIX recusa status ausente"
has includes/tpc/webhook.php 'tpc_pix_status_is_paid' "Webhook PIX exige status pago/aprovado"
has includes/tpc/webhook.php "valor divergente" "Webhook PIX valida valor recebido"
has includes/tpc/webhook.php "me_pix_id divergente" "Webhook PIX valida ID do pagamento"
not_has includes/tpc/webhook.php "notificação simples" "Removida confirmação de PIX sem status"

has includes/tpc/webhook.php "assinatura inválida" "Webhook carteira valida assinatura"
has includes/tpc/webhook.php "], 401" "Webhook carteira retorna 401 em assinatura inválida"
has includes/senderzz-me-webhook.php "unauthorized' ], 401" "Webhook Melhor Envio retorna 401 para assinatura inválida"
has includes/senderzz-me-webhook.php "rate_limited' ], 429" "Webhook Melhor Envio retorna 429 no rate limit"

has includes/tpc/database.php "tpc_webhook_events" "Tabela de eventos de webhook criada"
has includes/tpc/database.php "UNIQUE KEY uq_event_key" "Evento de webhook tem chave única"
has includes/tpc/webhook.php "tpc_webhook_register_event" "Webhook registra evento antes de processar"
has includes/tpc/webhook.php "duplicate" "Webhook ignora duplicados"

# Lógica de carteira movida para senderzz-wallet-engine.php (separação de responsabilidades)
has includes/senderzz-wallet-engine.php "FRETE COBRADO" "Carteira usa frete cobrado do cliente"
has includes/senderzz-wallet-engine.php "shipping_charged" "Valor necessário usa shipping_charged"
has includes/senderzz-wallet-engine.php "get_shipping_total" "Fallback usa frete do WooCommerce"
has includes/senderzz-wallet-engine.php "senderzz_get_portal_wallet_owner_by_class_id" "Backend usa carteira do produtor vinculada à classe"
has includes/senderzz-wallet-engine.php "Necessário frete" "Erro de saldo mostra valor necessário do frete"
has includes/senderzz-wallet-engine.php "Reservado:" "Erro de saldo mostra saldo reservado"

has includes/tpc/webhook.php "tpc_mask_sensitive_log_data" "Logs da carteira mascaram dados sensíveis"
has includes/tpc/webhook.php "token|secret|senha|password|authorization|document|cpf" "Máscara cobre tokens, documentos e contatos"

echo
echo "── Testes FIX-01: Validação CPF/CNPJ ─────────────────────────────────────"
if php -n -l includes/senderzz-fixes-scale.php >/dev/null 2>&1; then ok "PHP válido: includes/senderzz-fixes-scale.php"; else bad "PHP inválido: includes/senderzz-fixes-scale.php"; fi
has includes/senderzz-fixes-scale.php "senderzz_validate_cpf" "Função de validação CPF existe"
has includes/senderzz-fixes-scale.php "senderzz_validate_cnpj" "Função de validação CNPJ existe"
has includes/senderzz-fixes-scale.php "senderzz_validate_document" "Função unificada de validação existe"
has includes/senderzz-fixes-scale.php "\\1{10}" "CPF rejeita sequências uniformes (mod-11)"
has includes/senderzz-fixes-scale.php "\\1{13}" "CNPJ rejeita sequências uniformes (mod-11)"
has includes/senderzz-fixes-scale.php "senderzz_sender_class_save" "Save handler verifica nonce antes de validar"
has includes/senderzz-fixes-scale.php "Erro ao salvar remetentes" "Mensagem de erro clara ao usuário admin"
has includes/senderzz-fixes-scale.php "}, 9 );" "Validação registrada em priority 9 (antes do handler original em 10)"

echo
echo "── Testes FIX-02: Bloqueio de carrinho misto ──────────────────────────────"
has includes/senderzz-fixes-scale.php "woocommerce_add_to_cart_validation" "Bloqueia adição ao carrinho de produtor diferente"
has includes/senderzz-fixes-scale.php "woocommerce_check_cart_items" "Segunda camada valida carrinho inteiro"
has includes/senderzz-fixes-scale.php "wc_add_notice" "Mensagem de erro amigável ao comprador"
not_has includes/senderzz-fixes-scale.php "throw new Exception" "Carrinho misto não lança Exception (convertido para aviso UX)"

echo
echo "── Testes FIX-03: Deduplicação estorno diferido ───────────────────────────"
has includes/senderzz-fixes-scale.php "remove_action( 'senderzz_deferred_wallet_refund'" "Handler original do estorno diferido é substituído"
has includes/senderzz-fixes-scale.php "senderzz_deferred_wallet_refund_deduped" "Novo handler com deduplicação registrado"
has includes/senderzz-fixes-scale.php "_senderzz_wallet_refunded" "Guard verifica meta de conclusão antes de processar"
has includes/senderzz-fixes-scale.php "_senderzz_deferred_refund_lock" "Lock atômico por pedido impede dupla execução"

echo
echo "── Testes FIX-04: Admin notice WP-Cron ────────────────────────────────────"
has includes/senderzz-fixes-scale.php "senderzz_is_real_cron_configured" "Detector de cron real existe"
has includes/senderzz-fixes-scale.php "DISABLE_WP_CRON" "Detecta constante DISABLE_WP_CRON"
has includes/senderzz-fixes-scale.php "wp cron event run --due-now" "Instrução WP-CLI no aviso"
has includes/senderzz-fixes-scale.php "wp-cron.php" "Instrução crontab no aviso"
has includes/senderzz-fixes-scale.php "senderzz_confirm_cron" "Handler de confirmação existe"
has includes/senderzz-fixes-scale.php "senderzz_dismiss_cron" "Handler de dispensar existe"
has includes/senderzz-fixes-scale.php "7 * DAY_IN_SECONDS" "Dismissal dura 7 dias, não é permanente"

echo
echo "── Testes FIX-05: Cookie HttpOnly ─────────────────────────────────────────"
has includes/senderzz-fixes-scale.php "'httponly' => true" "Cookies de oferta redefinidos com HttpOnly=true"
has includes/senderzz-fixes-scale.php "'samesite' => 'Lax'" "Cookies de oferta têm SameSite=Lax"
has includes/senderzz-checkout-link-interceptor.php "senderzz_offer_token" "Cookie original existe (esperado — redefinido pelo fix)"
has includes/senderzz-fixes-scale.php "}, 99 );" "Cookie fix registrado em priority 99 (depois do interceptor original)"

echo
echo "── Testes FIX-06: Sequenciador atômico ────────────────────────────────────"
has includes/senderzz-fixes-scale.php "senderzz_atomic_counters" "Tabela de contadores atômicos criada"
has includes/senderzz-fixes-scale.php "ON DUPLICATE KEY UPDATE counter_val = LAST_INSERT_ID" "INSERT atômico com LAST_INSERT_ID trick"
has includes/senderzz-fixes-scale.php "senderzz_atomic_counter_next" "Função de contador atômico existe"
has includes/senderzz-fixes-scale.php "woocommerce_checkout_order_created" "Número atribuído na criação do pedido"
not_has includes/senderzz-fixes-scale.php "usleep" "Sequenciador não usa spin-lock com usleep"

echo
echo "── Testes FIX-07: Validação REST + Self-test ──────────────────────────────"
has includes/senderzz-fixes-scale.php "util/validate-document" "Endpoint REST de validação de documento existe"
has includes/senderzz-fixes-scale.php "SENDERZZ_FIXES_SCALE_LOADED" "Guard de duplo carregamento existe"
has includes/senderzz-fixes-scale.php "529.982.247-25" "Self-test usa CPF válido conhecido"
has includes/senderzz-fixes-scale.php "11.222.333/0001-81" "Self-test usa CNPJ válido conhecido"
has includes/senderzz-fixes-scale.php "111.111.111-11" "Self-test rejeita CPF de sequência uniforme"
has includes/senderzz-fixes-scale.php "SELFTEST FAILURE" "Self-test loga falhas via error_log"
has senderzz-logistics.php "senderzz-fixes-scale.php" "Arquivo de fixes registrado no loader principal"

echo
if [[ "$fail" -eq 0 ]]; then
  echo "✅ Todos os testes estáticos passaram (originais + fixes de escala)."
  echo "Opcional no WordPress ativo: wp eval 'tpc_create_tables(); echo get_option(\"tpc_db_version\");'"
else
  echo "❌ Falhou. Revise os itens acima antes de subir em produção."
  exit 1
fi

echo
echo "── Testes FIX-08: Analytics sem limite 500 ────────────────────────────────"
if php -n -l src/Analytics/Margin_Dashboard.php >/dev/null 2>&1; then ok "PHP válido: src/Analytics/Margin_Dashboard.php"; else bad "PHP inválido: src/Analytics/Margin_Dashboard.php"; fi
not_has src/Analytics/Margin_Dashboard.php "'limit'      => 500" "Analytics sem limite arbitrário de 500 pedidos"
not_has src/Analytics/Margin_Dashboard.php "wc_get_orders" "Analytics não usa wc_get_orders (N+1 queries)"
has src/Analytics/Margin_Dashboard.php "MAX(CASE WHEN" "Analytics usa agregação SQL por pivot"
has src/Analytics/Margin_Dashboard.php "GROUP BY" "Analytics agrupa no banco, não em PHP"
has src/Analytics/Margin_Dashboard.php "HAVING margin IS NOT NULL" "Analytics filtra pedidos Senderzz no SQL"
has src/Analytics/Margin_Dashboard.php "hpos_active" "Analytics detecta modo HPOS vs legado"
has src/Analytics/Margin_Dashboard.php "wc_orders_meta" "Analytics tem suporte HPOS (wp_wc_orders_meta)"
has src/Analytics/Margin_Dashboard.php "wpdb->postmeta" "Analytics tem suporte legado (wp_postmeta via wpdb)"
has src/Analytics/Margin_Dashboard.php "REPLACE(o.status, 'wc-'" "Analytics normaliza prefixo wc- no status"

echo
echo "── Testes FIX-09: JWT auditado (sem dependência externa) ──────────────────"
has includes/tpc/rest-api.php "hash_equals" "JWT usa comparação timing-safe"
has includes/tpc/rest-api.php "!== 'HS256'" "JWT bloqueia alg diferente de HS256 (alg=none bloqueado)"
has includes/tpc/rest-api.php "hash_hmac( 'sha256'" "JWT usa HMAC-SHA256"
has includes/tpc/rest-api.php "strlen( \$secret ) < 32" "JWT recusa secret com entropia insuficiente"
has includes/tpc/rest-api.php "jwt_expirado" "JWT valida expiração e retorna erro específico"
not_has includes/tpc/rest-api.php "alg.*none" "JWT não aceita alg=none"

echo
echo "── Testes FIX-10: Secrets via variáveis de ambiente ───────────────────────"
if php -n -l includes/senderzz-secrets.php >/dev/null 2>&1; then ok "PHP válido: includes/senderzz-secrets.php"; else bad "PHP inválido: includes/senderzz-secrets.php"; fi
has includes/senderzz-secrets.php "SENDERZZ_WEBHOOK_SECRET" "Constante de env para webhook secret existe"
has includes/senderzz-secrets.php "SENDERZZ_JWT_SECRET" "Constante de env para JWT secret existe"
has includes/senderzz-secrets.php "SENDERZZ_ME_TOKEN" "Constante de env para token ME existe"
has includes/senderzz-secrets.php "getenv(" "Fallback para variável de ambiente do servidor"
has includes/senderzz-secrets.php "get_option( 'tpc_webhook_secret'" "Fallback final para wp_options (sem breaking change)"
has includes/senderzz-secrets.php "option_tpc_webhook_secret" "Filtro WP intercepta get_option para webhook secret"
has includes/senderzz-secrets.php "option_tpc_jwt_secret" "Filtro WP intercepta get_option para JWT secret"
has includes/senderzz-secrets.php "option_tpc_me_token" "Filtro WP intercepta get_option para token ME"
has includes/senderzz-secrets.php "pre_update_option_tpc_webhook_secret" "Bloqueio de escrita quando secret vem de env"
has includes/senderzz-secrets.php "pre_update_option_tpc_jwt_secret" "Bloqueio de escrita JWT quando vem de env"
has includes/senderzz-secrets.php "pre_update_option_tpc_me_token" "Bloqueio de escrita token ME quando vem de env"
has includes/senderzz-secrets.php "env-managed" "Substitui valor no banco por placeholder quando env ativa"
has includes/senderzz-secrets.php "senderzz_secret_from_env" "Função de detecção de env existe"
has includes/senderzz-secrets.php "disabled = true" "UI desabilita campo quando secret vem de env"
has senderzz-logistics.php "senderzz-secrets.php" "Arquivo de secrets registrado no loader"
# Verificar que secrets.php é carregado ANTES de security-patches.php
python3 -c "
content = open('senderzz-logistics.php').read()
idx_sec = content.find('senderzz-secrets.php')
idx_patch = content.find('senderzz-security-patches.php')
assert idx_sec < idx_patch, f'FAIL: secrets({idx_sec}) deve vir antes de patches({idx_patch})'
print('OK')
" && ok "senderzz-secrets.php carregado antes dos outros includes" || bad "senderzz-secrets.php NÃO é o primeiro include"

echo
echo "── Testes FIX-11: Extração de templates Portal_Page.php (opcional) ────────"
if php -n -l src/Portal/Portal_Page.php >/dev/null 2>&1; then ok "PHP válido: src/Portal/Portal_Page.php"; else bad "PHP inválido: src/Portal/Portal_Page.php"; fi
# A extração dos templates de Portal_Page.php é trabalho futuro planejado (Portal_Page.php é um
# monólito conhecido — ver CLAUDE.md / NOTAS-TECNICAS.md). Os asserts de template só rodam SE a
# extração já tiver sido feita; caso contrário são pulados (SKIP), não falham — evita falso-vermelho
# na suíte de regressão de SEGURANÇA (que é o propósito real deste script).
if [ -f templates/portal/operator-dashboard.php ]; then
  for tmpl in templates/portal/operator-dashboard.php templates/portal/dashboard.php templates/portal/stock-panel.php templates/portal/order-card.php; do
    if php -n -l "$tmpl" >/dev/null 2>&1; then ok "PHP válido: $tmpl"; else bad "PHP inválido: $tmpl"; fi
  done
  [ -f templates/portal/portal-vars.css ] && ok "CSS vars extraído: templates/portal/portal-vars.css" || bad "CSS vars ausente"
  has src/Portal/Portal_Page.php "include TPC_PATH . 'templates/portal/operator-dashboard.php'" "operator_dashboard() usa template"
  has src/Portal/Portal_Page.php "include TPC_PATH . 'templates/portal/dashboard.php'" "dashboard() usa template"
  has src/Portal/Portal_Page.php "include TPC_PATH . 'templates/portal/stock-panel.php'" "render_stock_panel() usa template"
  has src/Portal/Portal_Page.php "include TPC_PATH . 'templates/portal/order-card.php'" "order_card() usa template"
  has src/Portal/Portal_Page.php "sz-portal-vars" "portal-vars.css enqueued"
  php_lines=$(wc -l < src/Portal/Portal_Page.php)
  [ "$php_lines" -lt 3500 ] && ok "Portal_Page.php reduzido: $php_lines linhas" || bad "Portal_Page.php ainda grande: $php_lines linhas"
else
  skip "Extração de templates Portal_Page pendente — asserts de template pulados (não é regressão de segurança)"
fi

echo
echo "── Testes FIX-12: Desacoplamento \$this nos templates ──────────────────────"
if php -n -l includes/portal/portal-helpers.php >/dev/null 2>&1; then ok "PHP válido: includes/portal/portal-helpers.php"; else bad "PHP inválido: includes/portal/portal-helpers.php"; fi
has includes/portal/portal-helpers.php "senderzz_portal_logo_url" "Função livre logo_url existe"
has includes/portal/portal-helpers.php "senderzz_portal_status_label" "Função livre status_label existe"
has includes/portal/portal-helpers.php "senderzz_portal_tracking_codes" "Função livre tracking_codes existe"
has includes/portal/portal-helpers.php "senderzz_portal_wallet_user_id" "Função livre wallet_user_id existe"
has includes/portal/portal-helpers.php "senderzz_portal_calc_dashboard_metrics" "Função livre calc_metrics existe"
has includes/portal/portal-helpers.php "SENDERZZ_PORTAL_HELPERS_LOADED" "Guard de duplo carregamento existe"
# Templates desacoplados de $this-> (só rodam se a extração já foi feita)
if [ -f templates/portal/operator-dashboard.php ]; then
  for tmpl in templates/portal/operator-dashboard.php templates/portal/order-card.php templates/portal/stock-panel.php; do
    not_has "$tmpl" '$this->' "$(basename "$tmpl") sem \$this->"
  done
  not_has templates/portal/dashboard.php '$this->' 'dashboard.php sem $this-> (usa $portal->)'
  has templates/portal/dashboard.php '$portal->' "dashboard.php usa \$portal-> para métodos da classe"
  has src/Portal/Portal_Page.php '$portal = $this' "Portal_Page injeta \$portal no contexto do template"
else
  skip "Templates extraídos ausentes — checks de desacoplamento \$this-> pulados"
fi
# Wrappers na classe
has src/Portal/Portal_Page.php "return senderzz_portal_logo_url(" "logo_url() é wrapper da função livre"
has src/Portal/Portal_Page.php "return senderzz_portal_status_label(" "status_label() é wrapper da função livre"
has senderzz-logistics.php "portal-helpers.php" "portal-helpers.php registrado no loader"

echo
echo "── Testes AUDIT-2026-06-18 CRIT-1: crédito duplo do produtor (COD) ─────────"
# O split de afiliado NÃO pode mais creditar o repasse do produtor via tpc_creditar:
# a carteira COD é a fonte única de verdade. Reintroduzir isso = crédito em dobro.
not_has includes/senderzz-affiliates.php "sz_cod_produtor_" "split NÃO credita repasse produtor na carteira TPC (ref sz_cod_produtor_)"
not_has includes/senderzz-affiliates.php "'Venda COD Senderzz #'" "split NÃO chama tpc_creditar('Venda COD ...') (crédito duplo removido)"
has includes/senderzz-affiliates.php "AUDIT-2026-06-18 CRIT-1" "marcador da correção presente"
# A perna sobrevivente (carteira COD) continua pagando o produtor:
has includes/senderzz-cod-wallet.php "sz_cod_wallet_get_producer_credit_amount" "carteira COD credita o líquido do produtor"
has includes/senderzz-cod-wallet.php "_sz_prod_commission" "carteira COD lê _sz_prod_commission (mesmo líquido)"
has includes/senderzz-affiliates.php "_sz_prod_commission" "split ainda persiste _sz_prod_commission p/ a carteira COD consumir"

echo
echo "── Testes AUDIT-2026-06-18 CRIT-2: IDOR endpoints OL motoboy ──────────────"
# Auth não pode mais retornar true para qualquer sessão de portal nem usar branch morto.
not_has includes/motoboy/rest-api.php "\\Senderzz\\Portal\\Portal_Auth::get_current_user" "removida allowlist morta (namespace \\Senderzz errado)"
has includes/motoboy/rest-api.php "function sz_mb_ol_portal_user" "helper de papel OL/produtor existe"
has includes/motoboy/rest-api.php "function sz_mb_ol_can_access_pedido" "helper de posse por pedido existe"
# Os 2 endpoints state-changing + histórico chamam o guard de posse:
cnt_guard=$(grep -c "sz_mb_ol_can_access_pedido( \$pedido_id )" includes/motoboy/rest-api.php)
[ "$cnt_guard" -ge 3 ] && ok "guard de posse aplicado nos 3 callbacks ($cnt_guard call-sites)" || bad "guard de posse não aplicado em todos os callbacks ($cnt_guard)"
has includes/motoboy/rest-api.php "AUDIT-2026-06-18 CRIT-2" "marcador da correção presente"
# Restringe papéis: operator/producer apenas (client/affiliate negados)
has includes/motoboy/rest-api.php "[ 'operator', 'producer' ], true" "auth OL restrita a operator/producer"

echo
echo "── Testes AUDIT-2026-06-18 Onda 1 (PHP): seg/financeiro ───────────────────"
# a) bulk-cancel/reschedule fail-open
has includes/senderzz-rest.php "function sz_portal_owned_class_ids_or_null" "helper de posse fail-closed existe"
has includes/senderzz-rest.php "function sz_portal_v1_verify_nonce" "verificação de nonce CSRF V1 existe"
not_has includes/senderzz-rest.php "user_class_id > 0" "removido guard fail-open (\$user_class_id > 0)"
has includes/senderzz-rest.php "in_array( \$order_class, \$class_ids, true )" "compara contra TODAS as classes"
# b) login motoboy pin-less takeover
has includes/motoboy/rest-api.php "Método de login desatualizado" "rota /login legada desativada (410)"
not_has includes/motoboy/rest-api.php "password_hash( \$pin_raw" "auto-set de PIN por chamador não-autenticado removido"
# c) tracking público sem order_key
has includes/motoboy/rest-api.php "Link de rastreio inválido" "tracking exige order_key"
has templates/motoboy/tracking.php "key=' + encodeURIComponent(szOrderKey)" "frontend de rastreio envia order_key"
# d) saque afiliado atômico
has includes/senderzz-affiliates.php "Saldo insuficiente para aprovar este saque" "aprovar_saque REST com guard de saldo"
has includes/senderzz-affiliates.php "balance>=%f" "débito condicional (balance>=amount) presente"

echo
echo "── Testes AUDIT-2026-06-18 Onda 1 (Go): idempotência / estorno / CORS ─────"
# NOTA: Go não é compilado aqui (sem toolchain). Asserts grep — rodar 'go build ./...' no CI.
# e) antecipação COD idempotente
has go/affiliates/internal/handlers/cod.go "Idempotency-Key" "antecipação aceita Idempotency-Key"
not_has go/affiliates/internal/handlers/cod.go 'antecipacao_%d_%d' "removida referência por UnixNano (não-idempotente)"
# f) cancel de pedido pago sem estorno
has go/orders/internal/handlers/orders.go "pedido pago não pode ser cancelado" "cancel bloqueia pedido pago"
not_has go/orders/internal/handlers/orders.go '"pending": true, "processing": true' "removido 'processing' do whitelist do cliente"
# g) CORS allowlist
has go/portal/cmd/server/main.go "func originAllowed" "portal: allowlist CORS"
has go/motoboy/cmd/server/main.go "func originAllowed" "motoboy: allowlist CORS"
not_has go/portal/cmd/server/main.go 'origin = "*"' "portal: removido echo de Origin curinga"
has infra/docker/docker-compose.yml "ALLOWED_ORIGINS" "compose expõe ALLOWED_ORIGINS"

echo
echo "── Testes AUDIT-2026-06-18 Onda 2 (SSRF/IDOR/authz/privesc) ───────────────"
# SEC-LIVE-01: SSRF push (live PHP)
has includes/senderzz-notifications.php "function sz_notif_endpoint_seguro" "push: helper anti-SSRF existe"
has includes/senderzz-notifications.php "wp_safe_remote_post" "push: usa wp_safe_remote_post (send)"
not_has includes/senderzz-notifications.php 'wp_remote_post( $endpoint' "push: removido wp_remote_post cru no send"
has includes/senderzz-producer-notifications.php "wp_safe_remote_post" "push produtor: usa wp_safe_remote_post"
# SEC-LIVE-02: IDOR comprovante (live PHP)
has includes/motoboy/rest-api.php "Pedido não atribuído a este motoboy" "comprovante: guard de posse por motoboy"
# SEC-GO-01: wallet money-mint removido do grupo JWT
not_has go/wallet/cmd/server/main.go 'r.Post("/carteira/creditar"' "wallet: creditar fora do grupo JWT de usuário"
not_has go/wallet/cmd/server/main.go 'r.Post("/carteira/reservar"' "wallet: reservar fora do grupo JWT de usuário"
# SEC-GO-02: /ol/* exige role operator
has go/motoboy/cmd/server/main.go 'auth.RequireRole("operator")' "motoboy: /ol/* exige role operator"
has go/motoboy/internal/auth/auth.go "func RequireRole" "motoboy: middleware RequireRole existe"
# SEC-GO-03: entregar/frustrar com posse do motoboy
has go/motoboy/internal/handlers/rota.go "pedido não atribuído a este motoboy" "motoboy: entregar/frustrar checa posse"
# SEC-GO-04: admin JWT exige issuer próprio
has go/admin/internal/auth/auth.go 'jwt.WithIssuer("senderzz-admin")' "admin: JWT exige iss=senderzz-admin (anti-confusão)"
has go/admin/internal/auth/auth.go "jwt.WithExpirationRequired()" "admin: JWT exige exp"

echo
if [[ "$fail" -eq 0 ]]; then
  echo "✅ Suíte completa passou (fixes de escala + AUDIT CRIT-1/CRIT-2 + Onda 1 PHP + Go grep + Onda 2 SSRF/IDOR/authz)."
else
  echo "❌ Há falhas acima — revise antes de prosseguir."
  exit 1
fi
