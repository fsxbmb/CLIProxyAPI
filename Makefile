BINARY := cliproxy-lite
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: build test vet clean install

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/cliproxy-lite

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -d "$(HOME)/bin"
	install -m 0755 bin/$(BINARY) "$(HOME)/bin/$(BINARY)"

clean:
	rm -rf bin dist coverage.out
