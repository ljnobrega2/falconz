# AUDITORIA FINANCEIRA FALK — 2026-06-25

Auditoria multi-agente (10 agentes, verificação adversarial) de TODA a estrutura financeira:
~16 telas de dinheiro, 9 handlers Go, contra a **regra de ouro #1587** e o DB local
(migrações aplicadas; `producer_net` golden local: 53/53 linhas não-zero batem; pedido 1587 = 63,54).

## Invariante de ouro (por pedido COD)

```
total = afiliado_bruta + taxa_entrega + taxa_transação_produtor + líquido_produtor
```
- `afiliado_bruta` = `affiliate_amount + transaction_fee` (= 150,00 — robusto pré/pós mig-460)
- `afiliado_líquida` = `affiliate_amount` (= 142,51 — já net do 4,99%)
- `taxa_transação_afiliado` = `transaction_fee` (= 7,49 = 4,99% da bruta)
- `taxa_entrega` = `delivery_fee` (= 23,98) **⚠️ ver bug S3**
- `taxa_transação_produtor` = `ROUND(total × 0,0499)` (= 12,48) **⚠️ NÃO tem coluna — só ao vivo**
- `senderzz_take` (receita FALK) = `transaction_fee + ROUND(total×0,0499)` = 19,97 + markup entrega 5,98 = **25,95**
- `líquido_produtor` = `GREATEST(total − bruta − delivery_fee − ROUND(total×0,0499), 0)` = **63,54**

---

## PARTE 1 — TABELA DE DIVERGÊNCIAS (por conceito, ranqueada por severidade financeira)

### S1 — LÍQUIDO PRODUTOR ❌ (P0 — dinheiro errado AGORA)
`producer_net` é uma **coluna inerte, não mantida**. SEM writer em Go (checkout.go:825-849 omite producer_net, delivery_fee E senderzz_fee → DEFAULT 0; 157/210 linhas locais = 0). SEM backfill correto (mig 460 só faz split da taxa afiliado). Valor = snapshot congelado da importação. **Import de prod esqueceu de descontar a comissão do afiliado** → pedido 1587 = **213,54** (= 250−23,98−12,48) em vez de **63,54** (overstated pela bruta 150). Novos pedidos via checkout Go = **0**.
- Leem a coluna podre: `cod_livro.go:114/117/257/483`, `order_detail.go:667`, `dashboard.go:316/340`, `audit.go` split/wallet.
- **Propagam** pro ledger COD: `orders.go:522`, `audit.go:333/451` (`net = COALESCE(producer_net,0)` → `sz_cod_wallet_transactions`).
- **Lag de migração (parou em 451) é red herring** p/ producer_net — nenhuma migração atrasada escreve essa coluna.
- ⚠️ **Tela motoboy NÃO é referência golden** — `motoboy_portal.go:578-589` lê o mesmo `producer_net` como caminho primário; o único fallback ao vivo (`bruto − valor_taxa − afiliado_NET`) **overstates o golden em 19,97** (subtrai afiliado NET 142,51 em vez de BRUTA 150 → +7,49; e omite o take produtor 12,48). **Não existe impl golden ao vivo pra copiar.**

### S2 — COMISSÃO AFILIADO "DISPONÍVEL" ❌ (P0 — gate de saque lê isso)
Três números pra mesma população: List `/affiliates` (ledger vivo, floored) = **1.271,36**; Carteira `/affiliates-wallet*` (**cache** stale) = **1.276,36**; fórmula WalletFix (unfloored) = **1.232,36**.
- Floored (per-afiliado) vs unfloored (passivo líquido) = **conceitos diferentes, decisão de política**, não bug.
- **Bug real**: cache (1.276,36) **não subtrai a penalty 44** (pré-fix −ABS(penalty), só atualiza no Sync/Release admin). `debt_amount` **sem writer Go** (read-only) = stale-by-construction, mesma doença do producer_net.

### S3 — DELIVERY_FEE ❌ (P0 co-igual — input do S1, não escrito em pedidos novos)
`checkout.go:825-849` **omite delivery_fee** do INSERT → **0 em todo pedido COD nativo**. Logo a fórmula viva de líquido **super-credita o produtor em ~23,98 por pedido novo**. ⚠️ **CORREÇÃO da verificação**: o delivery_fee COD **NÃO é `precoFrete`** (que é 0 pra COD/motoboy) — é o `valor_taxa` do motoboy (hardcoded '25.00' em checkout.go:1076; seed golden=23,98; linha motoboy=23,99). **Nenhuma fonte única dá o golden 23,98** → precisa **DECISÃO de design** da fonte canônica do delivery_fee COD, não um copy de precoFrete. Também **starva a captura de receita taxa_entrega** (trigger só dispara com delivery_fee>0).

### S4 — TAXAS FALK ❌ (P1)
`cod_livro` `taxas = delivery_fee + transaction_fee` — mas `transaction_fee` é o take do **AFILIADO** (7,49, que o produtor nunca paga) e **omite o take produtor** (12,48, escondido no producer_net). Por isso `bruto(250) ≠ afiliado(142,51) + taxas(31,47) + líquido`. Doc-drift: comentário cod_livro.go:84 diz `SUM(senderzz_fee)` mas código faz `delivery_fee+transaction_fee`. **Nenhuma tela mostra a decomposição golden de 5 vias.**

### S5 — SENDERZZ REVENUE ✅ (em grande parte MOOT)
⚠️ **CORREÇÃO**: o ledger `senderzz_revenue` é **golden nos DOIS ambientes**. Os triggers vivos (migrações **360**-revenue-taxa-produtor + **370**-revenue-entrega-markup, ambas <451) **já bookam** o take produtor 12,48 + markup entrega 5,98. Prod (lag 451) **tem** o trigger certo. mig-350 (que só booka o take afiliado + comentário stale "NÃO existe taxa transação produtor") foi **superseded**. P1-3 = só apagar o comentário velho. **Bug real**: projeção `pendente` (revenue.go) = `transaction_fee + delivery_fee inteiro` → mislabel; correto = `senderzz_take + (delivery_fee − repasse 18) = 25,95`, NÃO senderzz_take sozinho (sub-conta o markup).

### S6 — DEBT_AMOUNT (P1) — sem writer Go, frozen import, mesma classe do producer_net.
### S7 — VALOR VENDIDO / BRUTO (P2) — 4 definições (all-time / rolling-30d / período / net-of-shipping). Confunde, não perde dinheiro.
### S8 — COMMISSION % (P1 cosmético) — fallback `affiliate_amount/total` dá ~57% em vez de 60%; correto `(affiliate_amount+transaction_fee)/total`.
### S9 — KEYSPACE / double-count latente (P1/P2) — joins afiliado `wp_user_id=affiliate_id` **sem role filter** e **sem UNIQUE(wp_user_id)** → SUM dobra se algum wp_user_id repetir (31/31 únicos local, sem garantia). Colisão de path-param: `/affiliates/{id}` = portal id vs `/affiliates-wallet/{id}` = wp_user_id.

**Ortogonais / limpos (sem ação):** rail Motoboy (carteira/fechamento/dashboard — tabelas próprias, nunca lê producer_net; payout `sz_mbw_taxa_entrega` 18,00 ≠ delivery_fee 23,98 por design = 5,98 margem FALK). Rail Expedição/TPC (carteira pré-paga). Ledger afiliado **Comissões** (`bruta = amount + transaction_fee`) = controle positivo limpo.

---

## PARTE 2 — A CORREÇÃO DO BUG #1 (producer_net)

Fórmula viva canônica (verificada 53/53 local; funciona em prod HOJE sem limpar o lag, pois `affiliate_amount + transaction_fee` = bruta nos dois estados):
```sql
liquido_produtor = GREATEST(
   total − (affiliate_amount + transaction_fee) − delivery_fee − ROUND(total * 0.0499, 2),
   0)
```
Take produtor = `ROUND(total×0,0499)` (=12,48), **NÃO** `senderzz_fee` (=25,95 → erra).

**Três ações P0 co-iguais (cada uma sozinha é insuficiente):**
1. **Repointar todo READ** de `producer_net` → expressão viva / view `sz_order_financials`: cod_livro.go:114/117/257/483, order_detail.go:667, dashboard.go:316/340, audit.go split/wallet. Preservar o CASE de status (frustrado/recebido) de cada tela.
2. **checkout.go escrever `delivery_fee`** (+ `producer_net` como cache) no INSERT — **DEPENDE da decisão de fonte do delivery_fee COD (S3)**. Sem isso, ação 1 super-credita ~23,98 por pedido novo.
3. **Corrigir/gate os writers do auto-fix** orders.go:522, audit.go:333/451 (`net = COALESCE(producer_net,0)` → ledger COD). Shippar JUNTO com a ação 1 — senão rodar o reconcile **corrompe o ledger COD** com o valor stale. Nunca rodar o auto-fix entre repoint-reader e fix-writer.

Hardening: backfill 1x `UPDATE sz_orders SET producer_net = <expr> WHERE status NOT IN (frustrado/...)` (com backup), depois do delivery_fee populado → coluna vira cache confiável, nunca fonte.

⚠️ Floor `GREATEST(...,0)` quebra aditividade em pedidos comissão >~90% (clampa a 0). Aceitável ("produtor não fica negativo") mas invariante não é exato p/ todo pedido. Taxa 0,0499 deve ler de `senderzz_options.sz_producer_transaction_fee_pct` (MANDATÓRIO, p/ casar com o trigger de receita).

---

## PARTE 3 — UNIFICAÇÃO

### 3.1 Fonte única: VIEW `sz_order_financials`
Uma decomposição golden por pedido. Telas aplicam seu próprio filtro de status + keyspace por cima; **nenhuma tela recalcula dinheiro**.
```sql
CREATE OR REPLACE VIEW sz_order_financials AS
SELECT o.id AS order_id, o.produtor_id, o.affiliate_id, o.status, o.total,
  (COALESCE(o.affiliate_amount,0)+COALESCE(o.transaction_fee,0)) AS affiliate_bruta,   -- 150,00
  COALESCE(o.affiliate_amount,0)                                 AS affiliate_liquida,  -- 142,51
  COALESCE(o.transaction_fee,0)                                  AS affiliate_take,     -- 7,49
  COALESCE(o.delivery_fee,0)                                     AS delivery_fee,       -- 23,98 ⚠S3
  ROUND(o.total*0.0499,2)                                        AS producer_take,      -- 12,48
  (COALESCE(o.transaction_fee,0)+ROUND(o.total*0.0499,2))        AS senderzz_take,      -- 19,97
  GREATEST(o.total-(COALESCE(o.affiliate_amount,0)+COALESCE(o.transaction_fee,0))
           -COALESCE(o.delivery_fee,0)-ROUND(o.total*0.0499,2),0) AS producer_net_live  -- 63,54
FROM sz_orders o;
```
⚠️ View só é correta onde `delivery_fee` está populado → **NÃO shippar como verdade antes de checkout escrever delivery_fee + backfill**. Não encoda semântica de frustrado (cada tela aplica seu CASE). Keyspaces (produtor_id portal vs affiliate_id wp_user) passam crus — consumidores não podem cruzar.

### 3.2 Afiliado: matar o cache
`affiliate_dashboard_portal.go` já computa o saldo afiliado certo do **ledger vivo**. Torná-lo a única fonte: repointar `/affiliates` e `/affiliates-wallet*` (cache + debt_amount) pro ledger. Definir e documentar UM conceito de "disponível" (per-afiliado floored = sacável).

### 3.3 Colapso de telas (~16 → 6)
| Tela consolidada | Absorve | Fonte |
|---|---|---|
| **COD P&L (Livro)** | cod-livro summary/orders/producers/affiliates-summary | `sz_order_financials` |
| **COD Wallet (produtor)** | Carteira COD + Transações COD + Saques COD produtor | ledger `sz_cod_wallet_transactions` (+ writer Go real) |
| **Afiliados** | Lista + Comissões + Detalhe/Taxas + Carteira + Saques | ledger `senderzz_affiliate_transactions` (kill cache) |
| **Faturamento FALK** | revenue summary + list | ledger `senderzz_revenue` (fix projeção pendente) |
| **Motoboy** (inalterado) | carteira + fechamento + dashboard | `sz_motoboy_ganhos/_pedidos` |
| **Expedição/TPC** (inalterado) | wallet-expedition | `tpc_carteira` |

---

## PARTE 4 — FIXES PRIORIZADOS

**P0 — correção financeira (dinheiro errado agora):**
- **P0-1** Repointar reads de `producer_net` → expressão viva / view. *(S1)*
- **P0-2** checkout.go escrever `delivery_fee` (+ producer_net cache) — **BLOQUEADO pela decisão de fonte do delivery_fee COD (S3)**.
- **P0-3** Corrigir/gate writers auto-fix (orders.go:522, audit.go:333/451). Shippar com P0-1. *(S1 propagação)*
- **P0-4** Afiliado "disponível": parar de ler cache/debt_amount; servir ledger vivo. *(S2/S6)*

**P1 — categorização/reconciliação (engana, ainda não mal-pago):**
- **P1-1** "Taxas FALK" = `delivery_fee + producer_take` (tirar affiliate_take das telas de produtor); mostrar decomposição 5 vias; fix doc cod_livro.go:84. *(S4)*
- **P1-2** Projeção revenue `pendente` → `senderzz_take + (delivery_fee − repasse)` = 25,95, não senderzz_take sozinho. *(S5)*
- **P1-3** Apagar comentário stale mig 350-revenue-fix499.sql (trigger já é golden via 360/370). *(S5)*
- **P1-4** Dashboard `split` audit: trocar partição 3-termos pela invariante 5-vias (mata enxurrada de falso-positivo 18,00). *(S4)*
- **P1-5** commission_pct → `(affiliate_amount+transaction_fee)/total` (60% não 57%). *(S8)*
- **P1-6** `UNIQUE(wp_user_id)` / role filter nos joins afiliado. *(S9)*

**P2 — consolidação UX:**
- **P2-1** Colapsar ~16 → 6 telas (3.3).
- **P2-2** Normalizar "valor vendido/bruto" numa base rotulada. *(S7)*
- **P2-3** Writer Go real pro crédito `cod_received` (hoje só PHP/seed → COD nativo mostra R$0 disponível). Mesma classe do P0-2.
- **P2-4** Colisão de path-param id-space + fallback COALESCE wp→portal. *(S9)*

---

## DECISÕES ABERTAS (do dono)

1. **Fonte canônica do `delivery_fee` COD** (bloqueia P0-2): valor_taxa do motoboy (23,99/25,00) vs taxa de entrega configurada vs valor fixo. precoFrete (=0) está fora.
2. **Conceito de "disponível" afiliado**: per-afiliado floored (sacável) vs passivo líquido unfloored. + política de cobrança de penalty.
3. **Escopo de execução**: aplicar P0 (correção financeira) agora? Consolidação de telas (P2) é projeto maior.
