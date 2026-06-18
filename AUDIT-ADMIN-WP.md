# Auditoria EXAUSTIVA — Admin WordPress → Painel Admin React

Data: 2026-06-16 | Plugin v459 | Fonte: `senderzz-logistics.php` + `includes/*.php` + `src/Admin/*`

**Critério**: nenhuma tela, KPI, filtro, formulário, modal, botão ou ação visível no admin WP pode faltar no painel React. Cada item abaixo MAPEIA WP → REACT.

---

# RESUMO EXECUTIVO

| Métrica | Quantidade |
|---|---|
| Telas/abas admin WP totais | **41** |
| Modais distintos | **18** |
| Formulários de configuração | **27** |
| KPIs únicos exibidos | **52** |
| Tabelas de dados (grids) | **23** |
| Crons agendados | **18** |
| REST routes (admin + interno) | **109** |
| Capabilities customizadas | **5** + guard `manage_options` |
| Tabelas BD próprias | **23** |
| Options WP gravadas via admin | **60+** |
| Security patches a preservar | **20+** (CRIT-01..04 + V-NEW + A/B/C/P + M) |
| **Telas React já existentes** | **16** (esqueléticas) |
| **Telas React a criar** | **31 novas** |

---

# 1. UNIFIED MENU (5 ÁREAS)

Arquivo: `src/Admin/Unified_Menu.php` (1868 linhas)
URL base: `/wp-admin/admin.php?page=senderzz&area={area}&tab={tab}`
Areas: `visao | cod | financeiro | expedicao | config`

## 1.1 Visão Geral (`visao/overview`)

**Função**: `tab_overview_operacao()` :688
**React equivalente**: `Dashboard.tsx` (existe, esqueleto)

### KPIs (6 cards)
| Card | Cálculo | Cor |
|---|---|---|
| Pedidos Hoje | COUNT status IN (wc-processing/agendado/completed/frustrado) WHERE DATE(hoje) | neutro |
| Agendados | COUNT status='wc-agendado' | warn |
| Em Rota | COUNT status IN (wc-em-rota/wc-em_rota) | neutro |
| Entregues | COUNT status IN (completed/entregue) WHERE DATE(hoje) | ok |
| Frustrados | COUNT status='wc-frustrado' WHERE DATE(hoje) | neutro |
| Alertas | SUM(audit_counts) + webhook_failures + stopped_orders | warn/ok dinâmico |

### Seção "Alertas operacionais"
Botão: "Abrir auditoria" (se alerts) | "Ver pedidos" (?stopped=1)
Linhas (cada com contador + link):
1. Saldo divergente
2. Afiliado sem transação
3. Wallet divergente
4. Split financeiro divergente
5. Webhook falhando (7d)
6. Pedido parado (24h+ em agendado/embalado/em_rota)

### Funções auxiliares
- `webhook_failure_count()` :339 — query em 4 tabelas (wcme/sz/senderzz_webhook_logs/senderzz_producer_webhook_logs)
- `stopped_order_rows()` :646 — meta `_senderzz_motoboy_flow_status`

---

## 1.2 Motoboy (`cod/cod-pedidos`)

**Função**: `tab_cod_pedidos()` :579 — delega para `sz_mb_admin_page()` (motoboy módulo, ver §2)
**React equivalente**: Combinação de `Motoboys.tsx` + `MotoboysDay.tsx` + `Orders.tsx` + `Cds.tsx` + `Zonas.tsx`

### Filtros (form GET)
- `data` (date) → DATE(data_criacao) = valor
- `status` (select) → wc-agendado/em-rota/em_rota/frustrado/completed/processing
- `cidade` (text) → LIKE %cidade% em _billing_city
- `s` (search) → ID pedido OR email
- `stopped` (checkbox oculto) → mostra apenas 24h+ parados

### Tabela pedidos (HPOS-aware)
Colunas: Pedido | Cliente | Cidade | Status (badge) | Produto | Afiliado | Comissão | Ações
Ações por linha:
- "Abrir" → order_admin_url(id) (HPOS-aware)
- "Auditar" → POST `sz_admin_audit_fix_order` + nonce `sz_admin_audit_fix_order_{id}`

Limite: 150

---

## 1.3 Financeiro COD (`financeiro/*`)

### 1.3.1 Livro COD (`fin-livro-cod`)
**Função**: `tab_fin_taxas()` :973
**React novo**: `CodLivro.tsx` (P0)

#### KPIs (5)
- Bruto COD (sem frustrados)
- Afiliados (repasse)
- Taxas Senderzz (operação)
- Líquido produtor (cor ok)
- Previsto produtor (cor warn — agendados/em aberto)

#### Filtros
- `fin_de` (date, default -7d)
- `fin_ate` (date, default hoje)

#### Tabela split (22 colunas — TODAS necessárias no React)
1. Pedido | 2. Data | 3. Situação (Recebido/Estornado/Previsto) | 4. Afiliado | 5. E-mail afiliado
6. Produtor | 7. Comissão % | 8. Valor pedido | 9. Bruto válido | 10. Taxas Senderzz
11. Taxa entrega | 12. Taxa transação | 13. Valor afiliado | 14. Líquido produtor
15. Valor não recebido | 16. Bruto estornado | 17. Frustrado afiliado | 18. Frustrado produtor
19. Repasse pendente | 20. Repasse disponível | 21. Repasse estornado | 22. Status repasse

Limite: 300

#### Resumos colapsáveis
**Por Afiliado**: pedidos, recebidos, previstos, frustrados, pendente, disponível, previsto_valor
**Por Produtor**: pedidos, recebidos, frustrados, previstos, bruto, bruto_previsto, taxas_senderzz, afiliado, liquido_produtor, frustrado_produtor, frustrado_afiliados, frustrado_valor

---

### 1.3.2 Saques (`fin-saques`)
**Função**: `tab_fin_saques()` :1529
**React novo**: `CodSaques.tsx` (P0) + `AffiliateSaques.tsx` (P0)

#### Seção 1: Saques COD/Produtor
Delegação: `sz_cod_admin_page()` (cod-wallet.php)

**Form global** (line 858):
- `senderzz_cod_finance_settings.retention_days`
- `senderzz_cod_finance_settings.withdraw_fee`
- `senderzz_cod_finance_settings.anticipation_fee_pct`
- `sz_admin_motoboy_fee`
- `sz_admin_operational_fund_fee`
Nonce: `sz_cod_finance_save`

**Overrides por produtor** (user meta):
- `_senderzz_cod_retention_days`
- `_senderzz_cod_withdraw_fee`
- `_senderzz_cod_anticipation_fee_pct`

**Tabela saques pendentes** (cod-wallet.php :857):
- Status flow: `analysis → pending → paid`
- Form por linha: upload `proof_url`, input `admin_note`, botão "Marcar pago"
- Nonce: `sz_cod_complete_withdraw`

#### Seção 2: Saques Afiliados
Handler: `process_affiliate_withdrawal_admin_action()` :1482
- Nonce: `sz_aff_withdraw_action`
- Ações: approve (debita wallet, INSERT tx tipo=withdrawal, UPDATE status=approved) | reject

Tabela: ID | Afiliado | Valor | Taxa | Líquido | PIX/Conta | Status (badge) | Ação inline
Ordem: FIELD(status, pending, analysis, em_analise, approved, rejected), id DESC
Limite: 120

---

### 1.3.3 Taxas de Entrega (`fin-taxas-entrega`)
**Função**: `tab_fin_taxas_entrega()` :1333
**React novo**: `CodTaxasEntrega.tsx` (P0)

Nonce: `sz_save_taxas_entrega`

#### KPI Grid 1 — Taxas Principais (4 inputs)
| Campo | Option | Default |
|---|---|---|
| Cliente COD entrega | sz_motoboy_taxa_entrega | 25 |
| Venda taxa transação % | sz_motoboy_taxa_percentual | 0 |
| Motoboy entrega | sz_mbw_taxa_entrega | 18 |
| Motoboy frustrado | sz_mbw_taxa_frustrado | 5 |

Modo transação hard-coded: `sz_motoboy_taxa_transacao_modo='split_producer_affiliate'`

#### KPI Grid 2 — Penalidades Frustrado (4 inputs)
| Campo | Option | Default |
|---|---|---|
| Afiliado 1ª frustração | sz_aff_first_frustration_penalty | 5 |
| Afiliado reincidente | sz_aff_default_penalty_value | 5 |
| Produtor 1ª frustração | sz_prod_first_frustration_penalty | 0 |
| Produtor reincidente | sz_aff_producer_frustration_penalty | 8 |

#### Seções colapsáveis
**Taxas por motoboy**: Tabela com input entrega + frustrado por motoboy. Options `sz_mbw_taxa_entrega_mb_{id}`, `sz_mbw_taxa_frustrado_mb_{id}`.

**Config Senderzz por produtor**: Tabela com checkbox ativo + 4 inputs (entrega, manuseio, transação%, frustrado 1ª/reinc) por produtor. User meta `_sz_motoboy_ativo`, `_sz_motoboy_taxa_entrega`, `_sz_motoboy_taxa_manuseio`, `_sz_motoboy_taxa_percentual`. Cache invalidate: `wp_cache_delete('sz_mb_portal_uid_'.id, 'sz_motoboy')`.

**Regras particulares afiliado**: Tabela com 2 inputs (1ª frustração, reincidente) por afiliado. Option JSON `sz_frustration_aff_overrides[id]={first,repeat}`.

**Regras particulares produtor**: idem `sz_frustration_prod_overrides`.

---

### 1.3.4 Auditoria (`fin-auditoria`)
**Função**: `tab_auditoria()` :1590
**React novo**: `AuditEngine.tsx` (P0 — crítico finance)

#### KPIs (4 cards)
- Divergências totais (SUM)
- Total divergente (counts.split)
- Repasse pendente (counts.aff_missing)
- Carteira divergente (counts.aff_bad + counts.wallet)

#### Botão CTA
"Corrigir tudo" → POST `sz_admin_audit_fix_all` + nonce `sz_admin_audit_fix_all`

Métodos chamados:
1. `fix_missing_affiliate_transactions()` :496 — INSERT tx pending com meta_json=[source:admin_audit_fix]
2. `fix_bad_affiliate_transactions()` :534 — UPDATE amount + meta_json
3. `fix_bad_producer_wallet()` :553 — UPDATE gross/fee/net + invalidate cache
4. `fix_bad_affiliate_wallet_summaries()` :485 → `sync_affiliate_wallet_summary()` :440

#### Tabela problemas
Colunas: Pedido | Tipo (badge warn) | Esperado | Atual | Ação ("Corrigir pedido" POST `sz_admin_audit_fix_order`)
Tipos:
- "Total divergente" (split): bruto ≠ aff+taxa+prod
- "Afiliado sem transação" (aff_missing)
- "Comissão afiliado divergente" (aff_bad)
- "Produtor COD divergente" (wallet)

---

### 1.3.5 Carteiras (`fin-carteiras` — wrap de 2 seções)
**Função**: `tab_fin_carteiras()` :1476 → delega
**React novo**: `CodWalletProducer.tsx` (P1) + `AffiliateWallet.tsx` (P0)

#### Carteira Produtores `tab_fin_produtores()` :742
KPIs (5): Bruto COD | Afiliados | Taxas Senderzz | Líquido produtor (ok) | Previsto (warn)
Tabela (11 cols): Produtor | Bruto | Afiliados | Taxas | Líquido | Previsto | Potencial frustrado | Pedidos | Entregues | Previstos | Frustrados
Limite: 150

#### Carteira Afiliados `tab_fin_carteira_afiliados()` :918
Aviso: "Repasses somente de transações ativas. Comissões canceladas/estornadas NÃO entram."
KPIs (2): Pendente (warn) | Disponível (ok)
Tabela (7 cols): ID | Afiliado | Pendente | Disponível | Saques | Penalidades | Pedidos válidos
Limite: 300

---

## 1.4 Expedição (`expedicao/*`)

### 1.4.1 Pedidos (`exp-pedidos`)
**Função**: `tab_exp_pedidos()` :1712
**React equivalente**: `Labels.tsx` (existe, esqueleto)

KPIs (4): Hoje | Processando | Entregue | Cancelado
Delegação: `WC_MelhorEnvio\Analytics\Margin_Dashboard::render_inline()`

---

### 1.4.2 Integrações (`exp-integracoes`)
**Função**: `tab_exp_integracoes()` :1735
**React novo**: `ExpedicaoIntegracoes.tsx` (P1)

Atalhos: "Ver Webhooks" | "Carteira Frete"
Delegação: `senderzz_markup_admin_page()` (markup.php :149)

#### Markup Rules (markup.php)
- Option `senderzz_markup_default` = {pct, fixed}
- Option `senderzz_markup_rules` = array[class_id]=>{pct, fixed}
- Form: global + per-class override table com preview
- Nonce: `senderzz_salvar_markup`
- Filter aplicado: `wc_melhor_envio_rate_args` priority 74

---

### 1.4.3 Webhooks (`exp-webhooks`)
**Função**: `tab_exp_webhooks()` :1749
**React novo**: `ExpedicaoWebhooks.tsx` (P1)

Endpoint display: `rest_url('senderzz/v1/webhook')`
Delegação: `senderzz_pw_admin_page()` (producer-webhooks.php)

---

### 1.4.4 Carteira Frete (`exp-carteira`)
**Função**: `tab_exp_carteira()` :1762
**React equivalente**: `Wallet.tsx` (existe, esqueleto)
Delegação: `tpc_admin_render()` (ver §3)

---

## 1.5 Configurações (`config/*`)

### 1.5.1 Geral (`cfg-geral`)
**Função**: `tab_configuracoes()` :1767
**React equivalente**: `Settings.tsx` (existe, esqueleto)
Delegação: `tpc_tab_configuracoes()` (ver §3.3)

#### Inline: Normalização Order Meta `tab_meta_normalization()` :1780
**React novo**: `OrderMetaNormalization.tsx` (P2)
Form:
- batch_size (10-500, default 50)
- offset (default 0)
- checkbox dry_run
Nonce: `sz_normalize_meta`
Result: `?sz_norm_done=X&sz_norm_updated=Y&sz_norm_next=Z`
Relatório divergências via `Senderzz_Order_Meta::get_divergence_report()` (50 últimas)
Botão "Limpar log" (`sz_clear_meta_log`)

---

### 1.5.2 Usuários (`cfg-usuarios`)
Delegação: `Portal_Admin::render_page()`
**React equivalente**: `Users.tsx` (existe, esqueleto — precisa de paridade com Portal_Admin)

---

### 1.5.3 Notificações (`cfg-notificacoes`)
Delegação: `sz_app_pwa_render_notifications_admin()`
**React novo**: `NotificacoesPWA.tsx` (P1)

#### Templates push (app-pwa.php :188)
11 eventos: agendamento_cod | em_rota_cod | completo_cod | frustrado_cod | pedido_feito | enviado_pad | entregue | (+4 outros)
Por evento × 3 roles (producer/affiliate/admin) → 6 campos: title_producer, body_producer, title_affiliate, body_affiliate, title_admin, body_admin

#### Variáveis disponíveis
{{numero_pedido}} {{comissao_produtor}} {{comissao_afiliado}} {{cliente}} {{valor_total_pedido}} {{transportadora}} (role-based scrubbing)

#### Status binding (app-pwa.php :296)
Option `sz_app_pwa_notification_status_map[event_key] = wc-status` (mutual exclusion)

#### Recipients matrix
Option `sz_app_pwa_notification_recipients[event][role] = 0|1`
Default: producer=1, affiliate=1, admin=0 (exceto admin_*)

#### Admin recipients
Option `sz_app_pwa_admin_recipients[event] = [user_id, ...]` multi-checkbox WP admins

#### Order number flag
Option `sz_app_pwa_notification_order_number_flags[event] = bool`

Nonce: `sz_app_pwa_notif_save`

---

### 1.5.4 Push (`cfg-push`)
Delegação: `sz_notif_admin_page()` (notifications.php :1162)
**React novo**: `PushTecnico.tsx` (P2)

#### Status
- Device count (sz_push_subscriptions)
- Log count (sz_notif_log)
- VAPID keys status

#### Form Regenerar VAPID
Nonce + confirm — gera novo keypair EC (prime256v1) base64url
Options: `sz_notif_vapid_public`, `sz_notif_vapid_private`

#### Form Teste
Input user_id → envia push real → log HTTP response

#### Log table (last 25)
Date | User | Event | Recipient type | Order | Status | HTTP code | Error | Botão "Reprocessar"

---

### 1.5.5 API (`cfg-api`)
Delegação: `tpc_tab_api()` (ver §3.4)
**React novo**: `ApiDocs.tsx` (P2)

---

### 1.5.6 Diagnóstico (`cfg-saude`)
**Função**: `tab_saude_sistema()` :1850
**React equivalente**: `Tools.tsx` (existe, esqueleto)

Tabela 4 linhas:
- Pedidos (tabela meta presente?)
- Afiliados (sz_affiliate_transactions existe?)
- Carteira COD (sz_cod_wallet_transactions existe?)
- Problemas auditoria (contador)

Botões: "Abrir auditoria" | "Ver Push" | "Repair checkouts legacy" (condicional)

---

## 1.6 Redirects Legacy (17 slugs antigos)
Em `redirect_legacy_pages()` :54 — mapa de URL antiga → nova:
tpc-carteira/senderzz-markup/tp-preferida/senderzz-blocked-carriers/senderzz-tracking-brand/senderzz-remetentes/senderzz-webhooks/senderzz-portal-users/senderzz-analytics/senderzz-kits/senderzz-cod-finance/sz-motoboy/senderzz-affiliates/senderzz-dashboard/senderzz-onboarding/senderzz-notifications/senderzz-pwa-notificacoes

**React**: replicar via React Router redirects.

---

## 1.7 Admin POST Actions
| Action | Handler | Linha | Nonce |
|---|---|---|---|
| sz_admin_audit_fix_all | handle_audit_fix_all() | :407 | sz_admin_audit_fix_all |
| sz_admin_audit_fix_order | handle_audit_fix_order() | :418 | sz_admin_audit_fix_order_{id} |
| sz_admin_fix_affiliate_wallet | handle_fix_affiliate_wallet() | :430 | sz_admin_fix_affiliate_wallet_{id} |
| senderzz_normalize_meta | inline :1810 | :1810 | sz_normalize_meta |

---

# 2. MOTOBOY ADMIN (11 abas + metabox)

Arquivo: `includes/motoboy/admin.php` (2268 linhas) + `order-metabox.php`

## 2.1 Dashboard Motoboy
**Função**: `sz_mb_tab_dashboard()` :224
**React novo**: `MotoboyDashboard.tsx` (P0)

KPIs: Agendados 🟠 | Embalados 🟣 | Em Rota 🔵 | Entregues ✅ | Frustrados ❌ | Motoboys com pendência fechamento
Tabela ranking hoje: Motoboy | CD/Zona | Pedidos | Entregues | Frustrados | Taxa Sucesso %

## 2.2 Pedidos Motoboy
**Função**: `sz_mb_tab_pedidos()` :444
**React equivalente**: `Orders.tsx` (existe — precisa ações inline completas)

### Filtros
Botões inline (não select): todos | agendado | embalado | em_rota | entregue | frustrado | cancelado

### Tabela (cards)
ID Woo (link) | Cliente | Cidade+CEP | CD/Zona | Motoboy | Status badge + Valor R$ | Botões

### Ações inline (condicionais por status)
- **agendado/aprovado**: form "📦 Embalar" + select motoboy_id (+ "Automático")
- **embalado**: texto "Rota só via QR no PWA"
- **agendado/frustrado**: Botão "📅 Reagendar" (JS prompt data) + Form cancelar (confirm)
- **agendado/embalado/em_rota**: Form "✅ Entregue" (chama `sz_motoboy_mudar_status` actor='alan')
- **embalado/em_rota**: Form "🔄 Transferir" + select motoboy_id (atualiza baixa_motoboy_id se entregue/frustrado, dispara push)
- **frustrado com motivo**: Botão "🔍 Ver motivo" (modal: motivo, observação, foto)

Nonces: `sz_mb_embalar`, `sz_mb_reagendar`, `sz_mb_cancelar`, `sz_mb_entregar`, `sz_mb_transferir`
Limite: 500 (hardcoded)

Modal popup `szMbAdminModal` :729 — close button + backdrop click

---

## 2.3 Motoboys CRUD
**Função**: `sz_mb_tab_motoboys()` :792
**React equivalente**: `Motoboys.tsx` (existe esqueleto, precisa CRUD completo)

### Form (esquerda)
| Campo | Tipo | Required |
|---|---|---|
| Nome | text | sim |
| Telefone (DDD) | tel | sim |
| CPF | text | não |
| E-mail | email | não |
| CD | select | sim |
| Zonas atuação | multi-select (size=8, optgroup por CD) | sim |
| Tipo | select: Autônomo/PJ-MEI/CLT | — |
| Token App | readonly | edit only |
| PIN PWA | password 4-8 dígitos | não |

PIN: bcrypt PASSWORD_BCRYPT; help text muda baseado em estado
Zonas adicionais: `sz_motoboy_zona_pivot` (multi-zone)
Nonce: `sz_mb_motoboy_save`

### Tabela (direita)
Nome | CD/Zonas | Telefone | Token (10 chars + …) | Ações (✏️ editar, 🗑️ soft-delete `delete_mb=ID` + nonce `sz_mb_delete`)

Messages: saved | deleted | error | pin_curto

---

## 2.4 Zonas + CEPs
**Função**: `sz_mb_tab_zonas()` :1003
**React equivalente**: `Zonas.tsx` (existe) + `Cds.tsx` (existe)

### Seção 1: Novo CD `save_cd`
Nonce: `sz_mb_zona_save_save_cd`
Fields: nome*, cidade*, uf (default SP), endereço
INSERT `sz_motoboy_cds`

### Seção 2: Nova Zona + CEP `save_zona`
Nonce: `sz_mb_zona_save_save_zona`
Fields: cd*, nome*, descricao, dias_funcionamento* (checkboxes seg-dom), horário limite único* (time, default 21:00), cep_inicio* + cep_fim*

Validações:
- CEP normalize `preg_replace(/\D/, '')` max 8 chars
- cep_fim >= cep_inicio
- Overlap check SQL: NOT (cep_fim < inicio OR cep_inicio > fim)
- Dias passa por `sz_motoboy_sanitize_zone_days()`
- Cutoff passa por `sz_motoboy_single_cutoff_payload()`

INSERT zona + INSERT cep_zona (OBRIGATÓRIO — prevenir zona "fantasma")

### Seção 3: Adicionar CEP `save_cep`
Nonce: `sz_mb_zona_save_save_cep`
Fields: zona*, cep_inicio*, cep_fim*

### Tabela CEPs (com filtros client-side JS)
Filtros: `szCepSearch` (texto zona/CD/CEP), `szCepOperationFilter` (seg-sab/sexta/sabado/domingo), `szCepZoneFilter` (zonas)

Row inline edit: `is-editing` class toggle. Form: cep_inicio, cep_fim, dias_funcionamento checkboxes, horário limite, botão "Salvar CEP e dias" (nonce `sz_mb_zona_save_update_cep`)

Row delete:
- "🗑️ Remover faixa" → `delete_cep=ID` + nonce `sz_mb_delete_cep`
- "🗑️ Remover zona" → `delete_zona=ID` + nonce `sz_mb_delete_zona` (cleanup pivot + UPDATE motoboys zona_id=0)

CEP Preview tester :1277 — input CEP, botão "Verificar", result "✅ está em ZONA / Operação: DIAS" ou "❌ fora das faixas"

Tabela CDs cadastrados :1287 — Nome | Cidade/UF | Zonas (count)

Messages: saved | deleted | cep_invalid | cep_order | cep_overlap | ghost_prevented

---

## 2.5 Etiquetas (QR Code + Print)
**Função**: `sz_mb_tab_etiquetas()` :1383
**React novo**: `MotoboyEtiquetas.tsx` (P1)

Filtros: data_etiq (default hoje), st_etiq (agendado/embalado/em_rota)
Botão "🖨️ Imprimir etiquetas" — `window.print()` + CSS @media print

Grid auto-fill minmax(280px,1fr). Card:
- Header: #wc_order_id | Motoboy · Zona
- Cliente bold + Endereço formatado + Footer: R$ valor | forma pagto
- Telefone + QR (api.qrserver.com 160x160) com `sz_mbc_package_code(pedido_id,wc_order_id)`
- CEP formatado canto

@media print CSS :1424: hide tudo exceto #sz-etiquetas-print, page-break-inside:avoid

---

## 2.6 Estoque/Custódia
**Função**: `sz_mb_tab_estoque_motoboy()` :1510
**React novo**: `MotoboyCustodia.tsx` (P1)

### KPIs (5)
Com motoboy | Frustrados | Aguardando OL | Avariados | Reservados
GROUP BY physical_status em `sz_motoboy_stock_custody`

### Accordion 1: Rota assistida QR `sz_mb_custody_route_assist`
Fields: qr_code*, motoboy*. Nonce `sz_mb_custody_route_assist`. Chama `sz_mbc_start_route_by_qr()`.

### Accordion 2: Devolução `sz_mb_custody_return` (OPEN)
multipart/form-data. Fields: qr_code*, condition* (vendavel/avariado/extravio/perda/violado/divergente), note (obrigatório se avariado/extravio/perda), foto (file accept=image/*)
Nonce `sz_mb_custody_return`. wp_handle_upload. Chama `sz_mbc_return_by_qr()`.
Messages: custody_returned | custody_damaged | photo_required | error

### Accordion 3: Resumo por motoboy (OPEN)
Motoboy | Pedidos | Unidades | Frustrados | Aguardando OL | Custo custódia | Última movimentação

### Accordion 4: Pacotes em custódia
Pedido | Status (mapped label) | Motoboy | Produto | Qtd | QR | Ocorrência (tipo + nota 12 words + links fotos) | Atualizado
WHERE physical_status IN (with_motoboy, frustrated, return_declared, damaged) LIMIT 300

---

## 2.7 Mapa Ao Vivo (Leaflet)
**Função**: `sz_mb_tab_mapa()` :1761
**React novo**: `MotoboyMapa.tsx` (P1) — usar react-leaflet

Centro [-23.55,-46.63] zoom 11. Markers laranja divIcon. Popup: nome + zona + pedidos abertos + entregues hoje.
REST: `rest_url('sz-motoboy/v1/alan/localizacao')`
Auto-refresh setInterval 30s
Pill verde (ativo <5min) ou cinza (offline)

---

## 2.8 Conciliação Bancária
**Função**: `sz_mb_tab_conciliacao()` :2064
**React novo**: `MotoboyConciliacao.tsx` (P1)

Filtros: data_inicio (default -7d), data_fim (default hoje). Botões "Filtrar" | "Últimos 7 dias" | "↓ CSV"

KPIs (8): Pedidos período | Entregues (ok) | Frustrados (err) | Aguardando conciliação (warn) | Dinheiro | PIX | Cartão | Taxa frustrado

Tabela `sz_motoboy_pedidos` WHERE status IN (entregue, frustrado) AND data range:
Data | Pedido | Motoboy | Zona | Cliente | Status | Forma | Dinheiro | PIX | Cartão | Taxa frustrado | Total validado | Conciliação | Ação

Ação "Conciliar" (form POST `sz_mb_conciliar_pedido` confirm) → UPDATE `sz_motoboy_ganhos` status=disponivel
CSV export client-side `szMbExportCSV()` filename `conciliacao-pedidos-{inicio}-{fim}.csv`

---

## 2.9 Carteiras Motoboy (Pagamentos)
**Função**: `sz_mb_tab_carteiras()` :1837
**React novo**: `MotoboyCarteira.tsx` (P0)

Schema migration: adiciona coluna `valor_pago` em `sz_motoboy_ganhos`

### Sync wallets
Nonce `sz_mb_sync_wallets` → chama `sz_mbw_sync_all_data()`

### KPIs (3)
Disponível (ok) | A conciliar (warn) | Pago acumulado

### Tabela carteiras (9 cols)
Motoboy | Zona | Telefone | Disponível | A conciliar | Pago | Lançamentos | Pagamento | Histórico

#### Botão "Pagamento" (se saldo>0)
Form inline `sz_mb_wallet_pay`. Nonce `sz_mb_registrar_pagamento_valor`
Fields: valor_pago (prefill saldo), data_pagamento (default hoje), confirm
INSERT `sz_motoboy_pagamentos`, itera ganhos status=disponivel ASC, UPDATE valor_pago, status

#### Botão "Histórico" → `?historico_wallet=ID`
Tabela: Data | Pedido | Tipo | Valor ganho | Pago neste | Saldo aberto | Data saque | Status | Recebido cliente
LIMIT 150

---

## 2.10 Saques Motoboy
**Função**: `sz_mb_tab_saques()` :2016
**React novo**: `MotoboySaques.tsx` (P1)

Tabela `sz_motoboy_pagamentos`: ID | Motoboy | Telefone | Valor | Data saque | Status | Observação | Criado em
ORDER BY status (aguardando primeiro) DESC, created_at DESC LIMIT 200

---

## 2.11 Config Motoboy
**Função**: `sz_mb_tab_config()` :1345
**React novo**: `MotoboyConfig.tsx` (P0)

Nonce: `sz_mb_config_save`
Fields:
- geofence_metros (default 500)
- horario_inicio (default 08:00)
- horario_fim (default 18:00)
- cc_fee_pct (0-30%, default 0) — help: "0 = não exibir 2º valor; valor cartão = pedido × (1+taxa/100)"

---

## 2.12 Order Metabox Motoboy
Arquivo: `includes/motoboy/order-metabox.php` :112
**React equivalente**: parte de `OrderDetail.tsx` (P0 novo)

ID metabox: `sz_mb_entrega_cod` — title "🛵 Entrega COD — Senderzz"
Screens: shop_order + woocommerce_page_wc-orders
Context: normal, prioridade high

### Painel laranja Controle Manual
Botões (POST `sz_order_force_motoboy` + nonce `sz_order_force_motoboy_{id}`):
- "Definir/voltar Agendado" (laranja)
- "Marcar Embalado" (preto)
- "Marcar Entregue" (verde)
- "Marcar Frustrado" (vermelho)
- "Cancelar COD" (vermelho)

Em rota BLOQUEADO — msg `route_qr_only`
Help: "Em rota é exclusivo do motoboy lendo QR no PWA"

### Seção 1: Motoboy responsável (badge)
Motoboy nome | Status (cor) | Produto (via `senderzz_clean_product_summary()`)

### Seção 2: Dados entrega
- Baixa por (admin azul / motoboy verde)
- Motoboy entregador
- Data/hora baixa
- Recebedor + badge tipo (Cliente/Terceiro)
- CPF recebedor
- Total recebido (verde grande): Dinheiro | PIX | Cartão
- GPS entrega (link Google Maps)
- Confirmação motoboy (badge "✅ Confirmado em X" ou "⏳ Pendente")

### Seção 3: Comprovantes
Grid imagens 72x72 clicáveis. Badge tipo pagto (💵 💳 📱) + submarcador (ADM/MB)

### Seção 4: Histórico auditoria
Tabela vertical de `sz_motoboy_audit` WHERE pedido_id:
Timestamp | Actor (motoboy/admin/Operador) | Ação | Badges status (de → para) | Metadata (afiliado, recebedor)

---

## 2.13 Funções/Helpers Motoboy

| Função | Linha | Uso |
|---|---|---|
| sz_mb_admin_status_from_wc | :304 | WC status → motoboy status |
| sz_mb_admin_sync_row_from_wc | :393 | reconverge status |
| sz_mb_get_motoboy_zonas | :778 | zonas de um motoboy |
| sz_motoboy_backfill_recent_orders | (router) | backfill 3000 últimos |
| sz_mbw_sync_all_data | (wallet) | rebuild wallet + fechamento |
| sz_mbc_set_pedido_status | (custody) | custódia tracking |
| sz_mbc_start_route_by_qr | (custody) | inicia rota via QR |
| sz_mbc_return_by_qr | (custody) | devolução |
| sz_mbc_package_code | (custody) | QR code generator HMAC |

---

# 3. TPC / WALLET / PIX (5 abas)

Arquivo: `includes/tpc/admin.php` (1403 linhas)

## 3.1 Clientes
**Função**: linha 254–552
**React novo**: `TpcClientes.tsx` (P0)

KPIs por cliente: Saldo total | Disponível | Reservado | # Transações | Última atualização

### Tabela `wp_tpc_carteira`
ID | Nome | Email | Saldo | Reservado | Disponível | # Tx | Última atualização | Ações

Ações:
- "Ver extrato" → tab Transações filtrado
- "Ajuste manual" → BLOQUEADO (regra: crédito só via PIX confirmado)

### Recarga PIX (form inline)
Fields: Email cliente, Valor (R$), Motivo
Fluxo: `tpc_criar_recarga()` → `tpc_gerar_pix_me()` → QR + copia-cola
Transient: `tpc_admin_pix_{user_id}` (validade `tpc_pix_validade_minutos()+5min`)
Debug section `<details>`: HTTP code + logs + raw JSON

### Reset Carteira (destrutivo)
Confirm digitando "RESETAR". Nonce `tpc_reset_wallet_all`
Limpa: `tpc_carteira`, `tpc_transacoes`, `tpc_recargas`, metas pedidos `_senderzz_wallet_*`

---

## 3.2 Transações
**Função**: linha 555–653
**React novo**: `TpcTransacoes.tsx` (P1)

Filtros: tipo (crédito/débito/todos), data ini/fim, user_id (vindo do link)
Exclui: meta `senderzz_admin_me_allocation`

Tabela `wp_tpc_transacoes` paginada 20/page:
ID | Usuário | Tipo (badge cor) | Status (pendente/analise/confirmado/cancelado) | Valor | Saldo Após | Descrição | Data | Ações

Ações: "Excluir transação" (nonce `tpc_delete_transacao_{id}`) — recalcula saldo via `tpc_admin_recalcular_saldo_usuario()`

Botão "Verificar PIX Pendentes Agora" — trigger `tpc_verificar_pix` + nonce → `tpc_processar_recargas_pendentes(20,'admin')`

---

## 3.3 Configurações
**Função**: linha 656–778
**React novo**: `TpcConfiguracoes.tsx` (P1)

Nonce: `tpc_salvar_config`

### Melhor Envio
- Token de acesso (password mascarado, env override possível)
- Webhook secret (auto-gerado se vazio, env override)
- Webhook URL (read-only): `rest_url('tp-carteira/v1/webhook/pix')`
- Saldo atual ME (via `tpc_consultar_saldo_me()`)

### Regras Carteira
- Saldo mínimo alerta (float) — `tpc_saldo_minimo` (exibido em my-account)

### Motor Senderzz
- Validade visual PIX slider 5-1440 (default 30) — `tpc_pix_valid_minutes`
- Checkbox "Cancelar PIX expirado auto" — `tpc_pix_auto_cancel_expired`
- Checkbox "Validar saldo na emissão" — `senderzz_enforce_wallet_on_label`
- Checkbox "Uma etiqueta por pedido" — `senderzz_block_duplicate_label`
- Input "Template Checkout FunnelKit" (default 140) — `senderzz_checkout_template_id`

### Dono Financeiro por Classe
Tabela: classe → select usuário dono. Option JSON `senderzz_shipping_class_wallet_owners`
Prioridade: Portal user vinculado → mapa manual → cliente do pedido

---

## 3.4 REST API
**Função**: linha 781–842
**React novo**: `ApiDocs.tsx` (P2)

Base: `rest_url('tp-carteira/v1')` | Auth: JWT Bearer

Endpoints documentados:
- POST `/auth/token`
- GET `/me`
- GET `/saldo`
- GET `/extrato` (filtros: tipo, data_ini, data_fim, per_page, page)
- POST `/recarregar` (valor ≥10)
- GET `/recarga/{id}/pix`
- POST `/webhook/pix` (interno)
- POST `/webhook/envio` (interno)

Webhook ME config display: URL + event "Atualização das etiquetas..."
Exemplo JS fetch incluído.

---

## 3.5 PIX Reconciliação
**Função**: aba inline + `pix-auto-reconcile.php`
**React novo**: `Pix.tsx` (existe esqueleto — completar com reconciliação)

KPIs síntese: Última reconciliação | Pendentes (idade) | Divergências detectadas

### Admin notices (3)
1. Cron parou >15min (vermelho)
2. Erros reconciliação (amarelo)
3. Divergência saldo (vermelho com IDs)

Cron `sz_pix_auto_reconcile_cron` 5min via `sz_pix_reconcile_por_transactions()`:
- Recargas status IN (pendente, analise) AND me_pix_id NOT NULL AND age 3-29min
- Chama GET `/me/transactions?per_page=50` (cache 2min)
- Confirma se type=credit AND status=authorized AND div ≤ R$ 0.02
- Log `sz-pix-reconcile-*.log`

Daily `sz_wallet_divergence_check`: cada user, valida saldo == SUM(credito) - SUM(debito) confirmados. Se div >0.01: log + option `sz_wallet_divergence_last`

---

## 3.6 my-account.php (Cliente)
Não admin mas relevante para Portal user V2 (não admin):
- `/minha-conta/carteira/` endpoint
- Aba Recarga PIX: presets (50/100/200/500) + custom + AJAX
- QR + timer + polling 8s × 18 (~2.5min)
- Botão "Já paguei" → registra intent (não credita)
- Aba Histórico filtros tipo + data
- Saldo display classe `tpc-saldo-baixo` se ≤ `tpc_saldo_minimo`

---

# 4. AFILIADOS ADMIN

Arquivo: `includes/senderzz-affiliates.php`

## 4.1 Dashboard Financeiro COD
**Função**: `sz_aff_render_dashboard_admin()` :5030
**React novo**: `AffiliateDashboard.tsx` (P0) — pode mesclar com `CodLivro`

KPIs: Pedidos afiliados | Comissão pendente | Receita Senderzz | Obrigações | Saldo carteira | Afiliados ativos | Saques pendentes
Rankings: top afiliados + top produtores
Gráfico taxas entrega/manuseio/transação

---

## 4.2 Taxas COD Breakdown
**Função**: `sz_admin_taxas_cod_page()` :5867
**React equivalente**: parte de `CodTaxasEntrega.tsx`

Tabela pedidos: valor | entrega/manuseio/transação/comissão | KPIs | filtros data | card config penalidades
Nonces: `sz_aff_admin_nonce`, `sz_penalidades_save`

---

## 4.3 Edit User Profile (CPF/Telefone/PIX/Banco)
**Função**: `sz_aff_render_user_profile_fields()` :3753 + `_save_user_profile_fields()` :3779
**React novo**: parte de `Users.tsx` expandida

Hooks: `show_user_profile` + `edit_user_profile` + `personal_options_update` + `edit_user_profile_update`

---

## 4.4 Order Metabox Afiliado
**Função**: `sz_aff_render_order_metabox()` :5636
**React equivalente**: parte de `OrderDetail.tsx`

ID metabox + hook `add_meta_boxes` :5631

## 4.5 Order Column "Afiliado"
**Função**: `sz_aff_add_order_column()` :5775 + `sz_aff_render_order_column()` :5787
Mostra: nome + %

## 4.6 Hooks finance
- `woocommerce_order_status_completo/wc-completo` :1663 — split order
- `woocommerce_order_status_frustrado/frustracao/cancelado` :1744 — reverse + penalty
- `sz_aff_apply_frustration_penalty()` :1785

## 4.7 Options afiliados (admin settings)
- `sz_aff_default_commission_pct`
- `sz_aff_default_withdraw_fee`
- `sz_aff_first_frustration_penalty`
- `sz_aff_default_penalty_value`
- `sz_aff_producer_frustration_penalty`
- `sz_aff_default_retention_days`

## 4.8 Tabelas
- `wp_senderzz_affiliates`
- `wp_senderzz_affiliate_transactions` (type=commission/penalty/approval)
- `wp_senderzz_affiliate_wallet` (balance/pending/debt)
- `wp_senderzz_affiliate_withdrawals` (status=pending/paid)
- `wp_senderzz_admin_wallet` (id=1, balance)

---

# 5. LABELS / EXPEDIÇÃO ADMIN

## 5.1 Order Metabox ME
Arquivo: `src/Admin/Orders/Metabox.php` :14
**React equivalente**: parte de `OrderDetail.tsx`

Mostra: label status, protocol, print URL, pipeline status, delete link
Hooks: `add_meta_boxes`, `wp_ajax_melhor_envio_delete_meta`, `wp_ajax_melhor_envio_delete_meta_reverse`

Meta lidos: `_order_invoice_id`, `_order_me_custom_service_id`, `_melhor_envio_item_id`, `_melhor_envio_order_id`, `_melhor_envio_print_url`, `_melhor_envio_pdf_local_url`, `_melhor_envio_label_status`, `_melhor_envio_label_error`, `_melhor_envio_label_generated`, `_melhor_envio_bought_shipping`, reverse variants

## 5.2 Bulk Actions
Arquivo: `src/Admin/Bulk_Actions/Bulk_Pipeline.php` :23
**React novo**: `BulkActions.tsx` (P1)

Actions:
- "ME: Gerar etiquetas"
- "ME: Gerar (sem PDF)"
- "ME: Imprimir lote (PDF único)"

Hooks: `bulk_actions-edit-shop_order`, `bulk_actions-woocommerce_page_wc-orders`, `handle_bulk_actions-*`, `admin_notices`
Nonce: `me_batch_print`
Render batch print page: lista PDFs + composes via `Batch_Composer`

## 5.3 Order Actions
Arquivo: `src/Admin/Orders/Actions.php` :19
Hook: `woocommerce_admin_order_actions` — adiciona "Gerar etiqueta" / "Baixar PDF"
Hook: `manage_shop_order_posts_custom_column` — render tracking

## 5.4 Single Order AJAX
Arquivo: `src/Admin/Orders/Ajax.php` :21
`wp_ajax_melhor_envio_add_to_cart` — Cart → Checkout → Generate → Print
Nonces: `melhor_envio_request_label_{order_id}`, `melhor_envio_bulk_action`
Reverse shipping: `process_reverse_shipping()` :166

## 5.5 General Settings (WC Integration)
Arquivo: `src/Admin/General_Settings.php`
**React equivalente**: `Settings.tsx` expandido

Campos:
- client_secret (ME token)
- allowed_status (multi-select)
- status_after_print
- status_after_posted
- status_after_delivered
- undelivered_status
- status_to_tracking (multi-select)
- add_to_cart_only (checkbox)

Hook: `woocommerce_update_options_integration_wc-melhor-envio`

---

# 6. COD WALLET ADMIN

Arquivo: `includes/senderzz-cod-wallet.php`

## 6.1 sz_cod_admin_page :858
**React novo**: `CodWalletAdmin.tsx` (P0) — pode integrar em `CodSaques`

### Global Rules Form
Option `senderzz_cod_finance_settings`:
- retention_days
- withdraw_fee
- anticipation_fee_pct
- sz_admin_motoboy_fee
- sz_admin_operational_fund_fee
Nonce: `sz_cod_finance_save`

### Producer Overrides Table
User meta: `_senderzz_cod_retention_days`, `_senderzz_cod_withdraw_fee`, `_senderzz_cod_anticipation_fee_pct`, `_senderzz_cod_pix_key`, `_senderzz_cod_pix_type`, `_senderzz_cod_pix_holder`, `_senderzz_cod_pix_cpf`

### Pending Saques Table (flow analysis→pending→paid)
Form por linha:
- Upload `proof_url`
- Input `admin_note`
- Botão "Marcar pago"
Nonce: `sz_cod_complete_withdraw`
Email user notificado

### Hooks
- `init` :130 — DB migration
- `sz_cod_release_cron` :152 — release due transactions (cron hourly)
- `admin_init` :824 — process form
- `admin_notices` :856 — pending count

### Tables
- `wp_sz_cod_wallet_transactions` (user_id, order_id, type, status pending/available/paid, gross, fee, net, release_at)
- `wp_sz_cod_withdrawals` (user_id, account_id, amount, fee, net, pix_key, pix_type, holder_name, holder_cpf, proof_url, admin_note, completed_at, status analysis/pending/paid)
- `wp_sz_cod_withdraw_accounts` (user_id, holder_name, holder_cpf, pix_type, pix_key, bank info, is_default, status active/deleted)

### REST (sz-portal/v1)
- POST `/cod/pix-account`
- GET `/cod/accounts`
- POST `/cod/withdraw`
- POST `/cod/account-delete`

---

# 7. TRACKING BRAND

Arquivo: `includes/senderzz-tracking-brand.php`
**React novo**: `TrackingBrand.tsx` (P2)

Função: `senderzz_tracking_brand_page()` :87
Form: tabela classes com inputs logo URL, primary color, text color, brand name, footer text
Option JSON `senderzz_tracking_brands[class_id] = {logo, cor, cor_texto, nome, rodape}`
Nonce: `senderzz_tb_save` (admin_init :36)

REST (tp-carteira/v1): `POST/GET /rastreio-brand`

---

# 8. ME CANCEL LABEL

Arquivo: `includes/senderzz-me-cancel-label.php`
Init :26 — register WC status `wc-emcancelamento`
Hook `woocommerce_order_status_emcancelamento` :44 — enqueue cron `senderzz_cancel_me_label_async`
`senderzz_me_cancel_label_for_order()` :56 — POST `/api/v2/me/shipment/cancel`
REST (senderzz/v1): `POST /cancel-label` — manual trigger

---

# 9. MULTI-CLASS

Arquivo: `includes/senderzz-multi-class.php`
**React equivalente**: parte de `Users.tsx`

Table: `senderzz_portal_user_classes` (N:N portal_user × shipping_class)
Functions:
- `sz_get_user_class_ids()` :70 — fonte canônica
- `sz_set_user_class_ids()` :130 — UPDATE com legacy sync
- `sz_mc_sync_legacy_field()` :202 — auto-sync hourly transient
- `admin_post_sz_mc_force_sync` :243 — manual

---

# 10. MAINTENANCE MODE

Arquivo: `includes/senderzz-maintenance.php`
**React novo**: `MaintenanceMode.tsx` (P1)

### Função admin
`senderzz_maintenance_admin_page()` :195
Nonce: `senderzz_maintenance_save`
Option `senderzz_maintenance_settings`:
- enabled (0/1)
- return_date (date)
- return_time (time)
- title (max 90)
- message (textarea)

Defaults `senderzz_maintenance_defaults()` :14
Preview pane (live rendering)

### Frontend display
`senderzz_maintenance_render_screen()` :131
HTML dark gradient + orange top bar + "S" logo
Status 503 + Retry-After 3600 + robots noindex
Bypass: `manage_options` admin OU portal role='operator'

Portal role check `senderzz_maintenance_portal_role()` :69
Lê cookie `senderzz_portal_session` → query `senderzz_portal_sessions` + `senderzz_portal_users`

---

# 11. ONBOARDING

Arquivo: `includes/senderzz-onboarding.php`
**React novo**: `OnboardingRequests.tsx` (P0) — substitui shortcode atual

Table: `wp_senderzz_onboarding_requests` :70
Campos: id, nome, email, document (CPF), telefone, empresa, status (pending/approved/rejected), token, created_at, approved_at, notes
Unique index email + index document+status

### Shortcode `[senderzz_onboarding]` :202
Form public: nome*, email*, CPF*, WhatsApp, empresa
Validações: email format, CPF Luhn 11 dígitos, dedup via `sz_senderzz_account_exists_by_email_or_cpf()`
Conflito check: wp_users, senderzz_portal_users, wp_usermeta (sz_document), sz_affiliates

### Admin Approval Flow
Query param `sz_onboarding_approve` + nonce + token :228
Cria:
- WP user (role customer)
- Portal user (senderzz_portal_users, role client)
- Shipping class
Aplica: markup default `senderzz_markup_rules`
Email: portal URL + temp password
Marca: approved + approved_at

### Admin Tab
`sz_onboarding_admin_page()` :323
Tabela: ID | nome | email | telefone | empresa | status | data | Ação ("Aprovar" pending)

### Helpers
- `sz_onboarding_validate_cpf()`
- `sz_onboarding_format_cpf()` (XXX.XXX.XXX-XX)

---

# 12. NOTIFICATIONS (PUSH WEB)

Arquivo: `includes/senderzz-notifications.php`

## 12.1 Tabelas
- `wp_sz_push_subscriptions` (endpoint, p256dh, auth, user_id)
- `wp_sz_notif_prefs` (user_id, event, enabled, include_affiliates)
- `wp_sz_notif_log` (full audit: HTTP codes + retry)

## 12.2 Events Monitored :80
COD: agendamento_cod, em_rota_cod, completo_cod, frustrado_cod
Expedição: pedido_feito, enviado_pad, entregue
Mapped to WC status via `sz_notif_events` global

## 12.3 VAPID
Auto-generated :1148 (sz_notif_vapid_public/_private base64url EC prime256v1)
Encryption RFC 8291 aes128gcm + JWT VAPID ES256

## 12.4 REST (sz-notif/v1)
- POST `/subscribe`
- POST `/unsubscribe`
- POST `/prefs`
Auth: portal session cookie OR token param

## 12.5 Admin Panel `sz_notif_admin_page()` :1162
- Status: device count + log count + VAPID keys status
- Regenerar VAPID (nonce + confirm)
- Test user (input WP user_id, envia push real)
- Log table 25 últimas: Date | User | Event | Recipient | Order | Status | HTTP | Error | Reprocess btn

## 12.6 Dispatch :559
`sz_notif_dispatch_event_to_configured_recipients()` → recipients map em option `sz_app_pwa_notification_recipients`

## 12.7 Preferences Panel :825 (Portal)
Toggle por evento (3-grid) + include afiliados global + activate push (SW + VAPID subscribe) + admin recipients multi-checkbox

---

# 13. PRODUCER EMAIL + PUSH

Arquivo: `includes/senderzz-producer-notifications.php`
Status handlers :18 (enviado/acaminho/concluido/erro/extravio/cancelled/saldoinsuficiente)
Hook `woocommerce_order_status_changed` pri 20 :34
Lookup producer email via shipping_class → senderzz_portal_users
Multi-class: query senderzz_portal_user_classes
Idempotency: meta `_senderzz_notified_{status}`
HTML template colored + "Ver no painel" CTA

Push new orders :121 — `woocommerce_new_order` + `woocommerce_order_status_changed` (on-hold)
Throttle transient `sz_push_new_order_` HOUR_IN_SECONDS

---

# 14. APP PWA (/app/ ROUTE)

Arquivo: `includes/senderzz-app-pwa.php`
**React novo**: `PwaConfig.tsx` (P2 — opcional admin)

## 14.1 Rewrite rules
- `/app/` → `sz_app_pwa=1`
- `/app-sw.js` → `sz_app_sw=1`
- `/app-manifest.json` → `sz_app_manifest=1`

## 14.2 Service Worker :47 — dynamic JS
CACHE versionado (TPC_VERSION+'-app250')
Fetch network-only para AJAX/REST; navigate network para /app/
Push event → notification | Click → focus /app/

## 14.3 Manifest :136
name, short_name, start_url, scope /app/, display standalone, icons 192/512 PNG, bg+theme #f3f8fb

## 14.4 Notification Templates :188
11 eventos × 3 roles → 6 campos por evento (title/body × producer/affiliate/admin)
Variáveis com role-based scrub
Option `sz_app_pwa_notification_templates`

## 14.5 Status Map :296 (mutual exclusion)
Option `sz_app_pwa_notification_status_map`:
- agendamento_cod → wc-agendado
- em_rota_cod → wc-emrota
- completo_cod → wc-completo
- frustrado_cod → wc-frustrado
- pedido_feito → wc-processing
- enviado_pad → wc-enviado
- entregue → wc-entregue

## 14.6 Recipients Map :364
Option `sz_app_pwa_notification_recipients[event][role]` (default producer=1, affiliate=1, admin=0)

## 14.7 Admin Recipients :336
Option `sz_app_pwa_admin_recipients[event] = [user_id...]`

## 14.8 Admin Screen :397
Form: template editor com tabs producer/affiliate/admin
Status dropdown (mutual exclusion)
Checkboxes recipients (producer/affiliate/admin)
Multi-select admin WP users
Variable inserter dropdown + "Inserir" → injeta `{{var}}` em textarea
Save nonce `sz_app_pwa_notif_save` com validação role restrictions

## 14.9 Order Number Flag
Option `sz_app_pwa_notification_order_number_flags[event]` — incluir order# no push

---

# 15. SECRETS MANAGEMENT

Arquivo: `includes/senderzz-secrets.php`

## 15.1 Secrets
- SENDERZZ_WEBHOOK_SECRET (≥32 chars)
- SENDERZZ_JWT_SECRET (≥32 chars)
- SENDERZZ_ME_TOKEN (≥10 chars)

## 15.2 Resolution Order
1. PHP constant (wp-config.php)
2. Environment variable (getenv)
3. wp_options fallback (`tpc_webhook_secret`, `tpc_jwt_secret`, `tpc_me_token`)

## 15.3 Getters
- senderzz_get_webhook_secret() :56
- senderzz_get_jwt_secret() :71
- senderzz_get_me_token() :85
- senderzz_get_secret($key) :111
- senderzz_secret_from_env($which) :127

## 15.4 Admin UI Block :210
Inputs disabled + value '••••••••••••' se env ativo
CSS bg #f0f0f1 color #a7aaad

## 15.5 Write Protection :180
`pre_update_option_*` filter retorna old_value se env set

## 15.6 Security Cleanup :316
Replace DB value com 'env-managed' placeholder se env ativo (one-shot 30d transient)

## 15.7 Admin Notice :255
Mostra 1x/semana se env não configurado. Lista missing secrets + exemplo wp-config.php. Dismiss btn 30d transient.

---

# 16. STATUS AUTHORITY

Arquivo: `includes/senderzz-woo-status-authority.php`

## 16.1 9 Custom WC Statuses
wc-agendado | wc-reagendado | wc-embalado | wc-em-rota | wc-acaminho | wc-frustrado | wc-avariado | wc-completo (COD) | wc-entregue (Exp)

## 16.2 Mapping Functions
- senderzz_motoboy_status_to_woo_status :70
- senderzz_woo_status_to_motoboy_status :95
- senderzz_sync_motoboy_mirrors_from_woo_order :137 (one-way idempotent)
- senderzz_set_order_status_from_motoboy_status :172

## 16.3 Hooks
- `woocommerce_order_status_changed` pri 30 :187 — sync motoboy mirrors
- `sz_motoboy_status_changed` pri 5 :191 — update WC
- `admin_init` pri 50 :199 — sanity check last 150 motoboy orders vs WC

---

# 17. ACCESS SCOPE + CAPABILITIES

Arquivo: `includes/senderzz-access-scope.php`

## 17.1 Capabilities (5 customizadas + WC)
- senderzz_admin
- senderzz_manage
- senderzz_manage_motoboy
- senderzz_manage_finance
- (+ manage_woocommerce + view_woocommerce_reports + edit_shop_orders + read_shop_order)

## 17.2 Guard `senderzz_admin_capability_guard()` :21
manage_options → auto-grant todos via filter `user_has_cap` pri 20

## 17.3 Scope Functions
- `senderzz_current_user_order_scope()` :38 — admin/affiliate/producer/operator/motoboy
- `senderzz_user_can_access_order()` :108 — scope + action (view/approve/cancel/status/edit/delete)
- `senderzz_get_visible_order_ids_for_user()` :148
- `senderzz_get_visible_orders_for_user()` :183
- `senderzz_affiliate_wallet_summary_scoped()` :201 — available/pending/analysis/future

---

# 18. AUDIT ENGINE + AUDIT LOG

## 18.1 Audit Engine
Arquivo: `includes/senderzz-audit-engine.php`
- senderzz_audit_meta_table() :10 — HPOS detect
- senderzz_audit_table_exists() :19
- senderzz_audit_order_should_ignore_finance() :26
- senderzz_audit_problem_rows() :35 — split/aff_bad/aff_missing/wallet
- senderzz_audit_counts() :84
- senderzz_audit_order() :95 — per-order context

## 18.2 Audit Log Viewer
Arquivo: `includes/senderzz-audit-log.php`
Table `senderzz_portal_audit_log` :13: id, portal_user_id, action, order_id, meta (JSON), ip, created_at
Keys: portal_user_id, action, created_at
senderzz_audit_log() :49 — writes actions (approve/cancel/loss/retry_label/support)
Hook `senderzz_portal_action` :76 — post-AJAX log
senderzz_render_affiliate_audit_page() :297 — v369 menu removido, consolidado

---

# 19. CRONS (18 TOTAL)

| Hook | Frequência | Handler | Tabelas |
|---|---|---|---|
| tpc_cron_verificar_recargas_pix | 5min | pix.php | tpc_recargas, tpc_transacoes |
| senderzz_db_cleanup | daily | senderzz-logistics.php | sessions, 2FA, webhook events 90d, logs 90d, PIX cancelled 30d |
| senderzz_me_reconcile_shipments | 5min | senderzz-me-webhook.php | postmeta/wc_orders_meta |
| sz_posted_polling_cron | 5min | senderzz-cancel-intercept.php | ME API |
| senderzz_auto_generate_label | single | wallet-engine/integrations | labels queue |
| senderzz_check_low_balance | single 30s | wallet-engine | transients |
| sz_cod_release_cron | hourly | cod-wallet.php | sz_cod_wallet_transactions |
| sz_aff_release_commissions | hourly | affiliates.php | sz_affiliate_transactions |
| sz_motoboy_geofence_check | 1min | motoboy/router.php | sz_motoboy_* |
| sz_pix_auto_reconcile_cron | 5min | pix-auto-reconcile.php | tpc_carteira/_transacoes |
| sz_wallet_divergence_check | daily | pix-auto-reconcile.php | tpc_carteira/_transacoes |
| senderzz_generate_label_cron | single 1s | async-approval.php | labels |
| senderzz_check_me_refund_status | single 300s | me-refund-validation/wallet-engine | orders meta |
| senderzz_cancel_me_label_async | single imediato | me-cancel-label.php | ME API |
| senderzz_push_new_order | single 3-5s | producer-notifications.php | push subscriptions |
| senderzz_retry_label_pipeline | single 60s | Pipeline/Label_Pipeline.php | label queue |
| senderzz_portal_cleanup_sessions | twicedaily | Portal_Auth.php | senderzz_portal_sessions |
| wc_melhor_envio_check_posted | hourly (configurable) | Queue/Register.php | label queue |

**React new**: `CronStatus.tsx` (P1) — listar todos + última execução + log + manual trigger

---

# 20. REST APIS (109 ROUTES)

## 20.1 Namespaces
- `wc-melhor-envio/v1` — Label CRUD
- `tp-carteira/v1` — Wallet + PIX (JWT auth, rate-limited)
- `senderzz/v1` — Orders + Webhooks + Reports
- `sz-motoboy/v1` — Motoboy PWA + OL Portal (45 rotas)
- `sz-aff/v1` — Affiliate wallet + PIX
- `sz-portal/v1`+`sz-portal/v2` — Portal genérico
- `sz-notif/v1` — Push subscribe/prefs

## 20.2 Routes-chave
- `/emitir-etiqueta` POST — rate 30/min, 200/dia (V-NEW-09); cross-user validation (V-NEW-07)
- `/webhooks` CRUD — SSRF validation DNS→IP block private (V-NEW-03); soft-delete `url=''`
- `/webhooks/{id}/test` — rate 10/hr (V-NEW-13)
- `/webhook/pix` POST — fail-closed empty secret 401; confirmation guard
- `/webhook/me` POST — event whitelist exato ['ping','test','webhook.ping'] (CRIT-04); idempotent UNIQUE event_key
- `/ol/mudar-status` POST — motoboy OL
- `/ol/trocar-motoboy` POST
- `/ol/motoboys-do-dia` GET
- `/sz-portal/v2/cod/*` — COD withdrawal/anticipation
- `/auth/token-from-portal-session` — rate 10/min/IP (A-08)
- `/recarregar` — rate 5/hr, 30/dia (V-NEW-21)
- `/change_password` — rate 5/hr per user (V-NEW-27)

---

# 21. SECURITY PATCHES (CRIT + V-NEW + A/B/C/P + M)

## 21.1 CRIT (críticos)
- **CRIT-01** Wallet debit nunca user-supplied price; recalcular via `/me/shipment/calculate`
- **CRIT-02** Order status transition whitelist: pending|on-hold|pendente|aguardando → cancelled only (não-admin)
- **CRIT-04** ME webhook event whitelist exato `['ping','test','webhook.ping']` (não substring)

## 21.2 V-NEW (webhooks idempotency + cross-check)
- V-NEW-01/02 PIX confirmation guard fail-closed
- V-NEW-03 SSRF DNS→IP private/loopback/link-local block
- V-NEW-04 Body/error redaction em /test /logs
- V-NEW-06 Strong idempotency `tpc_webhook_events` UNIQUE event_key
- V-NEW-07 Cross-check user_id claim vs tx owner em `/webhook/envio`
- V-NEW-09 Rate limit `/emitir-etiqueta` 30/min 200/dia
- V-NEW-13 Rate limit `/webhooks/{id}/test` 10/hr
- V-NEW-16 Global `pre_http_request` re-validate SSRF
- V-NEW-21 Rate limit `/recarregar` 5/hr 30/dia
- V-NEW-25 AJAX SSRF gate pri 3
- V-NEW-27 Rate limit change_password 5/hr/user

## 21.3 V-NEW Login
- V-NEW-05 Login rate 10/IP/5min, 5/email/5min (v89 desabilitado email, só IP)
- 2FA: operator=required, high-balance producer=required (≥R$500)

## 21.4 A/B/C/P
- A-08 Rate limit `/auth/token-from-portal-session` 10/min/IP
- B-03 Log wallet_refund_skip_deferred com order_id
- B-04 Trusted proxy IP parsing (REMOTE_ADDR prioritized)
- C-03 PIX guard filter `senderzz_pix_pre_confirmar_recarga` aceita só 'webhook' ou 'admin_reconciliation'
- P-01 tpc_confirmar_recarga guard implemented

## 21.5 M-01
Admin notice se `tpc_webhook_events` table missing (fallback 24h transient)

---

# 22. HPOS COMPATIBILITY

Arquivo: `includes/senderzz-hpos-fix.php`
Detecta HPOS via `wp_wc_orders_meta`
Fixes:
1. Tracking code preservado via `http_response` hook quando status ≠ posted/delivered
2. `/rastreio/{code}` page HPOS-aware (rewrite rule + handler)
3. ME webhook HPOS-aware via `rest_pre_dispatch`

Helpers:
- senderzz_hpos_find_order_by_meta()
- senderzz_hpos_find_order_by_meta_like() (fallback legacy)

---

# 23. ORDER CONTEXT HELPERS

Arquivo: `includes/senderzz-order-context.php`
**Read-only snapshot** — safe para audit/triage

`senderzz_order_context(order_id)` retorna 23 campos:
exists, order_id, order_number, status_raw, status (normalized), flow_status, delivery_mode (motoboy/expedition/unknown), producer_id, affiliate_id, affiliate_user_id, shipping_class_id, total, offer_value, affiliate_commission, producer_commission, motoboy_fee_total, split_sum, financially_settled, frustrated

Helpers: `senderzz_order_delivery_mode()`, `senderzz_order_context_meta()` (fallback chain), `senderzz_order_context_money()` (float normalize)

---

# 24. PRODUCT LABEL NORMALIZATION

Arquivo: `senderzz-logistics.php`
- senderzz_clean_product_label() :55
- senderzz_clean_product_summary() :71
- senderzz_order_item_label() :91 — `{qty}x {name}`
- senderzz_order_primary_item_label() :101 (OFFICIAL — nunca lista múltiplos)

**Regra**: "quantidade e nome, uma vez só" — nunca juntar por `,` ou `+`

---

# 25. TABELAS BD COMPLETAS

## 25.1 Wallet/PIX (TPC)
- `wp_tpc_carteira` — saldo/saldo_reservado per user (UNIQUE user_id)
- `wp_tpc_transacoes` — ledger imutável (UNIQUE user_id+referencia+tipo idempotência)
- `wp_tpc_recargas` — PIX tracking (UNIQUE me_pix_id)
- `wp_tpc_webhook_events` — idempotência (UNIQUE event_key)

## 25.2 Motoboy
- `sz_motoboys` — registro (UNIQUE token_app, pin_hash bcrypt)
- `sz_motoboy_pedidos` — pedidos (UNIQUE wc_order_id)
- `sz_motoboy_cds`
- `sz_motoboy_zonas` (dias_funcionamento CSV, cutoff_horarios JSON)
- `sz_motoboy_cep_zonas`
- `sz_motoboy_zona_pivot` (multi-zone per motoboy)
- `sz_motoboy_comprovantes`
- `sz_motoboy_fechamento` (UNIQUE motoboy_id+data_fechamento)
- `sz_motoboy_audit`
- `sz_motoboy_ganhos`
- `sz_motoboy_pagamentos`
- `sz_motoboy_stock_custody`

## 25.3 Affiliates
- `senderzz_affiliates`
- `senderzz_affiliate_transactions` (commission/penalty/approval)
- `senderzz_affiliate_wallet` (balance/pending/debt)
- `senderzz_affiliate_withdrawals` (pending/paid)
- `senderzz_admin_wallet` (id=1)

## 25.4 COD
- `sz_cod_wallet_transactions` (pending/available/paid)
- `sz_cod_withdrawals` (analysis/pending/paid)
- `sz_cod_withdraw_accounts`

## 25.5 Portal
- `senderzz_portal_users`
- `senderzz_portal_sessions`
- `senderzz_portal_2fa`
- `senderzz_portal_user_classes` (multi-class)
- `senderzz_portal_audit_log`
- `sz_portal_tickets`
- `sz_portal_ticket_msgs`

## 25.6 Webhook Logs
- `senderzz_webhook_log`
- `sz_webhook_logs`
- `senderzz_producer_webhook_logs`
- `wcme_webhook_logs` (legacy)

## 25.7 Notifications
- `sz_push_subscriptions`
- `sz_notif_prefs`
- `sz_notif_log`

## 25.8 Onboarding
- `senderzz_onboarding_requests` (UNIQUE email)

## 25.9 Labels
- `wc_melhor_envio_label_request`
- `wc_melhor_envio_label_queue`

---

# 26. OPTIONS WP (60+ — TODAS VIA ADMIN UI REACT)

## 26.1 Senderzz Core
- senderzz_get_me_token
- senderzz_markup_rules / senderzz_markup_default
- senderzz_declared_value_v
- senderzz_block_duplicate_label
- senderzz_enforce_wallet_on_label
- senderzz_plugin_version
- senderzz_portal_page_id
- senderzz_sender_by_class
- senderzz_trusted_proxy_ips
- senderzz_webhook_notice_hidden
- senderzz_checkout_template_id
- senderzz_shipping_class_wallet_owners
- senderzz_tracking_brands
- senderzz_db_indexes_v
- senderzz_db_version

## 26.2 TPC
- tpc_me_token
- tpc_webhook_secret (env-overridable)
- tpc_jwt_secret (env-overridable)
- tpc_saldo_minimo
- tpc_pix_valid_minutes
- tpc_pix_auto_cancel_expired
- tpc_db_version
- sz_pix_reconcile_last_run
- sz_wallet_divergence_last

## 26.3 Motoboy
- sz_motoboy_cc_fee_pct
- sz_motoboy_taxa_entrega
- sz_motoboy_taxa_percentual
- sz_motoboy_taxa_transacao_modo
- sz_mbw_taxa_entrega / _frustrado (+ per-motoboy `_mb_{id}`)
- sz_motoboy_admin_push_subs
- sz_motoboy_geofence_metros
- sz_motoboy_horario_inicio / _fim

## 26.4 Afiliados
- sz_aff_default_commission_pct
- sz_aff_default_retention_days
- sz_aff_default_withdraw_fee
- sz_aff_default_penalty_value
- sz_aff_first_frustration_penalty
- sz_aff_producer_frustration_penalty
- sz_aff_retention_days
- sz_prod_first_frustration_penalty
- sz_frustration_aff_overrides (JSON)
- sz_frustration_prod_overrides (JSON)

## 26.5 COD
- senderzz_cod_finance_settings (retention_days/withdraw_fee/anticipation_fee_pct)
- sz_admin_motoboy_fee
- sz_admin_operational_fund_fee
- sz_cod_notify_admin_withdrawal
- sz_cod_db_version

## 26.6 Notifications PWA
- sz_notif_app_name
- sz_notif_vapid_public / _private
- sz_app_pwa_notification_templates
- sz_app_pwa_notification_status_map
- sz_app_pwa_notification_recipients
- sz_app_pwa_admin_recipients
- sz_app_pwa_notification_order_number_flags

## 26.7 Maintenance
- senderzz_maintenance_settings (enabled/return_date/return_time/title/message)

## 26.8 UTM
- senderzz_utm_*

---

# 27. MAPEAMENTO TELA WP → TELA REACT

## ✅ Existem no React (precisam de paridade funcional completa)

| Tela WP | React atual | Status |
|---|---|---|
| Visão Geral Overview | Dashboard.tsx | Esqueleto — adicionar 6 KPIs + 6 alertas |
| Cod Pedidos (filtros+tabela) | Orders.tsx | Esqueleto — adicionar filtros + ações inline |
| Cod Motoboys CRUD | Motoboys.tsx | Esqueleto — CRUD completo + zonas multi-select |
| Cod Dashboard dia | MotoboysDay.tsx | Esqueleto — KPIs + ranking |
| Cod CDs | Cds.tsx | Esqueleto — CRUD |
| Cod Zonas+CEPs | Zonas.tsx | Esqueleto — CRUD + faixas + preview |
| Fin Livro COD | Commissions.tsx | Esqueleto — renomear/expandir |
| Exp Pedidos ME | Labels.tsx | Esqueleto — listar+ações |
| Exp Carteira Frete | Wallet.tsx | Esqueleto — saldo+recarga |
| PIX Reconciliação | Pix.tsx | Esqueleto — KPIs + reconciliação |
| Afiliados Dashboard | Affiliates.tsx | Esqueleto — KPIs + listagem |
| Config Usuários (Portal_Admin) | Users.tsx | Esqueleto — listagem + edit |
| Saúde Sistema | Tools.tsx | Esqueleto — diagnóstico |
| Config Geral | Settings.tsx | Esqueleto — opções |
| Logs (combo) | Logs.tsx | Esqueleto — webhooks+integrations+audit |
| Login | Login.tsx | OK |

## ❌ FALTANDO — CRIAR no React

### P0 (críticos — finance/operação)
| Tela nova | Origem WP | Arquivo |
|---|---|---|
| **OnboardingSetup.tsx** | senderzz-onboarding.php | Setup wizard inicial admin |
| **OnboardingRequests.tsx** | senderzz-onboarding.php :323 | Lista requests + aprovar |
| **CodLivro.tsx** | Unified_Menu :973 | Livro COD 22 colunas + resumos |
| **CodSaques.tsx** | cod-wallet.php :858 + Unified_Menu :1529 | Saques produtor flow |
| **CodTaxasEntrega.tsx** | Unified_Menu :1333 | 4 KPI + 4 penalty + 4 colapsáveis |
| **AuditEngine.tsx** | Unified_Menu :1590 | Auditoria + fix batch/per-order |
| **AffiliateWallet.tsx** | senderzz-affiliates.php | Saldo + tx + fix wallet |
| **AffiliateSaques.tsx** | Unified_Menu :1529 sec 2 | Approve/reject afiliado |
| **TpcClientes.tsx** | tpc/admin.php :254 | Lista + emitir PIX + reset |
| **OrderDetail.tsx** | combo metaboxes | Page única por pedido (motoboy + afiliado + label + tx + audit) |
| **MotoboyConfig.tsx** | motoboy/admin.php :1345 | geofence + horários + cc_fee |
| **MotoboyDashboard.tsx** | motoboy/admin.php :224 | KPIs + ranking dia |
| **MotoboyCarteira.tsx** | motoboy/admin.php :1837 | Carteiras + pagamento + histórico |
| **MotoboyFechamento.tsx** | (sz_motoboy_fechamento) | Confirmação Alan + repasse |

### P1 (importantes)
| Tela nova | Origem WP |
|---|---|
| **TpcTransacoes.tsx** | tpc/admin.php :555 |
| **TpcConfiguracoes.tsx** | tpc/admin.php :656 |
| **ExpedicaoIntegracoes.tsx** | markup.php :149 |
| **ExpedicaoWebhooks.tsx** | producer-webhooks.php |
| **NotificacoesPWA.tsx** | app-pwa.php :397 |
| **MaintenanceMode.tsx** | senderzz-maintenance.php :195 |
| **AffiliateRules.tsx** | (admin settings padrão afiliados) |
| **CodWalletProducer.tsx** | Unified_Menu :742 |
| **MotoboyEtiquetas.tsx** | motoboy/admin.php :1383 |
| **MotoboyCustodia.tsx** | motoboy/admin.php :1510 |
| **MotoboyMapa.tsx** | motoboy/admin.php :1761 |
| **MotoboyConciliacao.tsx** | motoboy/admin.php :2064 |
| **MotoboyComprovantes.tsx** | (sz_motoboy_comprovantes viewer) |
| **MotoboySaques.tsx** | motoboy/admin.php :2016 |
| **CronStatus.tsx** | (18 crons listing) |
| **BulkActions.tsx** | Bulk_Pipeline.php |
| **AuditLogViewer.tsx** | senderzz_portal_audit_log |
| **CodWalletTransactions.tsx** | sz_cod_wallet_transactions viewer |

### P2 (polish)
| Tela nova | Origem WP |
|---|---|
| **TrackingBrand.tsx** | senderzz-tracking-brand.php :87 |
| **PushTecnico.tsx** | notifications.php :1162 |
| **ApiDocs.tsx** | tpc/admin.php :781 |
| **OrderMetaNormalization.tsx** | Unified_Menu :1780 |
| **CapabilitiesGuard.tsx** | senderzz-access-scope.php viewer |
| **PwaConfig.tsx** | app-pwa.php (opcional) |

---

# 28. PRIORIZAÇÃO IMPLEMENTAÇÃO (sequência sugerida)

## Sprint 1 (P0 finance/auth)
1. OnboardingSetup + OnboardingRequests
2. AuditEngine (critical fix)
3. CodLivro + CodSaques + CodTaxasEntrega
4. AffiliateWallet + AffiliateSaques
5. TpcClientes
6. OrderDetail (consolida 3 metaboxes)

## Sprint 2 (P0 operação)
7. MotoboyDashboard + MotoboyConfig + MotoboyCarteira + MotoboyFechamento
8. Completar Dashboard.tsx (6 KPIs + 6 alertas)
9. Completar Orders.tsx (filtros + ações inline)
10. Completar Motoboys.tsx + Cds.tsx + Zonas.tsx (CRUD completo)

## Sprint 3 (P1 financeiro/expedição)
11. TpcTransacoes + TpcConfiguracoes
12. ExpedicaoIntegracoes + ExpedicaoWebhooks
13. CodWalletProducer + CodWalletTransactions
14. MotoboyEtiquetas + MotoboyCustodia + MotoboyMapa
15. MotoboyConciliacao + MotoboyComprovantes + MotoboySaques

## Sprint 4 (P1 notificações/diagnóstico)
16. NotificacoesPWA + PushTecnico
17. MaintenanceMode
18. CronStatus + BulkActions
19. AuditLogViewer + AffiliateRules
20. Completar Pix.tsx (reconciliação completa)

## Sprint 5 (P2 polish + cleanup)
21. TrackingBrand + ApiDocs
22. OrderMetaNormalization + CapabilitiesGuard
23. PwaConfig (opcional)
24. Auditoria visual final (screenshots side-by-side WP × React)

---

# 29. INVARIANTES — NUNCA QUEBRAR

| Invariante | Onde validar |
|---|---|
| CRIT-01 preço server-side | go/labels/internal/me/client.go::Calculate |
| CRIT-02 status whitelist | go/orders/internal/handlers/orders.go |
| CRIT-04 ME ping whitelist exato | go/labels webhook handler |
| V-NEW-01 PIX fail-closed | go/wallet/internal/handlers/pix.go |
| V-NEW-06 idempotency UNIQUE event_key | schema-wallet.sql tpc_webhook_events |
| V-NEW-07 cross-user check user_id | go/wallet shipment webhook |
| V-NEW-09 rate emitir-etiqueta | go/labels |
| Capability guard manage_options | go/admin auth middleware |
| QR HMAC14 WP_SALT_AUTH | go/motoboy custody.go::PackageCode |
| 9 custom statuses | go/orders state machine |
| Product label "quantity+name once" | go/orders item formatter |
| HPOS dual read | go/orders DB helpers |
| 18 crons → Asynq jobs Go | go/* internal/jobs |
| Soft-delete webhook url='' | go/portal webhooks handler |
| `sz_aff_panel` nonce (não senderzz_portal) | n/a (Go usa JWT, mas preservar action keys) |

---

# 30. PRÓXIMA AÇÃO IMEDIATA

Construir Sprint 1 — começar pelo **AuditEngine.tsx** + endpoint Go correspondente.

Endpoint Go a criar em `go/admin/internal/handlers/audit.go`:
- `GET /audit/counts` — retorna `{split, aff_bad, aff_missing, wallet}`
- `GET /audit/problems?limit=100` — lista problem_rows
- `POST /audit/fix-all` — chama 4 funções batch
- `POST /audit/fix-order/{id}` — fix single
- `POST /affiliates/{id}/wallet-fix` — sync_affiliate_wallet_summary

Tela React:
- 4 KPI cards
- Botão "Corrigir tudo" (com confirm modal)
- Tabela problemas com filtro tipo
- Botão "Corrigir pedido" por linha
- Toast resultado
