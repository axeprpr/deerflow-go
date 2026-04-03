# deerflow-go

A Go reimplementation of [DeerFlow](https://github.com/bytedance/deerflow) - a multi-agent research framework with LangChain-compatible API.

## Features

### 🔄 Multi-Agent System
- **Subagent Pool**: Spawn background agents for parallel task execution
- **Task Tool**: Delegate subtasks with `description`, `prompt`, and `subagent_type`
- **Task Events**: Real-time SSE events (`task_started`, `task_running`, `task_completed/failed`)

### 🧠 Memory & Context
- **LLM-based Memory Service**: Automatically summarize and maintain conversation context
- **Long-term Memory**: Persistent memory across sessions via Postgres checkpointing
- **Gateway Memory Editing**: Read and replace memory snapshots through `/api/memory`

### 🛠️ Tool System
- **Built-in Tools**: bash, file_read, file_write, file_search, web_search, visit_page
- **Sandbox Isolation**: Secure execution via Bubblewrap + Landlock (Linux)
- **MCP Support**: Connect to Model Context Protocol servers

### 🔊 Gateway UX
- **Text-to-Speech Gateway**: `POST /api/tts` proxies OpenAI-compatible speech synthesis for reading reports aloud
- **Upload Probing**: `HEAD /api/threads/{id}/uploads/{filename}` returns headers without downloading the file body
- **Broader Thread Search**: Search now indexes workspace config and log files such as `.env`, `.toml`, `.cfg`, and `.log`

### 📊 Present Files
- Track and display generated artifacts
- Auto MIME type detection
- Support text and binary files

### ❓ Clarification
- Agent can request user clarification
- Support choice/text/confirm types
- Async resolution via SSE events

### 🤖 Agent Types
Pre-configured agent profiles:

| Type | Description | Default Tools |
|------|-------------|---------------|
| `general-purpose` | Balanced assistant | bash, file |
| `researcher` | Research tasks | web_search, file_read |
| `coder` | Code generation | bash, file_write |
| `analyst` | Data analysis | file_read, python |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Frontend (UI)                      │
└─────────────────────────┬───────────────────────────────┘
                          │ LangGraph API (SSE)
                          ▼
┌─────────────────────────────────────────────────────────┐
│                 LangGraph Compat Layer                  │
│  ┌──────────┐ ┌──────────┐ ┌───────────────────────┐  │
│  │ Threads  │ │   Runs   │ │   Clarifications      │  │
│  └──────────┘ └──────────┘ └───────────────────────┘  │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                       Agent Core                         │
│  ┌────────────┐ ┌────────────┐ ┌────────────────────┐  │
│  │   ReAct    │ │   Tools    │ │   Subagent Pool    │  │
│  │   Loop     │ │  Registry  │ │                    │  │
│  └────────────┘ └────────────┘ └────────────────────┘  │
└─────────────────────────┬───────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        ▼                 ▼                 ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│  LLM Layer   │ │ Memory Svc   │ │  Sandbox     │
│  (OpenAI/    │ │              │ │ (Bubblewrap) │
│  SiliconFlow)│ │              │ │              │
└──────────────┘ └──────────────┘ └──────────────┘
```

## Quick Start

### 1. Clone & Build

```bash
git clone https://github.com/axeprpr/deerflow-go.git
cd deerflow-go
make build
```

### 2. Configure

```bash
cp .env.example .env
# Edit .env and add the provider key you use:
# SILICONFLOW_API_KEY=
# OPENAI_API_KEY=
# OPENAI_API_BASE_URL=
# ANTHROPIC_API_KEY=
```

### 3. Run

```bash
# With Docker
docker-compose up -d

# Or directly
./bin/langgraph --addr :8080 --model "qwen/Qwen3.5-9B"
```

### 4. Connect Frontend

Set `NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://localhost:8080` in your deerflow-ui `.env.local`

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
| `/api/tts` | POST | Generate speech audio from text |

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `SILICONFLOW_API_KEY` | LLM API key | Required |
| `OPENAI_API_KEY` | OpenAI-compatible provider API key | Optional |
| `OPENAI_API_BASE_URL` | OpenAI-compatible provider base URL | Optional |
| `TTS_API_KEY` | Override API key for `/api/tts` | Optional |
| `TTS_API_BASE_URL` | Override base URL for `/api/tts` | `OPENAI_API_BASE_URL` or OpenAI |
| `TTS_MODEL` | Override model for `/api/tts` | `gpt-4o-mini-tts` |
| `TTS_VOICE` | Override voice for `/api/tts` | `alloy` |
| `ANTHROPIC_API_KEY` | Anthropic gateway API key | Optional |
| `DEFAULT_LLM_MODEL` | Default model | `qwen/Qwen3.5-9B` |
| `DEERFLOW_TITLE_ENABLED` | Enable automatic thread title generation | `true` |
| `DEERFLOW_TITLE_MAX_WORDS` | Max words for generated thread titles (1-20) | `6` |
| `DEERFLOW_TITLE_MAX_CHARS` | Max characters for generated thread titles (10-200) | `60` |
| `DEERFLOW_TITLE_MODEL` | Optional model override for title generation | unset |
| `DEERFLOW_MODELS` | Optional model catalog, e.g. `gpt-5=openai/gpt-5,claude=anthropic/claude-3-7-sonnet` | Optional |
| `DEERFLOW_MODELS_JSON` | Optional JSON model catalog for `/api/models` metadata | Optional |
| `POSTGRES_URL` | Postgres connection | Optional |
| `PORT` | Server port | `8080` |
| `LOG_LEVEL` | Log level | `info` |

## License

MIT
