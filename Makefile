GO := PATH=/usr/local/go/bin:$$PATH go

.PHONY: tidy build test run docker checkpoint-test checkpoint-migrate

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

docker:
	docker build -t deerflow-go:local .
