GO := PATH=/usr/local/go/bin:$$PATH go

.PHONY: tidy build build-release release test test-p0 run docker docker-run clean

tidy:
	$(GO) mod tidy

build:
	mkdir -p bin
	$(GO) build -o bin/deerflow ./cmd/langgraph

build-release: build

release:
	./scripts/release.sh

test:
	$(GO) test -v -cover ./...

test-p0:
	./scripts/run_df_feature_regression.sh

run: build
	./bin/deerflow

docker:
	docker build -t deerflow-go .

docker-run:
	docker run -p 8080:8080 --env-file .env deerflow-go

clean:
	rm -rf bin/ dist/ .tmp/
