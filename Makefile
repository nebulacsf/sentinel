BINARY      := sentinel
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS     := -s -w -X github.com/nebulacsf/sentinel/internal/version.Version=$(VERSION)
GO          ?= go

.PHONY: all build run test race vet fmt install clean

all: build

build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) .

run: build
	./$(BIN_DIR)/$(BINARY)

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

install:
	CGO_ENABLED=0 $(GO) install -trimpath -ldflags "$(LDFLAGS)" .

clean:
	rm -rf $(BIN_DIR)
