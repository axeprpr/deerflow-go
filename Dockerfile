FROM golang:1.25-alpine AS builder

WORKDIR /app
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
COPY third_party/x_text ./third_party/x_text
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /deerflow ./cmd/langgraph

FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /app
COPY --from=builder /deerflow ./deerflow

EXPOSE 8080

ENV PORT=8080
ENV DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B

CMD ["./deerflow"]
