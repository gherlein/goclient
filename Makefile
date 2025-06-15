# Variables
BINARY_NAME=goclient
GO=go

# Default target
.DEFAULT_GOAL := build

# Build the application
.PHONY: build
build:
	$(GO) build -o $(BINARY_NAME) .

# Run tests
.PHONY: test
test:
	$(GO) test -v ./...

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(BINARY_NAME)
	$(GO) clean

# Run the application with default settings
.PHONY: run
run: build
	./$(BINARY_NAME) -model codellama -agent default

# Run with code agent
.PHONY: run-code
run-code: build
	./$(BINARY_NAME) -model codellama -agent code

# Run with explain agent
.PHONY: run-explain
run-explain: build
	./$(BINARY_NAME) -model codellama -agent explain

# Install dependencies
.PHONY: deps
deps:
	$(GO) mod tidy

# Format code
.PHONY: fmt
fmt:
	$(GO) fmt ./...

# Run all checks (format and test)
.PHONY: check
check: fmt test
