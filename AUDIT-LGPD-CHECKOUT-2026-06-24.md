# Auditoria LGPD — Checkout FALK COD (2026-06-24)

> Gerada por auditoria multi-agente (41 agentes, 6 dimensões: consentimento, transparência, minimização, segurança, direitos, tracking). Cada achado foi verificado adversarialmente contra o código antes de entrar aqui. 31 achados confirmados → consolidados em 15 distintos.

## Resumo executivo

**Score de conformidade: 6,5/10** — base sólida (consentimento bem-feito, minimização de coleta, trilha de direitos para usuários cadastrados), mas com furos de segurança/retenção em dados pessoais que precisam de correção antes de ligar integrações.

**Achados por severidade:**

| Severidade | Quantidade |
|---|---|
| Crítico | 0 |
| Alto | 4 |
| Médio | 6 |
| Baixo | 6 |
| Info (já conforme / informativo) | 7 |

**Top 3 riscos:**

1. **PII completa e sem máscara vazando em webhooks** (Alto) — quando o produtor liga o dispatch de webhooks, nome + telefone cru + e-mail + endereço completo do cliente são enviados para uma URL arbitrária controlada pelo produtor, **HTTP em texto claro é aceito** (não só HTTPS), e o mesmo payload é gravado verbatim em `senderzz_webhook_log` **sem retenção/expurgo**.
2. **Canal de direitos do titular (Art. 18) é só de entrada** (Alto) — o `POST /portal/data-request` grava a solicitação, mas **nenhum handler lê ou processa a fila**. O SLA de 15 dias nunca é observado. Pior para o comprador COD: o canal localiza o titular **só por e-mail**, e o checkout COD não coleta e-mail.
3. **CPF em texto claro retido para sempre** (Alto) — o CPF é gravado como dígitos puros em `sz_order_meta._billing_cpf`, sem criptografia, e a anonimização de 2 anos `sz_anonymize_old_order_pii()` **nunca toca `sz_order_meta`**.

---

## Achados confirmados

### 🔴 Alto

#### A1 — PII completa, sem máscara, enviada a URLs de webhook do produtor (HTTP em texto claro permitido)
- **Arquivo:** `go/cron/internal/dispatch/webhook.go:730-746` (payload) e `:578-580` (validador de URL)
- **Evidência:** o payload inclui `cliente` (nome, telefone, telefone_completo, email) e `entrega` (nome, cep, endereco, numero, complemento, bairro, cidade, estado). `validatePublicURL` aceita `http` cleartext (`if u.Scheme != "http" && u.Scheme != "https"`). Destino é URL configurada pelo produtor. HMAC dá integridade, não confidencialidade. **Mitigante:** dispatch é *fail-closed*, default OFF.
- **Artigo LGPD:** Art. 46/47/49 (segurança), Art. 39 (operador), Art. 6 III (minimização).
- **Recomendação:** Rejeitar `http://` quando o payload contiver `cliente`/`entrega` (exigir TLS). Minimizar payload. Tratar recipients como operadores (contrato + inventário) e divulgar na política.

#### A2 — Payload com PII gravado verbatim em `senderzz_webhook_log`, sem TTL/expurgo
- **Arquivo:** `go/cron/internal/dispatch/webhook.go:478-482` (dup em `go/portal/internal/jobs/webhook_dispatcher.go:217-221`); deleção manual em `go/portal/internal/handlers/webhooks.go:464-481`
- **Evidência:** `INSERT INTO senderzz_webhook_log (... payload ...)` grava `string(payload)` (mesmo mapa do A1) em toda tentativa, inclusive retries. Únicas deleções são manuais e escopadas ao produtor — inalcançáveis pelo titular. Nenhum DELETE/anonimização por TTL em `go/cron`/`infra/postgres`.
- **Artigo LGPD:** Art. 16 (eliminação), Art. 6 III, Art. 46-47.
- **Recomendação:** Cron de retenção que expurga/anonimiza `senderzz_webhook_log` por TTL curto; ou gravar payload redigido / só metadados + hash.

#### A3 — Canal de solicitação do titular (Art. 18) é write-only: nenhum handler lê/processa a fila
- **Arquivo:** `go/portal/internal/handlers/lgpd_request.go:144-148` (INSERT); nenhum SELECT/UPDATE em `go/`
- **Evidência:** única referência não-teste é o INSERT em `senderzz_data_subject_requests`. Nenhum handler de operador/DPO lê a fila ou move `status` de `'recebido'`. Schema (`380-lgpd-completo.sql:41-47`) define `status`/`handled_by`/`fulfilled_at`/`response` que nada escreve; `idx_dsr_sla` existe mas a fila nunca é lida.
- **Artigo LGPD:** Art. 18 + Art. 19 II (resposta em 15 dias).
- **Recomendação:** Endpoint admin/DPO para listar pendentes por `sla_deadline`, abrir e escrever `status`/`handled_by`/`fulfilled_at`/`response`.

#### A4 — CPF em texto claro retido indefinidamente — único identificador sensível fora da anonimização de 2 anos
- **Arquivo:** `infra/postgres/441-lgpd-retencao-cpf-endereco.sql:40-106` + `go/orders/internal/handlers/checkout.go:828`
- **Evidência:** checkout grava `{"_billing_cpf", cpfDigits}` (dígitos planos). `sz_anonymize_old_order_pii()` faz UPDATE só em `sz_order_addresses` e `sz_motoboy_pedidos` — nunca `sz_order_meta`. CPF do pedido correio sobrevive para sempre. `sz_mask_cpf()` não tem chamador.
- **Artigo LGPD:** Art. 6, Art. 15/16, Art. 46/49.
- **Recomendação:** Estender a função para NULL/DELETE de `_billing_cpf` em `sz_order_meta` > 2 anos; cifrar CPF em repouso (pgcrypto/envelope) ou só hash salgado.

---

### 🟠 Médio

#### M1 — Servidor registra consentimento pela mera presença de `consent_doc_version`; `consent_accepted=false` é ignorado
- **Arquivo:** `go/orders/internal/handlers/checkout.go:332, 391-407, 900-906`
- **Evidência:** struct parseia `ConsentAccepted` mas o valor nunca é lido; a trava é só a presença da versão. `recordConsent=true` grava `_sz_consent_privacy=1` hardcoded e INSERT em `senderzz_consents`. **Exposição limitada:** único chamador (`checkout-ui`) sempre manda `true` e a UI bloqueia submit não marcado.
- **Artigo LGPD:** Art. 8 §1/§2 (ônus de provar o consentimento é do controlador).
- **Recomendação:** Ler `req.ConsentAccepted`; se `false`, rejeitar/não gravar; persistir o boolean real.

#### M2 — Identidade do controlador só como nome fantasia "Falk Log" — sem CNPJ / razão social
- **Arquivo:** `infra/falk/lp/privacidade.html:36, 38-39`
- **Evidência:** "Falk Log · falklog.com.br · Versão 1.0". Sem CNPJ, razão social ou endereço. DPO só com e-mail `privacidade@falklog.com.br`, sem encarregado nomeado.
- **Artigo LGPD:** Art. 9 I, Art. 41.
- **Recomendação:** Adicionar razão social + CNPJ + endereço à política e ao contexto de consentimento; nomear o encarregado.

#### M3 — Transparência na coleta é só um checkbox que aponta para fora do site
- **Arquivo:** `checkout-ui/src/Checkout.tsx:1403-1414` (campos PII em `:712-875`)
- **Evidência:** único artefato in-context é o checkbox com link à política. Campos Nome/CPF/telefone/endereço sem aviso adjacente de controlador/finalidade/terceiros (exceto `infoTitle` do CPF).
- **Artigo LGPD:** Art. 9 caput + §1º, Art. 6 VI.
- **Recomendação:** Resumo ostensivo in-checkout (controlador; finalidade; terceiros = Correios/transportadora/motoboy/produtor; link).

#### M4 — Rastreio público expõe nome + endereço completos em texto claro
- **Arquivo:** `go/orders/internal/handlers/tracking.go:443, 453-461` (rota sem JWT)
- **Evidência:** `GET /checkout-api/order/{code}` mascara telefone/email/cpf mas retorna nome e endereço claros (render `Rastreio.tsx:382-386`). **Mitigante:** `{code}` é HMAC-assinado + rate-limit; exposição de registro único.
- **Artigo LGPD:** Art. 46, Art. 6 VII, Art. 9.
- **Recomendação:** Mascarar parte do endereço e/ou check leve de titularidade; excluir essas URLs de logs com query string.

#### M5 — CPF como texto claro em repouso em `sz_order_meta._billing_cpf`
- **Arquivo:** `go/orders/internal/handlers/checkout.go:827-828`
- **Evidência:** `metaPairs = append(metaPairs, [2]string{"_billing_cpf", cpfDigits})` — dígitos crus, sem cifragem, em tabela genérica chave/valor.
- **Artigo LGPD:** Art. 46, Art. 49.
- **Recomendação:** Cifrar em repouso ou hash salgado; isolar se o claro for necessário p/ etiqueta. *(Retenção = A4; este = repouso.)*

#### M6 — `sz_orders.ip_address`, `user_agent`, `customer_note` nunca anonimizados
- **Arquivo:** `infra/postgres/441-lgpd-retencao-cpf-endereco.sql:60-61, 91-92` (`sz_orders` só como JOIN)
- **Evidência:** checkout persiste IP/UA/nota livre em `sz_orders`; nas funções de retenção `sz_orders` aparece só como fonte de JOIN, nunca `UPDATE SET`.
- **Artigo LGPD:** Art. 6, Art. 16.
- **Recomendação:** Ramo UPDATE em `sz_anonymize_old_order_pii()` mascarando essas colunas > 2 anos.

---

### 🟡 Baixo

#### B1 — Egress ViaCEP (CEP + IP + UA) dispara antes do consentimento, sem disclosure
- **Arquivo:** `checkout-ui/src/validation.ts:93-110` (call sites `Checkout.tsx:307-308, 316, 335`)
- **Evidência:** `fetch(https://viacep.com.br/ws/${d}/json/)` em debounce/on-blur, antes da trava de consentimento (só no submit). Política não nomeia ViaCEP. **Baixo:** defensável como execução de contrato; não envia nome/CPF/telefone.
- **Recomendação:** Proxyar pelo backend same-origin, ou divulgar ViaCEP como sub-operador.

#### B2 — Rastreio público expõe nome + endereço; não informado na coleta
- **Arquivo:** `checkout-ui/src/Checkout.tsx:1403-1414`
- **Recomendação:** Informar na política; opcional mascarar nome. *(Faceta de transparência do M4.)*

#### B3 — IP e UA gravados em todo pedido sem finalidade de entrega (duplicata do ledger de consentimento)
- **Arquivo:** `go/orders/internal/handlers/checkout.go:743-759`
- **Evidência:** IP/UA já capturados em `senderzz_consents`/`_sz_consent_ip`; em `sz_orders` são só escritos, nunca lidos.
- **Recomendação:** Remover do INSERT de `sz_orders` (fonte única = ledger de consentimento), ou documentar finalidade antifraude + retenção curta. *(Complementa M6.)*

#### B4 — Campo livre "Informações adicionais" é canal de PII de terceiros
- **Arquivo:** `checkout-ui/src/Checkout.tsx:856-875` (`NOTE_MAX=200`)
- **Recomendação:** Dica inline para não incluir dados de terceiros; herdar retenção do pedido (ver M6).

#### B5 — CEP de destino logado em Info em toda cotação de frete
- **Arquivo:** `go/orders/internal/freight/calc.go:224, 245`; `checkout.go:468`
- **Recomendação:** Remover/rebaixar/truncar o CEP nesses logs; reter logs por janela limitada.

#### B6 — DSR localiza titular por e-mail, mas COD não armazena e-mail
- **Arquivo:** `go/portal/internal/handlers/lgpd_request.go:126-131`
- **Recomendação:** Permitir busca por telefone/CPF (ao construir o handler do A3), ou capturar e-mail opcional no COD.

---

## Plano de correção priorizado

**Quick wins (horas, baixo risco):**
- [ ] **(M2)** Razão social + CNPJ + endereço do controlador + encarregado em `privacidade.html`.
- [ ] **(B1)** Divulgar ViaCEP como sub-operador na política.
- [ ] **(B2)** Frase na política: nome + endereço de entrega visíveis via link de rastreio.
- [ ] **(B5)** Remover/rebaixar/truncar CEP nos `slog.Info` de `calc.go:224,245` e `checkout.go:468`.
- [ ] **(B4)** Dica inline no campo "Informações adicionais".
- [ ] **(M1)** Ler `req.ConsentAccepted`; rejeitar/não gravar se `false`; persistir boolean real.
- [ ] **(B3)** Remover `ip_address`/`user_agent` do INSERT de `sz_orders` (ou documentar finalidade + retenção).

**Médio esforço (transmissão e payloads):**
- [ ] **(A1)** `webhook.go`: rejeitar `http://` com PII; minimizar payload; operadores + disclosure.
- [ ] **(A2)** Cron de retenção / payload redigido em `senderzz_webhook_log`.
- [ ] **(M3)** Resumo ostensivo de tratamento in-checkout.
- [ ] **(M4)** Mascarar parte do endereço no rastreio público; excluir URLs de logs.

**Estrutural (schema + novo handler):**
- [ ] **(A4 + M5)** Anonimizar `_billing_cpf` > 2 anos; cifrar CPF em repouso.
- [ ] **(M6 + B4 + B3)** Ramo `UPDATE sz_orders SET` para IP/UA/customer_note > 2 anos.
- [ ] **(A3 + B6)** Handler de atendimento DSR (admin/DPO) com busca por telefone/CPF, não só e-mail.
- [ ] **(Limpeza)** Remover rota/`postConsent` `/consent` morta e comentário enganoso em `api.ts:28-33`.

---

## O que já está conforme

- **Consentimento bem-formado** (`Checkout.tsx:137, 544-549, 1403-1414`): checkbox não pré-marcado, obrigatório, link funcional, texto específico, não atrelado a marketing; servidor carimba IP/UA/timestamp server-side. CPF não é dado sensível do Art. 11.
- **Minimização de coleta** (`api.ts:700`, `Checkout.tsx:723-736`): e-mail removido; CPF só no fluxo correio; sem UTM/gclid/pixel/dataLayer/geolocalização/fingerprint.
- **Sem superfície clássica de web-tracking** em `checkout-ui/src`: sem cookie, localStorage, pixel, analytics ou geolocalização.
- **Egress de frete (Melhor Envio) minimizado** (`calc.go:131-226`): só CEP origem/destino, dimensões/peso, valor segurado; host SSRF-whitelisted. É o modelo que o webhook deveria seguir.
- **Trilha de direitos sólida para titulares cadastrados** (`go/portal/cmd/server/main.go`): data-request, account/delete, export, consent, consent/revoke; duas funções de anonimização via cron diário.
- **Política cobre substantivamente o Art. 9** (`privacidade.html:41-73`): categorias, finalidades + bases legais, compartilhamento, retenção, direitos do Art. 18.
- **Endpoint `/consent` dedicado é código morto inofensivo**: chamada best-effort que engole 404; consentimento real persiste pelo payload `/order`. Recomenda-se limpeza.
