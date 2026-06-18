# go/ — Senderzz Migration (PHP → Go)

Implementação Go dos serviços Senderzz. Migração strangler fig — cada serviço
roda em paralelo ao WordPress durante a transição.

## Estrutura

```
go/
├── shared/                  # Pacotes compartilhados entre serviços
│   └── pkg/
│       ├── contract/        # Tipos de resposta API (contrato PHP→Go)
│       └── doublewrite/     # Padrão double-write para migração
│
├── motoboy/                 # Fase 1 — gerado pelo oapi-codegen + handlers
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── auth/            # Middleware motoboy_token, alan_token, portal_session
│   │   ├── db/              # Pool pgx Postgres
│   │   └── handlers/        # Handlers HTTP (tracking, zona, lote, etc.)
│   └── go.mod
│
└── README.md
```

## Ordem de migração

| Fase | Serviço | Status | OpenAPI |
|------|---------|--------|---------|
| 1 | Motoboy | Em desenvolvimento | `docs/openapi-sz-motoboy-v1.yaml` |
| 2 | Wallet + PIX | Pendente | `docs/openapi-tp-carteira-v1.yaml` |
| 3 | Affiliates + COD | Pendente | — |
| 4 | Labels (ME) | Pendente | `docs/openapi-wc-melhor-envio-v1.yaml` |
| 5 | Portal SPA | Pendente | `docs/openapi-sz-portal-v2.yaml` |
| 6 | Checkout/Orders | Pendente | — |

## Desenvolvimento local

```bash
# Iniciar infraestrutura (Postgres + Redis)
cd infra/docker && docker compose up -d

# Migrar tabelas sz_motoboy_* do MySQL para Postgres
cd infra/pgloader && pgloader motoboy-migration.load

# Verificar migração
MYSQL_URL="..." PG_URL="..." infra/scripts/verify-migration.sh

# Rodar serviço motoboy
cd go/motoboy && make run
```

## Gerar server stubs a partir do OpenAPI

```bash
# Instalar oapi-codegen
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Gerar tipos + chi server stub
oapi-codegen -generate chi-server,types,strict-server \
  -package motoboy \
  docs/openapi-sz-motoboy-v1.yaml \
  > go/motoboy/internal/handlers/api.gen.go
```

## Padrão de resposta (contrato PHP→Go)

```go
// Erro
{"ok": false, "erro": "mensagem de erro"}

// Sucesso
{"ok": true, "campo": valor, ...}
```

Ver `shared/pkg/contract/api.go` para os tipos Go.

## Double-write durante migração

Durante Fase 1, escritas em `sz_motoboy_*` vão para Postgres (primary) E MySQL/WP (secondary).
Ver `shared/pkg/doublewrite/doublewrite.go`.

Após 30 dias de double-write sem divergências (verificar com `Reconciler.Reconcile()`),
remover as escritas MySQL e atualizar nginx para rotear permanentemente para Go.
