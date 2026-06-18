.PHONY: run build test tidy migrate lint

## run: inicia o servidor em modo desenvolvimento
run:
	go run ./cmd/server

## build: compila o binário para bin/motoboy-service
build:
	go build -o bin/motoboy-service ./cmd/server

## test: executa toda a suite de testes
test:
	go test ./...

## tidy: sincroniza go.mod / go.sum e verifica dependências
tidy:
	go mod tidy

## lint: análise estática (requer golangci-lint instalado)
lint:
	golangci-lint run ./...

## migrate: migra tabelas sz_motoboy_* do MySQL para Postgres com pgloader
##
## Variáveis obrigatórias:
##   MYSQL_URL  — ex: user:pass@tcp(localhost:3306)/wordpress_db
##   PG_URL     — ex: user:pass@localhost:5432/senderzz_db
##
## Instalar pgloader: https://pgloader.io/
migrate:
	pgloader \
		--with "include only table names matching ~/^sz_motoboy/" \
		--with "reset sequences" \
		--with "create tables" \
		--with "create indexes" \
		--with "foreign keys" \
		--set "work_mem to '128MB'" \
		mysql://$(MYSQL_URL) \
		postgresql://$(PG_URL)

## migrate-portal: migra tabelas de sessão do portal necessárias para AuthPortal.
##
## Tabelas MySQL: wp_senderzz_portal_sessions, wp_senderzz_portal_users
## (prefixo wp_ preservado pelo pgloader — Go queries usam wp_senderzz_portal_* por padrão).
## Se o prefixo WP do seu install for diferente de "wp_", ajustar
## auth/auth.go constantes de tabela E o filtro abaixo.
migrate-portal:
	pgloader \
		--with "including only table names matching /^wp_senderzz_portal_(sessions|users)/" \
		--with "reset sequences" \
		--with "create tables" \
		--with "create indexes" \
		mysql://$(MYSQL_URL) \
		postgresql://$(PG_URL)

## help: lista os alvos disponíveis
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
