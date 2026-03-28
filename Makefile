GO := PATH=/usr/local/go/bin:$$PATH go

.PHONY: tidy build test run docker checkpoint-test checkpoint-migrate gateway-build gateway-test gateway-run

tidy:
	$(GO) mod tidy

build: tidy
	$(GO) build ./...

test:
	$(GO) test ./...

checkpoint-test:
	$(GO) test ./pkg/checkpoint/... -v

checkpoint-migrate:
	$(GO) run ./cmd/checkpoint migrate

run:
	$(GO) run ./cmd/agent serve

gateway-build:
	$(GO) build ./cmd/gateway

gateway-test:
	$(GO) test ./pkg/gateway/...

gateway-run:
	$(GO) run ./cmd/gateway

docker:
	docker build -t deerflow-go:local .
