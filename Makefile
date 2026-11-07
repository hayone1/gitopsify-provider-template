VERSION ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
BINARY := gitopsify-provider-template

.PHONY: build install test lint upstream record

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(BINARY) .

# gitopsify finds providers on PATH: this is how to try a change with
# gitopsify export. Named $(BINARY) whatever the repository is called.
GOBIN_DIR = $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)
install:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o "$(GOBIN_DIR)/$(BINARY)$(shell go env GOEXE)" .

test:
	go test ./...

lint:
	go vet ./...
	gofmt -l . | (! grep .)

# Refresh testdata/upstream (the Terraform schema and the Crossplane CRDs)
# from the versions pinned in internal/provider/describe.go. Needs terraform
# (TF=tofu for OpenTofu); runs crane through Go when it isn't installed.
upstream:
	go run ./tools/upstream

# Re-record testdata/api from a throwaway instance (answers can hold secrets),
# with the settings' environment variables (internal/provider/describe.go):
#   TEMPLATE_URL=http://localhost:8080 TEMPLATE_TOKEN=... make record
# It asks before replacing anything (RECORD_FLAGS=-yes doesn't).
record:
	go run ./tools/record $(RECORD_FLAGS)
