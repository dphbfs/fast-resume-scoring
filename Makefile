BIN := bin/extract
# Load local API keys when present (gitignored).
ENV := set -a; [ -f .env ] && . ./.env; set +a;

.PHONY: all build test race vet wire wire-check lint eval eval-checker clean

all: wire-check vet test build

build:
	go build -o $(BIN) ./cmd/extract
	go build -o bin/check ./cmd/check

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# Regenerate cmd/extract/wire_gen.go after any constructor signature change.
wire:
	go tool wire ./cmd/extract ./cmd/check ./cmd/eval

wire-check:
	go tool wire check ./cmd/extract ./cmd/check ./cmd/eval

lint:
	golangci-lint run ./...

# Runs the golden set against live Jev and the generative model, and writes
# eval/reports/<timestamp>.{json,md}. Not part of `go test`. Pass flags with
# EVAL_ARGS, e.g. make eval EVAL_ARGS="-only 01a0eeca -parallel 2".
eval:
	go build -o bin/eval ./cmd/eval
	@$(ENV) test -n "$$TYPESAFE_API_KEY" || (echo "eval: TYPESAFE_API_KEY is not set (.env)" >&2; exit 1)
	@$(ENV) LOG_LEVEL=$${LOG_LEVEL:-warn} ./bin/eval $(EVAL_ARGS)

# Runs the Resume Checker pairs (testdata/checker) against live Jev and
# writes eval/reports/checker/<timestamp>.{json,md}.
eval-checker:
	go build -o bin/eval ./cmd/eval
	@$(ENV) test -n "$$TYPESAFE_API_KEY" || (echo "eval: TYPESAFE_API_KEY is not set (.env)" >&2; exit 1)
	@$(ENV) LOG_LEVEL=$${LOG_LEVEL:-warn} ./bin/eval -checker $(EVAL_ARGS)

clean:
	rm -rf bin
