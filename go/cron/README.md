# go/cron — CRON runner do Senderzz

Daemon de **instância única** que, em intervalo fixo, chama funções Postgres
**idempotentes** (criadas e testadas na DB viva, ver
`infra/postgres/230-fixes-v472-crons.sql`) e grava observabilidade em duas tabelas:

- `senderzz_cron_status` — status atual por nome (UPSERT, espelha o contrato lido
  pelo viewer admin em `go/admin/internal/handlers/cron_status.go`).
- `senderzz_cron_runs` — histórico de cada execução (1 linha por job por tick).

Antes deste serviço existia só o **viewer** no admin — nenhum runner disparava os
jobs de fato. Este serviço fecha o ciclo.

## Os 4 jobs

Todos idempotentes (re-disparo é inofensivo), por isso os 4 rodam a **cada tick**:

| Nome (catálogo) | Função SQL | O que faz |
|---|---|---|
| `sz_cod_release_due` | `sz_cod_release_due()` | Libera COD `pending → available` (vencidos) |
| `sz_affiliate_release_due` | `sz_affiliate_release_due()` | Libera comissão de afiliado `pending → approved` (vencidos) |
| `senderzz_db_cleanup` | `sz_cleanup_expired_sessions()` | Apaga sessões de portal expiradas |
| `sz_cancel_preagendados_vencidos` | `sz_cancel_preagendados_vencidos()` | Autocancela pré-agendados vencidos |

> O nome `senderzz_db_cleanup` casa com o catálogo de 18 crons do viewer admin.
> Os outros três não têm entrada exata no catálogo — não aparecem na tela
> hardcoded, mas `senderzz_cron_runs`/`_status` registram normalmente. Isso é
> esperado e correto.

Cada função retorna um `int` (quantidade de registros processados). Em sucesso, a
mensagem gravada é `"N registros processados"`; em falha, `err.Error()`. Um job que
falha (erro SQL ou panic) **não derruba o runner nem bloqueia os outros** — é
capturado, logado e a execução segue.

## Crons de API externa — NÃO implementados

Os crons que dependem de API externa (**Melhor Envio reconcile**, **recarga PIX**)
**não** rodam aqui: em DEV não há token ME. O runner loga um aviso honesto no
startup e **não** grava linhas de status falsas `ok` para eles.

## Variáveis de ambiente

| Variável | Obrigatória | Default | Descrição |
|---|---|---|---|
| `DATABASE_URL` | sim | — | DSN Postgres (formato pgx / libpq). Nunca é logada (carrega a senha). |
| `CRON_INTERVAL_SECONDS` | não | `300` | Intervalo do ticker em segundos. Valor inválido ou `≤ 0` é ignorado (volta ao default). |
| `CRON_RUN_ONCE` | não | — | Se `1`, roda todos os jobs **uma vez**, imprime um resumo por job e sai `0` (smoke test). |

## Como rodar

### Daemon (produção)

```bash
export DATABASE_URL="postgresql://user:pass@host:5432/senderzz?sslmode=disable"
export CRON_INTERVAL_SECONDS=300   # opcional
go run ./cmd/server                # ou: go build -o cron ./cmd/server && ./cron
```

Roda todos os jobs imediatamente no startup e depois a cada intervalo. Encerra de
forma limpa em `SIGINT`/`SIGTERM` (cancela o contexto e sai).

### Smoke test (execução única)

```bash
set -a; . ../../infra/docker/.env; set +a
export DATABASE_URL="postgresql://senderzz:${POSTGRES_PASSWORD}@localhost:5432/senderzz?sslmode=disable"
CRON_RUN_ONCE=1 go run ./cmd/server
```

Imprime uma linha de resumo por job (`name + count + status + duração`) e sai `0`.
O status por linha já comunica sucesso/falha — o processo não sai com código `≠ 0`
mesmo se um job individual falhar.

## Build / teste

```bash
go mod tidy
go build ./...
```
