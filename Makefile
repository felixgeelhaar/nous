.PHONY: all build test test-race test-cover bench lint clean docker run help

# Default target
all: build test

# Build the binary
build:
	go build -o bin/nous ./cmd/nous

# Run all tests
test:
	go test ./... -count=1

# Run tests with race detector
test-race:
	go test ./... -race -count=1

# Run tests with coverage
test-cover:
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run benchmarks
bench:
	go test ./... -bench=. -benchtime=1s -count=1

# Run linting
lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout=5m; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

# Database migrations
migrate-postgres:
	@if command -v goose >/dev/null 2>&1; then \
		goose -dir internal/store/postgres/migrations postgres "$(POSTGRES_DSN)" up; \
	else \
		echo "goose not installed. Install: go install github.com/pressly/goose/v3/cmd/goose@latest"; \
	fi

migrate-sqlite:
	@if command -v goose >/dev/null 2>&1; then \
		goose -dir internal/store/sqlite/migrations sqlite3 "$(SQLITE_DSN)" up; \
	else \
		echo "goose not installed. Install: go install github.com/pressly/goose/v3/cmd/goose@latest"; \
	fi

migrate-status:
	@if command -v goose >/dev/null 2>&1; then \
		echo "PostgreSQL:"; goose -dir internal/store/postgres/migrations postgres "$(POSTGRES_DSN)" status; \
		echo "SQLite:"; goose -dir internal/store/sqlite/migrations sqlite3 "$(SQLITE_DSN)" status; \
	else \
		echo "goose not installed"; \
	fi

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html *.db

# Build Docker image
docker:
	docker build -t nous:latest .

# Run locally with SQLite
run:
	go run ./cmd/nous

# Run with memory backend
run-memory:
	NOUS_DB_TYPE=memory go run ./cmd/nous

# Run with PostgreSQL (requires running Postgres)
run-postgres:
	NOUS_DB_TYPE=postgres NOUS_DB_DSN=$${POSTGRES_DSN} go run ./cmd/nous

# Format code
fmt:
	go fmt ./...

# Tidy dependencies
tidy:
	go mod tidy

# Security scan
scan:
	@if command -v nox >/dev/null 2>&1; then \
		nox scan; \
	else \
		echo "nox not installed, skipping"; \
	fi

# Coverage check with coverctl
cover-check:
	@if command -v coverctl >/dev/null 2>&1; then \
		coverctl check; \
	else \
		echo "coverctl not installed, skipping"; \
	fi

# Generate protobuf code
gen-proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/nous/v1/nous.proto

# Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  test        - Run all tests"
	@echo "  test-race   - Run tests with race detector"
	@echo "  test-cover  - Run tests with coverage report"
	@echo "  bench       - Run benchmarks"
	@echo "  lint        - Run linters"
	@echo "  clean       - Clean build artifacts"
	@echo "  docker      - Build Docker image"
	@echo "  run         - Run with SQLite (default)"
	@echo "  run-memory  - Run with memory backend"
	@echo "  run-postgres- Run with PostgreSQL backend"
	@echo "  fmt         - Format Go code"
	@echo "  tidy        - Tidy Go modules"
	@echo "  scan        - Run security scan"
	@echo "  cover-check - Check coverage against thresholds"
	@echo "  gen-proto   - Generate protobuf Go code"
	@echo "  migrate-postgres - Run PostgreSQL migrations (requires POSTGRES_DSN)"
	@echo "  migrate-sqlite   - Run SQLite migrations (requires SQLITE_DSN)"
	@echo "  migrate-status   - Show migration status for all backends"
