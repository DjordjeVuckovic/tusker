# Build variables
APP_NAME := news-hunter
CMD_DIR := ./cmd
BIN_DIR := ./bin
PKG := github.com/DjordjeVuckovic/news-hunter
MIGRATIONS_PATH := ./db/migrations
DB_CONN := "postgresql://news_user:news_password@localhost:54320/news_db?sslmode=disable"
# ParadeDB runs on 54321 (docker-compose: pg-news-parade); BM25 via @@@ index.
MIGRATIONS_PATH_PARADE := ./db/parade_migrations
DB_CONN_PARADE := "postgresql://news_user:news_password@localhost:54321/news_db?sslmode=disable"
# pg_textsearch (TimescaleDB-HA) runs on 54322 (docker-compose: pg-news-tiger); BM25 via generated column.
MIGRATIONS_PATH_TIGER := ./db/tiger_migrations
DB_CONN_TIGER := "postgresql://news_user:news_password@localhost:54322/news_db?sslmode=disable"

# Go variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ARGS ?=

# Build commands
.PHONY: build build-all clean test fmt vet lint lint-fix install-lint schema-gen build-bench bench-validate bench-run bench-pool bench-judge-lexical bench-judge-cli bench-judge-api bench-qrels bench-show-spec bench-show-pool bench-show-judgments migrate-up migrate-down migrate-up-parade migrate-down-parade migrate-up-tiger migrate-down-tiger migrate-up-all

migrate-up:
	@echo "Running database migrations up (native pg)..."
	@migrate -path $(MIGRATIONS_PATH) -database $(DB_CONN) up

migrate-down:
	@echo "Running database migrations down (native pg)..."
	@migrate -path $(MIGRATIONS_PATH) -database $(DB_CONN) down

migrate-up-parade:
	@echo "Running database migrations up (ParadeDB)..."
	@migrate -path $(MIGRATIONS_PATH_PARADE) -database $(DB_CONN_PARADE) up

migrate-down-parade:
	@echo "Running database migrations down (ParadeDB)..."
	@migrate -path $(MIGRATIONS_PATH_PARADE) -database $(DB_CONN_PARADE) down

migrate-up-tiger:
	@echo "Running database migrations up (pg_textsearch)..."
	@migrate -path $(MIGRATIONS_PATH_TIGER) -database $(DB_CONN_TIGER) up

migrate-down-tiger:
	@echo "Running database migrations down (pg_textsearch)..."
	@migrate -path $(MIGRATIONS_PATH_TIGER) -database $(DB_CONN_TIGER) down

# Apply migrations to all Postgres backends (native + ParadeDB + pg_textsearch).
migrate-up-all: migrate-up migrate-up-parade migrate-up-tiger

# Build all commands
build-all: build-datapipe build-news-api build-schemagen build-bench

build-datapipe:
	@echo "Building datapipe..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/datapipe $(CMD_DIR)/datapipe

build-news-api:
	@echo "Building news-api..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/news-api $(CMD_DIR)/news_api

build-schemagen:
	@echo "Building schema generator..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/schemagen $(CMD_DIR)/schemagen

# Generate schemas from Go structs
schema-gen: build-schemagen
	@echo "Generating schemas..."
	@./$(BIN_DIR)/schemagen -output=api
	@echo "Schemas generated in api/ directory"

# Development commands
test:
	@echo "Running tests..."
	@go test -v ./...

fmt:
	@echo "Formatting code..."
	@go fmt ./...

vet:
	@echo "Vetting code..."
	@go vet ./...

GOLANGCI_LINT_VERSION ?= v2.12.2

install-lint:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint:
	@echo "Linting code..."
	@golangci-lint run ./...

lint-fix:
	@echo "Linting code (with autofix)..."
	@golangci-lint run --fix ./...

# Database commands
dc-up:
	@echo "Starting database..."
	@docker-compose up -d

dc-down:
	@echo "Stopping database..."
	@docker-compose down

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -rf schema/generated

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

run-schemagen: build-schemagen
	@echo "Running schema generator..."
	@./$(BIN_DIR)/schemagen -output=api

# Preprocess a raw dataset into canonical JSONL
run-datapipe-preprocess: build-datapipe
	@echo "Running datapipe preprocess..."
	@ENV_PATHS="cmd/datapipe/preprocess.env" ./$(BIN_DIR)/datapipe preprocess

# Load a dataset into the articles store (Postgres)
run-datapipe-articles-pg: build-datapipe
	@echo "Running datapipe load articles (pg)..."
	@ENV_PATHS="cmd/datapipe/articles.env,cmd/datapipe/pg.env" ./$(BIN_DIR)/datapipe load articles

# Load a dataset into the articles store (Elasticsearch)
run-datapipe-articles-es: build-datapipe
	@echo "Running datapipe load articles (es)..."
	@ENV_PATHS="cmd/datapipe/articles.env,cmd/datapipe/es.env" ./$(BIN_DIR)/datapipe load articles

# Load precomputed embeddings into the article_embeddings store
run-datapipe-embeddings: build-datapipe
	@echo "Running datapipe load embeddings..."
	@ENV_PATHS="cmd/datapipe/embeddings.env" ./$(BIN_DIR)/datapipe load embeddings

run-api: build-news-api
	@echo "Running news search service..."
	@ENV_PATHS="cmd/news_api/.env" ./$(BIN_DIR)/news-api

run-api-pg: build-news-api
	@echo "Running news search service..."
	@ENV_PATHS="cmd/news_api/.env,cmd/news_api/pg.env" ./$(BIN_DIR)/news-api

run-api-es: build-news-api
	@echo "Running news search service..."
	@ENV_PATHS="cmd/news_api/.env,cmd/news_api/es.env" ./$(BIN_DIR)/news-api

# Benchmark commands
build-bench:
	@echo "Building bench..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/bench $(CMD_DIR)/bench

TRACK ?= fts_quality

bench-validate: build-bench
	@./$(BIN_DIR)/bench validate $(TRACK)

bench-run: build-bench
	@./$(BIN_DIR)/bench run $(TRACK)

bench-pool: build-bench
	@./$(BIN_DIR)/bench pool $(TRACK)

bench-judge-lexical: build-bench
	@./$(BIN_DIR)/bench judge $(TRACK) --strategy lexical

bench-judge-cli: build-bench
	@./$(BIN_DIR)/bench judge $(TRACK) --strategy claude-cli --resume

bench-judge-api: build-bench
	@./$(BIN_DIR)/bench judge $(TRACK) --strategy claude-api --resume

bench-show-spec: build-bench
	@./$(BIN_DIR)/bench show spec $(TRACK)

bench-show-pool: build-bench
	@./$(BIN_DIR)/bench show pool $(TRACK)

bench-show-judgments: build-bench
	@./$(BIN_DIR)/bench show judgments $(TRACK) --strategy lexical

bench-qrels: build-bench
	@./$(BIN_DIR)/bench qrels $(TRACK) --strategy lexical

# Development workflow
dev: fmt vet lint test build-all

.DEFAULT_GOAL := build-all