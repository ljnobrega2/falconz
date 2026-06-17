# Roadmap — Migração WordPress → Painel Admin React + Portal User V2 React

Data: 2026-06-16 | Versão plugin legado: `2.6.11-v459`

Decisão arquitetural: **não rodamos WordPress no VPS**. MariaDB é apenas legado read-only para reconciliação. Postgres é source of truth desde já.

---

## ESTADO ATUAL (16/06/2026)

### Infra rodando no VPS (93.127.141.6)
- ✅ Postgres 16 (`senderzz-postgres`) — schema motoboy aplicado, `synced_at` adicionado
- ✅ MariaDB (`senderzz-mariadb`) — banco legado, read-only para migração
- ✅ Redis 7 (`senderzz-redis`) — Asynq queues
- ✅ Rede docker `senderzz-net`

### Serviços Go deployados
- ✅ `senderzz-motoboy` (porta 8081) — Up, build com fix `OVERRIDING SYSTEM VALUE` + `synced_at`
- ⏳ `senderzz-wallet` — pendente subir
- ⏳ `senderzz-affiliates` — pendente subir
- ⏳ `senderzz-labels` — pendente subir
- ⏳ `senderzz-portal` — pendente subir
- ⏳ `senderzz-orders` — pendente subir
- ⏳ `senderzz-admin` (admin API) — pendente subir

### Dados migrados MariaDB → Postgres (sync 100%)
| Tabela | MariaDB | Postgres |
|---|---|---|
| `sz_motoboy_pedidos` | 35 | ✅ 35 |
| `sz_motoboys` | 7 | ✅ 7 |
| `sz_motoboy_cds` | 1 | ✅ 1 |
| `sz_motoboy_zonas` | 36 | ✅ 36 |
| `sz_motoboy_cep_zonas` | 35 | ✅ 35 |
| `sz_motoboy_comprovantes` | 8 | ✅ 8 |
| `sz_motoboy_audit` | 130 | ✅ 130 |

### Admin UI React (`admin-ui/src/pages/`)
Páginas existentes (incompletas):
- Affiliates, Cds, Commissions, Dashboard, Labels, Login, Logs, Motoboys, MotoboysDay, Orders, Pix, Settings, Tools, Users, Wallet, Zonas

---

# FASE A — FINALIZAR PAINEL ADMIN

**Estimativa: 3–4 semanas. Objetivo: paridade 100% com admin WP atual.**

## A.0 — Inventário de telas WP que faltam no React

Cruzando admin WP (`src/Admin/Unified_Menu.php` + `includes/motoboy/admin.php` + `includes/tpc/admin.php`) com React atual:

### ✅ Já existem no React (precisam revisão/funcionalidade)
| Tela React | Equivalente WP | Status |
|---|---|---|
| Dashboard | "Visão Geral" Overview | esqueleto — falta KPIs reais |
| Motoboys | Motoboy > Motoboys | esqueleto — falta CRUD completo |
| MotoboysDay | Motoboy > Dashboard do dia | esqueleto |
| Cds | Motoboy > CDs | esqueleto |
| Zonas | Motoboy > Zonas | esqueleto |
| Orders | Motoboy > Pedidos | esqueleto |
| Wallet | Carteira COD (cod-wallet) | esqueleto — falta saques |
| Pix | TPC > PIX Reconciliação | esqueleto |
| Affiliates | Afiliados | esqueleto |
| Commissions | Financeiro > Livro COD | esqueleto |
| Labels | Expedição > Pedidos ME | esqueleto |
| Users | Configurações > Usuários (portal_users) | esqueleto |
| Logs | (combinação webhooks+integrations+audit) | esqueleto |
| Settings | Configurações Geral | esqueleto |
| Tools | Configurações > Saúde Sistema | esqueleto |

### ❌ FALTANDO no React — criar
| Tela nova | Origem WP | Prioridade |
|---|---|---|
| **CodLivro** | Financeiro > Livro único COD | P0 — crítico finance |
| **CodSaques** | Financeiro > Saques (approve/reject/pay) | P0 — finance |
| **CodTaxasEntrega** | Financeiro > Taxa entrega por produtor/motoboy | P0 — finance |
| **AuditEngine** | Financeiro > Auditoria (divergências + batch fix) | P0 — finance crítico |
| **TpcClientes** | TPC > Clientes (saldo + emitir PIX + cancelar recarga) | P0 — finance |
| **TpcTransacoes** | TPC > Transações | P1 |
| **TpcConfiguracoes** | TPC > Configurações (token ME, validade PIX) | P1 |
| **ExpedicaoIntegracoes** | Expedição > Integrações (markup rules, webhooks ME) | P1 |
| **ExpedicaoWebhooks** | Expedição > Webhooks por classe | P1 |
| **NotificacoesPWA** | Configurações > Notificações (templates push) | P1 |
| **PushTecnico** | Configurações > Push técnico (VAPID keys) | P2 |
| **ApiDocs** | Configurações > API (docs endpoints) | P2 |
| **MaintenanceMode** | (em senderzz-maintenance.php) | P1 |
| **OnboardingSetup** | (em senderzz-onboarding.php) — setup inicial ME/secrets | P0 |
| **AffiliateWallet** | (em senderzz-affiliates.php > tab carteira) | P0 — finance |
| **AffiliateRules** | Configurações afiliados padrão (commission, retention, withdraw fee) | P1 |
| **CodWalletTransactions** | wp_sz_cod_wallet_transactions viewer | P1 |
| **CodWalletProducer** | Carteira COD por produtor (admin view) | P1 |
| **MotoboyConfig** | Motoboy > Configurações (taxas, cutoff, cc_fee_pct) | P0 |
| **MotoboyFechamento** | Motoboy > Fechamento diário (sz_motoboy_fechamento) | P0 |
| **MotoboyComprovantes** | viewer + admin baixa manual | P1 |
| **OrderDetail** | view completa pedido + metabox motoboy + metabox afiliado + metabox label | P0 — view central |
| **ProductsAdmin** | Custódia + checkouts + envios + movimentações (V1 producer-pwa interno) | P1 |
| **CronStatus** | Status dos 11 crons (próx execução + último run + log) | P1 |
| **CapabilitiesGuard** | Listar capabilities + auto-grant manage_options | P2 |
| **BulkActions** | Ações em lote pedidos (gerar labels, mudar status) | P1 |

### ⚠️ Crons que precisam virar jobs Go (Asynq) — não esquecer
1. `tpc_cron_verificar_recargas_pix` (5 min) — PIX auto-check
2. `senderzz_db_cleanup` (daily) — sessions + 2FA + webhook logs >90d
3. `senderzz_me_reconcile_shipments` (5 min) — ME status reconcile
4. `sz_posted_polling_cron` (5 min) — cancel intercept
5. `senderzz_auto_generate_label` (one-shot) — async label gen
6. `senderzz_check_low_balance` (one-shot) — low balance notify
7. `sz_cod_release_cron` (custom) — COD release in wallet
8. `sz_aff_release_commissions` (custom) — affiliate commission release
9. `sz_motoboy_geofence_check` (custom) — geofence validation
10. `sz_pix_auto_reconcile_cron` (custom) — PIX reconcile vs banco
11. `wc_melhor_envio_check_posted` (custom) — posted status check

---

## A.1 — Subir admin-service Go + autenticação JWT (Semana 1)

- [ ] Build `go/admin/cmd/server/main.go` no VPS
- [ ] Container `senderzz-admin` (porta 8087) na rede `senderzz-net`
- [ ] Migration: criar admin user inicial (`POST /tools/users`)
- [ ] Confirmar `POST /login` retorna JWT válido
- [ ] Configurar `VITE_API_BASE` no admin-ui apontando para Go admin
- [ ] Deploy admin-ui (build estático via nginx)

## A.2 — Páginas P0 (Semanas 2–3) — finanças críticas

Cada tela = (1) endpoint Go novo em `go/admin/internal/handlers/` (2) página React em `admin-ui/src/pages/`

- [ ] **OnboardingSetup** — wizard primeira instalação (admin user, ME token, secrets validation)
- [ ] **CodLivro** — listar transações `wp_sz_cod_wallet_transactions` com filtros (produtor, período, tipo)
- [ ] **CodSaques** — fluxo `pending → approved → paid` com botões + comprovante upload
- [ ] **CodTaxasEntrega** — visualização por produtor/motoboy + ajuste taxa
- [ ] **AuditEngine** — endpoint `POST /audit/fix-all` + `POST /audit/fix-order/{id}` (porta `handle_audit_fix_all` PHP)
- [ ] **AffiliateWallet** — saldo + transações + fix carteira (`POST /affiliates/{id}/wallet-fix`)
- [ ] **TpcClientes** — listar `tpc_carteira` + emitir PIX (`POST /tpc/clientes/{id}/pix`) + cancelar recarga
- [ ] **OrderDetail** — view única consolidando: pedido WC + motoboy + afiliado + label ME + transações
- [ ] **MotoboyConfig** — opções (`sz_motoboy_cc_fee_pct`, `sz_mbw_taxa_entrega`, `sz_mbw_taxa_frustrado`)
- [ ] **MotoboyFechamento** — listar `sz_motoboy_fechamento` + ações Alan confirmar + repasse confirmado

## A.3 — Páginas P1 (Semana 3–4)

- [ ] **TpcTransacoes**, **TpcConfiguracoes**
- [ ] **ExpedicaoIntegracoes**, **ExpedicaoWebhooks**
- [ ] **NotificacoesPWA**
- [ ] **MaintenanceMode** — toggle global + título + mensagem + data retorno
- [ ] **AffiliateRules** — opções padrão (commission %, retention days, withdraw fee, penalty)
- [ ] **CodWalletTransactions**, **CodWalletProducer**
- [ ] **MotoboyComprovantes** viewer
- [ ] **ProductsAdmin** — custódia + checkouts + envios + movimentos
- [ ] **CronStatus** — 11 jobs Asynq com próx execução, último run, status, log
- [ ] **BulkActions** — selecionar N pedidos + ação batch

## A.4 — Páginas P2 + polish (Semana 4)

- [ ] **PushTecnico** (VAPID keys editor)
- [ ] **ApiDocs** (lista endpoints, exemplos curl)
- [ ] **CapabilitiesGuard** viewer
- [ ] Capability check no admin-ui (somente `senderzz_admin`, `senderzz_manage`, etc.)
- [ ] Auditoria visual: cada tela WP tem equivalente React? checklist final

## A.5 — Migração de dados que faltam (durante Semanas 2–3)

- [ ] Wallet/PIX: `tpc_carteira`, `tpc_transacoes`, `tpc_recargas`, `tpc_webhook_events`
- [ ] Affiliates: `sz_affiliates`, `sz_affiliate_transactions`, `sz_affiliate_wallet`
- [ ] COD wallet: `sz_cod_wallet_transactions`
- [ ] Labels: `wc_melhor_envio_label_request`, `wc_melhor_envio_label_queue`
- [ ] Portal: `senderzz_portal_users`, `senderzz_portal_sessions`, `senderzz_portal_2fa`
- [ ] Webhook logs: `senderzz_webhook_log`, `sz_webhook_logs`, `senderzz_audit_log`
- [ ] Orders WC: `wc_orders`, `woocommerce_order_items` → `sz_orders`, `sz_order_items`

Scripts: usar pgloader configs em `infra/pgloader/*` (existem para wallet, affiliates, labels, orders). Verificar via `verify-migration.sh` após cada.

---

# FASE B — PORTAL USER V2 (REACT)

**Estimativa: 4–6 semanas. Objetivo: paridade 100% com portal V2 PHP atual (mesmo layout, mesma UX).**

## B.0 — Setup

- [ ] Criar `portal-ui/` (estrutura análoga ao `admin-ui/`)
- [ ] Vite + React + TS + Tailwind
- [ ] Copiar tokens CSS do `senderzz-dashboard-v2.css` → CSS modules
- [ ] Reusar componente `szV2Confirm` como `<ConfirmModal/>`
- [ ] Setup `api.ts` com `Authorization: Bearer <JWT>` (portal-service `:8085`)

## B.1 — Auth + Onboarding (Semana 1)

- [ ] **Login** (email + senha) — `POST /senderzz/v1/portal/login`
- [ ] **2FA** (email code 6 dígitos, 10min) — `POST /senderzz/v1/portal/2fa/verify`
- [ ] **Cadastro novo usuário** — formulário público (CPF/CNPJ + email + telefone + senha)
- [ ] **Recuperar senha** (token 24h single-use)
- [ ] **Logout**
- [ ] Sessão "lembrar 7 dias" (HttpOnly cookie + hash)
- [ ] Layout pós-login: sidebar V2 com seções por role

## B.2 — Dashboard Home (Semana 2)

Espelhar EXATAMENTE `templates/portal/v2/sections/dashboard.php`:
- [ ] Aba toggle COD ↔ Expedição
- [ ] **KPI Cards COD** — Saldo / Faturamento / Comissões aff / Pedidos / Ticket médio (cap 500 pedidos 30d)
- [ ] **Status bars COD** — Entregue/Em rota/Frustrado/Cancelado %
- [ ] **Top 5 produtos COD** — nome / pedidos / faturamento / comissão
- [ ] **Regiões COD** — região / pedidos / %
- [ ] **Afiliados ativos COD** (produtor) — afiliado / pedidos / faturamento / comissão
- [ ] **KPI Cards Exp** — Saldo Exp / Total cobrado / Pedidos / Clientes únicos / Itens / Custo médio frete
- [ ] **Transportadora destaque Exp**
- [ ] **Tempo médio envio Exp** — AVG TIMESTAMPDIFF `_senderzz_operator_sent_at`
- [ ] **Situação pedidos Exp** — grid 3×2
- [ ] **Top produtos expedidos**
- [ ] Filtro período: 1/7/30/90 + custom range
- [ ] Export Excel (CSV download)

## B.3 — Carteiras (Semana 2–3)

- [ ] **Carteira COD** (`wallet.php` espelho)
  - KPIs: Saldo disponível / Pendente / Saque em análise
  - Aba Transações (30d)
  - Aba Saques (pending/approved/paid/rejected)
  - Botão "Saque" (flag `senderzz_dashboard_v2_withdraw_enabled`)
  - Modal saque (valor + taxa flat + data)
  - Saques futuros (lançamentos 30d)
- [ ] **Carteira Expedição** (`wallet-expedition.php` espelho)
  - Saldo disponível
  - Recarga PIX presets (R$ 50/100/200/500/custom)
  - Modal PIX (QR + copy + auto-verify)
  - Histórico 30d

## B.4 — Pedidos (Semana 3)

- [ ] **Pedidos Motoboy / COD** (`motoboy.php` espelho)
  - Chips status (Todos/Agendado/Embalado/A caminho/Em rota/Entregue/Frustrado)
  - Busca texto + filtros (Produto/Oferta/Afiliado)
  - Tabela 200 max + ações inline (Ver/Reagendar 3 dias/Cancelar)
  - Modal detalhe (endereço/cliente/itens/motoboy)
  - Highlight complemento >32 chars
  - Dois valores motoboy (oficial + cartão com taxa)
- [ ] **Expedição** (`expedicao.php` espelho)
  - Chips (Todos/Pendente/Em rota/Entregue/Cancelado)
  - Botões Aprovar/Cancelar/Reprocessar
  - Inline tracking copy
- [ ] **Pedidos visão geral** (`orders.php` espelho)
  - Status chips + período + dropdowns + busca
  - Tabela 300 max
  - Export CSV

## B.5 — Vitrine + Afiliados (Semana 3–4)

- [ ] **Vitrine** (`vitrine.php` espelho)
  - Grid de cards (nome/imagem/"Ganhe até R$ X"/% comissão)
  - Modal 3 tabs (Produto/Ofertas/Localidades)
  - Edição inline `_sz_vitrine_description` (produtor dono)
  - Botão "Afiliar-me" (afiliado)
  - Decode `\uXXXX` Unicode em nomes CDs
- [ ] **Afiliados** (`affiliates.php` espelho)
  - Produtor: Pendentes/Aprovados/Minhas/Configurações
  - Afiliado: Suas afiliações/Links de venda
  - Modal aprovar/recusar (szV2Confirm)
  - Inline edit comissão %
  - Datas formato `wp_date('d/m/Y')`

## B.6 — Conexões + Integrações (Semana 4)

- [ ] **Webhooks** (`webhooks.php` espelho)
  - `<details>` payload exemplo
  - Form novo webhook (classe + URL + eventos checkbox)
  - Tabela ativos com toggle + soft-delete
  - Aba Histórico (últimos 50 com payload inline)
  - Botão Testar (mock)
- [ ] **Integrações** (`integrations.php` espelho)
  - Endpoint público (URL + token + renew)
  - Toggle: active / paused / require_paid / ignore_duplicates / auto_cheapest
  - "Pausar recebimento" envia `key=active` invertido
  - Histórico últimos 50 + clear logs
  - Mapeamento campos (read-only v459)

## B.7 — Configurações + Suporte (Semana 4–5)

- [ ] **Settings** (`settings.php` espelho)
  - Conta: nome/CPF/email/telefone + alterar email/senha
  - 2FA toggle
  - Saques: PIX keys + taxas read-only + retention
  - Notificações: email + WhatsApp toggles
  - Taxas & Prazos: taxa produtor motoboy read-only
  - autocomplete `new-password` nos campos sensíveis
- [ ] **Suporte** (`support.php` espelho)
  - Novo chamado (assunto/categoria/mensagem)
  - Lista tickets com status badge
  - Detalhe + responder + fechar
- [ ] **Sub-usuários** (`users.php` espelho)
  - Tabela sub-usuários (parent_user_id)
  - Modal adicionar (nome/email/senha)
  - Ações inline (editar/deletar/reset senha)

## B.8 — Demais seções (Semana 5)

- [ ] **Frete** (`freight.php`) — remetente + transportadoras favoritas/bloqueadas
- [ ] **Localidades** (`localidades.php`) — lista CDs + zonas (sem ícone circular, sem badge "Armazém Próprio")
- [ ] **Produtos** (`products.php`) — CD filter (não filtra produto) + grid + KPIs custódia + checkouts + envios + movimentos
- [ ] **Stock** (`stock.php`) — KPIs custódia + movimentações + export CSV
- [ ] **Links/Checkouts** (`links.php`) — cards link + copy + toggle afiliado + comissão inline
- [ ] **Relatórios** (`reports.php`) — período + status + KPIs + ranking produtos/regiões + export
- [ ] **Motoboys Dia** (`motoboys-dia.php`) — APENAS operador (OL); cards motoboy + KPIs + lista pedidos + alert sem-motoboy

## B.9 — Polish + role visibility (Semana 5–6)

- [ ] Sidebar dinâmica por role (matriz `$sz_v2_sections_*`)
- [ ] Producer: full access except `motoboys-dia`
- [ ] Affiliate: bloquear `motoboy/expedicao/products/motoboys-dia`
- [ ] Operator: APENAS `motoboys-dia` (+ comum)
- [ ] Brand colors: `var(--szv2-brand)` `#EA580C` em CTAs; **nunca** verde `#22c55e` em UI
- [ ] Botão sistema: `szv2-btn-brand` (CTA laranja), `szv2-btn-secondary` (outline), `szv2-btn-danger` (vermelho)
- [ ] Confirm modal: `<ConfirmModal/>` (equivalente `szV2Confirm`) em TODOS destrutivos
- [ ] Telefone: strip `+55` se 12–13 dígitos
- [ ] Fonte sidebar 12.5px + scrollbar oculta
- [ ] PWA installable (manifest + service worker)

---

# FASE C — CUTOVER FINAL + KILL WORDPRESS

**Estimativa: 1 semana. Após Fase A + B prontas e testadas.**

## C.1 — Deploy production
- [ ] Build admin-ui + portal-ui (Vite production)
- [ ] nginx serve estáticos + proxy `/api/*` → Go services
- [ ] TLS via certbot (Let's Encrypt) para `app.senderzz.com.br` + `admin.senderzz.com.br`
- [ ] DNS apontar para VPS

## C.2 — Smoke tests end-to-end
- [ ] Admin login → ver dashboard → criar motoboy → criar CD/zona → ver pedido → audit batch
- [ ] User cadastro novo → 2FA → recarga PIX → criar webhook → afiliar-se vitrine → criar pedido motoboy → ver no portal
- [ ] Operador login → motoboys-dia → mudar status pedido → trocar motoboy
- [ ] Comparar visualmente cada tela WP × React (screenshots side-by-side)

## C.3 — Migração final dados (qualquer delta)
- [ ] Re-run `verify-migration.sh` em todas tabelas — 0 divergências
- [ ] Backup MariaDB final (`mysqldump > final-backup.sql.gz`)
- [ ] Snapshot Postgres (`pg_dump`)

## C.4 — Desligar WP/MariaDB
- [ ] MariaDB → standby (parar container, manter volume)
- [ ] Plugin PHP `senderzz-logistics.php` → standby
- [ ] Apagar referências MariaDB do código Go (remover doublewrite)
- [ ] Remover `infra/scripts/migrate-pedidos.py` + `/tmp/*.sql`

---

# DEPENDÊNCIAS CRÍTICAS — NUNCA QUEBRAR

| Invariante | Onde validar |
|---|---|
| Preço SEMPRE server-side (CRIT-01) | `go/labels/internal/me/client.go::Calculate` |
| PIX fail-closed (secret vazio = 503) | `go/wallet/internal/handlers/pix.go` |
| Idempotência DB: `UNIQUE(user_id, referencia, tipo)` | `schema-wallet.sql` |
| QR HMAC14 = `WP_SALT_AUTH` nos dois lados | PHP: `sz_mbc_package_code()` / Go: `rota.go::IniciarRota` |
| Session tokens HMAC (não raw) | `go/motoboy/internal/auth/auth.go::AuthPortal` |
| Cancellation whitelist (CRIT-02) | apenas `pending|on-hold|pendente|aguardando` → `cancelled` para não-admin |
| ME webhook ping whitelist exato (CRIT-04) | `['ping','test','webhook.ping']` (não substring) |
| Quantidade + nome único em product label | `senderzz_clean_product_label()` |
| Capability guard: `manage_options` → full Senderzz | `senderzz_admin_capability_guard()` |
| 11 crons Asynq rodando | tabela acima — todos precisam virar jobs Go |

---

# REGRAS DE NEGÓCIO QUE NÃO PODEM SER ESQUECIDAS

## Finance
1. **Reserva PIX** — `tpc_reservar() → ME call → tpc_debitar_reserva()` (rollback automático em falha)
2. **Confirmação PIX** explícita: nunca confiar em "notification" bare; só `paid|approved`
3. **Saldo total wallet**: tolerância zero em `verify-migration.sh`
4. **Soft-delete webhook**: hard-delete faz cron recriar slot (a cada 10 min)
5. **Afiliado** — visibilidade de ofertas independente de `affiliate_visible` (filtro removido)
6. **Sub-aba "Minha afiliação"** em afiliados — mostra `WHERE user_id = current_wp_user_id`
7. **Auditoria batch** — `handle_audit_fix_all` corrige: missing aff tx + bad aff tx + bad producer wallet + bad aff wallet summaries

## Motoboy
1. **Pedidos nascem sem motoboy** — `motoboy_id = NULL` (router.php v349+)
2. **OL pode mudar status + trocar motoboy** — endpoints `/ol/mudar-status` `/ol/trocar-motoboy`
3. **Dois valores cobrar** — option `sz_motoboy_cc_fee_pct` (oficial + cartão com taxa %)
4. **QR Code etiqueta** — `SZ-{wc_order_id}-{pedido_id}-{hmac14}` assinado com `wp_salt('auth')`
5. **Bipar saída** — PWA usa `BarcodeDetector` + câmera traseira, fallback input manual
6. **Botões ação OL** — REMOVIDOS do `motoboy.php` V2 (exclusivos em `motoboys-dia`)
7. **Cancelar pedido** (produtor) — só `agendado|embalado` (`frustrado|cancelado` só reagendar)

## Portal V2
1. **dashboard_v2_active() = true** sempre (emergência: `senderzz_v2_emergency_off`)
2. **Brand colors** — `--szv2-brand` `#EA580C`; nunca `#22c55e` em UI
3. **Confirm modal** — `szV2Confirm` (nunca `confirm()` nativo)
4. **Overlay confirm** — class `szv2-confirm-overlay` SEM `sz-dashboard-v2` (quebra layout)
5. **Nonce afiliados** — `sz_aff_panel` (não `senderzz_portal`)
6. **Integrações toggle** — keys permitidas: `active/paused/require_paid/ignore_duplicates/auto_cheapest`
7. **Rate limit login** — DEV: 1000/10000; PRODUÇÃO: restaurar `ip_max=10`, `email_max=5`
8. **Datas** — `wp_date('d/m/Y', strtotime(...))` formato brasileiro
9. **autocomplete="new-password"** em email/senha settings
10. **Telefone** — strip +55 se 12–13 dígitos começando com `55`

## HPOS (orders meta)
- Novo código que lê/escreve order meta deve funcionar legacy + HPOS
- Usar helpers `senderzz-order-context.php` quando possível
- `wp_wc_orders_meta` patches em `senderzz-hpos-fix.php`

## Status authority
- WC order status writes centralizados em `senderzz-woo-status-authority.php`
- Audit engine só vê via essa camada — não usar `$order->update_status()` direto

---

# ROLLBACK / DISASTER

| Cenário | Ação |
|---|---|
| Bug em página admin React | Deploy nginx anterior (Atomic switch via symlink) |
| Bug em portal user V2 React | Idem |
| Corrupção dados Postgres | Restore `pg_dump` + replay logs Asynq |
| Go service crashes | docker restart + Asynq retry com backoff |
| Disaster total | MariaDB ainda standby — religar plugin PHP + apontar DNS |

---

# PRÓXIMOS PASSOS IMEDIATOS (esta sessão)

1. ✅ Pedidos motoboy migrados (35/35)
2. ✅ Schema `synced_at` adicionado
3. ✅ Bug `OVERRIDING SYSTEM VALUE` corrigido em `internal.go`
4. ✅ Motoboy-service rebuild + redeployado
5. ✅ Tabelas comprovantes (8) + audit (130) migradas
6. **PRÓXIMO**: Subir `senderzz-admin` (porta 8087) + `senderzz-portal` (porta 8085) + `senderzz-wallet` (porta 8082)
7. **PRÓXIMO**: Migrar wallet (`tpc_*`), affiliates (`sz_affiliates*`), portal_users via pgloader
8. **PRÓXIMO**: Construir as 10 telas P0 do admin React (Onboarding, CodLivro, CodSaques, AuditEngine, AffiliateWallet, TpcClientes, OrderDetail, MotoboyConfig, MotoboyFechamento, CodTaxasEntrega)

---

# CHECKPOINT 2026-06-17 — Sessão Admin React v463/v464

## O que ficou pronto nesta sessão

### Migrations Postgres
- ✅ `schema-fixes-v463.sql` aplicado / pendente deploy:
  - RENAME `producer_id → user_id` em `sz_cod_wallet_transactions`, `sz_cod_withdrawals`, `sz_cod_withdraw_accounts`
  - ADD `gross/fee/net/release_at` em `sz_cod_wallet_transactions`
  - ADD `holder_name/holder_cpf/admin_note/completed_at/updated_at/fee/net` em `sz_cod_withdrawals`
  - ADD `holder_name/holder_cpf` em `sz_cod_withdraw_accounts`
  - ADD `fee/net_amount/bank_info/admin_note/decided_at/decided_by` em `senderzz_affiliate_withdrawals`
  - RENAME `saldo_disponivel/saldo_pendente → balance/pending_balance` + ADD `debt_amount` em `senderzz_affiliate_wallet`
  - ADD `available_at/meta_json/updated_at` em `senderzz_affiliate_transactions` (DO block guard)

### Componentes React novos
- ✅ `admin-ui/src/components/FilterTopPanel.tsx` — slide-down do topo (substitui drawer lateral). Exporta `FilterField`, `filterInputStyle`, `ActiveFilterChips`, `ActiveChip`
- ✅ `admin-ui/src/components/FilterButton.tsx` — funil + badge contador
- ✅ `admin-ui/src/components/CopyButton.tsx` — copy clipboard reutilizável (variantes inline e icon)
- ✅ `admin-ui/src/components/ErrorBoundary.tsx` — fallback amigável

### FilterTopPanel — 31 telas migradas
**Lote v463 (15):** Orders, Affiliates, Commissions, AuditLogViewer, Labels, MotoboyComprovantes, MotoboyConciliacao, MotoboySaques, CodWalletTransactions, Pix, TpcTransacoes, Wallet, BulkActions, Motoboys, CodSaques.

**Lote v464 (16):** Users, Products, OnboardingRequests, AuditEngine, TpcClientes, Logs, MotoboyCarteira, MotoboyCustodia, MotoboyDashboard, MotoboyEtiquetas, MotoboyFechamento, MotoboysDay, CodLivro, CodWalletProducer, AffiliateWallet, Zonas.

Padrão: draft state local → aplica só ao confirmar; data inicial/final padrão; chips ativos abaixo do header com X-to-remove; badge contador no FilterButton.

### Handlers Go novos / enriquecidos
- ✅ `checkout_links.go` (novo) — CRUD `senderzz_affiliate_links`, token via `crypto/rand`, enriquece nome/email afiliado+produtor, produto via `sp.wp_post_id`, conversões + receita gerada
- ✅ `products.go` — fix `afiliados_count` (`COALESCE(sp.wp_post_id, sp.id)`), `POST /products/sync-from-orders` (importa do histórico de `sz_order_items`), `GET /products/stats`
- ✅ `affiliates.go::List` — adiciona `telefone, cpf, pix_key, links_count, total_clicks, last_order_at, total_vendido_30d, total_comissao_30d, pedidos_count_30d`
- ✅ `affiliates.go::Commissions` — LEFT JOIN `sz_orders` para `order_number/total/status`, `afiliado_email/produtor_email`, `link_url` computada (`https://app.senderzz.com.br/checkout/<token>`), `available_at`
- ✅ `order_detail.go` — `loadOrderMetaMap` batch query + 3 loaders novos:
  - `marketing` (UTM source/medium/campaign/term/content + referrer + landing_page)
  - `fiscal` (NF-e chave/numero/serie/url/status)
  - `tracking` (code/url/carrier — `_tracking_*` meta wins sobre wc_me_labels)
  - extende motoboy section com `telefone/placa`, customer com `rg`

### Páginas React enriquecidas
- ✅ `OrderDetail.tsx` — cards Marketing, Fiscal, Rastreio, Motoboy/Entregador full (tel:), CPF/RG com copy
- ✅ `Orders.tsx` — drawer inline ao clicar linha (lazy fetch `/orders/{id}`) com Motoboy/Rastreio/NFe/UTM compactos
- ✅ `Affiliates.tsx` — 15 colunas (Tel/CPF/PIX/links/clicks/Vendido 30d/Comissão 30d/Último pedido)
- ✅ `Commissions.tsx` — Pedido#, total, emails, link checkout com CopyButton, liberação em
- ✅ `Products.tsx` — 3 KPI cards, botão "Sincronizar do histórico", empty state
- ✅ `CheckoutLinks.tsx` (nova) — tabela 11 col + form create + KPIs + toggle/delete

### Layout / navegação
- ✅ Sidebar collapsible com `localStorage` (já entregue v461)
- ✅ Grupo "Cash on Delivery" unificando Motoboy + COD (19 itens)
- ✅ Menu "Links Checkout" adicionado em "Usuários"
- ✅ ErrorBoundary wrappando Routes

### Commits enviados
- `cbb0a96` — v462 (Cash on Delivery menu + Products + FilterDrawer + Orders detail inline)
- `4b675e5` — v463 (FilterTopPanel + OrderDetail UTM/NFE/tracking + migration v463)
- `81f79c2` — v464 (CheckoutLinks CRUD + Affiliates/Commissions enriquecidos + Products sync + 16 telas migradas)

---

## ⏳ Status deploy

| Commit | Conteúdo | VPS rodou? |
|---|---|---|
| `4b675e5` v463 | FilterTopPanel + OrderDetail UTM/NFE/tracking + migration v463 (SQL renames + ADD cols) | ❌ NÃO |
| `81f79c2` v464 | CheckoutLinks CRUD + Affiliates/Commissions enriquecidos + Products sync + 16 telas FilterTopPanel | ❌ NÃO |
| `54e9187` v465 | Wallet + CodWalletProducer — inputs inline movidos p/ dentro do FilterTopPanel | ✅ SIM (só admin-ui) |

**Bloqueio:** validação completa depende de rodar v463+v464 (SQL + admin-service rebuild). Sem isso, Wallet Produtor COD / Transações COD / Saques COD seguem com erro `column "user_id" does not exist`.

---

## 🚀 Deploy v463+v464 completo (PENDENTE)

```bash
ssh -p 10037 administrator@93.127.141.6
sudo -i

cd /opt/senderzz-v459 && git pull origin main

docker exec -i senderzz-postgres psql -U senderzz -d senderzz < infra/postgres/schema-fixes-v463.sql 2>&1 | tail -20

cd infra/docker && docker compose build admin-service admin-ui 2>&1 | tail -10

docker rm -f senderzz-admin senderzz-admin-ui 2>/dev/null

docker run -d --name senderzz-admin --network senderzz-net --restart unless-stopped -p 8087:8087 \
  -e DATABASE_URL='postgresql://senderzz:VB76BoHhg94K16jcVnzJxvBLEp47fLnXG17YKVbu@senderzz-postgres:5432/senderzz?sslmode=disable' \
  -e ADMIN_JWT_SECRET=98cd2c9d45c8a84b9ffbd5ca7d5fd3386ab138e5155aed7ec85e06e354e66bed \
  -e APP_BASE_URL=https://app.senderzz.com.br -e PORT=8087 -e TZ=America/Sao_Paulo \
  docker-admin-service:latest

docker run -d --name senderzz-admin-ui --network senderzz-net --restart unless-stopped -p 8089:80 docker-admin-ui:latest

sleep 3 && docker logs senderzz-admin --tail 25 && echo "---UI---" && docker logs senderzz-admin-ui --tail 10
```

---

## ✅ Validação em browser (após deploy)

Cada tela com FilterTopPanel: confirmar abre/fecha, draft state, chips com X-to-remove, aplica só ao confirmar.

Telas críticas:
- [ ] Wallet Produtor COD — sem erro `column "user_id" does not exist`
- [ ] Transações COD — sem erro `t.user_id`
- [ ] Saques COD — colunas novas (`fee/net/holder_*`)
- [ ] Carteira Afiliados — colunas `balance/pending_balance/debt_amount`
- [ ] Products — botão "Sincronizar do histórico" funciona quando vazio
- [ ] CheckoutLinks — criar link + copy URL
- [ ] OrderDetail — UTM/NFE/tracking aparecem quando há meta gravada
- [ ] OrderDetail — telefone motoboy clicável (tel:) + placa
- [ ] Affiliates — Tel/CPF/PIX/links/clicks/Vendido 30d preenchidos
- [ ] Commissions — Pedido#, emails, link checkout com copy

---

## 📋 Próximos passos (curto prazo, após deploy)

- ⏳ **Task #8** — Empty states + loading skeletons (esqueletos cinza em todas listas durante fetch)
- ⏳ **Skeleton de OrderDetail** quando lazy fetch
- ⏳ **DNS `app.senderzz.com.br`** — apontar via CloudFlare CNAME ao túnel nomeado persistente (hoje em URL trycloudflare temporária `pioneer-salon-domains-wheel.trycloudflare.com` — cai quando `cloudflared` reinicia)
- ⏳ **Backup automatizado Postgres** — `pg_dump` cron + retenção 30 dias + offsite

## 🗺️ Médio prazo

- ⏳ **Drag-and-drop reorder** no sidebar (planejado)
- ⏳ **Lote 3 FilterTopPanel decidido SKIP** (NotificacoesPWA, OrderMetaNormalization, AffiliateRules, CodTaxasEntrega, TrackingBrand — config-only sem lista filtrável; validado nesta sessão)
- ⏳ **Telas P2 restantes**: PushTecnico admin (VAPID), ApiDocs viewer
- ⏳ **Webhook bridge MELHOR ENVIO** — migrar do plugin WP p/ `senderzz-labels` Go
- ⏳ **PIX confirmation guard** — migrar PHP → Go com mesma fail-closed semantic

## 🌐 Longo prazo (Fase B)

- ⏳ **Portal User V2 (React)** — substituir templates `portal/v2/sections/*.php`
- ⏳ **PWA Motoboy migração** — ainda em PHP/JS, mover p/ React/SW dedicado
- ⏳ **CDC WP → Postgres** — substituir pgloader read-once por replicação contínua (debezium ou writes Go diretas)

---

## ⚠️ Riscos conhecidos

- **CDN admin-ui** — rebuilds frequentes geram hash novo dos assets; navegador agressivo precisa hard-refresh (Ctrl+Shift+R) após cada deploy
- **`senderzz_affiliate_transactions`** — só existe em `schema-fixes-v460.sql`. Se algum env não tiver v460 aplicado, queries falham. v463 guard com DO block protege ALTER, mas SELECT/INSERT do código não — risco baixo (todos prod já tem v460)
- **`sync-from-orders`** — só preenche nome/sku/preço; descrição/categoria/imagem ficam vazias. Lojista completa via UI após import
- **Cloudflare quick tunnel** — URL `trycloudflare.com` é temporária. Se `cloudflared` cair, deploy URL muda; CNAME `app.senderzz.com.br` é a solução
- **NAT DatabaseMart** — SSH bloqueado direto do Mac; toda interação VPS via porta 10037 + tunnel cloudflare

