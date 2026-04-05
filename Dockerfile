FROM node:24-alpine AS ui-builder

WORKDIR /app

RUN apk --no-cache add bash

COPY . .
RUN if [ -d third_party/deerflow-ui/frontend ]; then ./scripts/build_ui.sh; else echo "skip embedded ui build"; fi

FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui-builder /app/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o /deerflow ./cmd/langgraph

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /app
COPY --from=builder /deerflow ./deerflow

EXPOSE 8080

ENV PORT=8080
ENV DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B

CMD ["./deerflow"]
