BINARY  := dist/gomaki
WEB_DIR := web
GO      ?= go
BUN     ?= bun

.PHONY: all web build run test lint fmt check clean

all: build

# Build the Svelte frontend into web/dist for the Go embed.
web:
	cd $(WEB_DIR) && $(BUN) install --frozen-lockfile
	cd $(WEB_DIR) && $(BUN) run build

# Compile the GoMaki binary with the freshly built frontend.
build: web
	@mkdir -p dist
	$(GO) build -o $(BINARY) ./cmd/gomaki

run: build
	./$(BINARY)

test:
	$(GO) test ./...

lint:
	golangci-lint run

fmt:
	$(GO) fmt ./...

check: lint test

# Keep web/dist: it is committed and required by //go:embed.
clean:
	rm -f $(BINARY)
	rm -rf $(WEB_DIR)/node_modules
