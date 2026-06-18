# ROADMAP V2 — Senderzz Logistics

Data: 2026-06-15 · Versão atual: `2.6.11-v459`
Última atualização: 2026-06-17 (status Go reconciliado com o código real — ver `DIAGNOSTICO-2026-06-17.md`)

> ⚠️ **Atenção:** este roadmap (06-15) subestimava o progresso Go. A verificação cruzada de 2026-06-17 mostrou que **todos os serviços Go estão substancialmente mais completos** do que descrito aqui (Motoboy completo, Portal funcional, `wallet/doublewrite.php` já existe). O documento mais preciso de status de migração é **`docs/MIGRATION-GO-CHECKLIST.md`**. A tabela abaixo foi corrigida; o restante do texto mantém o planejamento original.

Mapa consolidado: estado atual do projeto + caminho até **começar a migração WP-independente em produção**. Strangler Fig por subsistema.

---

## 0. Sumário executivo

**Estado atual:**

| Camada | Status |
|---|---|
| Hardening segurança (S1–S14) | ✅ Concluído |
| Portal V2 (D1–D8 + F3–F13) | ✅ Concluído |
| Documentação OpenAPI (4 namespaces) | ✅ `docs/openapi-*.yaml` |
| Infra Fase 0 (nginx + Postgres + pgloader + docker-compose) | ✅ Pronta para subir |
| Go Motoboy (Fase 1) | ✅ Completo — 42 rotas wired, `WriteNotImplemented` nunca chamado |
| Go Wallet (Fase 2) | ✅ Code-complete — ledger + PIX + reconcile + `internal.go` double-write receiver |
| Go Affiliates/COD (Fase 3) | ✅ Code-complete — `affiliates.go`, `cod.go`, `internal.go` |
| Go Labels (Fase 4) | ✅ Code-complete — `labels.go`, `me/client.go` (CRIT-01 Calculate), `label_jobs.go` |
| Go Orders (Fase 6) | ✅ Code-complete — `orders.go`, `transitions.go` (state machine), `payments.go` |
| Go Portal (Fase 5) | ✅ Substancial — auth/jwt/webhooks/integrations/settings + dispatcher |
| Go Admin (painel) | ✅ Construído — 54 handlers, ~225 rotas, 55 telas React (`admin-ui/`) |
| PHP double-write Motoboy → Go | ✅ `includes/motoboy/doublewrite.php` |
| PHP double-write Wallet → Go | ✅ `includes/tpc/doublewrite.php` (existe — antes marcado pendente) |
| **Pendente real (ops/runtime, sem artefato no repo)** | ⏳ Provisionar VPS, TLS, rotação de secret, janelas double-write 7/30 dias, cutover |

**O que falta para começar migração em produção (Fase 0 cutover):**

1. Provisionar Postgres + Redis em host de produção.
2. Rodar `pgloader` com snapshot inicial das tabelas `sz_motoboy_*`.
3. Subir Go motoboy em modo "shadow" (lê Postgres, escreve só via double-write).
4. Ativar `MOTOBOY_GO_URL` no PHP → double-write começa a rodar.
5. Validar 7 dias com `verify-migration.sh` (diff MySQL ↔ Postgres ≤ 0,01%).
6. Cutover nginx: redirecionar rotas read-only (`/tracking`, `/zona-cep`, `/lote`) para Go.

Tudo isso está possível com o código atual. **Marcos abaixo descrevem o que falta para chegar lá com confiança.**

---

## FASE 0 — Provisionar infraestrutura paralela (em andamento)

Janela: 3–5 dias. WP continua intocado em produção.

### 0.1 Concluído ✅

- `infra/postgres/schema-motoboy.sql` (350 linhas, +`sz_motoboy_otps`)
- `infra/pgloader/motoboy-migration.load` — pgloader CFG MySQL→Postgres
- `infra/nginx/senderzz-gateway.conf` — reverse proxy `/wp-json/sz-*/v1/*`
- `infra/docker/docker-compose.yml` — Postgres 16 + Redis 7 + Go services
- `infra/scripts/verify-migration.sh` — diff de rows MySQL ↔ Postgres
- `.env.example` + `.github/workflows/go-services.yml`

### 0.2 Pendente (bloqueia subir em produção) ⚠️

| # | O que | Quem |
|---|-------|------|
| 0-A | Provisionar VPS/instância com Docker + Postgres 16 + Redis 7 | Infra |
| 0-B | Configurar TLS no nginx gateway (Let's Encrypt) | Infra |
| 0-C | Definir secrets de produção em `.env.production`: `WP_SALT_AUTH`, `JWT_SECRET`, `WEBHOOK_SECRET`, `MOTOBOY_INTERNAL_SECRET`, `DATABASE_URL`, `REDIS_URL` | Sec |
| 0-D | Rotacionar `tpc_jwt_secret` + `tpc_webhook_secret` ANTES de exportar para Go (não reutilizar em prod) | Sec |
| 0-E | Backup completo MySQL + dump WP antes do primeiro pgloader | Infra |

---

## FASE 1 — Motoboy em Go (em andamento — 60%)

Primeiro alvo: DB isolada de `wp_posts`, 41 rotas REST documentadas, PWA já desacoplada do WP PHP, regra CEP→CD→zona é matemática pura.

### 1.1 Concluído ✅

**Go service (`go/motoboy/`):**
- `main.go` — chi router, todas 41 rotas mapeadas, 4 grupos de auth, CORS + slog + Recoverer
- `internal/auth/auth.go` — `AuthMotoboy` (X-MB-Token via pgx), `AuthAlan` (token estático), `AuthPortal` (HMAC sessão WP via `WP_SALT_AUTH`)
- `internal/db/db.go` — pool pgx
- `internal/handlers/tracking.go` — `GET /tracking/{order_id}`, `POST /tracking/{order_id}/reagendar` (V-SEC-03 order key)
- `internal/handlers/zona.go` — `GET /zona-cep?cep=XXX` com CEP cache em memória
- `internal/handlers/lote.go` — `GET /motoboy/lote` (pedidos do dia para o motoboy logado)
- `internal/handlers/ol.go` — `POST /ol/mudar-status`, `POST /ol/trocar-motoboy`, `GET /ol/motoboys-do-dia`, `GET /ol/motoboys`
- `internal/handlers/rota.go` — `POST /motoboy/iniciar-rota` (HMAC14 QR), `POST /motoboy/entregar`, `POST /motoboy/frustrar`
- `internal/handlers/motoboy_auth.go` — `POST /otp/solicitar`, `POST /otp/confirmar`, `GET /motoboy/token/validar` (JWT HS256 30d)
- `internal/handlers/internal.go` — receptor double-write HMAC-SHA256 (`/internal/pedidos`, `/internal/pedidos/{id}/status`, `/internal/pedidos/{id}/motoboy`, `/internal/comprovantes`)

**PHP (`includes/motoboy/doublewrite.php`):**
- Adapter `wp_remote_post` não-bloqueante, HMAC SHA256 via `MOTOBOY_INTERNAL_SECRET`
- Hooks: `sz_motoboy_pedido_criado`, `sz_motoboy_status_changed`, `sz_motoboy_motoboy_trocado`, `sz_motoboy_comprovante_salvo`
- `do_action('sz_motoboy_pedido_criado')` disparado em `router.php:514` após insert
- `do_action('sz_motoboy_status_changed', $pedido_id, $de, $novo, $pedido)` em `router.php:697`
- Bootstrap registrado em `senderzz-logistics.php:419`

### 1.2 Pendente (bloqueia cutover Fase 1) ⚠️

**28 rotas ainda `WriteNotImplemented`:**

| Grupo | Rotas pendentes | Prioridade |
|---|---|---|
| Auth legado | `/login`, `/login/verificar`, `/login/definir-senha`, `/login/autenticar`, `/motoboy/trocar-senha` | M (OTP cobre app novo) |
| Motoboy app | `/motoboy/devolver-qr`, `/motoboy/ping`, `/motoboy/fechamento`, `/motoboy/confirmar-repasse`, `/motoboy/pendentes-confirmacao`, `/motoboy/comprovante`, `/motoboy/comprovantes/{order_id}`, `/motoboy/push-subscribe` | **A** |
| Wallet motoboy | `/wallet/saldo`, `/wallet/historico`, `/wallet/bancario` (GET+POST) | **A** |
| Alan/Expedição | `/alan/localizacao`, `/alan/historico/{id}`, `/alan/etiquetas`, `/alan/push-subscribe`, `/alan/pedidos`, `/alan/embalar`, `/alan/confirmar-fechamento`, `/alan/dashboard` | **A** |
| OL extra | `/ol/pedido-historico` | B |
| Público | `/link-expedicao`, `/dispensar-cpf` | M |

**Outros pendentes:**

| # | O que | Esforço |
|---|-------|---------|
| 1-A | Asynq workers (Go): `reconcile-motoboy`, `purge-otps-expirados` | S |
| 1-B | Testes integração Go: `httptest` + `pgxtxn` em transação | M |
| 1-C | Endpoint `/internal/pedidos/{id}/status` precisa fazer `INSERT…ON CONFLICT` em `sz_motoboy_audit` (hoje insere duplicado em replays) | S |
| 1-D | PHP: `do_action('sz_motoboy_motoboy_trocado', $pedido_id, $motoboy_id, $actor_id)` ainda não disparado em nenhum lugar — adicionar em `router.php` quando OL troca motoboy | S |
| 1-E | PHP: `do_action('sz_motoboy_comprovante_salvo', $comprovante_id, $pedido_id)` adicionar onde comprovantes são insertados | S |
| 1-F | Reconcile diário: cron Go que compara `COUNT(*)` MySQL vs Postgres por status — alerta se divergência > 0,1% | M |

### 1.3 Critério de cutover Fase 1

Antes de redirecionar tráfego no nginx:

- [ ] 7 dias de double-write contínuo, ≤ 0,01% de divergência diária
- [ ] `verify-migration.sh` rodado e passando
- [ ] Smoke test PWA motoboy: login OTP → embalar → bipar QR → entregar → comprovante
- [ ] Smoke test OL portal: mudar status, trocar motoboy, ver KPIs do dia
- [ ] Rollback plan: nginx pode reverter rotas para PHP em < 60s

### 1.4 Ordem de cutover por rota (incremental)

```
Semana 1: /tracking, /zona-cep (read-only, sem efeito colateral)
Semana 2: /motoboy/lote (read-only autenticado)
Semana 3: /ol/* (mudanças de status — PHP ainda escreve via doublewrite legacy)
Semana 4: /motoboy/iniciar-rota + /entregar + /frustrar (write path completo Go-first)
Semana 5: Parar double-write PHP→Go. Go vira source of truth. PHP lê via /internal/pedidos read-only.
Semana 6: Remover hooks doublewrite.php. Deletar arquivo.
```

---

## FASE 2 — Wallet + PIX em Go

### 2.1 Concluído ✅

- `go/wallet/cmd/server/main.go` — chi router + JWT middleware
- `go/wallet/internal/db/db.go` — pool pgx
- `go/wallet/internal/handlers/wallet.go` (773 linhas):
  - `GET /carteira/saldo`, `GET /carteira/extrato`
  - `POST /carteira/reservar`, `POST /carteira/debitar-reserva`, `POST /carteira/creditar`, `POST /carteira/liberar-reserva`
  - `SELECT FOR UPDATE` em transação pgx
  - Idempotência via `UNIQUE (user_id, referencia, tipo)` no Postgres
- `go/wallet/internal/handlers/pix.go` (347 linhas):
  - `POST /pix/webhook` fail-closed HMAC SHA256
  - Idempotência via `wp_tpc_webhook_events.uq_event_key`
- `go/wallet/internal/middleware/auth.go` — JWT WP-compatível

### 2.2 Pendente ⚠️

| # | O que | Esforço |
|---|-------|---------|
| 2-A | Schema Postgres wallet: `infra/postgres/schema-wallet.sql` (`tpc_carteira`, `tpc_transacoes`, `tpc_recargas`, `tpc_webhook_events`) | M |
| 2-B | pgloader config wallet: `infra/pgloader/wallet-migration.load` | S |
| 2-C | PHP double-write Wallet: `includes/tpc/doublewrite.php` (espelhar `motoboy/doublewrite.php`) com hooks `tpc_transacao_inserida`, `tpc_recarga_pix_aprovada` | M |
| 2-D | `wallet/internal/handlers/internal.go` — receptor double-write HMAC (igual ao motoboy) | M |
| 2-E | Cron Asynq reconcile diário: saldo carteira Go vs saldo carteira PHP. Alerta divergência > R$ 0,01 | M |
| 2-F | Cron PIX reconcile: comparar `tpc_recargas.status` com ME balance via API | M |
| 2-G | Rotacionar `tpc_jwt_secret` em prod ANTES de Go ler | S |
| 2-H | Endpoint `GET /carteira/divergencias` (admin) — lista linhas com mismatch | S |

### 2.3 Critério de cutover Fase 2

- [ ] 30 dias double-write contínuo (financeiro = janela maior)
- [ ] Reconcile diário PIX vs ME: 0 divergências por 14 dias seguidos
- [ ] Auditoria manual de 100 transações aleatórias por dia (saldo_apos confere)
- [ ] Plano de incidente: como reverter saldo se Go calcular errado

---

## FASE 3 — Affiliates + COD Wallet em Go

Janela estimada: 2 meses depois da Fase 2 estável.

- [ ] Schema Postgres: `senderzz_affiliates`, `senderzz_cod_wallet`, `senderzz_cod_ledger`
- [ ] Go service `go/affiliates/` espelhando padrão Wallet (ledger + double-write)
- [ ] Endpoints: convite, vínculo, antecipação COD, comissão por venda
- [ ] Cutover por rota: read-only primeiro, escritas por último

---

## FASE 4 — Melhor Envio + Labels em Go

Janela estimada: 3 meses.

- [ ] Serviço de cotação Go: `POST /me/shipment/calculate` proxy stateless
- [ ] Labels Go: `gofpdf` + `skip2/go-qrcode` (substitui FPDI/FPDF do PHP)
- [ ] Branding Python: manter sidecar OU portar PyMuPDF → libvips/imagemagick em Go
- [ ] Hook checkout WC ainda existe — só substituir o backend que processa

---

## FASE 5 — Portal SPA (Next.js → Go)

### 5.1 Concluído ✅

- `go/portal/internal/auth/session.go` — validação HMAC + JWT idêntica ao Portal_Auth.php

### 5.2 Pendente ⚠️

- [ ] Todos os endpoints `/wp-json/senderzz/v1/*` em Go
- [ ] 2FA por e-mail em Go (SMTP relay)
- [ ] Sessions em Postgres (`senderzz_portal_sessions`)
- [ ] Frontend Next.js 14 + App Router consumindo Go (templates V2 PHP viram React)
- [ ] Design tokens `--szv2-*` viram Tailwind config

---

## FASE 6 — Checkout / Orders (substituir Woo)

O mais difícil. Só depois que tudo orbital saiu.

- [ ] State machine própria de pedidos em Postgres (`senderzz_orders`)
- [ ] Integração gateways de pagamento direto (sem WC_Order)
- [ ] Hooks de carrinho viram API: `POST /orders`, `POST /orders/{id}/pay`
- [ ] HPOS export final → import em Postgres
- [ ] Painel admin novo (Next.js) substituindo WP admin

---

## FASE 7 — Desligar WordPress

- [ ] Migrar últimos usuários WP para auth Go
- [ ] Apontar DNS de produção do nginx WP para Go-only
- [ ] Manter WP em standby 30 dias como fallback
- [ ] Apagar WP definitivamente após esses 30 dias

---

## Marcos para "começar migração em produção"

**Próximas 2 semanas — bloqueia primeiro cutover:**

1. **0-A a 0-E** (provisionar infra prod) — 3 dias
2. **1-C a 1-F** (gaps double-write motoboy) — 2 dias
3. Rodar `pgloader` snapshot inicial em staging — 1 dia
4. 7 dias double-write em staging com tráfego real espelhado — 7 dias
5. `verify-migration.sh` ≤ 0,01% por 3 dias seguidos — paralelo

**Próximas 4 semanas — primeiro cutover real:**

6. Cutover `/tracking` + `/zona-cep` em produção
7. Implementar handlers pendentes 1.2 (28 rotas) em paralelo
8. Smoke tests automatizados rodando em CI a cada deploy

**Após primeiro cutover bem-sucedido (mês 2+):**

- Iniciar Fase 2 (Wallet) — schema Postgres + double-write + 30 dias paralelos
- Continuar Motoboy cutover incremental até deletar `doublewrite.php`

---

## Invariantes que NÃO podem quebrar na migração

| Invariante | Onde implementado hoje | Equivalente Go |
|---|---|---|
| Preço sempre server-side (CRIT-01) | `wp_remote_post /me/shipment/calculate` | `go/labels/` (Fase 4) — mesmo padrão |
| Webhook PIX fail-closed | `pix-confirmation-guard.php` | `go/wallet/internal/handlers/pix.go` ✅ |
| Idempotência por UNIQUE KEY | `uq_user_ref_tipo`, `uq_event_key` | Mesmo schema, mesmo constraint ✅ |
| Session tokens HMAC (não raw) | `Portal_Auth::session_token_values` | `go/motoboy/internal/auth/auth.go` ✅ |
| QR HMAC14 com `wp_salt('auth')` | `sz_mbc_package_code()` | `go/motoboy/internal/handlers/rota.go` ✅ |
| IDOR protection (order key) | `sz_motoboy_tracking_url()` | `tracking.go::Reagendar` ✅ |
| Rate limit login/2FA | `senderzz-security-patches.php` + `Portal_Auth.php` | Pendente Fase 5 |
| `sz_motoboy_*` independente WC | router.php / database.php | Schema Postgres já tem ✅ |

---

## Pré-migração — limpeza adicional (paralela)

Itens do roadmap antigo ainda válidos, podem rodar em paralelo:

- Extrair JS/HTML de `Portal_Page.php` (5.177 linhas) — progresso ~290 linhas extraídas
- DT-CODE-01: decidir Mini Envios (reativar `continue` ou apagar)
- DT-CODE-02: ✅ implementado (event_types whitelist em `ajax_webhooks_save`)
- 2FA portal: toggle em settings V2

---

## Riscos atuais

| Risco | Mitigação |
|---|---|
| Double-write fire-and-forget (`blocking:false`) perde eventos em crash WP | Cron diário `verify-migration.sh` detecta; re-sync via `/internal/pedidos` |
| `tpc_jwt_secret` reutilizado em Go (vaza ambos se um cair) | Rotacionar antes (0-D), Go usa secret novo |
| `WP_SALT_AUTH` precisa ser idêntico nos dois lados (QR + sessão portal) | Variável de ambiente compartilhada, NUNCA hardcoded |
| `pgloader` derruba performance MySQL durante snapshot inicial | Rodar em janela off-peak; usar `--with batch size = 4096` |
| Diff de timezone (DATETIME Postgres vs Brasília no PHP) | Schema usa `TIMESTAMP WITHOUT TIME ZONE`; Go usa `time.Local` = `America/Sao_Paulo` |

---

## Versões e arquivos

- Plugin: `senderzz-logistics.php` v `2.6.11-v459`
- Go services: `go/motoboy/`, `go/wallet/`, `go/portal/`, `go/shared/`
- Infra: `infra/postgres/`, `infra/pgloader/`, `infra/nginx/`, `infra/docker/`, `infra/scripts/`
- Checklist técnico: `docs/MIGRATION-GO-CHECKLIST.md`
- Smoke tests: `docs/SMOKE-TEST-CHECKLIST.md`
- OpenAPI specs: `docs/openapi-*.yaml` (4 namespaces)
- Runbook: `RUNBOOK.md`
- Convenções: `CLAUDE.md`

---

## Resumo prático: o que fazer essa semana

1. Provisionar VPS produção: Docker + Postgres 16 + Redis 7 (item 0-A). 1 dia.
2. Configurar `.env.production` com secrets rotacionados (0-C, 0-D, 2-G). 0,5 dia.
3. Adicionar `do_action` faltantes em `router.php` (1-D, 1-E). 0,5 dia.
4. Implementar handlers Alan + Wallet motoboy (28 stubs). 3 dias.
5. Rodar `pgloader` em staging com snapshot MySQL. 1 dia.
6. Ligar `MOTOBOY_GO_URL` em staging e monitorar `verify-migration.sh`. 7 dias.

**Status:** pronto para iniciar passo 1 (infra prod). Todo o resto do código necessário para Fase 0 + Fase 1 read-only cutover já está commitado.
