GO ?= go

.PHONY: run test build fmt
run:
	$(GO) run -buildvcs=false ./cmd/server
test:
	$(GO) test -race ./...
build:
	$(GO) build -buildvcs=false -o researchos ./cmd/server
fmt:
	$(GO) fmt ./...
