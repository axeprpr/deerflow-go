# deerflow-go

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](./go.mod)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Build Status](https://github.com/axeprpr/deerflow-go/actions/workflows/ci.yml/badge.svg)](https://github.com/axeprpr/deerflow-go/actions/workflows/ci.yml)

`deerflow-go` is a Go-native DeerFlow runtime that preserves the LangGraph-style thread/run API while replacing the original Python + LangChain + multi-service stack with a single self-hostable Go backend.

Core difference from the original DeerFlow: `deerflow-go` keeps the protocol shape and DeerFlow UI compatibility goals, but swaps LangGraph/LangChain internals for a custom Go agent core, tool registry, memory service, and Linux sandbox pipeline.

## Features

### LangGraph-compatible runs and threads

The server exposes thread, run, state, history, clarification, upload, artifact, and SSE streaming endpoints under the same shape that DeerFlow-style frontends expect.

```bash
curl -N \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/runs/stream \
  -d '{
    "input": {
      "messages": [
        {"role": "human", "content": "Research the key features of deerflow-go."}
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

### Go-native agent core

- Custom ReAct-style runtime instead of LangChain/LangGraph execution internals
- Thread-scoped checkpoint persistence with PostgreSQL fallback to local file storage
- Native SSE event streaming for token chunks, tool calls, clarifications, and subagent lifecycle events
- Optional Bearer auth with `DEERFLOW_AUTH_TOKEN`

### Subagents and delegated work

- Built-in subagent pool with bounded concurrency and task lifecycle events
- `task` delegation tool for background work
- Agent profiles for general, research, code, and analysis workflows

```json
{
  "description": "Compare deployment paths",
  "prompt": "List the differences between Docker deployment and source deployment in this repository.",
  "subagent_type": "general-purpose"
}
```

### Memory and checkpointing

- LLM-based memory extraction and durable memory snapshots
- Gateway endpoints to read, replace, delete, and edit memory state
- PostgreSQL-backed checkpoint store for thread continuity

```bash
curl http://localhost:8080/api/memory
```

### Tool system with sandbox isolation

- Built-in tools for shell, files, web access, image viewing, artifacts, clarification, ACP, skills, and subagents
- JSON-schema-based tool registration and validation
- Per-thread sandbox execution with Landlock when available, Bubblewrap fallback on Linux
- MCP tool import through Go clients instead of `langchain-mcp-adapters`

```go
registry := tools.NewRegistry()
registry.Register(models.Tool{
    Name:        "my_tool",
    Description: "Example tool",
    InputSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "query": map[string]any{"type": "string"},
        },
        "required": []string{"query"},
    },
    Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
        return models.ToolResult{CallID: call.ID, ToolName: call.Name, Status: models.CallStatusCompleted, Content: "ok"}, nil
    },
})
```

### Gateway extensions beyond baseline LangGraph routes

- `POST /api/tts` for OpenAI-compatible speech synthesis
- `GET /api/models` for model catalog discovery
- `GET/PUT /api/mcp/config` for MCP runtime configuration
- `GET/PUT/DELETE /api/memory` for editable gateway memory
- upload HEAD probing for frontend UX
- skill discovery and `.skill` installation endpoints

## Quick Start

Run the backend in 3 steps.

### 1. Configure environment

```bash
git clone https://github.com/axeprpr/deerflow-go.git
cd deerflow-go
cp .env.example .env
```

Set at least one model key in `.env`:

```bash
SILICONFLOW_API_KEY=sk-your-key-here
DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B
```

### 2. Start with Docker

```bash
docker compose up -d --build api db
```

If you also have `../deerflow-ui/frontend`, you can start the optional UI service too:

```bash
docker compose up -d --build api db ui
```

### 3. Verify health

```bash
curl -fsS http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```

## Deployment Guide

### Docker single-command deployment

Current `docker-compose.yml` ships with:

- `api`: Go API server on `:8080`
- `db`: PostgreSQL 16 for checkpoints and memory
- `ui`: optional DeerFlow UI dev container mounted from `../deerflow-ui/frontend`

Minimum deployment:

```bash
cp .env.example .env
docker compose up -d --build api db
```

Optional full stack with adjacent frontend checkout:

```bash
docker compose up -d --build api db ui
```

For ephemeral mode, leave `DATABASE_URL` unset and the server runs in memory only.

### 4. Connect Frontend

Set `NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://localhost:8080` in your deerflow-ui `.env.local`

## Embedded UI

`cmd/langgraph` can serve an embedded `deerflow-ui` bundle directly from the Go binary.

- Recommended layout: `third_party/deerflow-ui/frontend`
- Build embedded assets: `make build-ui`
- Build binary with embedded UI: `make build-release`
- Override source path: `make build-release UI_DIR=/abs/path/to/deerflow-ui/frontend`

When embedded assets are present, the binary serves the UI at `/` and the LangGraph-compatible API at `/api/langgraph`.

## Release

Build local release archives:

```bash
make release
```

Artifacts are written to `dist/`:

- `deerflow-go_<version>_linux_amd64.tar.gz`
- `deerflow-go_<version>_windows_amd64.zip`
- `checksums.txt`

GitHub Actions release automation is defined in:

- `.github/workflows/release.yml`

It supports:

- manual runs via `workflow_dispatch`
- tag-triggered builds for tags like `v0.1.0`

On tag builds, the workflow also uploads the archives to the GitHub Release.

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/runs/stream` | POST | Stream run execution |
| `/threads` | GET/POST | List/create threads |
| `/threads/{id}` | GET/PUT/DELETE | Thread CRUD |
| `/threads/{id}/history` | GET | Get message history |
| `/threads/{id}/files` | GET | Get presented files |
| `/threads/{id}/state` | GET/PATCH | Thread state |
| `/threads/{id}/clarifications` | GET/POST | Clarification |
| `/health` | GET | Health check |

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `SILICONFLOW_API_KEY` | LLM API key | Required |
| `OPENAI_API_KEY` | OpenAI-compatible provider API key | Optional |
| `OPENAI_API_BASE_URL` | OpenAI-compatible provider base URL | Optional |
| `ANTHROPIC_API_KEY` | Anthropic gateway API key | Optional |
| `DEFAULT_LLM_MODEL` | Default model | `qwen/Qwen3.5-9B` |
| `DEERFLOW_MODELS` | Optional model catalog for `/api/models`; accepts comma-separated names or JSON array | Optional |
| `DEERFLOW_SKILLS_DIRS` | Additional skill roots, comma-separated | Optional |
| `DEERFLOW_MCP_CONFIG` | Optional JSON for `/api/mcp/config` | Optional |
| `DEERFLOW_MEMORY_CONFIG` | Optional JSON for `/api/memory/config` | Optional |
| `DEERFLOW_MEMORY_PATH` | Override memory storage path | Optional |
| `DEERFLOW_CHANNELS_CONFIG` | Optional JSON for `/api/channels` | Optional |
| `DATABASE_URL` | Database URL (`sqlite:///...` or `postgres://...`) | Optional |
| `POSTGRES_URL` | Legacy Postgres connection fallback | Optional |
| `PORT` | Server port | `8080` |
| `LOG_LEVEL` | Log level | `info` |

Examples:

Health check:

```bash
curl -fsS http://localhost:8080/health
```

Smoke test:

```bash
curl -N \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/runs/stream \
  -d '{"input":{"messages":[{"role":"human","content":"hello"}]}}'
```

### Source deployment

```bash
cp .env.example .env
make build
./bin/deerflow --addr :8080
```

With explicit model override:

```bash
./bin/deerflow --addr :8080 --model "qwen/Qwen3.5-9B"
```

With optional Bearer auth:

```bash
DEERFLOW_AUTH_TOKEN=change-me ./bin/deerflow --addr :8080
```

### Required and useful environment variables

| Variable | Required | Description |
| --- | --- | --- |
| `SILICONFLOW_API_KEY` | Usually yes | Default provider key for the out-of-box model path |
| `OPENAI_API_KEY` | Optional | OpenAI-compatible provider key |
| `OPENAI_API_BASE_URL` | Optional | OpenAI-compatible base URL override |
| `ANTHROPIC_API_KEY` | Optional | Anthropic-compatible provider key |
| `DEFAULT_LLM_MODEL` | Recommended | Default model name used when requests do not override it |
| `POSTGRES_URL` | Optional | Enables durable checkpoint and memory persistence |
| `PORT` | Optional | HTTP port, default `8080` |
| `LOG_LEVEL` | Optional | Startup log verbosity |
| `DEERFLOW_AUTH_TOKEN` | Optional | Enables simple Bearer-token protection on the API |
| `TTS_API_KEY` | Optional | Key override for `POST /api/tts` |
| `TTS_API_BASE_URL` | Optional | Base URL override for TTS proxying |
| `TTS_MODEL` | Optional | TTS model override |
| `TTS_VOICE` | Optional | TTS voice override |

### Frontend integration

`deerflow-go` is designed to be consumed by DeerFlow-compatible frontends.

For `deerflow-ui`:

```bash
NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://localhost:8080
NEXT_PUBLIC_BACKEND_BASE_URL=http://localhost:8080
```

In the provided Compose file, the optional `ui` service already points to `http://api:8080` internally.

## API Documentation

### LangGraph API endpoints

These routes are exposed at both `/` and `/api/langgraph`.

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/health` | `GET` | Liveness and readiness check |
| `/runs` | `POST` | Create a non-streaming run |
| `/runs/stream` | `POST` | Create a streaming run over SSE |
| `/runs/{run_id}` | `GET` | Get run status |
| `/runs/{run_id}/stream` | `GET` | Subscribe to run events |
| `/runs/{run_id}/cancel` | `POST` | Cancel a running task |
| `/threads` | `GET`, `POST` | List or create threads |
| `/threads/search` | `POST` | Search thread metadata and artifacts |
| `/threads/{id}` | `GET`, `PUT`, `PATCH`, `DELETE` | Thread CRUD |
| `/threads/{id}/state` | `GET`, `PUT`, `PATCH` | Read or mutate thread state |
| `/threads/{id}/history` | `GET`, `POST` | Get or append history |
| `/threads/{id}/runs` | `GET`, `POST` | List or create thread-scoped runs |
| `/threads/{id}/runs/stream` | `POST` | Create thread-scoped streaming run |
| `/threads/{id}/runs/{run_id}/stream` | `GET` | Stream a thread-scoped run |
| `/threads/{id}/clarifications` | `GET`, `POST` | List or create clarification requests |
| `/threads/{id}/clarifications/{id}/resolve` | `POST` | Resolve a clarification |
| `/uploads` | `GET`, `POST`, `DELETE` | Upload lifecycle |
| `/artifacts/{path...}` | `GET` | Download generated artifacts |
| `/suggestions` | `POST` | Generate follow-up suggestions |

### Gateway API endpoints

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/models` | `GET` | List configured models |
| `/api/skills` | `GET` | List available skills |
| `/api/skills/{name}` | `GET`, `PUT` | Read or enable/disable a skill |
| `/api/skills/install` | `POST` | Install a `.skill` archive |
| `/api/agents` | `GET`, `POST` | List or create gateway agent profiles |
| `/api/agents/{name}` | `GET`, `PUT`, `DELETE` | Agent profile CRUD |
| `/api/channels` | `GET` | Inspect gateway channel status |
| `/api/channels/{name}/restart` | `POST` | Restart a configured channel |
| `/api/mcp/config` | `GET`, `PUT` | Read or update MCP config |
| `/api/memory` | `GET`, `PUT`, `DELETE` | Read, replace, or clear gateway memory |
| `/api/user-profile` | `GET`, `PUT` | User profile data |
| `/api/tts` | `POST` | OpenAI-compatible speech synthesis proxy |

## 与原版 DeerFlow 对比

以下结论基于对原版 DeerFlow 当前主干代码与 `deerflow-go` 实现的逐项对照。原版主干已经是 DeerFlow 2.0，但依然保留 Python、LangChain、LangGraph、多服务部署这些核心特征。

| 维度 | 原版 DeerFlow | deerflow-go |
| --- | --- | --- |
| 核心架构 | LangGraph Server + FastAPI Gateway + Next.js + nginx | 单 Go 运行时，LangGraph 兼容层直接内嵌在服务内 |
| 多 Agent / Subagent | LangChain agent + middleware + Python subagent executor | Go 原生 ReAct runtime + Go subagent pool |
| Memory | LangGraph store/checkpointer + middleware 队列更新 | Go LLM memory service + PostgreSQL/文件持久化 |
| Tool 系统 | LangChain `BaseTool`、community tools、MCP adapters | 自研 JSON Schema tool registry + Go MCP client |
| Sandbox | local provider / Docker AioSandboxProvider / provisioner | Landlock 优先，Bubblewrap 回退，线程目录隔离 |
| LangGraph API | 原生 LangGraph | 兼容实现，目标是前端与协议复用 |
| Gateway API | 模型、skills、memory、uploads、artifacts、suggestions 更完整 | 已覆盖核心路由，并增加简单 Bearer auth 与 TTS；部分 agents/channels 仍偏轻量 |
| 前端集成 | 官方 Next.js 前端 + nginx rewrite | 可直接对接 DeerFlow UI，也可只跑 API |
| 部署方式 | Python + Node + nginx + Docker，多进程 | Docker Compose 或单二进制，后端部署明显更简单 |
| 依赖与性能 | 生态成熟但依赖重、进程多 | 后端依赖轻、冷启动和资源占用更友好，但生态适配面更窄 |

结论：

- `deerflow-go` 不是把原版逐行翻译成 Go，而是保留协议兼容目标后重新做了 runtime。
- 如果你要的是原版最完整的生态能力，Python 版仍然更成熟。
- 如果你要的是更容易私有部署、更容易裁剪、后端链路更短的 DeerFlow 形态，`deerflow-go` 更合适。
- 详细分析见生成报告 `/tmp/deerflow-comparison.md`。

## Development Guide

### Local development

```bash
make tidy
make build
make run
```

### Run tests

```bash
make test
```

### Useful binaries

- `./cmd/langgraph`: main LangGraph-compatible HTTP server
- `./cmd/checkpoint`: checkpoint migrations and inspection helpers
- `./cmd/memory`: memory migrations and one-shot memory update tooling
- `./cmd/agent`: lower-level agent runtime entrypoint

### Debugging notes

- Thread and run compatibility lives in `pkg/langgraphcompat`
- Tool registration and execution live in `pkg/tools`
- Memory extraction and storage live in `pkg/memory`
- Sandbox backends live in `pkg/sandbox`
- Subagent orchestration lives in `pkg/subagent`

## Architecture

```text
                    +------------------------------+
                    | DeerFlow UI / API Clients    |
                    +---------------+--------------+
                                    |
                                    v
                    +------------------------------+
                    | LangGraph Compatibility API  |
                    | threads, runs, SSE, uploads  |
                    +---------------+--------------+
                                    |
          +-------------------------+-------------------------+
          |                         |                         |
          v                         v                         v
 +------------------+   +---------------------+   +------------------+
 | Go Agent Core    |   | Gateway Extensions  |   | Checkpoints      |
 | ReAct loop       |   | models/memory/tts   |   | Postgres or file |
 | tool execution   |   | skills/mcp/channels |   | persistence      |
 +---------+--------+   +----------+----------+   +------------------+
           |                       |
           v                       v
 +------------------+   +---------------------+
 | Tool Registry    |   | Memory Service      |
 | JSON Schema      |   | extract/summarize   |
 | MCP + built-ins  |   | inject durable ctx  |
 +---------+--------+   +---------------------+
           |
           v
 +------------------+     +-------------------+
 | Sandbox          |<--->| Subagent Pool     |
 | Landlock/Bwrap   |     | bounded parallel  |
 | per-thread FS    |     | delegated tasks   |
 +------------------+     +-------------------+
```

## License

MIT
