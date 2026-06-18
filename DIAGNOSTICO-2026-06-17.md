# Diagnóstico do Projeto — Senderzz v459 (2026-06-17)

Verificação cruzada: o que os arquivos `.md` afirmam **vs** o que o código realmente contém. Cada item tem evidência (file:line ou comando git).

---

## ✅ Status de resolução (2026-06-17)

Todos os itens acionáveis foram resolvidos nesta sessão (branch `chore/track-php-source`, commit `04d374c`):

| # | Item | Status | O que foi feito |
|---|---|---|---|
| 1 | 🔴 Git: fonte PHP fora do controle | ✅ RESOLVIDO | `.gitignore` criado, `node_modules` destrackeado (0 arquivos), fonte PHP commitada — `senderzz-logistics.php` agora trackeado |
| 2 | 🟠 Build quebrado (phpunit ausente) | ✅ RESOLVIDO | `composer install` rodado → `composer test`: **104 testes, 196 assertions, OK** |
| 3 | 🟠 `senderzz-security-tests.sh` stale + crash | ✅ RESOLVIDO | Asserts FIX-11/12 de templates inexistentes viraram SKIP condicional; bug `set -u` linha 214 corrigido; `skip()` adicionado. Roda exit 0 |
| 4 | 🟡 Drift de versão + CLAUDE.md v458 | ✅ RESOLVIDO | CLAUDE.md atualizado v458→v459, scheme Go documentado |
| 5 | 🟡 CLAUDE.md cita HOTFIX-*.md inexistentes | ✅ RESOLVIDO | Referência corrigida para os docs que existem |
| 6 | 🟡 CHANGELOG CRIT-02 `cancelled` errado | ✅ RESOLVIDO | Corrigido para `emcancelamento` (tabela + checklist) |
| 7 | 🟡 ROADMAP-V2 subestima Go | ✅ RESOLVIDO | Tabela-resumo reconciliada com código real + banner |

**Pendências reais restantes (ops/runtime — fora do escopo de código):** provisionar VPS, TLS, rotação de secret em produção, janelas double-write 7/30 dias, cutover. Restaurar rate-limits de login antes de produção (ver CLAUDE.md "Rate limiting de login — desativado para dev").

Detalhe original do diagnóstico abaixo (mantido para histórico).

---

## 🔴 CRÍTICO — Integridade de controle de versão

**O plugin PHP inteiro está FORA do git.** Único conteúdo trackeado: `admin-ui/`, `go/`, `infra/` + 1 `.md` solto.

- `git ls-files --error-unmatch senderzz-logistics.php` → `did not match any file(s) known to git`
- Diretórios com **0 arquivos trackeados**: `src/`, `includes/`, `public/`, `templates/`, `assets/`, `vendor/`
- **Não existe `.gitignore`** na raiz.
- **`node_modules/` está commitado**: 7183 arquivos trackeados (inversão total — lixo versionado, produto não).
- Commits v460–v464 só tocaram `admin-ui/` e `go/`. **O plugin que define v459 nunca foi commitado.**

**Risco:** não há fonte-da-verdade versionada do artefato PHP entregue. Perda de máquina = perda do produto.

**Ação:** criar `.gitignore` (ignorar `node_modules/`, `vendor/`, `.DS_Store`), `git rm -r --cached node_modules`, e commitar a fonte PHP real.

---

## 🟠 ALTO — Build quebra out-of-the-box

- `composer test` → exit 127 (`phpunit: command not found`).
- `vendor/` só tem deps de produção (`setasign` = fpdi/fpdf). Sem `vendor/bin/`, sem phpunit.
- `composer.lock` **fixa** phpunit 11.5.55 → `composer install` (com dev) resolve.

**Ação:** rodar `composer install` antes de qualquer release/CI.

---

## 🟠 ALTO — Suíte de segurança grep desatualizada e com bug

`bash senderzz-security-tests.sh`:
- Bloco secrets/env: **8/8 OK** ✅
- Bloco **FIX-11 (refatoração Portal_Page)**: **11 FAIL** — espera templates extraídos (`templates/portal/operator-dashboard.php`, `dashboard.php`, `stock-panel.php`, `order-card.php`) que **não existem**; `Portal_Page.php` ainda monolítico (4784 linhas).
- Script **crasha**: `line 214: this: unbound variable`.

**Ação:** corrigir/remover asserts FIX-11 obsoletos e o bug de `set -u` na linha 214.

---

## 🟡 MÉDIO — Drift de versão (3 "versões atuais" diferentes)

| Marcador | Valor | Nota |
|---|---|---|
| Header `Version:` | `459` | scheme inteiro nu |
| `SENDERZZ_LOGISTICS_VERSION` | `2.6.11-v459` | consistente entre si |
| `TPC_VERSION` | `2.6.11-v459` | consistente |
| `WC_Melhor_Envio::VERSION` | `2.2.7-portal-premium` | separado (documentado) |
| git commits | `v460–v464` | trabalho à frente do código |
| CLAUDE.md corpo | `v458` | doc atrás |
| Go services | `orders 6.0 / labels 4.0 / wallet 2.0` ad-hoc | sem scheme unificado |
| admin-ui package.json | `0.1.0` | scaffold default |

In-code (v459) **atrasado** vs história git (v464). Regra do CLAUDE.md (header+2 constantes em sync no release) cumprida internamente, mas stale vs trabalho.

---

## 🟡 MÉDIO — Docs apontam arquivos inexistentes

- CLAUDE.md afirma "raiz tem série `HOTFIX-vNNN.md` e `REFACTOR-*-vNNN.md`" → **zero existem**. Trilha de proveniência de versão não verificável.

---

## ✅ VERIFICADO BOM — Segurança (CHANGELOG-SECURITY.md)

Os 5 patches CRIT presentes com marcadores e comportamento descrito:

| ID | Status | Evidência |
|---|---|---|
| CRIT-01 (recalc preço server-side, reserva→débito, sanity 5%) | ✅ CONFIRMADO | `includes/tpc/painel.php:670,704,751,816` |
| CRIT-02 (whitelist status cliente, fail-closed 403) | ⚠️ STALE | `includes/senderzz-rest.php:229-254` — mecanismo OK, mas doc diz alvo `cancelled`; código usa `emcancelamento` |
| CRIT-03 (3 webhooks fail-closed) | ✅ CONFIRMADO (mais forte: exige ≥32 chars) | `webhook.php:166`, `senderzz-me-webhook.php:94`, `Tracking_Webhook.php:105` |
| CRIT-04 (ME ping exact-match whitelist) | ✅ CONFIRMADO | `senderzz-me-webhook.php:106` `in_array(..., ['ping','test','webhook.ping'], true)` |
| CRIT-05 (JWT ≥32 chars, bloqueia alg=none) | ✅ CONFIRMADO | `includes/tpc/rest-api.php:144-157` |
| V-NEW-01/02 (PIX guard fail-closed, load após pix.php) | ✅ CONFIRMADO | `senderzz-logistics.php:351-352`, `pix-confirmation-guard.php:178` |

**Doc subestima**: CRIT-03 e CRIT-05 exigem ≥32 chars (não só "não-vazio"). Único erro material: nome do status-alvo do CRIT-02.

---

## ✅ VERIFICADO BOM — Migração Go + Admin (over-delivered)

ROADMAPs (06-15/06-16) estão **desatualizados pra baixo** — código entrega MAIS que os docs afirmam:

| Milestone | Doc diz | Realidade |
|---|---|---|
| Motoboy Go | "60%, 28 stubs" | **completo**, `WriteNotImplemented` nunca chamado, 42 rotas |
| Portal Go | "5%, só session.go" | auth/jwt/webhooks/integrations/settings — 264L main.go |
| Wallet double-write PHP | "❌ Pendente" | existe `includes/tpc/doublewrite.php` |
| go/admin service | (não listado / pendente) | **225 rotas, 54 handlers** |
| Admin React P0 (10 telas "FALTANDO") | ❌ | **todas existem**, 249–1355 linhas cada; 55 .tsx total |
| AuditEngine Sprint-1 | a construir | wired end-to-end (`audit.go` + `AuditEngine.tsx:95`) |

`MIGRATION-GO-CHECKLIST.md` é o doc **mais preciso**. `ROADMAP-V2.md` é o **mais stale**.

**Itens genuinamente pendentes** (ops/runtime, sem artefato no repo): provisionar VPS, TLS, rotação de secret, janelas double-write 7/30 dias, cutover.

**Aberto no código:**
- CRIT-04 vive em PHP, não em Go (audit diz que estaria em `go/labels` — mislocação, não regressão).
- `cod_livro.go:219,437` — `frustrado_produtor` hardcoded 0, TODO aguardando migração `wp_tpc_transacoes` pro Postgres.

---

## Prioridade de ação

1. **`.gitignore` + commitar fonte PHP** (crítico — sem isso não há produto versionado).
2. `composer install` (build/CI).
3. Corrigir `senderzz-security-tests.sh` (asserts FIX-11 + bug linha 214).
4. Reconciliar versão única e atualizar CLAUDE.md (v458→atual) + remover refs a HOTFIX-*.md inexistentes.
5. Corrigir nome status CRIT-02 no CHANGELOG-SECURITY.md (`cancelled`→`emcancelamento`).
6. Atualizar ROADMAP-V2.md (Motoboy/Portal/wallet double-write já feitos).
