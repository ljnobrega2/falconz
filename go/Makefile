# Makefile raiz — orquestra todos os serviços Go do Senderzz
# Uso: make build | make test | make docker-build | make tidy

SERVICES := motoboy wallet affiliates labels portal orders
GO       := go

.PHONY: all build test tidy vet lint docker-build docker-push clean

# ── Build todos os serviços ───────────────────────────────────────────────────

all: build

build:
	@echo "==> Build todos os serviços Go"
	@for svc in $(SERVICES); do \
		echo "--- Build $$svc"; \
		$(GO) build -o bin/$$svc ./$$svc/cmd/server/... || exit 1; \
	done
	@echo "==> Build completo"

# ── Testes ───────────────────────────────────────────────────────────────────

test:
	@echo "==> Testes todos os serviços"
	@for svc in $(SERVICES); do \
		echo "--- Test $$svc"; \
		$(MAKE) -C $$svc test 2>/dev/null || $(GO) test ./$$svc/... -count=1 -timeout 60s; \
	done

test-svc:
	@if [ -z "$(SVC)" ]; then echo "Uso: make test-svc SVC=motoboy"; exit 1; fi
	$(GO) test ./$(SVC)/... -v -count=1 -timeout 60s

# ── Go módulo ─────────────────────────────────────────────────────────────────

tidy:
	@for svc in $(SERVICES) shared; do \
		echo "--- Tidy $$svc"; \
		cd $$svc && $(GO) mod tidy && cd ..; \
	done

vet:
	@for svc in $(SERVICES); do \
		echo "--- Vet $$svc"; \
		cd $$svc && $(GO) vet ./... && cd ..; \
	done

lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "Instale golangci-lint: https://golangci-lint.run" && exit 1)
	@for svc in $(SERVICES); do \
		echo "--- Lint $$svc"; \
		cd $$svc && golangci-lint run ./... && cd ..; \
	done

# ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	@for svc in $(SERVICES); do \
		echo "--- Docker build $$svc"; \
		docker build -t senderzz/$$svc:latest ./$$svc || exit 1; \
	done

docker-build-svc:
	@if [ -z "$(SVC)" ]; then echo "Uso: make docker-build-svc SVC=motoboy"; exit 1; fi
	docker build -t senderzz/$(SVC):latest ./$(SVC)

docker-push:
	@for svc in $(SERVICES); do \
		docker push senderzz/$$svc:latest; \
	done

# ── Dev local ─────────────────────────────────────────────────────────────────

# Sobe infraestrutura (Postgres + Redis) sem os services Go
infra-up:
	cd ../infra/docker && docker compose up -d postgres redis

# Sobe tudo
up:
	cd ../infra/docker && docker compose up -d

down:
	cd ../infra/docker && docker compose down

logs:
	cd ../infra/docker && docker compose logs -f

# Sobe service específico com hot-reload (requer air: go install github.com/cosmtrek/air@latest)
dev:
	@if [ -z "$(SVC)" ]; then echo "Uso: make dev SVC=motoboy"; exit 1; fi
	cd $(SVC) && air

# ── pgloader — migrations ──────────────────────────────────────────────────────

migrate-motoboy:
	pgloader ../infra/pgloader/motoboy-migration.load

migrate-wallet:
	pgloader ../infra/pgloader/wallet-migration.load

migrate-affiliates:
	pgloader ../infra/pgloader/affiliates-migration.load

migrate-labels:
	pgloader ../infra/pgloader/labels-migration.load

migrate-orders:
	pgloader ../infra/pgloader/orders-migration.load

migrate-all: migrate-motoboy migrate-wallet migrate-affiliates migrate-labels migrate-orders

# ── Verify ───────────────────────────────────────────────────────────────────

verify:
	bash ../infra/scripts/verify-migration.sh

# ── Limpeza ───────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/

# ── Help ─────────────────────────────────────────────────────────────────────

help:
	@echo "Targets disponíveis:"
	@echo "  build              — compila todos os serviços para bin/"
	@echo "  test               — roda testes de todos os serviços"
	@echo "  test-svc SVC=X     — testa um serviço específico"
	@echo "  tidy               — go mod tidy em todos"
	@echo "  vet                — go vet em todos"
	@echo "  docker-build       — build imagens Docker de todos"
	@echo "  docker-build-svc SVC=X — build imagem de um serviço"
	@echo "  infra-up           — sobe Postgres + Redis"
	@echo "  up                 — sobe todos os containers"
	@echo "  dev SVC=X          — hot-reload com air"
	@echo "  migrate-all        — roda pgloader para todos os schemas"
	@echo "  verify             — roda verify-migration.sh"
