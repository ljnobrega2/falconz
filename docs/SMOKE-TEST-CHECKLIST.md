# Smoke Test Checklist — Senderzz V2

Executar antes de cada deploy em produção. Cobrir iOS Safari + Android Chrome para PWA.

---

## 1. Portal V2 — Auth

- [ ] Login com e-mail + senha corretos → entra no portal
- [ ] Login com senha errada → mensagem genérica (não revela se e-mail existe)
- [ ] 5 tentativas erradas → "Muitas tentativas" (rate limit)
- [ ] 2FA: código enviado por e-mail, expirado após 15min
- [ ] 3 tentativas erradas no 2FA → bloqueio
- [ ] "Esqueci a senha" → e-mail recebido, link funciona, expira em 30min
- [ ] Logout → sessão invalidada, acesso negado na rota protegida
- [ ] Esc fecha modais (portal)
- [ ] Tab navega dentro do modal sem sair (focus trap)

---

## 2. Portal V2 — Pedidos

- [ ] Lista carrega com filtros (produto, status, afiliado)
- [ ] Botões período (Hoje/7d/30d/Todos) filtram corretamente
- [ ] Filtro "Entrega de:" filtra por data de entrega motoboy
- [ ] Busca por número de pedido funciona
- [ ] Cancelar pedido em `pending` → move para `emcancelamento`
- [ ] Tentar cancelar pedido em `aprovado` → 403 (CRIT-02)

---

## 3. Portal V2 — Carteira COD

- [ ] KPIs (disponível, pendente, em análise) mostram valores corretos
- [ ] Botão "Saque" → modal de contas PIX → solicitar saque
- [ ] Botão "Antecipar" com saldo_futuro=0 → toast "Não há valores"
- [ ] Botão "Antecipar" com saldo_futuro>0 → modal com valor, taxa, líquido, select conta
- [ ] Confirmar antecipação → saque criado com tipo=antecipacao
- [ ] Histórico de transações carrega
- [ ] Filtro por período no histórico funciona

---

## 4. Portal V2 — Webhooks / Conexões

- [ ] Listar webhooks configurados
- [ ] Criar novo webhook com URL + classe + eventos selecionados
- [ ] Testar webhook → recebe callback no endpoint
- [ ] Soft-delete webhook → desaparece da lista
- [ ] Histórico de disparos carrega ao clicar aba "Histórico"
- [ ] Filtro de evento_type no histórico funciona (quando há >1 tipo)
- [ ] Botão "Payload" abre szV2Confirm com JSON (não alert nativo)

---

## 5. Portal V2 — Afiliados (produtor)

- [ ] Lista de afiliados carrega com KPIs
- [ ] Aprovar afiliado → szV2Confirm, não confirm() nativo
- [ ] Excluir afiliado → szV2Confirm com danger
- [ ] Comissão padrão salva corretamente
- [ ] Aba "Links de convite" carrega ao clicar Config
- [ ] Gerar link → URL copiável aparece na tabela
- [ ] Revogar link → szV2Confirm, link some da lista

---

## 6. Portal V2 — Vitrine

- [ ] Grid de produtos carrega
- [ ] Badge "✓ AFILIADO" aparece em laranja (não verde)
- [ ] Modal abre: tabs Produto/Ofertas/Localidades clicáveis
- [ ] "Afiliar-me" funciona para produtor diferente
- [ ] Descrição inline salva (produtor dono)
- [ ] "Disponível" badge laranja (não verde)

---

## 7. Portal V2 — Estoque (novo)

- [ ] Seção "Estoque" aparece na nav (produtor com classe de entrega)
- [ ] KPIs (disponíveis/reservados/em rota/entregues/frustrados) carregam
- [ ] Tabela de produtos com busca funciona
- [ ] Tabela de movimentações com filtro por produto
- [ ] Empty state aparece quando sem dados

---

## 8. Portal V2 — Configurações

- [ ] Toggle 2FA ativa/desativa
- [ ] Alterar e-mail → szV2Confirm, não confirm() nativo
- [ ] Alterar senha → szV2Confirm
- [ ] Conta PIX salva corretamente
- [ ] autocomplete desabilitado nos campos PIX

---

## 9. PWA Motoboy (iOS Safari + Android Chrome)

- [ ] Instalar como PWA (Add to Home Screen)
- [ ] Login via OTP WhatsApp
- [ ] Lote do dia carrega corretamente
- [ ] Filtro de status (KPI cards) funciona
- [ ] Bipar QR de etiqueta (BarcodeDetector) → pedido em_rota
- [ ] Bipar devolução QR
- [ ] Confirmar entrega: dinheiro + PIX + cartão
  - [ ] Modal mostra valor cartão com taxa (cc_fee_pct)
  - [ ] Upload de foto comprovante funciona
- [ ] Frustrar entrega com motivo
- [ ] GPS ping envia localização
- [ ] Touch targets ≥ 44px (verificar botões act-btn)
- [ ] Zoom habilitado (usuário pode dar pinch-to-zoom)
- [ ] Header não sobrepõe nav (z-index 101 > 100)
- [ ] Botão "Confirmar Entrega" laranja brand (não verde)

---

## 10. Tracking público (`/rastreio-motoboy/`)

- [ ] Acessar `?pedido=ID` sem key → status visível
- [ ] Tentar reagendar sem `?key=ORDER_KEY` → erro 403
- [ ] Reagendar com key correta → sucesso
- [ ] Reagendar com key incorreta → erro 403 (V-SEC-03)

---

## 11. Segurança rápida

- [ ] `POST /wp-json/sz-motoboy/v1/tracking/{ID}/reagendar` sem `key` → 403
- [ ] `GET /wp-json/tp-carteira/v1/saldo` sem auth → 401
- [ ] `POST /wp-json/tp-carteira/v1/auth/token-from-portal-session` com cookie expirado → 401
- [ ] Login portal 6x → bloqueio rate limit

---

## 12. Admin WP

- [ ] Admin notice NÃO aparece se cron PIX rodou <15min atrás
- [ ] Admin notice aparece se cron parado >15min (forçar: `wp transient delete sz_pix_reconcile_lock && wp option update sz_pix_reconcile_last_run '{"at":"2020-01-01 00:00:00"}'`)
- [ ] Admin notice divergência carteira: `wp option update sz_wallet_divergence_last '{"at":"hoje","items":[{"user_id":1}]}'`

---

## Comandos úteis para testar

```bash
# Forçar cron PIX
wp cron event run sz_pix_auto_reconcile_cron

# Verificar rate limits
grep "ip_max\|email_max" includes/senderzz-security-patches.php

# Rodar suite de testes
composer test
bash senderzz-security-tests.sh

# Simular cron wallet divergência
wp cron event run sz_wallet_divergence_check
```
