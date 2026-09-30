# Dockerfile do serviço CRON runner — Senderzz/FALK.
#
# Daemon de instância única (cmd/server/main.go): NÃO expõe porta HTTP — é um ticker
# que chama funções Postgres idempotentes em intervalo fixo (CRON_INTERVAL_SECONDS,
# default 300s). Por isso NÃO há EXPOSE nem HEALTHCHECK (não existe /healthz).
#
# CA certs SÃO necessários no runtime: o dispatcher de webhooks (internal/dispatch)
# faz POST HTTPS de saída aos endpoints dos produtores (gated por webhook_dispatch_enabled,
# default '0'). Sem os certs, o TLS quebraria quando a flag for ligada.
#
# Espelha go/orders/Dockerfile (build em golang:1.22-alpine → runtime scratch).
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /service ./cmd/server/

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /service /service
ENV TZ=America/Sao_Paulo
ENTRYPOINT ["/service"]
