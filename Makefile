# chess-to-music — common developer tasks.
# Run `make` or `make help` to list available targets.

BIN_DIR    := bin
SERVER_BIN := $(BIN_DIR)/server
CLI_BIN    := $(BIN_DIR)/chess2music
IMAGE      := chess-to-music:latest
ADDR       ?= :8080
PGN        ?= testdata/sample.pgn
OUT        ?= game

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- Frontend ---

.PHONY: frontend
frontend: ## Install deps and build the Svelte frontend (web/dist)
	cd web && bun install --frozen-lockfile && bun run build

.PHONY: frontend-dev
frontend-dev: ## Run the Vite dev server with hot reload
	cd web && bun run dev

# --- Go build ---

.PHONY: build
build: build-cli build-server ## Build both binaries into bin/

.PHONY: build-cli
build-cli: ## Build the CLI (chess2music)
	mkdir -p $(BIN_DIR)
	go build -o $(CLI_BIN) ./cmd/chess2music

.PHONY: build-server
build-server: frontend ## Build the web server (embeds the frontend)
	mkdir -p $(BIN_DIR)
	go build -o $(SERVER_BIN) ./cmd/server

# --- Run ---

.PHONY: run
run: frontend ## Run the web server locally (ADDR=:8080)
	go run ./cmd/server -addr $(ADDR)

.PHONY: cli
cli: ## Run the CLI on a sample PGN (PGN=... OUT=...)
	go run ./cmd/chess2music -in $(PGN) -out $(OUT)

# --- Quality ---

.PHONY: test
test: ## Run the Go test suite
	go test ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	go mod tidy

.PHONY: check
check: fmt vet test ## Format, vet and test

# --- Database ---

.PHONY: db-up
db-up: ## Start only the Postgres database
	docker compose up -d db

.PHONY: db-down
db-down: ## Stop the Postgres database
	docker compose stop db

# --- Docker ---

.PHONY: docker-build
docker-build: ## Build the application Docker image
	docker build -t $(IMAGE) .

.PHONY: docker-run
docker-run: ## Run the built image (needs a reachable database for the library)
	docker run --rm -p 8080:8080 $(IMAGE)

.PHONY: up
up: ## Build and start the full stack (app + db) via compose
	docker compose up --build -d

.PHONY: down
down: ## Stop the full stack
	docker compose down

.PHONY: logs
logs: ## Tail compose logs
	docker compose logs -f

# --- Housekeeping ---

.PHONY: clean
clean: ## Remove build artifacts and generated files
	rm -rf $(BIN_DIR) web/dist
	rm -f game.abc game.mid game.wav game.mp3
