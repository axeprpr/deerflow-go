GO := PATH=/usr/local/go/bin:$$PATH go

.PHONY: tidy build test run docker docker-run clean

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o bin/deerflow ./cmd/langgraph

test:
	$(GO) test -v -cover ./...

run: build
	./bin/deerflow

docker:
	docker build -t deerflow-go .

docker-run:
	docker run -p 8080:8080 --env-file .env deerflow-go

clean:
	rm -rf bin/
