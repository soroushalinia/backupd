BINARY := backupd
CMD := ./cmd/backupd
VERSION := $(or $(shell git describe --tags --always 2>/dev/null),dev)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build install clean lint test cover tidy fmt run release

all: lint test build

build:
	go build $(LDFLAGS) -o bin/$(BINARY) $(CMD)

install:
	go install $(LDFLAGS) $(CMD)

clean:
	rm -rf bin/ dist/

lint:
	go vet ./...

test:
	go test ./... -count=1 -timeout=30s

cover:
	go test ./... -count=1 -coverprofile=coverage.out && go tool cover -html=coverage.out -o coverage.html

tidy:
	go mod tidy

fmt:
	gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')

run: build
	./bin/$(BINARY) list --config examples/backupd.yaml

release:
	@goreleaser release --clean
