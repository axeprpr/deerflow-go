# deerflow-go Integration Guide

## Overview

`deerflow-go` is a Go reimplementation of DeerFlow focused on a lightweight, self-hosted agent runtime. It exposes a LangGraph-compatible HTTP API, streams execution over Server-Sent Events (SSE), supports built-in tools for shell and file operations, and can persist thread checkpoints in PostgreSQL.

Core characteristics:

- LangGraph-compatible thread and run endpoints
- Streaming execution via SSE
- Built-in tools for shell, files, clarification, artifacts, and subagents
- Multiple agent profiles optimized for general, research, coding, and analysis workflows
- Optional PostgreSQL persistence for checkpoints
- Compatible with `deerflow-ui` via `NEXT_PUBLIC_LANGGRAPH_BASE_URL`

## Quick Start

This path gets a local server running in about five minutes.

### 1. Prerequisites

- Go 1.23+
- A SiliconFlow API key
- Optional: PostgreSQL 16+ for checkpoint persistence

### 2. Configure environment

Create a local `.env` from the example and update the API key:

```bash
cp .env.example .env
```

Minimum required variables:

```bash
export SILICONFLOW_API_KEY=sk-your-key-here
export DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B
export PORT=8080
```

Optional persistence:

```bash
export POSTGRES_URL=postgres://postgres:password@localhost:5432/deerflow?sslmode=disable
```

### 3. Build and run

```bash
make build
make run
```

Or run directly:

```bash
go run ./cmd/langgraph --addr :8080
```

The server listens on `http://localhost:8080` by default.

### 4. Verify health

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{
  "status": "ok"
}
```

### 5. Start a streaming run

```bash
curl -N \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/runs/stream \
  -d '{
    "input": {
      "messages": [
        {"role": "human", "content": "Summarize what deerflow-go does."}
      ]
    },
    "config": {
      "configurable": {
        "model_name": "qwen/Qwen3.5-9B",
        "agent_type": "general-purpose"
      }
    }
  }'
```

## Configuration

The server can be configured through environment variables. `cmd/langgraph` reads `PORT`, `POSTGRES_URL`, and `DEFAULT_LLM_MODEL` directly; the LLM provider reads its own API credentials from the environment.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `SILICONFLOW_API_KEY` | Yes for SiliconFlow | none | API key used by the default `siliconflow` provider. |
| `POSTGRES_URL` | No | empty | PostgreSQL connection string for checkpoint persistence. When unset, state is in-memory only. |
| `DEFAULT_LLM_MODEL` | No | `qwen/Qwen3.5-9B` for `cmd/langgraph` | Default model passed to runs when the request does not override `config.configurable.model_name`. |
| `PORT` | No | `8080` | HTTP port used by `cmd/langgraph`. |
| `ADDR` | No | derived from `PORT` | Optional full listen address override such as `0.0.0.0:8080` or `:8080`. |
| `LOG_LEVEL` | No | empty | Logged at startup for deployment consistency. Current runtime logging still uses Go's standard logger. |
| `DEFAULT_LLM_PROVIDER` | No | `siliconflow` from server construction | Provider fallback used by `pkg/llm` when a caller does not pass a provider name explicitly. |
| `OPENAI_API_KEY` | Only for OpenAI | none | API key when using the OpenAI-compatible provider path. |
| `OPENAI_API_BASE_URL` | No | provider-specific | Optional OpenAI-compatible base URL override. |
| `ANTHROPIC_API_KEY` | Only for Anthropic gateway usage | none | API key when using the Anthropic provider via an OpenAI-compatible endpoint. |
| `SANDBOX_ENABLED` | No | implementation-defined | Reserved deployment variable for sandbox-related rollout policy. |
| `SANDBOX_TIMEOUT` | No | implementation-defined | Reserved deployment variable for sandbox timeout policy. |

Example:

```bash
export SILICONFLOW_API_KEY=sk-xxx
export POSTGRES_URL=postgres://postgres:password@localhost:5432/deerflow?sslmode=disable
export DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B
export PORT=8080
export LOG_LEVEL=info
```

## API Reference

The server exposes LangGraph-compatible endpoints implemented in `pkg/langgraphcompat`.

### Streaming Event Types

`POST /runs/stream` and `POST /threads/{thread_id}/runs/stream` return SSE with event names observed in the current implementation:

- `metadata`
- `chunk`
- `messages-tuple`
- `tool_call`
- `tool_call_start`
- `tool_call_end`
- `clarification_request`
- `task_*` lifecycle events emitted by subagents
- `updates`
- `values`
- `end`
- `error`

### GET /health

Health check endpoint.

**Request:**

```bash
curl http://localhost:8080/health
```

**Response:**

```json
{
  "status": "ok"
}
```

### POST /runs/stream

Stream a run execution. If `thread_id` is omitted, the server creates one automatically.

**Request:**

```json
{
  "thread_id": "optional-thread-id",
  "input": {
    "messages": [
      {"role": "human", "content": "Hello"}
    ]
  },
  "config": {
    "configurable": {
      "model_name": "qwen/Qwen3.5-9B",
      "agent_type": "general-purpose"
    }
  }
}
```

**Response:** Server-Sent Events

- `metadata`
- `chunk`
- `messages-tuple`
- `tool_call`
- `tool_call_start`
- `tool_call_end`
- `clarification_request`
- `updates`
- `values`
- `end`
- `error`

Example:

```bash
curl -N \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/runs/stream \
  -d '{
    "input": {
      "messages": [{"role": "human", "content": "Hello"}]
    },
    "config": {
      "configurable": {
        "model_name": "qwen/Qwen3.5-9B",
        "agent_type": "general-purpose"
      }
    }
  }'
```

### GET /runs/{run_id}

Get run metadata.

**Request:**

```bash
curl http://localhost:8080/runs/<run_id>
```

**Response:**

```json
{
  "run_id": "8ac9d7f1-...",
  "thread_id": "df3a8ab9-...",
  "assistant_id": "",
  "status": "success",
  "created_at": "2026-03-29T10:00:00Z",
  "updated_at": "2026-03-29T10:00:04Z"
}
```

### GET /runs/{run_id}/stream

Replay recorded SSE events for a run.

**Request:**

```bash
curl -N http://localhost:8080/runs/<run_id>/stream
```

**Response:** Server-Sent Events

### POST /threads

Create a thread.

**Request:**

```json
{
  "thread_id": "optional-thread-id",
  "metadata": {
    "title": "Demo thread"
  }
}
```

**Response:**

```json
{
  "thread_id": "optional-thread-id",
  "created_at": "2026-03-29T10:00:00Z",
  "updated_at": "2026-03-29T10:00:00Z",
  "metadata": {
    "title": "Demo thread"
  },
  "status": "idle",
  "config": {
    "configurable": {
      "agent_type": ""
    }
  },
  "values": {
    "title": "Demo thread",
    "artifacts": []
  }
}
```

Example:

```bash
curl -H "Content-Type: application/json" \
  -X POST http://localhost:8080/threads \
  -d '{"metadata":{"title":"Demo thread"}}'
```

### GET /threads/{thread_id}

Fetch thread metadata.

**Request:**

```bash
curl http://localhost:8080/threads/<thread_id>
```

**Response:** Same shape as `POST /threads`.

### PATCH /threads/{thread_id}

Merge metadata into an existing thread.

**Request:**

```json
{
  "metadata": {
    "title": "Updated title",
    "owner": "frontend"
  }
}
```

**Response:** Same shape as `GET /threads/{thread_id}`.

### DELETE /threads/{thread_id}

Delete a thread.

**Request:**

```bash
curl -X DELETE http://localhost:8080/threads/<thread_id>
```

**Response:** `204 No Content`

### POST /threads/search

List threads with simple pagination and sorting.

**Request:**

```json
{
  "limit": 10,
  "offset": 0,
  "sort_by": "updated_at",
  "sort_order": "desc"
}
```

**Response:**

```json
[
  {
    "thread_id": "df3a8ab9-...",
    "created_at": "2026-03-29T10:00:00Z",
    "updated_at": "2026-03-29T10:00:04Z",
    "metadata": {
      "title": "Demo thread"
    },
    "status": "idle",
    "config": {
      "configurable": {
        "agent_type": "coder"
      }
    },
    "values": {
      "title": "Demo thread",
      "artifacts": []
    }
  }
]
```

### GET /threads/{thread_id}/files

List files registered with the `present_file` tool.

**Request:**

```bash
curl http://localhost:8080/threads/<thread_id>/files
```

**Response:**

```json
{
  "files": [
    {
      "id": "file_1",
      "path": "/app/output/report.md",
      "description": "Generated report",
      "mime_type": "text/markdown",
      "content": "IyBSZXBvcnQK",
      "created_at": "2026-03-29T10:00:04Z"
    }
  ]
}
```

### GET /threads/{thread_id}/state

Get current thread state.

**Request:**

```bash
curl http://localhost:8080/threads/<thread_id>/state
```

**Response:**

```json
{
  "checkpoint_id": "d4c7...",
  "values": {
    "messages": [
      {
        "type": "human",
        "id": "msg_1",
        "role": "human",
        "content": "Hello"
      }
    ],
    "title": "Demo thread",
    "artifacts": []
  },
  "next": [],
  "tasks": [],
  "metadata": {
    "thread_id": "df3a8ab9-...",
    "step": 0
  },
  "created_at": "2026-03-29T10:00:04Z"
}
```

### POST /threads/{thread_id}/state

Update state values. Current implementation supports setting `values.title`.

**Request:**

```json
{
  "values": {
    "title": "New thread title"
  }
}
```

**Response:** Same shape as `GET /threads/{thread_id}/state`.

### PATCH /threads/{thread_id}/state

Merge metadata into thread state.

**Request:**

```json
{
  "metadata": {
    "title": "Retitled thread",
    "owner": "api"
  }
}
```

**Response:** Same shape as `GET /threads/{thread_id}/state`.

### POST /threads/{thread_id}/history

Return thread state history. Current implementation returns the latest state snapshot.

**Request:**

```json
{
  "limit": 1
}
```

**Response:**

```json
[
  {
    "checkpoint_id": "d4c7...",
    "values": {
      "messages": [],
      "title": "Demo thread",
      "artifacts": []
    },
    "next": [],
    "tasks": [],
    "metadata": {
      "thread_id": "df3a8ab9-...",
      "step": 0
    },
    "created_at": "2026-03-29T10:00:04Z"
  }
]
```

### POST /threads/{thread_id}/runs/stream

Start a streaming run scoped to an existing thread.

**Request:**

```json
{
  "input": {
    "messages": [
      {"role": "human", "content": "Continue from previous context."}
    ]
  },
  "config": {
    "configurable": {
      "agent_type": "coder",
      "model_name": "qwen/Qwen3.5-9B"
    }
  }
}
```

**Response:** Server-Sent Events with the same event set as `POST /runs/stream`.

### GET /threads/{thread_id}/runs/{run_id}/stream

Replay a run stream for a specific thread.

**Request:**

```bash
curl -N http://localhost:8080/threads/<thread_id>/runs/<run_id>/stream
```

**Response:** Server-Sent Events

### GET /threads/{thread_id}/stream

Join the latest run stream for a thread and replay stored events.

**Request:**

```bash
curl -N http://localhost:8080/threads/<thread_id>/stream
```

**Response:** Server-Sent Events

### POST /threads/{thread_id}/clarifications

Create a clarification item manually.

**Request:**

```json
{
  "type": "choice",
  "question": "Which environment should I deploy to?",
  "options": [
    {"label": "Staging", "value": "staging"},
    {"label": "Production", "value": "production"}
  ],
  "default": "staging",
  "required": true
}
```

**Response:**

```json
{
  "id": "clar_1",
  "thread_id": "df3a8ab9-...",
  "type": "choice",
  "question": "Which environment should I deploy to?",
  "options": [
    {"label": "Staging", "value": "staging"},
    {"label": "Production", "value": "production"}
  ],
  "default": "staging",
  "required": true,
  "created_at": "2026-03-29T10:00:04Z"
}
```

### GET /threads/{thread_id}/clarifications/{id}

Fetch a clarification item.

**Request:**

```bash
curl http://localhost:8080/threads/<thread_id>/clarifications/<id>
```

**Response:** Same object shape as creation, with `answer` and `resolved_at` populated when resolved.

### POST /threads/{thread_id}/clarifications/{id}/resolve

Resolve a clarification item with a user answer.

**Request:**

```json
{
  "answer": "production"
}
```

**Response:**

```json
{
  "id": "clar_1",
  "thread_id": "df3a8ab9-...",
  "type": "choice",
  "question": "Which environment should I deploy to?",
  "answer": "production",
  "resolved_at": "2026-03-29T10:01:00Z",
  "created_at": "2026-03-29T10:00:04Z"
}
```

## Tool Reference

Built-in tools registered by the LangGraph-compatible server:

### `bash`

Execute shell commands through the sandbox execution path.

**Arguments:**

```json
{
  "command": "go test ./...",
  "timeout": 60
}
```

**Returns:** JSON containing `stdout`, `stderr`, and `exit_code`.

### `read_file`

Read a file from disk.

**Arguments:**

```json
{
  "path": "README.md",
  "limit": 4096
}
```

### `write_file`

Write a file to disk.

**Arguments:**

```json
{
  "path": "output/report.md",
  "content": "# Report"
}
```

### `glob`

List files matching a glob pattern.

**Arguments:**

```json
{
  "pattern": "pkg/**/*.go"
}
```

### `present_file`

Register a generated file as an artifact for the UI.

**Arguments:**

```json
{
  "path": "output/chart.png",
  "description": "Generated chart",
  "mime_type": "image/png"
}
```

### `ask_clarification`

Pause for user input when requirements are ambiguous.

**Arguments:**

```json
{
  "type": "choice",
  "question": "Which branch should I use?",
  "options": [
    {"label": "main", "value": "main"},
    {"label": "release", "value": "release"}
  ],
  "default": "main",
  "required": true
}
```

### `task`

Spawn a bounded subagent for delegated work.

**Arguments:**

```json
{
  "description": "Inspect the repository",
  "prompt": "List the API handlers and summarize them.",
  "subagent_type": "general-purpose",
  "max_turns": 4
}
```

Supported `subagent_type` values:

- `general-purpose`
- `bash`

## Agent Types

The server supports four built-in agent profiles.

### `general-purpose`

- Balanced default profile
- Best for chat, orchestration, and mixed workflows
- Default tools: unrestricted registry

### `researcher`

- Focused on reading, evidence gathering, and synthesis
- Default tools: `read_file`, `glob`, `present_file`, `ask_clarification`, `task`
- Lower temperature, higher turn budget

### `coder`

- Focused on code changes, debugging, and verification
- Default tools: `bash`, `read_file`, `write_file`, `glob`, `present_file`, `ask_clarification`, `task`
- Highest built-in turn budget

### `analyst`

- Focused on structured analysis and artifact generation
- Default tools: `read_file`, `write_file`, `glob`, `present_file`, `ask_clarification`

Pass the agent type through run configuration:

```json
{
  "config": {
    "configurable": {
      "agent_type": "coder"
    }
  }
}
```

Additional supported run configuration fields:

- `config.configurable.model_name`
- `config.configurable.reasoning_effort`
- `config.configurable.temperature`
- `config.configurable.max_tokens`

## Frontend Setup

`deerflow-ui` can connect to `deerflow-go` using the LangGraph-compatible API.

### Local UI configuration

Set the frontend API base URL:

```bash
export NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://localhost:8080
```

If the UI runs in Docker Compose, use the internal service name:

```bash
export NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://api:8080
```

### Expected compatibility points

- Streaming runs: `POST /runs/stream`
- Thread-scoped streaming: `POST /threads/{thread_id}/runs/stream`
- Thread management: `POST /threads`, `GET /threads/{thread_id}`, `PATCH /threads/{thread_id}`
- Artifact listing: `GET /threads/{thread_id}/files`
- Clarification flows: `POST /threads/{thread_id}/clarifications/...`

### Compose-based UI startup

The provided `docker-compose.yml` expects a sibling checkout of the UI at:

```text
../deerflow-ui/frontend
```

If your UI lives elsewhere, update the bind mount in the `ui` service.

## Deployment

### Docker

Build and run the API image:

```bash
docker build -t deerflow-go .
docker run --rm -p 8080:8080 --env-file .env deerflow-go
```

### Docker Compose

Bring up the API, PostgreSQL, and UI:

```bash
docker compose up --build
```

Notes:

- `SILICONFLOW_API_KEY` must be present in your shell or `.env`
- PostgreSQL data is stored in the `postgres_data` volume
- The UI service depends on a local checkout of `deerflow-ui`

### Kubernetes

Minimal deployment pattern:

1. Create a `Secret` containing `SILICONFLOW_API_KEY`.
2. Create a `ConfigMap` or env block for `DEFAULT_LLM_MODEL`, `PORT`, and `POSTGRES_URL`.
3. Deploy the API container with port `8080`.
4. Add a PostgreSQL service or point `POSTGRES_URL` at a managed database.
5. Expose the API through an ingress or internal service for `deerflow-ui`.

Example container env section:

```yaml
env:
  - name: PORT
    value: "8080"
  - name: DEFAULT_LLM_MODEL
    value: "qwen/Qwen3.5-9B"
  - name: POSTGRES_URL
    valueFrom:
      secretKeyRef:
        name: deerflow-go
        key: postgres-url
  - name: SILICONFLOW_API_KEY
    valueFrom:
      secretKeyRef:
        name: deerflow-go
        key: siliconflow-api-key
```

Recommended production settings:

- Run multiple API replicas behind a load balancer only if thread state is externalized appropriately
- Use PostgreSQL for durable checkpoint persistence
- Terminate TLS at ingress or the edge proxy
- Add readiness and liveness checks against `/health`
