# FALK — STATUS (md vivo único)

> Documento ÚNICO de situação. Atualizado a cada prompt. Tudo marcado ✅ está **aplicado e no ar**.
> Última atualização: 2026-08-07.
> Docs de referência preservados (não são tracking): `CLAUDE.md` (instruções), `SERVIDOR.md`,
> `MOTOBOY-README.md`, `RUNBOOK.md`, `NOTAS-TECNICAS.md`, `CHANGELOG-SECURITY.md`, `docs/*`.

## O que é

Plataforma logística FALK (rebrand do Senderzz): **Fulfillment** (Melhor Envio) + **Cash on
Delivery** (motoboy próprio) + carteiras (Expedição pré-paga + COD) + afiliados/produtores +
portal de OLs + tracking white-label. Stack: PHP plugin (legado) → migrado p/ **8 microserviços Go**
(`go/`) + **3 UIs React/Vite** (admin-ui, portal-ui, checkout-ui) + Postgres + Docker/nginx/Cloudflare.

## Regra permanente de rastreio

- Toda mudança automática de status confirmada pela Melhor Envio, 17TRACK ou Jadlog deve atualizar `sz_orders` e inserir uma linha em `sz_order_status_history` na mesma operação lógica, com `actor_tipo='webhook'`. Falha no registro deve aparecer no log.

## Produção (VPS)

- `/opt/falk` na VPS `93.127.141.6` (SSH porta 10037, user `administrator`, key `~/.ssh/senderzz_vps`).
- DB = `falk` (Postgres, container `falk-postgres`). Containers `falk-*` via `docker-compose.falk.yml`.
- Stack de teste `falk-test-*` reusa `falk-admin-ui:latest`.
- Domínios LIVE: **app.falklog.com.br** (painel), **testes.falklog.com.br** (teste), **falklog.com.br** (LP).
- **Sem serviço de cron no stack falk** — funções SQL de cron (`sz_cod_release_due`,
  `sz_affiliate_release_due`, `sz_referral_payout`) existem mas **nada as agenda**. ⚠️ Gap de infra
  (decisão de deploy: host crontab ou deploy do go/cron).
- Deploy: rsync `go/admin/internal` + `admin-ui/src` → VPS; `docker build --no-cache -t falk-admin-ui:latest admin-ui`;
  `docker compose -f infra/docker/docker-compose.falk.yml build admin-service`; `up -d --no-deps --force-recreate`;
  `docker restart falk-test-admin-ui`. LP = rsync `infra/falk/lp/`.

## Regra financeira de ouro (#1587 — NÃO quebrar)

total 250 → comissão bruta 150 → taxa transação afiliado 7,49 (4,99% da bruta) → líquida 142,51 →
taxa entrega 23,98 → taxa transação produtor 12,48 (250×4,99%) → líquido produtor 63,54.
`valor` em `senderzz_affiliate_transactions.amount` é a comissão **LÍQUIDA** (já net) — nunca recalcular o 4,99%.

## Regras financeiras permanentes — não reintroduzir

- Carteira COD usa `senderzz_portal_users.wp_user_id` quando existe; produtor nativo FALK sem
  vínculo WordPress usa `-senderzz_portal_users.id`. Nunca rejeitar IDs negativos válidos da carteira.
- Entrega motoboy (`sz_motoboy_pedidos.status='entregue'`) cria exatamente um `cod_received` pendente
  na carteira do produtor, com `release_at` em D+7. O trigger/migration `539` é a proteção automática;
  qualquer backfill/auditoria deve ser idempotente e não duplicar lançamento.
- Antecipação feita pelo Admin promove o recebível real de `pending` para `available`, sem taxa e sem
  lançamento sintético. Antecipação self-service do produtor é outro fluxo e pode aplicar a taxa configurada.

---

## ✅ DONE (aplicado no site)

### 2026-07-14 (auditoria financeira produtor/afiliado + Audit Engine cego) — ✅ DEPLOYADO / NO AR (prod)
- ✅ **Crédito faltante produtor 15 (Gabriel Campos)**: 5 pedidos entregues (1591,1594,1595,1596,1597)
  nunca tiveram lançamento `cod_received` na wallet — R$294,45 nunca creditados. Backfill manual +
  `sz_financials_refresh()`. Saldo real confirmado: R$425,16 (bate com transferência p/ Expedição
  de R$20 já debitada).
- ✅ **Bug raiz no Audit Engine** (`go/admin/internal/handlers/audit.go`): filtro `type='credit'`
  nunca existiu na tabela (`sz_cod_wallet_transactions` só tem `cod_received/withdrawal/adjustment/
  refund/fee/freight_transfer`, ver CHECK constraint) — contador "Produtor COD divergente" e o Fix
  ficavam cegos desde sempre (mesmo bug em `dashboard.go` e `orders.go`). Corrigido tipo real +
  novo tipo `wallet_missing` (LEFT JOIN detecta pedido SEM nenhum lançamento, não só valor errado).
  `FixAll`/`FixOrder` agora também INSEREM linha ausente (antes só faziam UPDATE em linha existente).
  **Guarda anti-clawback**: correção de `net` só sobe valor, nunca desce — drift histórico de
  fórmula (~R$1,02/pedido em 12 pedidos antigos, R$12,32 total) já foi pago/disponibilizado ao
  produtor, não se toca retroativamente.
- ✅ **Check "split" do Audit Engine 100% falso-positivo**: comparava contra `o.senderzz_fee`
  (coluna NUNCA populada em `sz_orders`, sempre 0) → 33/33 pedidos entregues acusados errado.
  Trocado por auto-consistência da própria `sz_order_financials` (fórmula golden). Confirmado 0
  divergência real pós-fix.
- ✅ **Migração 499**: `sz_cod_wallet_transactions` tinha CHECK constraints (type/status/gross/net)
  que NUNCA permitiam `type IN ('anticipation','anticipation_credit')` nem `status='approved'` —
  a feature "Antecipar recebíveis" do portal (self-service produtor) sempre quebrava com 500 no
  INSERT. Nenhuma linha desse tipo jamais existiu em prod (feature morta desde o deploy). Ampliado
  constraint. Deploy: migração 499 aplicada direto + registrada em `schema_migrations`; rebuild +
  recreate `falk-admin`.
- ✅ Removido literal morto `'credit'`/`'manual_credit'` em `cod_wallet_transactions.go` (fora do
  CHECK, nunca existiu).
- Auditoria completa: afiliados 100% batendo (0 divergência, 6 vínculos). Nenhum crédito duplicado.
- ✅ **Estorno do drift histórico** (decisão do dono: generosidade indevida reverte pra Falk):
  os 12 pedidos com R$12,32 a mais (fórmula antiga) foram debitados num lançamento único
  `type='adjustment'` (migração 500 liberou negativo p/ esse tipo). Saldo produtor 15:
  R$425,16 → R$412,84. Linhas `cod_received` originais preservadas (auditoria intacta).

### 2026-07-03 (troca CPF ⇄ CNPJ com aprovação) — ✅ DEPLOYADO / NO AR (prod + teste)
- ✅ **DEPLOY OK** (2026-07-03): migração 483 aplicada em prod (1 nova) + teste (26 incl. 483);
  imagens rebuild (portal-service, admin-service, admin-ui, portal-ui --no-cache); recreate prod
  (`falk-portal` c/ volume `falk_uploads`, `falk-admin` c/ `DOC_CHANGE_UPLOAD_PATH`) + stack teste
  (novo volume `falk_test_uploads` em portal+admin de teste). Smoke: app.falklog.com.br + testes →
  `/document-changes` e `/portal/account/document-change` = 401 (rota registrada), SPA serve.
- ✅ **Troca de documento CPF ⇄ CNPJ** (produtor/afiliado/cliente): drawer lateral no card
  "Sua conta" (portal Settings) → solicitação vai p/ o admin → após **aprovado**, atualiza
  `senderzz_portal_users.nome` (razão social / nome civil) + `document` num único UPDATE
  (o site inteiro lê essa coluna → vale em todas as menções dali pra frente).
  - `cpf_to_cnpj`: razão social + CNPJ (validado, DV) + **anexo do cartão CNPJ** (PII).
  - `cnpj_to_cpf`: nome civil + CPF (validado, sem anexo — decisão do dono).
- ✅ **Migração 483** `senderzz_document_change_request` (status pending/approved/rejected,
  snapshot old_nome/old_document, índice único parcial = 1 pendente por usuário).
- ✅ **Portal Go**: `GET/POST /portal/account/document-change` (multipart; magic-byte jpeg/png/pdf;
  unicidade do doc; grava anexo PRIVADO em `/app/uploads/doc-changes`). `api()` do portal-ui agora
  não força JSON quando o body é FormData (upload de arquivo).
- ✅ **Admin Go**: `GET /document-changes[/{id}]`, `POST .../approve|reject`,
  `GET .../{id}/attachment` (auth-only, Content-Disposition: attachment — nunca inline).
  Aprovar limpa a chave PIX (força reconfiguração) + grava `senderzz_portal_audit_log`.
- ✅ **Admin-ui**: página `/document-changes` (fila aprovar/rejeitar + baixar cartão CNPJ),
  botão em Usuários + Cmd-K. **Portal-ui**: `DocumentChangeDrawer` no card Sua conta.
- ✅ **Compose**: `falk_uploads` montado tb no `portal-service`; `DOC_CHANGE_UPLOAD_PATH` nos 2.
- ✅ **PIX reset COMPLETO**: aprovar limpa `settings.pix_key/pix_key_tipo` (afiliado/JSONB) **E**
  desativa (`active=FALSE`, soft — nunca DELETE) as contas de saque do PRODUTOR em
  `sz_cod_withdraw_accounts` cujo `holder_cpf` OU `pix_key` casa com o documento ANTIGO. Força
  recadastro sem apagar conta bancária. Escopo por `user_id = COALESCE(wp_user_id, id)` (mesma chave
  do codWalletKey). SQL validado em DB scratch (desativa só as do doc antigo).
- Deploy: migração 483 + rsync go/{portal,admin}+UIs+compose; build+recreate portal-service (agora com
  volume) e admin-service. tsc/go build limpos; DDL 483 + jsonb + regexp validados em DB scratch.
- ✅ **Migração é SEGURA de rodar** (revisado): apesar de `schema_migrations` prod ter parado em ~451
  ([prod-migration-lag]), TODAS as migrações 452–482 são idempotentes/convergentes (DDL `IF NOT
  EXISTS`; backfill 460 guardado por `WHERE transaction_fee=0`; 481 por `status='available'`; 465/466
  setam valor fixo; 468 `ON CONFLICT DO NOTHING`). Rodar `scripts/db-migrate.sh` reaplica os lagged
  sem duplicar dado e registra tudo + aplica 483. Caminho padrão, sem isolamento especial.

### 2026-06-26 (upload + estoque canônico + branding checkout) — ✅ DEPLOYADO / NO AR
- ✅ **Upload de foto do produto** (arquivo do PC): endpoint admin `POST /products/upload-image` (magic-byte jpeg/png/webp, crypto/rand) + FileServer `/uploads/products/*` + **volume persistente `falk_uploads:/app/uploads`** + rota gateway `/uploads/products/` + nginx reload. Form do produto tem botão "Enviar imagem".
- ✅ **Estoque canônico por produto** (COD+FF compartilham, sem esgotado falso): migração **480** `sz_resolve_product_id()` (resolve post_id da oferta / wp_post_id / sz_products.id → produto canônico, inclusive offer-post órfão via produtor+nome). `resolveQtySellable` soma o pool do produto por (id, wp_post_id), ignora post fantasma. Datalaprox COD (post 1075) agora lê **289** (era −1/esgotado). Provado em prod. Sem apagar dado.
- ✅ **Branding checkout v1**: logo FALK fallback + cor primária por produtor. Meta `sz_brand_logo_url`/`sz_brand_primary_color` (senderzz_portal_user_meta); GetOffer emite `logo_url`/`cor` (reusa pipeline white-label do checkout-ui); aba "Marca do checkout" no portal do produtor (Settings); fallback `checkout-ui/public/falk-falcon.png`.
- Deploy: migração 480 + rsync go/{orders,portal,admin}+UIs+compose+gateway.conf; build+recreate; gateway/checkout 200.

### 2026-06-26 (rajada produto/checkout/usuários) — ✅ DEPLOYADO / NO AR
- ✅ Excluir **cliente/afiliado** (`DELETE /clientes/{id}` soft-delete role-gated + cascade checkouts; botões na aba Clientes e lista Afiliados). Fix bug `updated_at` (coluna não existe em senderzz_portal_users) — afetava delete E promoção cliente→produtor.
- ✅ **Relatório Checkouts** → submenu da aba Produtores (removido do menu lateral).
- ✅ Form de produto vira **modal overlay** (lista não some mais). Seletor de dono mostra **nome · e-mail · papel**.
- ✅ **Foto do produto** (campo URL no form → `sz_products.meta.image_url`; checkout/portal leem a mesma chave; fix `COALESCE(wp_post_id,id)` p/ produto nativo).
- ✅ **Variação**: se o produto tem variação, dropdown do checkout não oferece "Sem variação" (força variação real).
- ✅ **Link de Expedição** aparece no checkout do produto nativo (root cause = `post_id` nativo usa `sp.id`, não `wp_post_id`; branch corrigida em go/portal Checkouts).
- ✅ **Visível na vitrine** — toggle no produto (migração **479** `sz_products.vitrine_visible` + filtro vitrine.go + form admin). 479 aplicada+registrada na prod.
- ✅ Mini-copy abaixo de "Afiliação" (portal produtor).
- ✅ Carteira Expedição só produtor (tpc_clientes role filter) — já no ar.
- Deploy: migração 479 ANTES do código; rsync go/{admin,portal,orders}+UIs; build+recreate; gateway 200.

### 2026-06-26 (form Editar CD + Áreas de operação) — ✅ NO AR
- ✅ Form "Editar CD" (admin) reduzido a **só Nome + Ativo**: removidos Cidade, UF, Endereço, Lat, Lng (cidades vêm das ZONAS; lat/lng sem uso operacional). List card limpo (sem cidade—UF/endereço/lat-lng). `cidade`/`uf` seguem no payload com default (cidade←nome no create, SP no uf) p/ não quebrar NOT NULL nem os seletores de CD do portal. `admin-ui/src/pages/Cds.tsx`. Deploy: rsync SÓ Cds.tsx → /opt/falk/admin-ui + build/recreate admin-ui (evita vazar trabalho local do outro agente). Bundle sem "Lat / Lng"; /admin/ → 302.
- ✅ Portal "Áreas de operação" 2 fixes (`portal-ui/src/pages/Localidades.tsx`): (1) **"2x SP"** — subtítulo UF só renderiza quando difere do nome (CD prod tem nome='SP'+uf='sp'). (2) **zona morta "(Sem zona)"** filtrada no cliente (id 0, sentinela usada pelo admin via zona_id=0 — NÃO apagada do banco), some da cobertura e da contagem. Deploy rsync só Localidades.tsx + build/recreate portal-ui. Asset `index-DqEnjktS.js`; filtro '(sem zona)' no bundle; gateway 200.

### 2026-06-26 (sessão financeira+bugs) — ✅ DEPLOYADO / NO AR (app.falklog.com.br)
> Deploy feito: migração 478 aplicada+registrada na prod (1587: 63,54); rsync go/{admin,orders,portal}+UIs → /opt/falk; docker compose build + up --force-recreate dos 5 serviços; gateway 200; delete teste produtor 40 executado (backup _backup.*_20260626). SSH: `-i ~/.ssh/senderzz_vps -p 10037 administrator@93.127.141.6` (config alias vps-senderzz faltava IdentityFile).
> Extras deployados: saque afiliado `-$2::numeric` (fix "operator is not unique" em cod_saques+bulk_queues); Carteira Expedição filtro `role='produtor'` (tpc_clientes — só produtor tem carteira).
- ✅ **Auditoria financeira completa** → `AUDIT-FINANCEIRO-2026-06-25.md` (9 divergências, view canônica, unificação 16→6).
- ✅ **P0/P1 financeiro**: migração `478-financial-canonical-view.sql` (var única `sz_cod_delivery_fee` editável no menu Taxas + `sz_producer_tx_rate()` + **view `sz_order_financials`** = decomposição golden #1587 + backfills). LÍQUIDO PRODUTOR corrigido (pedido 1587: 63,54, era 213,54). Repointados pra view: cod_livro (summary/orders/producers), order_detail, dashboard (split+wallet audit), audit (readers+writers), orders writer. checkout grava delivery_fee+producer_net em COD novo. Carteira afiliado = ledger vivo 3-estados (disponível=sacável/pendente/sacado). commission_pct 60% (era 57%). Reconcilia gap 0,00; tests verdes.
- ✅ **Release pendente**: botão **Forçar** (override auditado) + fix `available_at IS NULL` + toast honesto (retenção 30d).
- ✅ **Cliente/afiliado vê ofertas na Vitrine** (removido gate de vínculo ativo; link trava até afiliar — `vitrine.go`).
- ✅ Botões Exportar/Filtros lado a lado; badge "Cliente"+avatar removido do topo portal.
- ✅ SQL delete produtores de teste (40+63) c/ backup: `infra/postgres/ops/delete-teste-producers-2026-06-26.sql` (não executado).
- ⏳ FALTA: feature cliente→produtor (cadastrar produto + aba Clientes + promoção ao aprovar); unificação P2 (telas); revenue `pendente` projeção (P1-2); UNIQUE(wp_user_id) (P1-6); bug "estoque insuficiente" (precisa query prod); DEPLOY.

### 2026-06-25 (sessão portal afiliado) — tudo NO AR
- ✅ **A** — saudação "Olá, {nome}" + barra ProgressTier (rumo a R$1mi) voltaram do topbar pro **topo da sidebar** (`portal-ui Layout.tsx`, reusa `.szv2-sidebar-hello`).
- ✅ **B** — card "Suas afiliações" (produtores que aprovaram + comissão) **removido** da visão do afiliado (`Affiliates.tsx`). Espelho do produtor ("Meus vínculos como afiliado") mantido de propósito.
- ✅ **C** — produto fantasma "Checkout" **apagado** da prod: `senderzz_checkout_links` id 69 (base_name='Checkout', producer 15, R$123, affiliate_visible, **0 pedidos**). Era o 1º link sem-nome do post Datalaprox (1075), NÃO confusão com Dorvax (Dorvax=producer 40, tb 0 pedidos). Backup: `/tmp/falk-checkout_links-backup-2026-06-25.sql`. Links 36→35.
- Deploy: rsync `portal-ui/` → `/opt/falk/portal-ui`; `docker compose -f docker-compose.falk.yml build portal-ui` + `up -d --force-recreate portal-ui`. Asset live = `index-B61xBply.js`. Gateway :9088 → 200.

### Infra / dados
- ✅ Banco WP real importado (MySQL 127MB → Postgres): 41 pedidos, motoboys, 20 afiliados, 2 produtores, carteiras.
- ✅ 3 domínios live com dados reais.

### Segurança
- ✅ Auditoria ABIN (14 deptos) + ~22 fixes aplicados (CRIT/ALTO/MED/BAIXO).
- ✅ Fixes: binds loopback, LGPD CPF webhook, CORS allowlist, JWT fail-closed, security headers, rate-limit.
- ✅ Migrações: labels-owner-scope, LGPD retenção CPF/endereço, admin-password-resets, expedicao-webhook-reprocess.

### Financeiro
- ✅ **Bug sistêmico de fuso (TZ)** no filtro de data corrigido em **35+ sites / 10 arquivos** (admin+motoboy):
  coluna `timestamptz` comparada com `::timestamp` cru descartava linhas (1 dia dava 0). Fix `AT TIME ZONE 'America/Sao_Paulo'`.
- ✅ **Backfill split afiliado** (#46): 4 pedidos importados com `affiliate_amount`=bruta/fee=0 corrigidos p/ split 4,99% (migração 460). Idempotência provada.
- ✅ Faturamento auditado: 4,99% bate exato nos 2 takes; TOTAL = soma dos componentes.
- ✅ Cancelado zera tudo; **frustrado mostra prejuízo** afiliado (bruta+taxa frustração) + produtor (entrega+taxa frustração).
- ✅ Taxa de frustração configurável (`sz_frustration_fee_*`, hoje `none`=0).

### Painel admin (admin-ui + go/admin)
- ✅ **Merges de tela** (sidebar ~18 → ~12 itens): Dashboard+Faturamento (`/`), Etiquetas/QR+Ações em Lote (`/etiquetas-lote`),
  Produtos+Aprovar+Estoque (`/products`), Expedição ME (4→1), CDs/Zonas (2→1), Afiliados $ (Carteira+Regras numa tela só),
  Painel do Dia (Dashboard Motoboy + Motoboys do Dia).
- ✅ **66 selects nativos → FalkSelect** (tema FALK) site-wide (4 multiple/optgroup mantidos temáticos).
- ✅ Excluir produtor (soft-delete) + drawer `DetailDrawer`; "Fechar" duplicado removido (5 telas).
- ✅ Comissões: tabela enxuta + drawer lateral com breakdown rico.
- ✅ Pedidos: variação = campo de produto (Datalaprox="pote", migração 461); comissão azul; telefone sem +55.
- ✅ Divergência Produtos (predicate drift) → `productListableFilter` (2 de 2).
- ✅ Afiliados ordenado por faturamento DESC + filtros robustos (vendas/valor_min/produtor).
- ✅ Drawer do afiliado: seção "Taxas cobradas" (transação 4,99% + penalidades + saque).
- ✅ Contagem afiliados/produto (era 0): conta via produtor → Datalaprox/Dorvax = 22.
- ✅ Status universal motoboy (coluna duplicada removida); filtro data-de-entrega lê `_sz_delivery_date`.
- ✅ COD isolado das telas de Expedição (Transações Expedição sem aba COD; saldo Carteira Expedição sem COD).
- ✅ Campos obrigatórios no form de motoboy (client+backend).
- ✅ Máscaras no cadastro: WhatsApp `(11) 96348-6603`, CPF/CNPJ `419.002.918-19`.

### Landing + Login
- ✅ LP reconstruída (Fulfillment + Cash on Delivery como pilares separados, checkouts, transportadoras, preços interativos, ícone falcão).
- ✅ Login renovado: falcão preto+azul, copy afiliados, CPF/CNPJ no cadastro, "esqueci senha".

### Indicação (indique e ganhe) — fundação
- ✅ `referral_code` permanente por usuário + link `/r/{code}` (migrações 433/434).
- ✅ **Recompensa FIXA**: 2,5% por pedido COD + 1% por expedição, qualquer usuário (config removida da tela).
- ✅ Ledger dedicado `senderzz_referral_rewards` + função `sz_referral_payout()` idempotente (UNIQUE ref, provada 2×) — migração 462.
- ✅ Captura: `referred_by` (migração 462/463), signup resolve `?ref`, approve propaga, endpoint `/onboarding/referral/{code}`, Login.tsx captura ref.
- ⚠️ **Falta agendar o cron** do payout (gap de infra acima).

---

### Signup sem aprovação → role `cliente` (NOVO — 2026-06-23)
- ✅ Signup cria usuário **ATIVO** role='cliente' direto (sem aprovação) + **auto-login** (token portal). onboarding_requests vira só auditoria.
- ✅ go/portal: 'cliente' = afiliado-like em TODOS os gates (helper `isAffiliate` + inline) → COD/vitrine/comissões/carteira como afiliado. Produtor-only = allowlist (cliente 403).
- ✅ **Matrix de segurança empírica PASSOU**: produtor-only (products/approve/default-commission) → 403; COD/afiliado (me/vitrine/orders/affiliates-dashboard) → 200; sem token → 401. ZERO escalada.
- ✅ Promoção cliente→afiliado ao produtor aprovar afiliação (guard role='cliente', nunca rebaixa).
- ✅ admin-ui Login: "Solicitação enviada/aguardando aprovação" REMOVIDO → "Conta criada! Sua conta de cliente já está ativa."
- ⚠️ **portal-ui (frontend) NÃO é servido no gateway falk** (só a API `/wp-json/.../portal/`). Cliente é criado+autorizado, mas falta servir o `/portal/` p/ ele USAR. Próximo passo.

## 🚧 EM ANDAMENTO / decisões do dono

- 🟡 **Servir portal-ui no gateway falk** (`/portal/` SPA) — necessário p/ cliente/afiliado/produtor usarem o frontend.
- 🟡 **4 faixas de afiliado** (`FALK-FAIXAS-PROPOSTA` foi absorvido aqui): enquadramento automático por pedidos/mês +
  override por usuário; saque 30/21/14/7; sem métrica = faixa mais cara. **Aguarda limites/valores do dono.**
- 🟡 **Saldo COD contaminado** (#15 R$1.770 em `tpc_carteira`): display isolado ✅, mas o valor gravado é o único
  registro do saldo COD real → mover p/ carteira COD é migração delicada (aguarda OK do dono).
- 🟡 **Agendar cron de indicação/COD** (host crontab ou go/cron) — decisão de deploy.
- 🟡 2FA admin (pendência antiga).
- 🟡 Deferidos do dono: rotação de segredos, purge de git history, hardening gateway (rate-limit+CSP), `.env.test`.

---

## Convenções técnicas críticas (ex-CLAUDE.md — consolidado aqui)

- **Foco = Go + React.** O plugin PHP (`senderzz-logistics.php`, `includes/`, `src/`) é LEGADO. Trabalho novo
  vai em `go/admin` + `admin-ui` (painel), `go/portal` + `portal-ui` (portal), `go/orders|wallet|labels|motoboy` etc.
- **Regra financeira #1587** acima — `valor` (= `senderzz_affiliate_transactions.amount`) é LÍQUIDO; nunca recalcular 4,99%.
- **Datas TZ-safe:** coluna `timestamptz` NUNCA comparar com `::timestamp` cru. Usar
  `(col AT TIME ZONE 'America/Sao_Paulo')::date = $N::date` (single-date) ou
  `>= ($N::date)::timestamp AT TIME ZONE 'America/Sao_Paulo'` / `< (($N::date+1)::timestamp AT TIME ZONE 'America/Sao_Paulo')` (range).
- **Atribuição (classe de bug recorrente):** `sz_orders.produtor_id`/`sz_products.produtor_id` = `senderzz_portal_users.id` (portal id).
  `sz_order_items.produto_id` = `sz_products.wp_post_id` (NÃO `.id`). `senderzz_affiliates.produtor_id` = `id OU wp_user_id`.
  `senderzz_affiliates.afiliado_id`/`sz_orders.affiliate_id` = `wp_user_id`. Misturar id-spaces = nomes vazios/cross-attribution.
- **Ledger idempotente:** booking financeiro usa `UNIQUE(ref)` + `ON CONFLICT DO NOTHING`, ref ancorada no order_id estável
  (ex. `referral:order:{id}`). Crons SQL (`sz_*`) são idempotentes — re-disparo é inofensivo. PROVAR rodando 2×.
- **REST namespaces:** `wc-melhor-envio/v1` (labels), `senderzz/v1` (orders/portal), `tp-carteira/v1` (wallet/PIX), `sz-motoboy/v1` (motoboy).
- **Status financeiro:** frustrado/cancelado têm regra própria (`financeiroFrustratedStatuses`/`financeiroCancelledStatuses` em order_detail.go) — não tocar sem checar.
- **Migrações** em `infra/postgres/NNN-*.sql`, idempotentes (`IF NOT EXISTS`/`ON CONFLICT`/`CREATE OR REPLACE`). Aplicar ANTES do deploy do Go que referencia a coluna/tabela nova.
- **Patch-ID markers** nos comentários (`// CRIT-03`, `// SEC-02`, `// V-NEW-01`, `// REF-PAYOUT`) são âncoras de grep reais — não remover.
- **PHP legado** (se precisar): bootstrap order em `senderzz-logistics.php` é load-bearing; `function_exists()` em toda função procedural; `senderzz-fixes-scale.php` carrega por último de propósito.

## Convenções de deploy/trabalho

- Só painel **admin** por ora — NÃO mexer no nível afiliado/produtor/operador (portais de role), EXCETO o liberado p/ indicação + role cliente.
- PT-BR em mensagens/logs/comentários. Mutação financeira em prod = OK explícito do dono + teste em rollback.
- Verificar `go build ./...` + `npx tsc --noEmit` antes de todo deploy.
- VPS/deploy: ver seção "Produção (VPS)" no topo. Sempre `scp/rsync` do Mac (nunca heredoc grande no SSH).

## 🔎 AUDITORIA COMPLETA 2026-06-23 (60 achados · 27 crit/high confirmados)

### 🔴 CRÍTICOS
1. **Cron NÃO roda em produção** (sem serviço falk-cron no compose) → COD release, referral payout, liberação de comissão e anonimização LGPD **inertes**. **R$1.452,89 COD preso** em pending (vencido até 18d); **R$2.392,96 comissão afiliado trava em 2026-06-28**. → criar `go/cron/Dockerfile` + serviço `cron` no compose. ✅ migrate-runner já destravado (este prompt).
2. **Penalidade de frustração do afiliado é CREDITADA no sacável** (devia debitar) → perde dinheiro. `affiliate_wallet.go` WalletFix/ReleasePending.
3. **Receita COD vai pra carteira de Expedição** (`tpc_carteira`, R$1.770 user 15) — dono errado + gastável em frete. Cortar o caminho PHP `Venda COD`→tpc + migração corretiva.
4. **portal-ui 404 em testes.falklog.com.br/portal/** — só adicionei /portal/ no gateway.conf (prod), não no `gateway-test.conf`.
5. **`sz_referral_payout()` RETURNS void** mas o runner Go faz `Scan(&count int)` → job de indicação nunca credita. → `RETURNS integer`.
6. **/r/{code} resolver** rejeita códigos reais (8 chars vs exige 16) → indicação quebrada no go/affiliates.

### 🟠 ALTOS (resumo)
- Links "Política de Privacidade" → 404 (checkout-ui, portal CookieConsent/DpoFooter) → apontar pra `falklog.com.br/privacidade.html`.
- Canal DSR (LGPD) grava pedidos mas SEM tela admin/DPO p/ atender (SLA 15d).
- `checkout.go` grava `sz_order_items.produto_id = sz_products.id` mas leitores usam `wp_post_id` (convenção dividida).
- COD `net`/`fee` nunca calculados → produtor vê R$0 (fix de display feito; dado pendente).
- Comissão afiliado só entra no ledger por ação MANUAL do admin (não books na entrega).
- URL webhook hardcoded `app.senderzz.com.br` em 3 telas admin.
- 9 handlers usam `tableExists()` não-cacheado (~28 round-trips/request).
- Sem backup automatizado do Postgres falk.

### 🟡 MÉDIOS (destaques)
- portal-ui sem FalkSelect/FalkDatePicker (16 select + 11 date nativos do SO).
- admin-ui bundle 1.05MB sem code-splitting.
- Sem healthcheck nos serviços Go; 3 stacks na VPS (disco 82%); CORS admin no fallback dev; DPO email placeholder `@falk.log`.
- Indicação = passivo permanente sem teto/janela (regra de negócio antes de ativar).

## Changelog por prompt

- **2026-06-24 (overhaul sidebar/topbar + dashboard — portal-ui+admin-ui+go/portal — NO AR ✅)**: lote de UI
  pedido pelo dono. (1) **Logo centralizada** na sidebar (portal+admin, CSS `justify-content:center` no
  `.szv2-sidebar-head`; container do logo→44px). (2) **Menus NÃO colapsáveis** — grupos sempre expandidos, sem
  toggle/chevron `▾/▸` (kicker virou `<div>` rótulo; itens sempre renderizam; portal+admin). (3) **ProgressTier**
  (barra "rumo a R$ 1MM") **saiu da sidebar → topbar** ao lado de "Olá, {nome}" (some no mobile <860px). (4)
  **Dashboard do produtor**: removidos cards **"Saldo disponível"** + **"Taxa de entrega COD"**; adicionado
  **"Comissão líquida"** ao lado de Faturamento = Σ `sz_orders.affiliate_amount` (comissão de afiliados já net
  4,99%, regra #1587) no período — novo campo `comissao_liquida` em `go/portal reports_portal.go`
  (`computeReportMetrics`). Verificado: `affiliate_amount` existe; Gabriel SUM=R$5.479,83. `tsc` (2 apps) +
  `go build` limpos; `falk-portal`+`falk-portal-ui`+`falk-admin-ui` rebuild+recreate.

- **2026-06-24 (Exportar relatórios CSV na tela de Pedidos — admin-ui + portal-ui — NO AR ✅)**: botão
  **"↓ Exportar relatórios"** na tela de Pedidos (AMBAS: painel admin e portal), exporta os pedidos
  **conforme os filtros aplicados** em CSV. Implementação **client-side** (sem mudar backend): refaz o fetch
  da lista com os mesmos filtros + `limit=200` (teto atual do backend) e monta o CSV. **Layout pt-BR**: BOM
  UTF-8 (acentos no Excel), delimitador `;`, números com vírgula decimal (Excel soma), datas DD/MM/AAAA,
  valores escapados (aspas duplicadas). 17 colunas (Pedido, Data, Status, Cliente, Telefone, CEP, Cidade, UF,
  Produto, Variação, Oferta, Valor da oferta, Valor do pedido, **Comissão líquida** [já net 4,99% #1587],
  Taxa motoboy, Afiliado, Data de entrega). Arquivo `pedidos-AAAA-MM-DD.csv`; toast de confirmação. `tsc`
  limpo nos dois apps; layout do CSV testado com amostra (escaping de `;`/`"` ok). `falk-admin-ui`+`falk-portal-ui`
  rebuild+recreate. ⚠️ teto de 200 linhas (dataset atual ~41) — se crescer >200, vira endpoint streaming no backend.

- **2026-06-24 (header LP responsivo — `infra/falk/lp/index.html` — NO AR ✅)**: pedido do dono — no
  **mobile** os botões do topo (Entrar/Criar conta) + nav-links viravam **hambúrguer** pra liberar espaço e
  **aumentar a logo**; no **desktop** logo maior com mais destaque. Feito: logo desktop `38→54px` (header `74→88px`);
  mobile (≤980px) esconde `.nav-links`+`.nav-cta`, mostra `.nav-burger` (direita) que abre dropdown
  `position:absolute` (não empurra o hero) com os 4 links + Entrar/Criar conta full-width; logo mobile `38→46px`.
  Burger anima pra X via `aria-expanded`; fecha ao clicar link ou ao voltar pro desktop (resize). JS vanilla inline.
  Deploy: **só `index.html`** via `rsync --checksum` (NÃO subi `privacidade.html`/`termos.html` — seguem com
  placeholder `[PREENCHER: CNPJ]`, hold do dono). Verificado LIVE: disco VPS + container `falk-lp` + `falklog.com.br`
  via Cloudflare = 8 refs `nav-burger`, logo 54/46px (sem cache velho).

- **2026-06-24 (tela Afiliação: produtos+quantidade+valores — portal-ui+go/portal — NO AR ✅)**: a lista
  "Links de venda disponíveis" do AFILIADO virou rica como a do produtor (pedido do dono): **submenu de
  PRODUTOS** (abas, só quando >1 produto) + **filtro por QUANTIDADE** (chips 1/2/3/5, igual produtor: sem
  "Todos"/contadores, toggle) + **valor do link** (R$) + **valor da comissão** (R$ = preço × %, BRUTA,
  consistente c/ a Vitrine). Backend `go/portal affiliates_portal.go`: `affLink` + query ganham
  `cl.display_value` e `cl.base_name`. Front `portal-ui Affiliates.tsx`: agrupa por produto via `base_name`
  sem a quantidade ("1 Datalaprox"→"Datalaprox"). **Restrição de dado (documentada)**: links do afiliado NÃO
  são splitáveis por produto via `post_id` (= canal 22, não produto) nem pelo vínculo (`produto_id=0`,
  produto-agnóstico) — o ÚNICO sinal confiável é `base_name` (migração 471). Verificado na query de prod
  (display_value + base_name presentes em 15/15 links; 0 com base_name vazio; row-count idêntico, sem dup).
  `tsc` + `go build` limpos; `falk-portal`+`falk-portal-ui` rebuild+recreate. ⚠️ render e2e não verificável
  headless (tela exige login de afiliado) — provado por dados+componentes no ar.

- **2026-06-24 (layout tela Produtos do produtor — portal-ui — NO AR ✅)**: reorganização (pedido do dono):
  o card grande **"Centro de Distribuição"** no topo virou uma **linha fina única** ("Centro de Distribuição:"
  + chips CD inline) posicionada **ENTRE o cabeçalho do produto e os KPIs** de status; o botão **"+ Adicionar
  produto"** saiu do topo e foi pra **linha das abas de produto** (Datalaprox/Dorvax, alinhado à direita).
  Mesmo `cdFilter` (escopo global, função intacta). `portal-ui/src/pages/Products.tsx`; `tsc` limpo;
  `falk-portal-ui` rebuild+recreate.

- **2026-06-24 (LGPD fixes do checkout + normalização nomes antigos — NO AR ✅)**: (1) **Migração 472** normaliza
  os nomes LEGADOS de oferta pra regra de estágio: tira descritor livre (`Padrão/padrão/Downsell/Upsell/Remarketing[N]`
  + backtick), agrupa por produto real, re-estagia por preço com desempate por id (0 nome duplicado, 0 par quebrado).
  Aplicada prod+dev. Ex.: "5 Potes Padrão/Downsell/padrão/Remarketing" → "5 Potes / Downsell / Remarketing / Remarketing 2".
  (2) **8 fixes LGPD** (auditoria `AUDIT-LGPD-CHECKOUT-2026-06-24.md`) aplicados e **deployados na prod**:
  consent_accepted enforce no POST /order (M1); máscara nº+complemento no rastreio público (M4); webhook só-https
  quando carrega PII (A1); CEP fora dos logs de frete/zona (B5); migração **473** estende `sz_anonymize_old_order_pii`
  (CPF em order_meta + ip/ua/customer_note de sz_orders >2 anos, A4/M6); migração **474** + cron diário
  `sz_purge_webhook_log` (TTL 30d, A2); handler **DSR** admin-only no go/admin (lista/trata/lookup por telefone-CPF,
  Art.18/19, A3/B6); aviso de privacidade in-checkout + dica no campo livre (M3/B4, checkout-ui). Rebuild+recreate de
  orders/cron/portal/admin/checkout-ui; verificado e2e (offer buyer=base, cron 9 jobs, DSR sob auth, sem panic).
  ⚠️ **PENDÊNCIAS DO DONO**: (a) `privacidade.html` editada (controlador/CNPJ/encarregado + ViaCEP + aviso rastreio)
  mas com placeholders `[PREENCHER: CNPJ]` — **SEGURA o deploy do LP** até o dono passar CNPJ/razão social/endereço;
  (b) **mina de migração**: `seed-cod-wallet-producer46.sql` está no dir de migração da prod, não-registrado e fora
  dos padrões de skip → `docker compose up` completo injetaria dado demo na prod (meus deploys usam `--no-deps`,
  por isso não dispara) — apagar do dir ou baselinar; (c) M5 (cifrar CPF em repouso) deferido — precisa key-management.
- **2026-06-24 (filtro Quantidade dos checkouts — portal-ui — NO AR ✅)**: na tela do produtor
  (`portal-ui/src/pages/Products.tsx`, seção "Links de Expedição vinculados"), o filtro **Quantidade**
  perdeu o chip **"Todos"** e os **contadores `(N)`** de cada chip (pedido do dono). Agora: `1 | 2 | 3 | 5`
  (+ "Outros" quando houver). Como "Todos" saiu, clicar de novo no chip ativo **limpa o filtro** (= mostra
  todos) — sem prender o produtor num filtro. `tsc` limpo; `falk-portal-ui` rebuild+recreate; bundle servido
  sem "Todos (" (count=0).

- **2026-06-24 (imagem checkout + lag migração + afiliado — img NO AR ✅)**: **imagem do produto** sumia no
  resumo do checkout FALK. Causa REAL (provada em Chrome headless): `checkout-ui/index.html` tem uma meta
  CSP **própria** (mais estrita que o gateway) com `img-src 'self' data:` — bloqueava a imagem cross-origin
  hospedada em `app.senderzz.com.br` (curl ignora meta-CSP → dava 200, browser bloqueava). Fix: relaxar p/
  `img-src 'self' data: https:` (alinha com a CSP do gateway). Rebuild+force-recreate `falk-checkout-ui`.
  Verificado LIVE: `naturalWidth=634`, 200 image/webp, sem bloqueio CSP. ⚠️ imagem ainda vem do host senderzz
  (cross-origin OK) — migrar arquivos p/ host FALK = follow-up.
  **Lag de migração de prod = só BOOKKEEPING**: prod já tinha o EFEITO de 460..465 (aplicadas out-of-band sem
  registrar; verificado linha-a-linha: split 4,99% dos pedidos 1581/1584/1585/1586 correto, colunas/tabelas/fn
  presentes). Reconciliei `schema_migrations` (INSERT 460..465). **Sem problema de dados/financeiro.** Processo:
  alguém aplicou SQL fora do runner — vale disciplinar.
  **Afiliado — atribuição `?r=` PORTADA p/ o checkout FALK (NO AR ✅)**: dono pediu "melhor prática de mercado".
  Era: `affiliate_url` apontava p/ FALK mas a atribuição SUMIA (salt vazio → `?r=` nem emitido; checkout-ui não
  lia `?r=`; `go/orders` esperava outro `aff_token`/tabela vazia). Fix end-to-end fiel ao WP + best-practice:
  (1) migração **467** semeia `sz_aff_ref_salt` (presença habilita o `?r=` no portal `appendRefToken`);
  (2) **checkout-ui** lê `?r=`, PERSISTE em sessionStorage (last-click sobrevive a refresh) e envia no POST /order
  (`App.tsx useAffRef`, `Checkout.tsx`, `api.ts`); (3) **go/orders** `decodeAffRefToken` (inverso exato do encode:
  base64url→BE32→`senderzz_affiliates.id`) resolve o vínculo ATIVO e credita — **server-authoritative** (front só
  repassa token opaco; comissão % vem da precedência 2b, nunca do cliente). Teste de round-trip Go (todos ids/salts
  + lixo + id=0) **verde**. **e2e no DEV**: pedido SZ-0001951 com `?r=` → `affiliate_id=28` (vínculo 12), bruta 36,90
  (30% precedência) → fee 1,84 (4,99%) → líq 35,06 (golden #1587 intacto), depois limpo. **Prod**: 467 aplicada,
  `falk-orders`+`falk-checkout-ui` rebuild+recreate, bundle com `falk_aff_ref` ✓, resolver validado contra dados
  reais (20 vínculos ativos, 30 links visíveis). ⚠️ não rodei pedido de teste em PROD (evitar poluir financeiro);
  prova é o e2e do dev com código idêntico + componentes verificados no ar.

- **2026-06-24 (nome de oferta por ESTÁGIO + auditoria LGPD checkout — NO AR ✅)**: **regra de composição do
  nome mudou** (decisão do dono). O nome NÃO carrega mais **valor** nem **% de comissão**. O discriminador agora
  é o **estágio de funil por PREÇO** (a mais cara = Principal): rank 1 → base; 2 → `Downsell`; 3 → `Remarketing`;
  ≥4 → `Remarketing N` (N=rank-2 → "Remarketing 2", "Remarketing 3"…).
  **PRODUTOR vê o estágio; CLIENTE vê só o produto limpo** (decisão do dono): checkout-ui (resumo do pedido),
  rastreio e snapshot do item do pedido usam a BASE (`go/orders/checkout.go` GetOffer + POST /order →
  `COALESCE(NULLIF(base_name,''),name)`); a lista de ofertas do produtor mantém o nome com estágio.
  Migração **471** (`base_name` + backfill + recompute), `links_portal.go` (Create/Delete recomputam estágio em
  lockstep na tx; dedup agora por base+preço; pareamento correio↔motoboy por nome, **0 pares quebrados** nas 38
  ofertas reais), preview do portal-ui (sem valor; explica a regra), teste DB `links_naming_db_test.go`
  (5 cenários, **verde** no banco vivo). **Serviços `orders`+`portal` reiniciados** → tudo no ar; verificado
  e2e: produtor "3 Potes Padrão Downsell" / cliente "3 Potes Padrão". **Auditoria LGPD do checkout**
  (multi-agente, 31→15 achados, score 6,5/10, 0 crítico · 4 alto): `AUDIT-LGPD-CHECKOUT-2026-06-24.md`
  (top: PII em webhook HTTP claro + log sem TTL; canal Art.18 write-only; CPF claro retido pra sempre).
  **Auditoria = só relatório, 0 fixes aplicados** — pendência do dono priorizar (quick-wins: M2 CNPJ, M1
  consent_accepted, B5 CEP nos logs, B1 ViaCEP).
  **DEPLOY PROD (app.falklog.com.br) ✅**: rsync Mac→VPS (`go/portal`, `go/orders`, `portal-ui`, `checkout-ui`,
  `infra/postgres/471`); migração **471 aplicada standalone** no DB `falk` (42 ofertas — NÃO rodei o runner
  inteiro por causa do lag prod 451); rebuild + `up -d --force-recreate` em `falk-portal/orders/portal-ui/checkout-ui`
  (todos Up, sem erro de log). Verificado e2e ao vivo: produtor "1 Datalaprox Downsell" / cliente (offer API
  via gateway) "1 Datalaprox". ⚠️ schema_migrations da prod segue com lag 451 (não toquei — 471 é idempotente).
- **2026-06-24 (toast lateral site-wide — NO AR ✅)**: comissão do produtor virou **lápis discreto** (input só
  ao clicar; portal-ui Products). Depois **auditoria de toast** (62 arquivos, 171 mutações, 1 agente/arquivo):
  38 já no toast lateral `.szv2-toasts`; achados → **13 arquivos corrigidos** p/ todo save/mudança disparar o
  pop-up lateral temático. (a) **gaps silenciosos**: Cds toggle, Motoboys cadastrar/desativar, Pix
  confirmar/cancelar+verificar, Products sync/criar/editar/excluir, portal Affiliates auto-aprovação +
  Settings notificação. (b) **banners inline locais → lateral**: BulkActions, Orders (reagendar/clonar/cancelar),
  Zonas, CodWalletProducer (2 helpers), admin Settings, Producers, portal Freight — helper local `showToast`
  religado p/ `emitToast` (estado/JSX do banner morto removido). Falso-positivo (já cobertos via `onSuccess` do
  pai): MotoboyCarteira, Wallet — intocados. **Fora**: login/redirect (confirma navegando), `info` honesto
  (AffiliateRules), e **checkout-ui** (sem ToastHost; sucesso já confirma via ThankYou — erro padronizado fica
  como decisão). `tsc --noEmit` limpo nos 2 UIs. Deploy: rsync `--checksum` (só 10 admin + 3 portal sobem;
  resto casa) + build `admin-ui`+`portal-ui` + force-recreate. LIVE: admin `index-BhqmV2SL.js`
  (`CD ativado`/`Motoboy desativado`/`Produto excluído`/`Recarga confirmada`/`Configurações salvas`),
  portal `index-DDINjHSL.js` (`Auto-aprovação ativada`/`Preferência de notificação salva`/`Bloqueios salvos`).
  Relatório: `AUDIT-TOAST-2026-06-24.md`.
- **2026-06-24 (toast checkout-ui — NO AR ✅)**: checkout do cliente não tinha sistema de toast (só erro inline
  `fk-alert`). Criado `checkout-ui/src/Toast.tsx` (emitter `emitToast` + `CheckoutToastHost` via portal, **tema
  próprio do checkout** `--falkz-*`, não szv2) + CSS `.fk-toasts` (canto sup-dir, z-index acima do modal). Montado
  em `App.tsx`; disparado no **catch do `createOrder`** (única mutação real; sucesso já vai p/ ThankYou, postConsent
  best-effort segue silencioso). Mantido o `fk-alert` inline (contextual). `tsc` limpo. Deploy: rsync `--checksum`
  (4 arquivos) + build `checkout-ui` + force-recreate `falk-checkout-ui`. LIVE `app.falklog.com.br/checkout/` 200,
  bundle `index-T1W8W0TG.js` (`fk-toasts`/`Notificações` no bundle). Toast agora cobre os 3 UIs.
- **2026-06-24 (checkout FALK — NO AR ✅)**: links de checkout apontavam pro **WordPress legado senderzz**
  (`app.senderzz.com.br/checkouts/{checkout,codsfpc,lp}/?sz=...` = checkout antigo FunnelKit). Repontados
  pro **checkout FALK** (`app.falklog.com.br/checkout/?sz=<token>`, checkout-ui React; resolve correio/motoboy
  pelo `tipo` da oferta via `/checkout-api/offer`, sem `/codsfpc/`+`szm`). Fix no gerador `go/portal
  links_portal.go` (`checkoutPublicBase` default→falklog, `checkoutURL`/`codCheckoutURL`→`/checkout/?sz=`).
  Admin (`checkout_links.go:51` + `affiliates.go`) **já estava** falklog na VPS — sem mudança.
  **PRODUÇÃO (app.falklog.com.br)**: migração `466-checkout-url-falk.sql` aplicada no DB `falk`
  (UPDATE 42, 0 URLs antigas restantes); `falk-portal` rebuild+force-recreate (novas criações já FALK).
  Verificado LIVE: `/checkout-api/offer` resolve oferta + `/checkout/?sz=` serve a SPA FALK (motoboy 2-etapas
  pelo token). **DEV (tunnel)**: mesmo fix + checkout-ui(:5175)+orders(:8086) wired no Vite do painel
  (`/checkout`+`/checkout-api`) p/ click-through local; DB dev backfillado.
  ⚠️ **Achado p/ dono**: ledger `schema_migrations` de PROD está em **451** — 460..465 NÃO registradas
  (inclui `460-backfill-affiliate-fee-split` financeira). 466 foi aplicada ISOLADA de propósito (não rodei o
  migrate-runner p/ não disparar 460..465 sem revisão). Verificar lag de migração da VPS.

- **2026-06-24**: portal-ui Products — coluna **Comissão %** dos checkouts deixa de ser sempre editável.
  Padrão = valor + **lápis discreto** (ghost icon, `var(--szv2-text-muted)`); clicar abre input+Salvar+cancelar(X).
  Estado `commEditing` por checkout id; `startEditComm` semeia rascunho com valor atual, `cancelEditComm`
  descarta, save bem-sucedido recolhe. `tsc --noEmit` limpo. (Pediu o dono via screenshot — toca portal de role
  por instrução direta.) **✅ NO AR**: rsync `--checksum` + `docker compose build portal-ui` + `up -d --force-recreate
  portal-ui`; `app.falklog.com.br/portal/` HTTP 200 servindo `index-cQdO5l3i.js` (strings `Editar comissão` /
  `Cancelar edição da comissão` verificadas no bundle live).
- **2026-06-23 (cont. 2)**: **descrição da vitrine renderiza HTML rico sanitizado** (produto DATALAPROX,
  card ilustrado). Antes `{descricao}` escapava → tags cruas na tela. Agora portal-ui sanitiza via
  **DOMPurify** (`portal-ui/src/utils/sanitize.ts` — único portão): allowlist estrita de tags/attrs +
  **allowlist de propriedades CSS** (sem position/z-index/transform/viewport-unit/margem-negativa →
  anti-clickjacking) + bloqueio de url()/expression()/escape-CSS (`\75 rl()`) + rel=noopener. Render em
  `Vitrine.tsx:694` e `Products.tsx:996` com branch HTML-vs-texto e **wrapper de confinamento**
  (overflow:hidden+contain+isolation → conteúdo do produtor não pinta/clica fora da caixa). Texto puro
  ganha `white-space:pre-wrap` (quebras voltam). **ProductApproval (admin) fica escapado** de propósito
  (blast radius = só afiliado/produtor). Save grava cru (coluna TEXT, sem truncar). **37 testes XSS
  adversariais green** (`npm run test` no portal-ui; bateria com mXSS, overlay, escapes CSS). Backend e
  admin-ui intocados. **✅ NO AR**: rsync + `docker compose build portal-ui` + `--force-recreate falk-portal-ui`;
  `app.falklog.com.br/portal/` HTTP 200 servindo `index-BdLDvdyG.js` (sanitize verificado no bundle). Criado
  `portal-ui/.dockerignore` (build determinístico) + vitest fixado em 2.x (alinha vite 5; evita conflito esbuild no `npm ci`).
  **Restruturação do modal da vitrine** (design do dono): nome do produto sai do topo → **submenus sobem pro header do Drawer**
  (tabs como `title`); aba "Produto" → "**Informações**" e nela renderiza **só o card HTML full-bleed** (foto/comissão/stats removidos
  dessa aba — vivem em Ofertas/Localidades). `ModalProduto` ficou sem uso (mantido, `noUnusedLocals:false`).
  **Polish do card** (3 fixes): (a) **faixa branca** = footer sticky vazio (sem Afiliar-me p/ dono, fundo claro) → só renderiza
  quando há ação de afiliação; (b) card **preenche o drawer** (`flex:1 0 auto` + fundo `#0d0d0d` full-bleed → sem branco embaixo;
  cresce e rola se passar, sem clipar); (c) descrição DATALAPROX trocada por **versão compacta** no DB (chips em linha, ~620px)
  p/ caber sem rolagem. Deploy exigiu `rsync --checksum` (versão velha de `Products.tsx` no VPS c/ bug `linkForm.name` quebrava
  o `tsc` do Docker; rsync por mtime pulava o arquivo). Live: `index-CUT7ObXD.js`.
- **2026-06-23 (cont.)**: portal-ui SERVIDO no gateway (`/portal/` SPA + container falk-portal-ui); excluído pedido 1509;
  frustrado drawer (header nº+status, taxa frustração por parte) + **reagendar=clone** (copia tudo, nova data, original intacto)
  respeitando **regra da zona** (dias_funcionamento + cutoff); variação só no drawer; **FalkDatePicker** (calendário temático)
  aplicado em 33 telas (64 date inputs nativos migrados); filtros texto→**FalkSelect** (Produtor/Afiliado/Produto, 6 telas) — fim dos filtros digitados.
- **2026-06-23**: signup sem aprovação → role `cliente` (ativo + auto-login + promoção); gates go/portal cliente=afiliado-like
  (matrix de segurança 403/200 passou); admin-ui sem "aguardando aprovação".
- **2026-06-22**: consolidação de todos os MDs neste único STATUS.md (deletados 44 mds de tracking; CLAUDE.md tb absorvido aqui);
  merges de tela; selects→FalkSelect; fix TZ data; backfill afiliado; fundação de indicação; máscaras cadastro;
  taxas no drawer; COD isolado; redesign tela Afiliados; contagem afiliados/produto; variação=produto.
