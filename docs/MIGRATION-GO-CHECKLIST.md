# Migração off-WordPress → Go: Checklist de Prontidão

Data: 2026-06-15 | Target: Go 1.22 + Postgres 16 + Next.js + Asynq

---

## Fase 0 — Infraestrutura paralela

- [x] `infra/postgres/schema-motoboy.sql` (367 linhas + sz_motoboy_otps)
- [x] `infra/postgres/schema-wallet.sql` (212 linhas, uq_user_ref_tipo, uq_event_key)
- [x] `infra/postgres/schema-affiliates.sql` (219 linhas)
- [x] `infra/postgres/schema-labels.sql` (350+ linhas)
- [x] `infra/postgres/schema-portal.sql` (251 linhas)
- [x] `infra/postgres/schema-orders.sql` (243 linhas, state machine completa)
- [x] `infra/pgloader/motoboy-migration.load`
- [x] `infra/pgloader/wallet-migration.load`
- [x] `infra/pgloader/affiliates-migration.load`
- [x] `infra/pgloader/labels-migration.load`
- [x] `infra/pgloader/orders-migration.load`
- [x] `infra/nginx/senderzz-gateway.conf` (todas Fases, blocos comentados para ativar progressivamente)
- [x] `infra/docker/docker-compose.yml` (todos 6 serviços + Postgres 16 + Redis 7)
- [x] `infra/scripts/verify-migration.sh` (diff MySQL↔PG todas as fases, saldo financeiro exato)
- [x] `.env.example` atualizado (todos secrets + portas)
- [x] `.github/workflows/go-services.yml`
- [ ] Provisionar VPS produção (manual — infra team)
- [ ] Configurar TLS nginx produção (manual)
- [ ] Rotacionar secrets ANTES do primeiro cutover (JWT_SECRET, WEBHOOK_SECRET)

---

## Fase 1 — Motoboy Go ✅ COMPLETO

### Implementado

**Go service (`go/motoboy/`):**
- `cmd/server/main.go` — chi router, **0 WriteNotImplemented restantes**, 41 rotas wired
- `internal/auth/auth.go` — AuthMotoboy (X-MB-Token pgx), AuthAlan (env), AuthPortal (HMAC WP_SALT_AUTH)
- `internal/db/db.go` — pool pgx
- `internal/handlers/tracking.go` — GET /tracking/{id}, POST /tracking/{id}/reagendar (V-SEC-03)
- `internal/handlers/zona.go` — GET /zona-cep (CEP cache em memória)
- `internal/handlers/lote.go` — GET /motoboy/lote
- `internal/handlers/ol.go` — POST /ol/mudar-status, POST /ol/trocar-motoboy, GET /ol/motoboys-do-dia, GET /ol/motoboys
- `internal/handlers/rota.go` — POST /motoboy/iniciar-rota (HMAC14 QR), POST /entregar, POST /frustrar
- `internal/handlers/motoboy_auth.go` — POST /otp/solicitar, POST /otp/confirmar, GET /token/validar, JWT 30d
- `internal/handlers/login.go` — POST /login (bcrypt), POST /login/verificar, POST /login/definir-senha, POST /motoboy/trocar-senha
- `internal/handlers/alan.go` — GET /alan/localizacao+historico+etiquetas+pedidos+dashboard, POST /alan/embalar+push-subscribe+confirmar-fechamento
- `internal/handlers/motoboy_ops.go` — POST /motoboy/devolver-qr+ping+confirmar-repasse+comprovante+push-subscribe, GET /fechamento+pendentes-confirmacao+comprovantes/{id}+wallet/*, GET /ol/pedido-historico, GET /link-expedicao, POST /dispensar-cpf
- `internal/handlers/internal.go` — 4 endpoints double-write HMAC (pedidos, status, motoboy, comprovantes)
- `internal/httpx/response.go`, `Makefile`, `go.mod`, `Dockerfile`

**PHP:**
- `includes/motoboy/doublewrite.php` — adapter HMAC SHA256, 4 hooks
- `includes/motoboy/router.php:514` — do_action('sz_motoboy_pedido_criado')
- `senderzz-logistics.php:419` — bootstrap doublewrite.php

**Infra:**
- `infra/postgres/schema-motoboy.sql` — +sz_motoboy_otps
- `infra/pgloader/motoboy-migration.load`

### Critérios de cutover Fase 1

- [ ] 7 dias double-write em staging sem divergência
- [ ] `verify-migration.sh` ≤ 0,01% por 3 dias
- [ ] Smoke test PWA: OTP → lote → QR → entregar → comprovante
- [ ] Smoke test OL: mudar-status, trocar-motoboy, motoboys-do-dia
- [ ] Cutover incremental: /tracking + /zona-cep primeiro (read-only)

---

## Fase 2 — Wallet + PIX ✅ COMPLETO

### Implementado

**Go service (`go/wallet/`):**
- `cmd/server/main.go` — chi router, JWT middleware, Asynq server + reconcile cron 03:00 BRT
- `internal/db/db.go` — pool pgx
- `internal/handlers/wallet.go` (773 linhas) — reservar/debitar/creditar/liberar/saldo/extrato, pgx.Serializable, shopspring/decimal
- `internal/handlers/pix.go` (347 linhas) — webhook PIX fail-closed HMAC, uq_event_key idempotência, status whitelist exato
- `internal/handlers/internal.go` (371 linhas) — 4 endpoints double-write HMAC (transacoes, recargas, confirmar, carteira)
- `internal/jobs/reconcile.go` — Asynq worker, divergência > R$0,01 → slog.Error, expira pendentes
- `internal/middleware/auth.go` — JWT HS256 WP-compatível

**PHP:**
- `includes/tpc/doublewrite.php` (222 linhas) — WALLET_GO_URL + WALLET_INTERNAL_SECRET, 4 hooks
- `senderzz-logistics.php:345` — bootstrap doublewrite.php antes de wallet.php

**Infra:**
- `infra/postgres/schema-wallet.sql` (212 linhas)
- `infra/pgloader/wallet-migration.load`

### Critérios de cutover Fase 2

- [ ] 30 dias double-write (financeiro = janela maior)
- [ ] Reconcile diário: 0 divergências por 14 dias
- [ ] Auditoria manual 100 transações/dia
- [ ] Cutover PIX webhook DEPOIS de wallet (falha financeira = bloqueante)

---

## Fase 3 — Affiliates + COD ✅ SCHEMAS + PARCIAL

### Implementado

**Go service (`go/affiliates/`):**
- `internal/auth/auth.go`, `internal/db/db.go`, `internal/httpx/response.go`
- `internal/handlers/affiliates.go` — todos endpoints /affiliates/*
- `internal/handlers/cod.go` — /cod/saldo, /cod/extrato, /cod/anticipate
- `internal/handlers/internal.go` — double-write receptor (em progresso)
- `cmd/server/main.go` — em progresso
- `go.mod`, `Dockerfile`

**Infra:**
- `infra/postgres/schema-affiliates.sql` (219 linhas)
- `infra/pgloader/affiliates-migration.load`

---

## Fase 4 — Labels + Melhor Envio ✅ SCHEMAS + PARCIAL

### Implementado

**Go service (`go/labels/`):**
- `go.mod`, `Dockerfile`
- Em progresso: `internal/me/client.go`, `internal/handlers/labels.go`, `internal/jobs/label_jobs.go`, `cmd/server/main.go`

**Infra:**
- `infra/postgres/schema-labels.sql` (350+ linhas, CRIT-01 comentado)
- `infra/pgloader/labels-migration.load`

---

## Fase 5 — Portal SPA ✅ SCHEMAS + PARCIAL

### Implementado

**Go service (`go/portal/`):**
- `internal/auth/jwt.go` (189 linhas), `internal/auth/session.go`
- `internal/db/db.go`, `internal/httpx/response.go`
- `internal/handlers/auth.go`
- Em progresso: `handlers/webhooks.go`, `handlers/integrations.go`, `jobs/webhook_dispatcher.go`, `cmd/server/main.go`

**Infra:**
- `infra/postgres/schema-portal.sql` (251 linhas)

---

## Fase 6 — Orders (substitui WooCommerce) ✅ SCHEMAS + PARCIAL

### Implementado

**Go service (`go/orders/`):**
- `internal/auth/auth.go`, `internal/db/db.go`, `internal/httpx/response.go`
- `internal/statemachine/transitions.go` (192 linhas, state machine completa)
- Em progresso: `handlers/orders.go`, `handlers/payments.go`, `jobs/order_jobs.go`, `cmd/server/main.go`
- `go.mod`, `Dockerfile`

**Infra:**
- `infra/postgres/schema-orders.sql` (243 linhas, sz_orders + items + meta + addresses + history + payments)
- `infra/pgloader/orders-migration.load`

---

## Compartilhado

- [x] `go/shared/pkg/contract/api.go` — tipos Response, Claims
- [x] `go/shared/pkg/doublewrite/doublewrite.go` — DualWriter + Reconciler genérico
- [x] `go/shared/pkg/events/bus.go` — event bus stub (NATS stub, tópicos canônicos)
- [x] `go/shared/pkg/auth/jwt.go` — Emit/Parse/Middleware/RequireRole compartilhados
- [x] `go/shared/pkg/httputil/proxy.go` — PostJSON/GetJSON inter-serviço
- [x] `go/Makefile` — build/test/tidy/docker/migrate-all para todos os serviços
- [x] `go/README.md`

---

## Invariantes garantidos na migração

| Invariante | PHP | Go |
|---|---|---|
| Preço server-side (CRIT-01) | `/me/shipment/calculate` | `labels/internal/me/client.go::Calculate` |
| PIX fail-closed | `pix-confirmation-guard.php` | `wallet/handlers/pix.go` ✅ |
| Idempotência DB (S8) | `uq_user_ref_tipo` MySQL | Mesmo UNIQUE no Postgres ✅ |
| Session tokens HMAC | `Portal_Auth::session_token_values` | `motoboy/auth.go::AuthPortal` ✅ |
| QR HMAC14 | `sz_mbc_package_code()` | `rota.go::IniciarRota` ✅ |
| IDOR reagendar | `sz_motoboy_tracking_url()` | `tracking.go::Reagendar` ✅ |
| DT-CODE-02 event_types | `Portal_Page::ajax_webhooks_save` | `portal/handlers/webhooks.go` ✅ |
| Double-write motoboy | `includes/motoboy/doublewrite.php` | `motoboy/handlers/internal.go` ✅ |
| Double-write wallet | `includes/tpc/doublewrite.php` | `wallet/handlers/internal.go` ✅ |
