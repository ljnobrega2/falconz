# Senderzz Logistics — Tier 1 Security Patch

**Versão:** 2.5.38-security-tier1
**Base:** 2.5.37-webhook-me-completo
**Data:** 2026-04-25

Correção das 5 vulnerabilidades críticas identificadas na auditoria de segurança.
Detalhamento completo das vulnerabilidades em `auditoria-seguranca-senderzz.md`.

## Correções aplicadas

### CRIT-01 — Fraude na emissão de etiqueta
**Arquivo:** `includes/tpc/painel.php` → `tpc_ep_emitir_etiqueta()`

**Antes:** o cliente enviava o campo `preco` no POST e o backend confiava nesse valor para verificar saldo e debitar a carteira. Cliente podia emitir etiqueta de R$ 50 pagando R$ 0,01.

**Agora:**
1. Backend chama `/me/shipment/calculate` com os mesmos dados (CEP, dimensões, peso, service_id) que serão usados na emissão.
2. Extrai o preço da resposta do ME, aplica markup configurado pelo operador (se houver), e usa **esse** valor para verificar saldo.
3. Reserva o saldo via `tpc_reservar()` antes de chamar o ME — se o ME falhar, a reserva é liberada automaticamente.
4. Após `/me/shipment/checkout` retornar HTTP 2xx, confirma o débito via `tpc_debitar_reserva()`.
5. Sanity check: se `cart.price` divergir mais de 5% do preço cotado, aborta e libera a reserva (cliente refaz a cotação).
6. Validações novas: dimensões > 0 e ≤ 200 cm, peso > 0 e ≤ 100 kg, CEP com 8 dígitos.

**Compatibilidade frontend:** o campo `preco` no body da requisição agora é **ignorado**. O frontend continua funcionando — apenas o valor cobrado pode divergir do que ele exibiu se a cotação local estiver desatualizada. Recomendado atualizar o frontend para refazer cotação imediatamente antes de chamar `/emitir-etiqueta`. Resposta agora inclui `preco` e `preco_fmt` com o valor real cobrado.

---

### CRIT-02 — Cliente alterando status do próprio pedido
**Arquivo:** `includes/senderzz-rest.php` → `senderzz_rest_update_order_status()`
**Endpoint:** `POST /wp-json/senderzz/v1/orders/{order_id}/status`

**Antes:** se o pedido fosse do cliente, qualquer status era aceito (`completed`, `refunded`, `cancelled`, etc.).

**Agora:** lista branca rígida de transições para clientes:

| De | Para permitidos |
|---|---|
| `pending` | `emcancelamento` |
| `on-hold` | `emcancelamento` |
| `pendente` | `emcancelamento` |
| `aguardando` | `emcancelamento` |
| `emcancelamento` | (nenhuma) |

> `emcancelamento` é o status interno Senderzz de "cancelamento solicitado" — o cliente só pode **solicitar** cancelamento de pedidos ainda não despachados; não pode cancelar diretamente (`cancelled`). Ver `includes/senderzz-rest.php:229-254`.

Qualquer outra transição retorna 403 com a lista de transições permitidas. Admin (`manage_woocommerce`) continua sem restrição.

---

### CRIT-03 — Webhooks fail-open com secret vazio
**Arquivos:**
- `includes/tpc/webhook.php` → `tpc_validar_assinatura_webhook()`
- `includes/senderzz-me-webhook.php` → `senderzz_me_verify_signature()`
- `src/Webhook/Tracking_Webhook.php` → `handle_inbound()`

**Antes:** `if (!$secret) return true;` — todos os 3 webhooks aceitavam qualquer requisição sem assinatura quando o secret não estava configurado. Em deploy novo (default), isso significava aceitar PIX falso, debitar saldo arbitrariamente e mudar status de pedidos sem autenticação.

**Agora:**
- Os 3 webhooks rejeitam requisições com 401/503 se o secret estiver vazio.
- Adicionado `tpc_webhook_ensure_secret()` em `admin_init` que gera automaticamente um secret de 48 caracteres na primeira visita ao admin após ativação.
- Notice no admin (`admin_notices`) exibe o secret gerado para o operador copiar e colar no painel do Melhor Envio / provedor PIX. Notice é dispensável.

**Operação:**
- Após upgrade, ative o plugin e abra qualquer página do admin → o notice mostrará o secret gerado.
- Configure este secret no Melhor Envio em **Painel ME → Webhooks** e em qualquer provedor PIX que dispare em `/wp-json/tp-carteira/v1/webhook/pix`.
- Webhooks só passarão a funcionar **depois** dessa configuração — comportamento intencional.

---

### CRIT-04 — Bypass HMAC do Melhor Envio via "ping"
**Arquivo:** `includes/senderzz-me-webhook.php` → `senderzz_me_verify_signature()`

**Antes:** `if ( strpos( $payload['event'], 'ping' ) !== false ) return true;` — atacante mandava `event: "pingorder.cancelled"` e contornava o HMAC.

**Agora:** comparação exata contra whitelist `[ 'ping', 'test', 'webhook.ping' ]`.

---

### CRIT-05 — JWT secret vazio no primeiro acesso
**Arquivo:** `includes/tpc/rest-api.php` → `tpc_jwt_decode()`, `tpc_jwt_get_secret()`

**Antes:** `tpc_jwt_decode` chamava `get_option('tpc_jwt_secret', '')`. Em instalação nova antes de qualquer login, o secret era `''` e atacante podia forjar JWT assinado com chave vazia.

**Agora:**
- Nova função `tpc_jwt_get_secret()` garante que o secret tem ≥ 32 caracteres antes de retornar; gera com `wp_generate_password(64)` se não existir.
- `tpc_jwt_decode()` rejeita o request com 503 se o secret ainda assim for inválido (hard fail).
- Adicionada validação do header JWT — apenas `alg=HS256` e `typ=JWT` são aceitos. Bloqueia tokens com `alg=none` ou outras tentativas de algorithm confusion.
- Hook em `admin_init` (`tpc_jwt_ensure_on_activation`) gera o secret antes de qualquer endpoint REST ser chamado pela primeira vez.

---

## Arquivos modificados

```
includes/senderzz-me-webhook.php       (CRIT-03, CRIT-04)
includes/senderzz-rest.php             (CRIT-02)
includes/tpc/painel.php                (CRIT-01)
includes/tpc/rest-api.php              (CRIT-05)
includes/tpc/webhook.php               (CRIT-03 + auto-gen secret)
src/Webhook/Tracking_Webhook.php       (CRIT-03)
senderzz-logistics.php                 (bump de versão)
```

Nenhum arquivo foi removido. Nenhuma assinatura pública de função mudou. Banco de dados não precisa de migração.

---

## Plano de testes pós-deploy

Antes de subir pra produção, rodar:

### CRIT-01
- [ ] Tentar emitir etiqueta com `preco=0.01` — deve ignorar o campo e cobrar o valor real.
- [ ] Tentar emitir com `service_id=999` (inválido) — deve retornar 422.
- [ ] Tentar com `peso=0` ou `peso=999` — deve retornar 400.
- [ ] Saldo insuficiente — deve retornar 402 com `preco_real` no corpo.
- [ ] Etiqueta normal com saldo OK — deve cobrar valor cotado e debitar carteira.
- [ ] Forçar erro no `/me/cart` (token inválido temporário) — saldo deve voltar via reserva liberada.

### CRIT-02
- [ ] Cliente loga, pega pedido em `processing`, tenta `status=completed` — deve retornar 403.
- [ ] Cliente em pedido `pending` tenta `status=emcancelamento` — deve aceitar (solicitação de cancelamento).
- [ ] Cliente em pedido `pending` tenta `status=cancelled` — deve retornar 403 (só admin cancela direto).
- [ ] Cliente em pedido `pending` tenta `status=refunded` — deve retornar 403.
- [ ] Admin tenta qualquer status — deve aceitar (regressão).

### CRIT-03
- [ ] Em instalação limpa, abrir admin — deve aparecer notice com secret gerado.
- [ ] POST em `/webhook/pix` com header errado — deve retornar 401.
- [ ] POST em `/webhook/me` sem assinatura e sem secret configurado no settings ME — deve retornar 200 com `note: unauthorized` (já era assim, agora respeita o fail-closed).
- [ ] POST em `/tracking/inbound` com secret zerado em `wp_options` — deve retornar 503.

### CRIT-04
- [ ] POST em `/webhook/me` com `event: "pingorder.cancelled"` e sem assinatura — deve retornar `unauthorized` (fail).
- [ ] POST em `/webhook/me` com `event: "ping"` e sem assinatura — deve passar (test ping legítimo).

### CRIT-05
- [ ] Em instalação limpa, antes de qualquer login, GET em `/wp-json/tp-carteira/v1/me` com JWT forjado contra secret vazio — deve retornar 401/503.
- [ ] JWT com `alg: "none"` no header — deve retornar 401.
- [ ] JWT válido (gerado pelo próprio sistema) — deve retornar 200.

---

## Observações para o time de desenvolvimento

1. **Padrão fail-closed** deve ser regra em todo `verify_signature` — adicionar lint rule no CI para bloquear `if (!$secret) return true`.
2. **Backend nunca confia em valor monetário enviado pelo cliente** — sempre recalcular ou validar contra fonte externa.
3. **Secrets devem ser gerados na ativação do plugin** via `register_activation_hook`. Padrão aplicado aqui em `admin_init` por ser mais robusto contra ativação silenciosa via WP-CLI.
4. **Os Tier 2 e Tier 3 da auditoria continuam abertos** — recomendado endereçar antes de escalar para 50+ lojas.

## 2.5.39-security-tier2
- Adicionado endpoint seguro `POST /wp-json/tp-carteira/v1/auth/token-from-portal-session` para converter sessão válida do painel logístico em JWT REST.
- Ajustado acesso REST de pedidos para o modelo real Senderzz: pedido visitante vinculado por classe de entrega/carteira, não apenas `customer_id` WooCommerce.
- Mantido bloqueio de alteração indevida de status por cliente logístico (`completed`, `refunded`, etc.).
- Varredura estática básica executada nos pontos alterados e `php -l` sem erros nos arquivos modificados.

## v2.5.38-wallet-charged-freight
- Regra da carteira ajustada: valida e debita o FRETE COBRADO do cliente (`shipping_charged` / `order->get_shipping_total()`), não o custo real da etiqueta.
- Correção de divergência entre saldo exibido no painel e backend: a carteira do produtor ativo da classe de entrega no Portal passa a ter prioridade sobre mapa antigo de configurações.
- Mensagens de saldo insuficiente agora mostram “Necessário frete”.
- Corrigido schema SQL de `tpc_webhook_events` com defaults vazios válidos.

## 2.5.58-v24-operational-ux-security

- UX: adiciona Central de Ação operacional na tela de Pedidos com prioridades, busca rápida e cards clicáveis.
- UX: melhora estados vazios, tooltips e clareza da rotina diária do produtor.
- Segurança: novas sessões do portal passam a ser salvas como hash HMAC no banco, mantendo compatibilidade com sessões antigas.
- Segurança: endpoint AJAX de estoque passa a exigir nonce do portal para operações autenticadas por cookie.
- Segurança: consultas de sessão passam a aceitar token antigo ou hash para migração suave.
- E-mails: corrige HTML dos e-mails de 2FA e recuperação de senha, com layout Senderzz mais profissional.
