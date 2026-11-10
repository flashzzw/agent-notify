.PHONY: all build test vet fmt clean package

DIST ?= dist
VERSION ?= dev
PYTHON ?= python3
LDFLAGS := -ldflags "-s -w -X github.com/flashzzw/agent-notify/internal/cli.Version=$(VERSION)"

all: test vet build

build:
	mkdir -p bin
	go build -trimpath $(LDFLAGS) -o bin/ ./cmd/agent-notify

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	go clean
	$(PYTHON) -c "import shutil; shutil.rmtree('bin', ignore_errors=True); shutil.rmtree('dist', ignore_errors=True)"

package:
	$(PYTHON) scripts/package.py --version "$(VERSION)" --output "$(DIST)"
