BINARY    := aat
CMD       := ./cmd/aat
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -X github.com/gburgyan/aat/internal/version.Version=$(VERSION) \
             -X github.com/gburgyan/aat/internal/version.GitCommit=$(COMMIT) \
             -X github.com/gburgyan/aat/internal/version.BuildDate=$(DATE)

.PHONY: build test test-race lint check clean frontend

build: frontend
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

frontend:
	cd server/web && npm install && npm run build

test: frontend
	go test ./...

test-race: frontend
	go test -race ./...

lint:
	golangci-lint run --timeout 5m

check: test-race lint

clean:
	rm -f $(BINARY)
	rm -rf server/web/dist server/web/node_modules
