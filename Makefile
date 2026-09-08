# Meterrail monorepo.
#
# `make help` lists every target. The common ones:
#
#   make setup     install every toolchain dependency
#   make dev       run the whole stack (infra + api + worker + web)
#   make test      run every test suite
#   make check     format, lint, typecheck and test — what CI runs
#
# Targets are grouped by area and self-documenting: a target with a `## comment`
# after its dependencies shows up in `make help`.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# Every recipe is a shell command, not a file to build.
.PHONY: help setup setup-go setup-node setup-tools doctor \
        dev dev-infra dev-api dev-worker dev-web dev-indexer dev-stop \
        build build-api build-web build-shared build-docker \
        run-api run-worker run-migrate run-seed \
        db-up db-down db-migrate db-reset db-seed db-shell db-status db-backup \
        redis-shell redis-flush queues asynqmon \
        indexer indexer-codegen indexer-stop indexer-graphql \
        test test-api test-web test-coverage \
        lint lint-api lint-web fmt fmt-api fmt-web fmt-check \
        typecheck vet tidy check ci \
        docker-up docker-down docker-logs docker-ps docker-rebuild docker-clean \
        deploy-api deploy-web deploy-rollback tls-cert vps-bootstrap \
        env logs clean clean-all install

# --- configuration ---------------------------------------------------------

API_DIR      := apps/api
WEB_DIR      := apps/web
SHARED_DIR   := packages/shared
INDEXER_DIR  := indexer
DEPLOY_DIR   := deploy

COMPOSE      := docker compose
COMPOSE_PROD := $(COMPOSE) --file $(DEPLOY_DIR)/docker-compose.prod.yml

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# Ports; override on the command line, e.g. `make dev-api API_PORT=9000`.
API_PORT      ?= 8080
WEB_PORT      ?= 3000
ASYNQMON_PORT ?= 8081
POSTGRES_PORT ?= 5432
REDIS_PORT    ?= 6379

# Deploy targets: `make deploy-api DEPLOY_HOST=deploy@1.2.3.4`
DEPLOY_HOST ?=
IMAGE_TAG   ?= latest

# Local connection strings, used when running the Go binaries outside Docker.
export DATABASE_URL ?= postgres://meterrail:meterrail@localhost:$(POSTGRES_PORT)/meterrail?sslmode=disable
export REDIS_URL    ?= redis://localhost:$(REDIS_PORT)/0

CYAN  := \033[36m
BOLD  := \033[1m
DIM   := \033[2m
RESET := \033[0m

# --- help ------------------------------------------------------------------

help: ## Show this help
	@printf "$(BOLD)Meterrail$(RESET) $(DIM)$(VERSION) ($(COMMIT))$(RESET)\n\n"
	@awk 'BEGIN { FS = ":.*?## " } \
		/^# --- / { \
			section = $$0; \
			gsub(/^# --- /, "", section); gsub(/ -*$$/, "", section); \
			next \
		} \
		/^[a-zA-Z0-9_-]+:.*?## / { \
			if (section != "" && section != shown) { \
				printf "\n$(BOLD)%s$(RESET)\n", section; shown = section \
			} \
			printf "  $(CYAN)%-22s$(RESET) %s\n", $$1, $$2 \
		}' $(MAKEFILE_LIST)
	@printf "\n"

# --- setup -----------------------------------------------------------------

setup: setup-go setup-node setup-tools ## Install every dependency (run this first)
	@printf "\n$(BOLD)Setup complete.$(RESET) Next: cp .env.example .env, then 'make dev'\n"

setup-go: ## Download Go module dependencies
	@printf "$(CYAN)==>$(RESET) Downloading Go modules\n"
	@cd $(API_DIR) && go mod download && go mod verify

setup-node: ## Install workspace JavaScript dependencies
	@printf "$(CYAN)==>$(RESET) Installing pnpm workspace\n"
	@corepack enable 2>/dev/null || true
	@pnpm install

setup-tools: ## Install the Go developer tooling (air, golangci-lint)
	@printf "$(CYAN)==>$(RESET) Installing Go tools\n"
	@go install github.com/air-verse/air@latest
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

install: setup ## Alias for setup

doctor: ## Check that every required tool is present
	@printf "$(BOLD)Toolchain$(RESET)\n"
	@for tool in go node pnpm docker; do \
		if command -v $$tool >/dev/null 2>&1; then \
			printf "  $(CYAN)%-10s$(RESET) %s\n" "$$tool" "$$($$tool --version 2>&1 | head -1)"; \
		else \
			printf "  %-10s $(BOLD)MISSING$(RESET)\n" "$$tool"; \
		fi; \
	done
	@printf "\n$(BOLD)Optional$(RESET)\n"
	@for tool in air golangci-lint envio; do \
		if command -v $$tool >/dev/null 2>&1; then \
			printf "  $(CYAN)%-16s$(RESET) present\n" "$$tool"; \
		else \
			printf "  %-16s not installed (make setup-tools)\n" "$$tool"; \
		fi; \
	done
	@printf "\n$(BOLD)Environment$(RESET)\n"
	@if [ -f .env ]; then printf "  .env present\n"; \
		else printf "  .env $(BOLD)missing$(RESET) — run 'make env'\n"; fi
	@if [ -f $(WEB_DIR)/.env.local ]; then printf "  apps/web/.env.local present\n"; \
		else printf "  apps/web/.env.local $(BOLD)missing$(RESET) — run 'make env'\n"; fi

env: ## Create .env files from the examples
	@if [ ! -f .env ]; then cp .env.example .env && \
		printf "$(CYAN)==>$(RESET) Created .env\n"; \
		else printf "$(DIM).env already exists, leaving it alone$(RESET)\n"; fi
	@if [ ! -f $(WEB_DIR)/.env.local ]; then cp $(WEB_DIR)/.env.example $(WEB_DIR)/.env.local && \
		printf "$(CYAN)==>$(RESET) Created apps/web/.env.local\n"; \
		else printf "$(DIM)apps/web/.env.local already exists, leaving it alone$(RESET)\n"; fi
	@printf "\nFill in the Dynamic, Agora and Cloudflare R2 values before running 'make dev'.\n"

# --- development -----------------------------------------------------------

dev: dev-infra ## Start everything (infra, API, worker, web) in one terminal
	@printf "$(CYAN)==>$(RESET) Starting API, worker and web\n"
	@printf "$(DIM)  api      http://localhost:$(API_PORT)$(RESET)\n"
	@printf "$(DIM)  web      http://localhost:$(WEB_PORT)$(RESET)\n"
	@printf "$(DIM)  queues   http://localhost:$(ASYNQMON_PORT)$(RESET)\n"
	@printf "$(DIM)  Ctrl-C stops all three.$(RESET)\n\n"
	@trap 'kill 0' EXIT INT TERM; \
		$(MAKE) --no-print-directory dev-api & \
		$(MAKE) --no-print-directory dev-worker & \
		$(MAKE) --no-print-directory dev-web & \
		wait

dev-infra: db-up ## Start Postgres, Redis and Asynqmon, then migrate
	@printf "$(CYAN)==>$(RESET) Applying migrations\n"
	@$(MAKE) --no-print-directory db-migrate

dev-api: ## Run the API with hot reload (falls back to `go run`)
	@if command -v air >/dev/null 2>&1; then \
		cd $(API_DIR) && air -c .air.toml; \
	else \
		printf "$(DIM)air not installed; running without hot reload (make setup-tools)$(RESET)\n"; \
		cd $(API_DIR) && PORT=$(API_PORT) go run -ldflags "$(LDFLAGS)" ./cmd/server; \
	fi

dev-worker: ## Run the background worker and cron scheduler
	@cd $(API_DIR) && go run -ldflags "$(LDFLAGS)" ./cmd/worker

dev-web: ## Run the Next.js dev server
	@cd $(WEB_DIR) && PORT=$(WEB_PORT) pnpm dev

dev-indexer: indexer ## Alias for `make indexer`

dev-stop: ## Stop the local infrastructure containers
	@$(COMPOSE) stop postgres redis asynqmon

# --- build -----------------------------------------------------------------

build: build-shared build-api build-web ## Build every artifact

build-api: ## Compile the Go binaries into apps/api/bin
	@printf "$(CYAN)==>$(RESET) Building Go binaries ($(VERSION))\n"
	@cd $(API_DIR) && mkdir -p bin && \
		for cmd in server worker migrate seed; do \
			CGO_ENABLED=0 go build -trimpath -ldflags "-s -w $(LDFLAGS)" \
				-o bin/$$cmd ./cmd/$$cmd && printf "  bin/%s\n" "$$cmd"; \
		done

build-web: build-shared ## Build the Next.js production bundle
	@printf "$(CYAN)==>$(RESET) Building the web app\n"
	@cd $(WEB_DIR) && pnpm build

build-shared: ## Typecheck the shared package
	@cd $(SHARED_DIR) && pnpm typecheck

build-docker: ## Build the Docker images
	@printf "$(CYAN)==>$(RESET) Building Docker images ($(VERSION))\n"
	@docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		-t meterrail-api:$(VERSION) -t meterrail-api:latest $(API_DIR)
	@docker build -f $(WEB_DIR)/Dockerfile \
		-t meterrail-web:$(VERSION) -t meterrail-web:latest .

# --- run (compiled binaries) -----------------------------------------------

run-api: build-api ## Run the compiled API binary
	@cd $(API_DIR) && PORT=$(API_PORT) ./bin/server

run-worker: build-api ## Run the compiled worker binary
	@cd $(API_DIR) && ./bin/worker

run-migrate: build-api ## Run the compiled migrate binary
	@cd $(API_DIR) && ./bin/migrate -action=up

run-seed: build-api ## Run the compiled seed binary
	@cd $(API_DIR) && ./bin/seed

# --- database --------------------------------------------------------------

db-up: ## Start Postgres, Redis and Asynqmon, and wait for them
	@printf "$(CYAN)==>$(RESET) Starting infrastructure\n"
	@$(COMPOSE) up -d --wait postgres redis asynqmon

db-down: ## Stop the infrastructure containers
	@$(COMPOSE) stop postgres redis asynqmon

db-migrate: ## Apply database migrations
	@cd $(API_DIR) && go run ./cmd/migrate -action=up

db-status: ## Show which tables exist
	@cd $(API_DIR) && go run ./cmd/migrate -action=status

db-reset: ## Drop every table and re-migrate (destructive)
	@printf "$(BOLD)This deletes all local data.$(RESET) Continue? [y/N] "; \
		read -r reply; [ "$$reply" = "y" ] || { echo "aborted"; exit 1; }
	@cd $(API_DIR) && go run ./cmd/migrate -action=reset

db-seed: db-migrate ## Populate the database with development data
	@cd $(API_DIR) && go run ./cmd/seed

db-shell: ## Open a psql shell against the local database
	@$(COMPOSE) exec postgres psql -U meterrail -d meterrail

db-backup: ## Take a local logical backup
	@APP_DIR=. ENV_FILE=.env BACKUP_DIR=./backups $(DEPLOY_DIR)/scripts/backup-db.sh

redis-shell: ## Open a redis-cli shell
	@$(COMPOSE) exec redis redis-cli

redis-flush: ## Clear the cache (leaves queued jobs intact)
	@printf "$(BOLD)This clears cached data.$(RESET) Continue? [y/N] "; \
		read -r reply; [ "$$reply" = "y" ] || { echo "aborted"; exit 1; }
	@$(COMPOSE) exec redis redis-cli --scan --pattern 'meterrail:cache:*' \
		| xargs -r $(COMPOSE) exec -T redis redis-cli DEL

# --- jobs and queues -------------------------------------------------------

asynqmon: ## Open the Asynqmon queue dashboard
	@printf "$(CYAN)==>$(RESET) Asynqmon: http://localhost:$(ASYNQMON_PORT)\n"
	@$(COMPOSE) up -d asynqmon
	@command -v open >/dev/null 2>&1 && open "http://localhost:$(ASYNQMON_PORT)" || true

queues: ## Print queue depth as JSON
	@curl -fsS "http://localhost:$(API_PORT)/v1/internal/queues" \
		-H "X-Service-Token: $${INTERNAL_SERVICE_TOKEN:-}" \
		| { command -v jq >/dev/null 2>&1 && jq . || cat; }

# --- indexer ---------------------------------------------------------------

indexer: indexer-codegen ## Run the Envio indexer
	@printf "$(CYAN)==>$(RESET) Starting the Envio indexer\n"
	@printf "$(DIM)  GraphQL: http://localhost:8080/v1/graphql (password: testing)$(RESET)\n"
	@cd $(INDEXER_DIR) && pnpm dev

indexer-codegen: ## Generate indexer types from config.yaml and schema.graphql
	@cd $(INDEXER_DIR) && pnpm codegen

indexer-stop: ## Stop the indexer and its containers
	@cd $(INDEXER_DIR) && pnpm stop 2>/dev/null || true
	@cd $(INDEXER_DIR) && pnpm local-docker-down 2>/dev/null || true

indexer-graphql: ## Open the indexer's GraphQL playground
	@command -v open >/dev/null 2>&1 && open "http://localhost:8080" || \
		printf "Open http://localhost:8080\n"

# --- quality ---------------------------------------------------------------

check: fmt-check lint typecheck test ## Everything CI runs

ci: check ## Alias for check

test: test-api test-web ## Run every test suite

test-api: ## Run the Go tests with the race detector
	@printf "$(CYAN)==>$(RESET) Testing the API\n"
	@cd $(API_DIR) && go test -race ./...

test-web: ## Run the frontend tests
	@printf "$(CYAN)==>$(RESET) Testing the web app\n"
	@pnpm -r --if-present test

test-coverage: ## Run the Go tests and open the coverage report
	@cd $(API_DIR) && go test -race -coverprofile=coverage.out -covermode=atomic ./... && \
		go tool cover -func=coverage.out | tail -1 && \
		go tool cover -html=coverage.out

lint: lint-api lint-web ## Lint everything

lint-api: ## Lint the Go code
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(API_DIR) && golangci-lint run --timeout=5m; \
	else \
		printf "$(DIM)golangci-lint not installed; running go vet instead$(RESET)\n"; \
		cd $(API_DIR) && go vet ./...; \
	fi

lint-web: ## Lint the frontend
	@cd $(WEB_DIR) && pnpm lint

fmt: fmt-api fmt-web ## Format everything

fmt-api: ## Format the Go code
	@cd $(API_DIR) && gofmt -w . && go mod tidy

fmt-web: ## Format the JavaScript, TypeScript and CSS
	@pnpm format

fmt-check: ## Verify formatting without changing files
	@printf "$(CYAN)==>$(RESET) Checking formatting\n"
	@unformatted="$$(cd $(API_DIR) && gofmt -l .)"; \
		if [ -n "$$unformatted" ]; then \
			printf "These Go files need gofmt:\n%s\n" "$$unformatted"; exit 1; \
		fi
	@pnpm format:check

typecheck: ## Typecheck the TypeScript packages
	@printf "$(CYAN)==>$(RESET) Typechecking\n"
	@pnpm typecheck

vet: ## Run go vet
	@cd $(API_DIR) && go vet ./...

tidy: ## Tidy the Go module and the pnpm lockfile
	@cd $(API_DIR) && go mod tidy
	@pnpm install --lockfile-only

# --- docker ----------------------------------------------------------------

docker-up: ## Start the full stack in Docker
	@$(COMPOSE) up -d --build --wait
	@$(MAKE) --no-print-directory docker-ps

docker-down: ## Stop the stack (volumes are preserved)
	@$(COMPOSE) down

docker-rebuild: ## Rebuild the images and restart
	@$(COMPOSE) up -d --build --force-recreate --wait

docker-logs: ## Follow the logs of every container
	@$(COMPOSE) logs -f --tail=100

docker-ps: ## Show container status
	@$(COMPOSE) ps

docker-clean: ## Stop the stack and delete its volumes (destructive)
	@printf "$(BOLD)This deletes the local database and queue data.$(RESET) Continue? [y/N] "; \
		read -r reply; [ "$$reply" = "y" ] || { echo "aborted"; exit 1; }
	@$(COMPOSE) down --volumes --remove-orphans

logs: ## Follow the API and worker logs
	@$(COMPOSE) logs -f --tail=100 api worker

# --- deployment ------------------------------------------------------------

vps-bootstrap: ## Prepare a fresh VPS (needs DEPLOY_HOST=root@ip)
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST=root@your-server-ip"; exit 1; }
	@scp $(DEPLOY_DIR)/scripts/bootstrap-vps.sh $(DEPLOY_HOST):/tmp/
	@ssh $(DEPLOY_HOST) 'bash /tmp/bootstrap-vps.sh deploy'

deploy-api: ## Deploy the backend to the VPS (needs DEPLOY_HOST=deploy@ip)
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST=deploy@your-server-ip"; exit 1; }
	@printf "$(CYAN)==>$(RESET) Syncing deployment files to $(DEPLOY_HOST)\n"
	@rsync -az --delete $(DEPLOY_DIR)/ $(DEPLOY_HOST):/opt/meterrail/deploy/
	@printf "$(CYAN)==>$(RESET) Deploying\n"
	@ssh $(DEPLOY_HOST) 'cd /opt/meterrail && IMAGE_TAG=$(IMAGE_TAG) bash deploy/scripts/deploy.sh'

deploy-web: ## Deploy the frontend to Vercel
	@command -v vercel >/dev/null 2>&1 || { echo "install the Vercel CLI: pnpm add -g vercel"; exit 1; }
	@cd $(WEB_DIR) && vercel deploy --prod

deploy-rollback: ## Roll the backend back to a tag (TAG=sha-abc1234)
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST=deploy@your-server-ip"; exit 1; }
	@test -n "$(TAG)" || { echo "set TAG=sha-abc1234"; exit 1; }
	@ssh $(DEPLOY_HOST) 'cd /opt/meterrail && bash deploy/scripts/rollback.sh $(TAG)'

tls-cert: ## Issue a TLS certificate (DOMAIN=api.example.com EMAIL=you@example.com)
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST=deploy@your-server-ip"; exit 1; }
	@test -n "$(DOMAIN)" || { echo "set DOMAIN=api.example.com"; exit 1; }
	@test -n "$(EMAIL)" || { echo "set EMAIL=you@example.com"; exit 1; }
	@ssh $(DEPLOY_HOST) 'cd /opt/meterrail && bash deploy/scripts/issue-cert.sh $(DOMAIN) $(EMAIL)'

# --- housekeeping ----------------------------------------------------------

clean: ## Remove build artifacts
	@rm -rf $(API_DIR)/bin $(API_DIR)/coverage.out $(WEB_DIR)/.next $(WEB_DIR)/out
	@printf "$(CYAN)==>$(RESET) Build artifacts removed\n"

clean-all: clean ## Also remove dependencies and containers (destructive)
	@printf "$(BOLD)This removes node_modules and all containers.$(RESET) Continue? [y/N] "; \
		read -r reply; [ "$$reply" = "y" ] || { echo "aborted"; exit 1; }
	@find . -name node_modules -type d -prune -exec rm -rf {} + 2>/dev/null || true
	@rm -rf $(INDEXER_DIR)/generated
	@$(COMPOSE) down --volumes --remove-orphans 2>/dev/null || true
	@printf "$(CYAN)==>$(RESET) Clean. Run 'make setup' to start over.\n"
