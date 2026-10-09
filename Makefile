# Git-Review-Board build helpers.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BINARY  ?= revboard
WEB_DIST := internal/webui/dist/browser

.PHONY: all build web web-install placeholder go test test-go test-web dev-api dev-web install clean

all: build

## build: build the Angular UI and the single revboard binary
build: web go

## web-install: install the frontend dependencies
web-install:
	cd web && npm ci

## web: build the Angular UI into internal/webui/dist (embedded by go:embed)
web:
	@test -d web/node_modules || $(MAKE) web-install
	cd web && npx ng build

## placeholder: create a stub UI so the Go code builds without Node.js
placeholder:
	@test -f $(WEB_DIST)/index.html || (mkdir -p $(WEB_DIST) && cp internal/webui/placeholder/index.html $(WEB_DIST)/)

## go: build the revboard binary with whatever UI is in internal/webui/dist
go: placeholder
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

## test: run all tests
test: test-go test-web

test-go: placeholder
	go vet ./...
	go test ./...

test-web:
	@test -d web/node_modules || $(MAKE) web-install
	cd web && npx ng test --watch=false

## dev-api: run the API on port 7777 for "make dev-web"
dev-api: placeholder
	go run . serve --no-open --port 7777

## dev-web: run the Angular dev server (http://localhost:4200) proxying /api to port 7777
dev-web:
	cd web && npx ng serve

## install: install revboard into $GOBIN (default ~/go/bin)
GOBIN_DIR := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)
install: build
	install -d $(GOBIN_DIR)
	install -m 0755 $(BINARY) $(GOBIN_DIR)/$(BINARY)
	@echo "Installed $(GOBIN_DIR)/$(BINARY)"
	@command -v $(BINARY) | grep -qx '$(GOBIN_DIR)/$(BINARY)' || echo "warning: '$(BINARY)' resolves to $$(command -v $(BINARY)) in your PATH, not $(GOBIN_DIR)/$(BINARY)"

clean:
	rm -rf $(BINARY) internal/webui/dist
