# Auditoria — Integração Melhor Envio (ME) / Expedição — 2026-06-26

Ambiente: prod FALK (`falk-*`, DB `falk`). Avaliação ponta-a-ponta + bugs reportados pelo dono.

## Veredito: a expedição real NÃO funciona em produção hoje.
O **código** do ME é real (cliente HTTP, cotação, criar/gerar/cancelar etiqueta, rastreio).
O que quebra é **configuração/credencial + 3 gaps de código**.

---

## 1. Token ME inválido — BLOQUEADOR PRINCIPAL
- `falk-labels`: `ME_TOKEN` = **29 chars** (placeholder). `GET /me` → `{"message":"Unauthenticated."}`. Token real do ME é JWT longo.
- `falk-orders`: **sem ME_TOKEN nenhum**.
- Efeito:
  - Checkout cai no **fallback estimado** (`go/orders/internal/freight/calc.go`): "Senderzz Econômico R$15,90+4,50/kg / Expresso R$27,90+7,50/kg" → é o "ESTIMADO" que aparece pro cliente (NÃO é cotação real).
  - Criar etiqueta (POST /cart no ME) → 401 → `wc_me_labels` = **0 linhas** = "etiqueta foi pra lugar nenhum".
- **Ação dono:** gerar token OAuth no painel ME e me passar. Eu coloco em `falk-orders` E `falk-labels`.

## 2. Catálogo de transportadoras = STUB
- `go/portal/internal/handlers/freight.go::carriers()` retorna `[]` (vazio). Portal/admin mostram "Nenhuma transportadora".
- MAS `GET /me/shipment/companies` é **público** (testado: retornou Correios/PAC sem token). Dá pra puxar real já.
- **Fix:** método `Companies()` no cliente ME (`go/labels/.../me/client.go`) + ligar no `carriers()` do portal. Deploy: falk-portal (+labels lib).

## 3. Estoque não barra o checkout
- Produto id=5 "Egipzya" tem **0 linhas em `sz_stock`**, mas o pedido SZ-0001589 foi feito.
- Checkout não valida `sz_stock` (nem COD nem expedição). Estoque só é reservado por trigger no fluxo motoboy (downstream), nunca no checkout.
- **Fix:** guard pré-pedido em `go/orders` — sem estoque disponível (`qty_available-qty_reserved<=0` ou sem linha) → 409. Expedição (sem CD) = checar estoque global (soma CDs). Deploy: falk-orders.

## 4. Webhook ME = SEM receptor compatível
- URL atual no painel ME (`https://app.senderzz.com.br/wp-json/senderzz/v1/webhook/me`) → **404** (domínio velho + rota inexistente).
- O que existe no FALK: `POST /wp-json/wc-melhor-envio/v1/webhook/tracking` (falk-labels) — mas valida **HMAC com NOSSO secret** (`X-Webhook-Signature`) e payload `{tracking_code,status}` = **formato próprio, NÃO o do ME**. ME nativo → 401.
- Hoje o rastreio atualiza via **polling** (job `TypeSyncTracking` → ME TrackShipment), então não é bloqueador.
- **Opção:** construir receptor webhook nativo-ME (aceita payload/assinatura do ME) se quiser push em tempo real. Senão, deixar o webhook desligado no painel ME e confiar no polling.

## 5. Tela Expedição (portal produtor) pobre
- Lista fina (PEDIDO/CLIENTE/PRODUTO/TRANSPORTADORA/RASTREIO/STATUS/VALOR/COMISSÃO/DATA + Aprovar/Cancelar).
- Dono quer detalhamento estilo Cash On Delivery (endereço, itens, breakdown, status), tirando particularidades do COD.
- **Fix:** reescrever a página Expedição do **portal-ui** (não admin-ui) espelhando o detalhe rico do COD.

---

## O que JÁ funciona (uma vez com token válido)
Cliente ME, cotação real (calculate), criar/gerar/cancelar etiqueta, rastreio (polling). Markup de frete (admin), webhooks de produto (outro fluxo), config de token (admin TpcConfiguracoes).

## Ordem de execução proposta (lado-código, dado real)
1. Estoque barra checkout (falk-orders) — bug confirmado.
2. Puxar transportadoras reais do ME público (falk-portal).
3. Token ME (quando o dono passar) → orders+labels → cotação/etiqueta/rastreio reais.
4. Tela Expedição estilo COD (portal-ui).
5. (Opcional) receptor webhook nativo-ME.
