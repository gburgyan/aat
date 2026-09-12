BINARY    := aat
CMD       := ./cmd/aat
SANDBOX   := aat-sandbox
SANDBOX_CMD := ./cmd/aat-sandbox
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE      := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -X github.com/gburgyan/aat/internal/version.Version=$(VERSION) \
             -X github.com/gburgyan/aat/internal/version.GitCommit=$(COMMIT) \
             -X github.com/gburgyan/aat/internal/version.BuildDate=$(DATE)

.PHONY: build cli sandbox example-shop demos test test-race lint fmt check clean frontend docs docs-serve

build: frontend sandbox
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# The aat binary without rebuilding the web UI. It embeds whatever
# server/web/dist/app holds, so from a clean checkout everything but `aat web` works.
cli:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# The demo API server; no frontend needed.
sandbox:
	go build -ldflags "$(LDFLAGS)" -o $(SANDBOX) $(SANDBOX_CMD)

# Runs examples/shop against a local sandbox, as the CI example-shop job does.
# Needs curl, jq, and free ports 8765 and 8766.
example-shop: cli sandbox
	scripts/example-shop.sh

# Regenerates the recordings and screenshots in docs/user/assets (and the MP4 and
# social preview in demos/out) against a fresh sandbox. Needs ttyd, ffmpeg,
# gifsicle, jq, nc, and the JetBrains Mono font, plus free ports 8765, 8766, and
# 9129; demos/run.sh installs the pinned VHS and Playwright itself.
demos: build
	demos/run.sh

frontend:
	cd server/web && npm install && npm run build

test: frontend
	go test ./...

test-race: frontend
	go test -race ./...

lint:
	golangci-lint run --timeout 5m

fmt:
	gofmt -s -w .

check: fmt test-race lint

# The docs site (mkdocs.yml over docs/user), built as the docs workflow does:
# broken links, anchors, and pages missing from the nav fail the build.
DOCS_VENV := .venv-docs

$(DOCS_VENV)/.installed: docs/requirements.txt
	python3 -m venv $(DOCS_VENV)
	$(DOCS_VENV)/bin/pip install --quiet -r docs/requirements.txt
	touch $@

docs: $(DOCS_VENV)/.installed
	$(DOCS_VENV)/bin/mkdocs build --strict

docs-serve: $(DOCS_VENV)/.installed
	$(DOCS_VENV)/bin/mkdocs serve

# Removes build output only. The frontend bundle is server/web/dist/app; the
# tracked server/web/dist/placeholder.txt stays, so go:embed still compiles.
clean:
	rm -f $(BINARY) $(SANDBOX)
	rm -rf server/web/dist/app server/web/node_modules site
