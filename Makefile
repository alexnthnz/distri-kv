.PHONY: build clean test run run-cluster stop-cluster help

# Binary name
BINARY_NAME=distri-kv

# Build directory
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/distri-kv

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -rf data/

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Run a single node
run: build
	@echo "Starting single node..."
	./$(BUILD_DIR)/$(BINARY_NAME)

# Run a 3-node cluster
run-cluster: build
	@echo "Starting 3-node cluster..."
	@mkdir -p data/node1 data/node2 data/node3
	./$(BUILD_DIR)/$(BINARY_NAME) -config examples/node2.yaml &
	./$(BUILD_DIR)/$(BINARY_NAME) -config examples/node3.yaml &
	./$(BUILD_DIR)/$(BINARY_NAME) -config config.yaml

# Stop cluster (kill all distri-kv processes)
stop-cluster:
	@echo "Stopping cluster..."
	@pkill -f "$(BINARY_NAME)" || true

# Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOGET) github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Create example data
create-data:
	@echo "Creating example data..."
	@mkdir -p data/node1 data/node2 data/node3

# API examples (requires running cluster)
api-examples:
	@echo "Running API examples..."
	@echo "Storing some data..."
	curl -X PUT http://localhost:8080/api/v1/kv/user1 -d '{"value":"Alice"}'
	curl -X PUT http://localhost:8080/api/v1/kv/user2 -d '{"value":"Bob"}'
	curl -X PUT http://localhost:8080/api/v1/kv/config -d '{"value":"production"}'
	@echo ""
	@echo "Retrieving data..."
	curl http://localhost:8080/api/v1/kv/user1
	curl http://localhost:8080/api/v1/kv/user2
	curl http://localhost:8080/api/v1/kv/config
	@echo ""
	@echo "Getting cluster stats..."
	curl http://localhost:8080/api/v1/cluster/stats
	@echo ""
	@echo "Getting cluster health..."
	curl http://localhost:8080/api/v1/cluster/health

# Performance test
perf-test: build
	@echo "Running performance test..."
	@./scripts/perf-test.sh

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t distri-kv:latest .

# Docker run
docker-run:
	@echo "Running Docker container..."
	docker run -p 8080:8080 distri-kv:latest

# Generate documentation
docs:
	@echo "Generating documentation..."
	$(GOCMD) doc -all ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  build         - Build the application"
	@echo "  clean         - Clean build artifacts and data"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  deps          - Download dependencies"
	@echo "  run           - Run a single node"
	@echo "  run-cluster   - Run a 3-node cluster"
	@echo "  stop-cluster  - Stop the cluster"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  install-tools - Install development tools"
	@echo "  api-examples  - Run API examples"
	@echo "  perf-test     - Run performance test"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  docs          - Generate documentation"
	@echo "  help          - Show this help"

# Default target
all: build test 