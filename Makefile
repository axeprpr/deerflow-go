GO := PATH=/usr/local/go/bin:$$PATH go

.PHONY: tidy build test run docker

tidy:
	$(GO) mod tidy

build: tidy
	$(GO) build ./...

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/agent serve

docker:
	docker build -t deerflow-go:local .
