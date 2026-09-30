# FALK — Plano corretivo de 2 críticos financeiros (CRIT A e CRIT B)

> **INVESTIGAÇÃO read-only.** Nenhum código foi alterado, nenhum dado mutado.
> Todas as queries rodadas em produção (`falk-postgres`, DB `falk`) em 2026-06-23.
> **Risco: dinheiro real do afiliado (CRIT A) e do produtor (CRIT B). Revisar antes de aplicar.**

---

## CRIT A — Penalidade de frustração do afiliado é somada como CRÉDITO no saldo sacável

### Diagnóstico

A penalidade de frustração (`type='penalty'`) é uma **despesa** do afiliado, mas é gravada com
`amount` **POSITIVO** e somada ao saldo disponível em 3 lugares de leitura — em vez de subtraída.

**Origem do dado (PHP, `includes/senderzz-affiliates.php:1814`):**
```php
$wpdb->insert( sz_aff_table('sz_affiliate_transactions'), [
    'affiliate_id'=>$affiliate_id, 'type'=>'penalty', 'amount'=>$penalty, // POSITIVO
    'status'=>'applied', ...
]);
```
O débito real é aplicado direto no cache (`linha 1813`: `balance=GREATEST(0,balance-%f)`, resto → `debt_amount`).
A linha do ledger é só registro — **com sinal positivo**. Compare com o saque (`linha 4888`):
`'amount'=>-$amt` (NEGATIVO). Ou seja: o modelo canônico usa amount NEGATIVO para débito;
a penalty viola isso.

**Discrepância de status (importante):** o PHP insere `status='applied'`, mas em **produção as
penalties estão com `status='approved'`** (migração as promoveu). Por isso um fix baseado em
status é frágil — **o filtro de correção deve ser por `type='penalty'`, não por status.**

### Evidência (produção)

```
SELECT type, status, COUNT(*), ROUND(SUM(amount),2), MIN, MAX FROM senderzz_affiliate_transactions GROUP BY type,status:

   type     |  status   | count | sum_amount | min  | max
 commission | approved  |     7 |    1276.36 | 112  | 199
 commission | cancelled |     3 |     510.20 | 112  | 199
 commission | pending   |    21 |    2392.96 |  70  | 199
 penalty    | approved  |     8 |      68.00 | 5.00 | 12.00   ← POSITIVO, status approved
```

Penalties por afiliado:
```
 tx.id | affiliate_id | wp_user | order_id |  status  | amount
   3,6,7,22,33,40    12 |   28    | ...     | approved | 5,5,5,12,12,12  = R$51.00
   17,39             32 |   54    | ...     | approved | 5,12            = R$17.00
```

Cache de carteira atual (`senderzz_affiliate_wallet`) — JÁ contaminado:
```
 affiliate_id | wp_user | balance | pending | debt
      32       |   54    | 1293.36 |  465.74 |  5.00
      12       |   28    |   51.00 | 1105.89 | 51.00
```

**Reconstrução do erro (o que `WalletFix` calcula hoje = o que está no cache):**
```
SELECT SUM(amount) WHERE status NOT IN ('pending','cancelled'):
 affiliate 12 → 51.00  (100% penalty; afiliado NÃO tem comissão approved, só pending)
 affiliate 32 → 1293.36 = 1276.36 commissions approved + 17.00 penalty
```

### Quantificação do erro

| Afiliado | balance atual | correto (penalty como débito) | erro (sobra creditada) |
|---|---|---|---|
| 12 (wp 28) | R$ 51,00 | **R$ 0,00** (debt 51,00) | **+R$ 51,00** |
| 32 (wp 54) | R$ 1.293,36 | **R$ 1.259,36** | **+R$ 17,00** |
| **TOTAL** | | | **+R$ 68,00** |

A penalidade R$68 também é **receita já contabilizada da plataforma** (`senderzz_revenue.taxa_frustrado = 8 linhas = R$68,00`). Creditá-la ao afiliado é perda dupla (a plataforma reteve E o afiliado pode sacar).

### Os 4 sites do bug (todos somam penalty positiva)

1. **`go/admin/internal/handlers/affiliate_wallet.go:363-375`** (`WalletFix`) —
   `balance = SUM(amount) WHERE status NOT IN ('pending','cancelled')`. Penalty `approved`+positiva entra.
2. **`go/admin/internal/handlers/affiliate_wallet.go:441-453`** (`ReleasePending`) — mesma query.
3. **`go/portal/internal/handlers/wallet.go:354-362`** (`Summary` afiliado) —
   `available = SUM(CASE WHEN status='approved' THEN amount)` SEM filtro de type → penalty entra.
4. **`go/portal/internal/handlers/affiliate_dashboard_portal.go:122-134`** (`aggregateLedger`) —
   `a_receber = SUM(CASE WHEN status='approved' THEN amount)` SEM filtro de type → penalty entra.
   (O comentário lá admite: *"O débito... não subtraído"* — era intencional mostrar à parte, mas
   isso só funcionaria se penalty NÃO entrasse no available; com `status=approved` ela entra.)

> O afiliado **vê** o saldo inflado nos sites 3/4 (portal) e o admin **sincroniza** o cache inflado
> nos sites 1/2. Não é aditivo — é a mesma R$68 aparecendo em telas diferentes.

### PLANO de correção (CRIT A)

**Decisão de modelo (recomendada):** penalty REDUZ o sacável diretamente (fiel ao PHP linhas 1811-1813:
`debit=min(balance,penalty); balance-=debit; resto→debt`). NÃO usar "excluir do crédito" — isso
**superestima** quando havia saldo (afiliado 32 ficaria 1276,36 em vez de 1259,36). Subtrair é o correto.

**Fix de código (filtrar por `type`, não por status):**

1. `affiliate_wallet.go` WalletFix + ReleasePending — trocar a soma de balance para tratar penalty como débito:
   ```sql
   balance = COALESCE((SELECT SUM(
       CASE WHEN type='penalty' THEN -ABS(amount) ELSE amount END
     ) FROM senderzz_affiliate_transactions
     WHERE affiliate_id = w.affiliate_id AND status NOT IN ('pending','cancelled')), 0)
   ```
   (mantém saque negativo já correto; penalty positiva vira `-ABS`.)

2. `wallet.go:356` Summary afiliado — `available` deve **excluir penalty do crédito** e a tela já mostra
   penalidade à parte; OU subtrair. Recomendo subtrair p/ bater com o cache:
   ```sql
   SUM(CASE WHEN status='approved' AND type='penalty' THEN -ABS(amount)
            WHEN status='approved' THEN amount ELSE 0 END)
   ```

3. `affiliate_dashboard_portal.go:124` aggregateLedger — idêntico ao item 2 no campo `AReceber`.
   (`PenalidadesTotal` já é exibido à parte — manter como display informativo.)

4. **Fonte (PHP) — parar de recontaminar:** `senderzz-affiliates.php:1814` deve inserir penalty com
   `amount` NEGATIVO (`-$penalty`) OU os 4 sites Go devem padronizar por `type='penalty'`.
   O hook segue VIVO (`linha 1825`, `woocommerce_order_status_frustrado` → `sz_aff_apply_frustration_penalty`
   priority 30) — **cada nova frustração gera nova linha positiva**. Corrigir a fonte é obrigatório,
   senão o bug volta.

**Migração de dado (corrige o cache já contaminado):**
- Após corrigir o código, rodar `WalletFix` para os afiliados 12 e 32 (recomputa balance).
- Resultado esperado: afiliado 12 → balance 0,00; afiliado 32 → balance 1.259,36.
- **`debt_amount` também está inconsistente** (afiliado 32: debt=5,00 mas penalties=17,00) — recomendar
  recomputar debt do ledger corrigido, não confiar no cache. (Decisão do dono se quer reconstruir debt.)

---

## CRIT B — Receita COD do produtor está na carteira de Expedição (tpc) E sob dono errado na carteira COD

### Diagnóstico — MAIS grave que o enunciado da auditoria

A receita COD do produtor está **espalhada e mal-atribuída em DOIS lugares**, parcialmente duplicada:

- **`tpc_carteira` / `tpc_transacoes`** (carteira de Expedição — ERRADA para COD):
  R$ 1.770,08 em 27 transações `ref 'sz_cod_produtor_%'`, `descricao 'Venda COD ... (pendente de retenção)'`,
  sob `user_id=15`.
- **`sz_cod_wallet_transactions`** (carteira COD — CORRETA): R$ 1.705,10 (25 linhas `cod_received`),
  TODAS sob **`user_id=21`**.

**Quem é 15 e quem é 21:**
- **`user 15` NÃO existe** em nenhuma tabela de usuário (`senderzz_portal_users`, `wp_senderzz_portal_users`).
  É só um valor de `sz_orders.produtor_id`.
- **`user 21` = Gabriel Campos** (`Gabrielcamposvendas@gmail.com`), produtor REAL registrado no portal.
- **Os 41 pedidos do produtor têm `produtor_id=15`** — mas user 21 é o único produtor real e a carteira COD
  já está toda sob 21. Forte indício de que **15 (produtor_id/WP) e 21 (portal) são a MESMA pessoa em
  id-spaces diferentes** (mesmo padrão MED29 visto nos afiliados: afiliado_id vs id de vínculo).

**Universo COD verdadeiro (FULL OUTER JOIN tpc × cod_wallet por order_id): 29 pedidos, todos produtor 15.**
- 25 pedidos em AMBAS as tabelas (duplicados: tpc sob 15 + cod_wallet sob 21).
- 2 pedidos só na cod_wallet (1400, 1401) — também produtor 15.
- **4 pedidos só na tpc, AINDA NÃO na cod_wallet:** 1563 (R$44,99), 1566 (R$44,99, *frustrado*),
  1582 (R$42,18), 1587 (R$63,54).

> **Nenhum código Go vivo escreve COD na tpc.** Os escritores COD em Go vão todos para
> `sz_cod_wallet_transactions` (`wallet.go`). A string `'pendente de retenção'` / `sz_cod_produtor_`
> **não existe** no PHP `includes/` deste checkout — é dado de import legado, sem produtor vivo.
> `go/admin/.../tpc_clientes.go:43` JÁ exclui `sz_cod%` da visão de clientes Expedição
> (`sqlExcluiCOD`) — o time já sabe que COD não pertence à tpc.

### Evidência (produção)

```
tpc_carteira user 15 → saldo R$ 1770.08
tpc_transacoes ref 'sz_cod%' → user 15: 27 tx credito confirmado = R$ 1770.08   (único user)

sz_cod_wallet_transactions (type=cod_received):
  user_id 21: 21 available (R$1452.89) + 4 pending (R$252.21) = 25 tx, R$ 1705.10   (ÚNICO user)
senderzz_cod_wallet  → 0 rows   (vazia)
senderzz_cod_ledger  → 0 rows   (vazia)

JOIN sz_cod_wallet_transactions × sz_orders: cod_wtx.user_id=21 ⟷ order.produtor_id=15 (25 linhas)
sz_orders WHERE produtor_id=21 → 0 pedidos.  produtor_id=15 → 41 pedidos.
```

### Decisão que o DONO precisa confirmar (BLOQUEIA a direção da migração)

**15 e 21 são a mesma pessoa?** A evidência diz SIM (21 é o único produtor real, dono de toda a operação;
COD wallet já sob 21). Se confirmado:

- **A carteira COD correta é a de user 21** (real, com login). O dado da **tpc (sob 15) é a duplicata
  no lugar errado.**
- **NÃO mover tpc→cod sob 15** (15 não tem carteira/login — dinheiro ficaria inacessível).

### PLANO de correção (CRIT B) — assumindo 15 ≡ 21 confirmado pelo dono

**Passo 1 — Adicionar os 4 pedidos faltantes à carteira COD (sob user 21), antes de zerar a tpc:**
Pedidos 1563, 1566, 1582, 1587 só existem na tpc. Inserir como `cod_received` sob `user_id=21`.
> **ATENÇÃO ao valor:** o `valor` da tpc NÃO é o `producer_net`. Ex.: pedido 1582 tpc=R$42,18 mas
> `producer_net`=R$115,68. Decidir com o dono se o valor COD correto é o `producer_net` do `sz_orders`
> (gross − delivery_fee − transaction_fee) ou o valor parcial da tpc. As 25 linhas existentes na cod_wallet
> usaram valores que BATEM com a tpc (ver JOIN), então provavelmente replicar o `valor` da tpc é consistente
> com o histórico — confirmar.
> **Pedido 1566 é `frustrado`** — não deveria gerar recebimento COD. Provavelmente EXCLUIR da migração
> (decisão do dono).

**Passo 2 — Zerar / reverter o COD na tpc (remover a duplicata):**
As 27 tx `sz_cod%` na tpc de user 15 (R$1770.08) devem sair do saldo de Expedição. Opções:
- (a) Inserir 27 estornos `tipo=debito` com `ref 'sz_cod_revert_<id>'` zerando o efeito no `saldo` de
  `tpc_carteira` (preserva auditoria, não deleta histórico). **Recomendado.**
- (b) Recalcular `tpc_carteira.saldo` do user 15 excluindo `sz_cod%` e ajustar.
> `tpc_carteira` user 15 saldo = exatamente R$1770.08 = soma dos 27 COD → após reverter, saldo = R$0,00.
> Se houver outras tx não-COD do user 15, recalcular só a parcela COD.

**Passo 3 — Idempotência (CRÍTICO p/ não duplicar dinheiro real):**
- `sz_cod_wallet_transactions` **NÃO tem unique key em order_id** (ao contrário de `senderzz_cod_ledger`
  que tem `uq_cod_ledger_ref`). Uma re-execução do Passo 1 **insere de novo**. Antes de inserir os 4,
  fazer `WHERE NOT EXISTS (SELECT 1 ... WHERE order_id = X)`.
- A reversão da tpc (Passo 2a) deve usar `ref` único + a `UNIQUE (user_id, referencia, tipo)` da
  `tpc_transacoes` protege contra dupla reversão. Usar.

**Passo 4 — Cortar a fonte (não recontaminar):**
- Nenhum caminho Go vivo escreve COD na tpc (confirmado). O risco residual é **re-rodar o import legado**.
- Adicionar guarda no importador (se existir) para nunca inserir `sz_cod%` em `tpc_transacoes`.
- `senderzz_revenue` já tem `taxa_entrega`/`taxa_transacao_produtor` para esses pedidos — **não** re-disparar
  o trigger de revenue na migração (a inserção em `sz_cod_wallet_transactions` com `type='cod_received'`
  NÃO dispara `sz_revenue_capture_codfee` — só `type='fee'` dispara — então mover é seguro p/ revenue).

### Risco

- **Dinheiro real do produtor (R$1.770).** Se 15 ≠ 21, o plano inverte (re-atribuir 25 linhas cod 21→15
  + criar carteira/login p/ 15). **Não aplicar sem o dono confirmar 15 ≡ 21.**
- A duplicação significa que HOJE o produtor pode estar vendo o COD em duas telas — não dobrar ao migrar.
- Backup do DB antes de qualquer passo.
```

### Números-resumo

| Item | Valor |
|---|---|
| CRIT A — over-credit total no sacável de afiliados | **R$ 68,00** (afiliado 12: R$51, afiliado 32: R$17) |
| CRIT B — COD na carteira ERRADA (tpc, user 15) | **R$ 1.770,08** (27 tx) |
| CRIT B — COD na carteira CORRETA mas dono 21 | R$ 1.705,10 (25 tx) |
| CRIT B — pedidos só na tpc (faltam na COD) | 4: 1563, 1566(frustrado), 1582, 1587 |
| CRIT B — universo COD real | 29 pedidos, todos produtor 15 |
