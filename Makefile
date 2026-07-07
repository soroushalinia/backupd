BINARY := backupd
CMD := ./cmd/backupd

.PHONY: all build clean lint test tidy fmt install run

all: lint test build

build:
	go build -o bin/$(BINARY) $(CMD)

install:
	go install $(CMD)

clean:
	rm -rf bin/

lint:
	go vet ./...

test:
	go test ./... -count=1 -timeout=30s

tidy:
	go mod tidy

fmt:
	gofmt -l -w $$(find . -name '*.go' -not -path './vendor/*')

run: build
	./bin/$(BINARY) list --config examples/backupd.yaml
