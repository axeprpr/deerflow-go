GO := PATH=/usr/local/go/bin:$$PATH go
UI_DIR ?=

.PHONY: tidy build build-ui build-release release test run docker docker-run clean

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o bin/deerflow ./cmd/langgraph

build-ui:
	DEERFLOW_UI_DIR="$(UI_DIR)" ./scripts/build_ui.sh

build-release: build-ui
	mkdir -p bin
	$(GO) build -o bin/deerflow ./cmd/langgraph

release:
	DEERFLOW_UI_DIR="$(UI_DIR)" ./scripts/release.sh

test:
	$(GO) test -v -cover ./...

run: build
	./bin/deerflow

docker:
	docker build -t deerflow-go .

docker-run:
	docker run -p 8080:8080 --env-file .env deerflow-go

clean:
	rm -rf bin/ dist/ .tmp/
