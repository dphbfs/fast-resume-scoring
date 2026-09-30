BIN := bin/extract

.PHONY: all build test race vet wire wire-check lint eval clean

all: wire-check vet test build

build:
	go build -o $(BIN) ./cmd/extract

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# Regenerate cmd/extract/wire_gen.go after any constructor signature change.
wire:
	go tool wire ./cmd/extract

wire-check:
	go tool wire check ./cmd/extract

lint:
	golangci-lint run ./...

# Runs the golden set against live Jev (needs TYPESAFE_API_KEY). Not part of
# `go test`. Scoring is not implemented yet.
eval:
	@test -n "$$TYPESAFE_API_KEY" || (echo "eval: TYPESAFE_API_KEY is not set" >&2; exit 1)
	@echo "eval: not implemented yet" >&2; exit 1

clean:
	rm -rf bin
