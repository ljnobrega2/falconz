# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

WordPress plugin (`senderzz-logistics.php`) that bundles a Brazilian logistics platform on top of WooCommerce: Melhor Envio shipping integration, prepaid wallet (carteira) with PIX top-ups, white-label tracking, an in-house "Motoboy" courier module, a portal for OLs (operadores logísticos), affiliates/COD wallet, and an external REST API for a panel. Production runtime is PHP 8.1+, WooCommerce ≥ 7.0, MySQL 8 / MariaDB 10.4+ InnoDB. See `SERVIDOR.md` for full server requirements and the `wp_tpc_*` / `wp_senderzz_*` table list.

## Commands

```bash
composer install                       # install fpdi/fpdf + phpunit
composer test                          # phpunit (testsuite=All)
composer test-financial                # phpunit --testsuite Financial   (Pix/Wallet)
composer test-webhook                  # phpunit --testsuite Webhook
composer test-coverage                 # phpunit --coverage-text
vendor/bin/phpunit --filter PixTest    # run one test class
vendor/bin/phpunit tests/Unit/Financial/WalletTest.php  # run one file
bash senderzz-security-tests.sh        # regression grep-suite for the 5 CRIT- security patches
```

Tests run without a WordPress install — `tests/bootstrap/wp-stubs.php` stubs `$wpdb`, transients, `WP_REST_*`, `do_action`/`apply_filters`, sanitizers, etc. PHPUnit ^11 (config still uses the v9 `<filter><whitelist>` schema — do not "upgrade" it).

There is no JS/CSS build step. Assets in `assets/` are served as-is.

## Architecture

### Bootstrap order (matters)

`senderzz-logistics.php` is the single entry. It defines `TPC_PATH`, `TPC_URL`, `TPC_VERSION`, `TPC_ME_API`, then **sequentially** `require_once`s `includes/*.php` in a hand-curated order (see ~lines 334–429). The order is load-bearing:

1. `includes/senderzz-secrets.php` MUST be first — pulls `tpc_webhook_secret`, `tpc_jwt_secret`, ME token from env vars before anything else reads them.
2. `senderzz-security-patches.php` next — consolidated Tier 2 patches.
3. Status/context/audit engines.
4. TPC stack (`includes/tpc/*`): database → wallet → pix → pix-confirmation-guard → pix-auto-reconcile → webhook → rest-api → admin → my-account → painel. **`pix-confirmation-guard.php` must load AFTER `pix.php`** (V-NEW-01/02 fail-closed).
5. Engine, multi-class, wallet-engine, affiliates, COD wallet.
6. `senderzz-fixes-scale.php` last among the engine block — intentionally overrides earlier hooks.
7. Motoboy module is its own subdir (`includes/motoboy/*`), loaded after everything else.

When adding a new include, put it where its dependencies are already loaded and update neither the file nor the array in alphabetical order — order = explicit dependency.

After the requires, class `WC_Melhor_Envio` calls `$this->includes()`, which loads `vendor/autoload.php` and registers a **fallback PSR-4-ish autoloader** for `WC_MelhorEnvio\` → `src/`. The composer autoloader does not cover `src/` (only third-party libs) — the fallback is what makes `src/` classes resolve. Both must remain in place. The fallback blocks `..` and `/` in class names to prevent path traversal.

### Two namespaces, two styles

| Style | Where | Naming |
|---|---|---|
| Procedural functions, prefixed `tpc_*` (wallet, PIX) and `senderzz_*` (everything else) | `includes/*.php` | snake_case, all guarded with `function_exists()` |
| Namespaced OO classes under `WC_MelhorEnvio\` | `src/*` | PSR-4-ish, loaded via `src/Pipeline/Label_Pipeline.php` style autoload |

Functions in `includes/` are the legacy/business layer; `src/` is the newer WooCommerce-integration layer (Admin, Methods, Labels, Queue, REST, Portal, Webhook, Pdf, Database). When adding new business logic, prefer `src/` + namespaces unless you must hook into the procedural surface.

### Subsystems (where to look)

- **Wallet / financial ledger** — `includes/tpc/wallet.php` + `senderzz-wallet-engine.php`. Tables `wp_tpc_carteira`, `wp_tpc_transacoes`, `wp_tpc_recargas`. Uses `SELECT … FOR UPDATE` so **InnoDB is required**. Reservation pattern: `tpc_reservar()` → ME call → `tpc_debitar_reserva()` on success, auto-release on failure. Never debit from a user-supplied price — recalculate via `/me/shipment/calculate` (see CRIT-01 in `CHANGELOG-SECURITY.md`).
- **PIX** — `includes/tpc/pix.php` for issuance, `pix-confirmation-guard.php` for fail-closed status check, `pix-auto-reconcile.php` for cron reconciliation against ME balance, `webhook.php` for inbound. PIX confirmation requires explicit paid/approved status — never trust a bare "notification".
- **Webhooks (3 distinct endpoints)** — `includes/tpc/webhook.php` (PIX in), `includes/senderzz-me-webhook.php` (Melhor Envio events), `src/Webhook/Tracking_Webhook.php` (tracking in/out). All three are **fail-closed**: empty secret = 401/503, never `return true`. ME "ping/test" event names use exact-match whitelist `['ping','test','webhook.ping']` — substring match is the CRIT-04 bypass. Idempotency via `wp_tpc_webhook_events` with `UNIQUE KEY uq_event_key`.
- **Labels** — `src/Labels/Request.php`, `src/Pipeline/Label_Pipeline.php`, `src/Rest/Labels.php` (`wc-melhor-envio/v1` REST namespace). Background processing via `src/Queue/*`. Label PDFs go through optional Python+PyMuPDF branding (`includes/senderzz_clean_label.py`); when Python/exec are unavailable the flow still works, branding is skipped with an admin notice.
- **Portal de OLs** — `src/Portal/*` + `includes/portal/*` (helpers/banner). JWT-based auth; sessions in `wp_senderzz_portal_sessions`; 2FA in `wp_senderzz_portal_2fa`. `Portal_Page.php` is a known ~5.3k-line monolith (see `NOTAS-TECNICAS.md`) — extract JS/HTML before touching it heavily.
- **Motoboy** — `includes/motoboy/*` + `templates/motoboy/*`. Self-contained: own DB (`database.php`), router (CEP→CD→zone→motoboy with Haversine geofence), 41 REST routes under `/wp-json/sz-motoboy/v1/`, PWA at `/motoboy-app/`, public tracking at `/rastreio-motoboy/`. Tables: `sz_motoboy_cds`, `sz_motoboy_zonas`, `sz_motoboy_cep_zonas`, `sz_motoboy_zona_pivot`, `sz_motoboys`, `sz_motoboy_pedidos`, `sz_motoboy_comprovantes`, `sz_motoboy_fechamento`, `sz_motoboy_audit`, `sz_portal_tickets`, `sz_portal_ticket_msgs`. See `MOTOBOY-README.md` for the full status flow and endpoint table.
- **HPOS** — WooCommerce High-Performance Order Storage is supported. `includes/senderzz-hpos-fix.php` patches tracking reads in `wp_wc_orders_meta`. New code that reads/writes order meta must work under both legacy and HPOS — go through `senderzz-order-context.php` helpers when possible.
- **Status authority** — `senderzz-woo-status-authority.php` centralizes WC order status writes so the audit engine sees them. Don't bypass with raw `$order->update_status()` from new code.

### Product-label normalization

`senderzz_clean_product_label()` / `senderzz_clean_product_summary()` / `senderzz_order_primary_item_label()` at the top of `senderzz-logistics.php` are the canonical name normalizers. Rule: "quantity and name, once only" — never list multiple items joined by `,` or `+`. Use these everywhere a product label is shown to motoboy / OL / portal.

### Admin capability guard

`senderzz_admin_capability_guard()` (top of `senderzz-logistics.php`) auto-grants the full Senderzz capability set to anyone with `manage_options`. WP admins must never lose access to module tabs.

### REST API surfaces

- `wc-melhor-envio/v1/*` — label CRUD (`src/Rest/Labels.php`)
- `senderzz/v1/*` — orders, status updates, portal (`includes/senderzz-rest.php`)
- `tp-carteira/v1/*` — wallet, PIX webhook (`includes/tpc/rest-api.php`)
- `sz-motoboy/v1/*` — motoboy module (`includes/motoboy/rest-api.php`)

Client-driven order-status transitions are whitelisted (`pending|on-hold|pendente|aguardando` → `cancelled` only — CRIT-02). Anything else for a non-admin = 403.

### Secrets

`tpc_webhook_secret` and `tpc_jwt_secret` are auto-generated on first admin visit if absent. The values can also be supplied via env vars consumed by `includes/senderzz-secrets.php`. Empty secret = 401 on every webhook and unauthenticated JWT = rejected.

## Conventions

- **Comments and code are PT-BR.** Keep that — error messages, admin notices, log lines, audit reasons are user-facing in Portuguese.
- **All new procedural functions get a `function_exists()` guard** — required because `includes/*.php` can be loaded multiple times across plugin reactivations.
- **Log prefixes:** `[tpc_*]` for financial/PIX, `[senderzz_*]` for shipping/tracking. Keep the prefix when adding new log lines.
- **Patch IDs in comments are real markers** — `// CRIT-03`, `// SEC-02`, `// V-NEW-01`, `// REQ6` etc. reference fixes in `CHANGELOG-SECURITY.md` and audit docs. Don't strip them; reference them when adding related code.
- **Don't fix Mini Envios (ID 17) silently** — `src/Methods/Method.php:209` has a commented-out `continue` left as a tripwire (`DT-CODE-01`). If you re-enable it, drop the comment too.

## Hotfix/refactor history

Version-bump history and audits live in the root `.md` files: `CHANGELOG-SECURITY.md` (the 5 CRIT + FIX-01..12 patches), `AUDIT-ADMIN-WP.md` (WP→React admin parity audit), `ROADMAP-V2.md` / `ROADMAP-VALIDACAO-VPS.md` (Go migration + admin status), `docs/MIGRATION-GO-CHECKLIST.md` (most accurate migration status), and `DIAGNOSTICO-*.md` (cross-check of docs vs code). When asked why a particular hook or override exists, grep these first. (Earlier `HOTFIX-vNNN.md` / `REFACTOR-*-vNNN.md` files referenced in older docs do **not** exist in this checkout.) Current plugin version is in the plugin header and `SENDERZZ_LOGISTICS_VERSION` / `TPC_VERSION` constants — keep all three in sync on releases.

## Portal V2 — UI conventions

### Button system (`assets/css/senderzz-dashboard-v2.css`)

| Classe | Uso | Visual |
|---|---|---|
| `szv2-btn-brand` | CTA primário (salvar, criar) | Laranja sólido `#EA580C` |
| `szv2-btn-secondary` | Ação neutra (testar, atualizar, cancelar) | Cinza outline |
| `szv2-btn-danger` | Ação destrutiva (excluir) | Vermelho sólido `#DC2626`, hover `#B91C1C` |

`szv2-btn-primary` existe em `senderzz-components-v2.css` mas não é laranja — use `szv2-btn-brand` para CTAs laranja. O `dashboard-v2.css` sobrescreve `secondary` e `danger` com escopo `.sz-dashboard-v2`.

### Modal de confirmação (`assets/js/senderzz-dashboard-v2.js` — `szV2Confirm`)

O overlay usa classe `szv2-confirm-overlay` **sem** `sz-dashboard-v2` — adicionar `sz-dashboard-v2` quebra o posicionamento (aplica `background: var(--szv2-bg)` e `min-height: 100vh` sobrescrevendo o overlay). As regras CSS do confirm (`szv2-confirm-*`) em `senderzz-dashboard-v2.css` são independentes de scope (sem prefixo `.sz-dashboard-v2`), com `z-index: 99999`, `width: 100vw; height: 100vh`. Ícone usa laranja brand. Botão destrutivo usa `szv2-btn-danger` (vermelho sólido).

### Cores da marca no Portal V2

Sempre usar `var(--szv2-brand)` / `rgba(234,88,12,.10)` para acentos laranja. **Nunca** usar verde `#22c55e` / `rgba(34,197,94,...)` em elementos de UI do portal — cor reservada para badges de status externos.

### Afiliados — delete (`templates/portal/v2/sections/affiliates.php`)

Nonce da seção de afiliados usa `'sz_aff_panel'` (action `sz_aff_panel_action`). **Não usar** `'senderzz_portal'` — o handler em `senderzz-affiliates.php:3559` verifica `'sz_aff_panel'`. Confirmação de exclusão usa `szV2Confirm`, nunca `confirm()` nativo.

### Conexões / Webhooks (`templates/portal/v2/sections/webhooks.php`)

Layout: payload `<details>` → formulário "Novo webhook" → tabela "Webhooks configurados" (todos filhos diretos de `.szv2-conn-panel` para width 100%). Payload duplicado na aba Histórico foi removido — existe só na aba principal.

**Webhook delete = soft-delete** (`src/Portal/Portal_Page.php::ajax_webhooks_delete`): limpa `url=''`, `active=0` em vez de hard-delete. Hard-delete faz `senderzz_pw_ensure_user_webhook_slots()` recriar o slot no próximo `init` (cron a cada 10 min). A query de exibição filtra `AND url != ''` para não mostrar slots vazios.

### Localidades (`templates/portal/v2/sections/localidades.php`)

Header do painel sem ícone circular (removido). Badge "Armazém Próprio" removido. Botão copiar endereço removido. `szV2PrCdFilter` removido de `senderzz-v2-products.js` (era código morto). `szV2PrCdSelectBtn` mostra TODOS os produtos independente de CD selecionado (não filtra tabs por `cd_ids` — produto nunca some da lista ao trocar CD).

### Sidebar nav (`assets/css/senderzz-dashboard-v2.css`)

Fonte dos itens nav: `12.5px`, padding reduzido (`7px`), gap menor. Nav kicker: `10px`. Scrollbar oculta (`scrollbar-width:none` + `::-webkit-scrollbar { display:none }`). Ícone `vitrine` adicionado em `sidebar.php` `$szs_icons`.

### Vitrine (`templates/portal/v2/sections/vitrine.php`)

Seção disponível para produtores E afiliados. Grid de cards com imagem, nome, vendidos, comissão %, comissão máxima por venda (calculada da melhor oferta), total distribuído. Filtra produtos com nome contendo "recarga", "carteira frete", "frete interno" (não mostrar na vitrine). Modal com **3 tabs sempre clicáveis** (Produto / Ofertas / Localidades): tela 0 (imagem, KPIs vendidos+comissão+total distribuído, descrição + edição inline para produtor dono), tela 1 (ofertas: nome, valor, comissão em R$ por venda, link checkout), tela 2 (localidades CDs com badge "Disponível", nome decodificado via `$sz9vt_decode` para corrigir `\uXXXX` literais do banco). Botão "Afiliar-me" chama `sz_aff_panel_action` / `request_affiliation`. **Ofertas**: filtro `affiliate_visible` removido (mostrava 0 resultados pois default=0); só exclui tipo=motoboy. **Descrição**: WC meta `_sz_vitrine_description` — salva via `szaction=save_vitrine_description` (Portal_Page.php). Campo também adicionado no modal de criação de produto (products.php). **Datas**: todas as datas em affiliates.php usam `wp_date('d/m/Y', strtotime(...))` → formato brasileiro.

### Afiliados — aba "Minha afiliação" (`templates/portal/v2/sections/affiliates.php`)

Sub-aba adicionada ao painel de afiliados (produtores E afiliados a veem). Mostra os vínculos WHERE `user_id = current_wp_user_id` (o usuário como afiliado de outros produtores) + links de checkout disponíveis. Nav do afiliado inclui `vitrine` e `affiliates` no grupo Vendas. Afiliado NÃO tem acesso à seção `products` (não está em `$sz_v2_sections_affiliate`).

## Painel logístico — mudanças v1

### Status: pedidos nascem sem motoboy
`sz_motoboy_criar_pedido()` (router.php:393) — `motoboy_id = null` já implementado desde v349.

### OL pode mudar status / trocar motoboy
Novos endpoints REST `sz-motoboy/v1` (rest-api.php):
- `POST /ol/mudar-status` — muda para qualquer status (entregue, frustrado, em_rota etc.) via portal auth
- `POST /ol/trocar-motoboy` — troca `motoboy_id` do pedido
- `GET /ol/motoboys-do-dia` — lista motoboys com pedidos do dia
- `GET /ol/motoboys` — lista motoboys ativos

Auth: `sz_mb_auth_ol_portal()` — aceita `manage_woocommerce` OU portal session ativa (qualquer role).

### Complemento > 32 chars
Na seção `motoboy.php`, quando `dest_complemento` tem mais de 32 chars, linha recebe fundo laranja tênue e ponto indicador laranja no número do pedido. Complemento aparece no modal de detalhes em destaque.

### Dois valores para motoboy cobrar
Option `sz_motoboy_cc_fee_pct` (default 0) — definir via WP admin > Opções. No modal de detalhes do pedido (motoboy.php): mostra Valor (dinheiro) em laranja + linha "Valor (cartão X% taxa)" calculado dinamicamente em JS.

### Tela Motoboys do dia
Nova seção `templates/portal/v2/sections/motoboys-dia.php` — visível para OL/produtor, não afiliado. Cards por motoboy: KPIs (entregues/frustrados/em rota/pendentes), barra de progresso, lista de pedidos do dia, total R$. Alert se há pedidos sem motoboy. Registrada em `dashboard-v2.php` no grupo Pedidos, entre Motoboy e Expedição.

### QR Code na etiqueta + leitor no PWA (JÁ IMPLEMENTADO)

Sistema completo desde versão anterior:
- **Etiqueta** (`includes/motoboy/admin.php:1466`): QR gerado via `api.qrserver.com` com código `SZ-{wc_order_id}-{pedido_id}-{hmac14}` (assinado com `wp_salt('auth')` via `sz_mbc_package_code()`)
- **Leitor PWA** (`templates/motoboy/pwa.php`): `lerQrEtiqueta()` usa `BarcodeDetector` + câmera traseira; fallback para input manual
- **Endpoint** `POST /sz-motoboy/v1/motoboy/iniciar-rota`: valida QR via `sz_mbc_start_route_by_qr()` e muda status para `em_rota`
- **Botão** "📷 Bipar saída" aparece no card de cada pedido embalado no PWA

### Dois valores motoboy (oficial + cartão)

- `sz_motoboy_cc_fee_pct` option — configurável em **Admin > Motoboy > Config** (campo adicionado em `admin.php`)
- **Card PWA**: mostra valor original + valor com taxa (riscado/anotado como 💳)
- **Modal Entregar**: bloco azul "💳 Cartão (com taxa X%)" aparece automaticamente se taxa > 0
- `SZ_CC_FEE_PCT` injetado como constante JS no PWA via PHP

### Botões de ação OL na tabela motoboy
**REMOVIDOS** da seção `motoboy.php` (V2) — mudança de status e troca de motoboy são exclusivos do painel OL (`motoboys-dia`). O botão "Cancelar" permanece para produtor com regras:
- `agendado`/`embalado` → pode cancelar E reagendar
- `frustrado`/`cancelado` → só reagendar
- outros status → sem ação

## Portal V2 — regras críticas

### Foco V2: não misturar V1
Todo novo desenvolvimento vai em `templates/portal/v2/sections/*.php` e `assets/js/senderzz-dashboard-v2.js`. **Nunca** adicionar lógica V2 na dashboard clássica `templates/portal/dashboard.php`.

### Visibilidade por role (`dashboard-v2.php`)
- `motoboys-dia` → somente role `operator` (OL). Produtor e afiliado **não veem**.
- Afiliado não acessa: `motoboy`, `expedicao`, `products`, `motoboys-dia`.
- Detecção: `$sz_v2_is_ol = ( 'operator' === $sz_v2_role )` — role definida em `Portal_Auth`.

### Cancel de pedido motoboy (V2)
- Botão Cancelar usa class `szv2-mb-action-btn` + `data-action="cancel"`.
- **Sem `onclick`** — event delegation em `senderzz-dashboard-v2.js:handleCancel` trata.
- Nonce no botão = `senderzz_portal` (não `wp_rest`). Section precisa de `data-szv2-ajax-url`.
- AJAX: `szaction=cancel` → `Portal_Page::cancel_order`. REST `sz-portal/v2/motoboy/bulk-cancel` **não existe** (removida v446) — não usar.
- `handleCancel` usa `szV2Confirm` (modal custom) com fallback para `window.confirm`.

### szV2Confirm — assinatura única
```js
window.szV2Confirm({ title, message, btn, danger }, callbackFn)
```
Não confundir com `szConfirm(msg)` (V1, usa `window.confirm`). Sempre usar `szV2Confirm` em código V2.

### Telefone — strip +55
`$sz4mb_fmt_phone`: strip country code se `$d` tem 12–13 dígitos e começa com `55`.

### Vitrine — valor em destaque
Card mostra "Ganhe até R$ X por venda" como elemento principal (28px bold, fundo laranja tênue). % de comissão é secundário. Sem endereço do CD. `affiliate_visible` não filtra ofertas.

### Integrations toggle — chaves permitidas
`Portal_Page.php` `integrations_toggle`: allowed = `['active','paused','require_paid','ignore_duplicates','auto_cheapest']`. "Pausar recebimento" toggle envia `key=active` com valor **invertido** (checked → 0, unchecked → 1).

### Rate limiting de login — desativado para dev
`Portal_Auth.php` e `senderzz-security-patches.php`: limites elevados para 1000/10000 para não bloquear testes. Em produção, restaurar `ip_max=10`, `email_max=5`, Portal_Auth `>= 5`, 2FA `>= 3`.

### Seção Pedidos V2 (orders.php)
Reconstruída com lista real + filtros client-side: produto, status, afiliado, kit de venda, data de venda (range), data de entrega. Carrega via `senderzz_get_visible_orders_for_user`. Data de entrega lida de `_sz_delivery_date` meta + `sz_motoboy_pedidos.reagendado_para`.

### Webhook/Integração — limpar logs
- `integrations_clear_logs`: deleta `senderzz_integration_log` por `user_id`.
- `webhooks_clear_history`: deleta `senderzz_webhook_log` via `webhook_id` do usuário.
- Ambos pedem `szV2Confirm` antes.

### autocomplete em settings.php
Campos de e-mail/senha em `settings.php` usam `autocomplete="new-password"` (email) e `autocomplete="current-password"/"new-password"` (senha) para inibir sugestões do browser. Campos PIX usam `autocomplete="off"`.

## Things that look weird but are intentional

- `WC_Melhor_Envio::VERSION = '2.2.7-portal-premium'` while `SENDERZZ_LOGISTICS_VERSION = '2.6.11-v459'` (and `TPC_VERSION = '2.6.11-v459'`; plugin header `Version: 459`). Three version-bearing strings for the PHP plugin, kept in sync at v459. Two different versioning schemes coexist (the `2.2.7-portal-premium` class const is intentionally separate). The Go services under `go/` use their own per-service version scheme (orders 6.0, labels 4.0, wallet 2.0) unrelated to the PHP plugin version.
- `phpunit.xml` `bootstrap` points into `tests/bootstrap/wp-stubs.php` and the autoloader uses Composer's `autoload-dev` classmap on `tests/bootstrap/` — both required, tests fail without WP stubs.
- `senderzz-fixes-scale.php` is loaded last specifically to win hook-priority conflicts. Don't reorder it.
