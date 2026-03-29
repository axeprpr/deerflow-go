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

### 🛠️ Tool System
- **Built-in Tools**: bash, file_read, file_write, file_search, web_search, visit_page
- **Sandbox Isolation**: Secure execution via Bubblewrap + Landlock (Linux)
- **MCP Support**: Connect to Model Context Protocol servers

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
# Edit .env and add your SILICONFLOW_API_KEY
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

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `SILICONFLOW_API_KEY` | LLM API key | Required |
| `DEFAULT_LLM_MODEL` | Default model | `qwen/Qwen3.5-9B` |
| `POSTGRES_URL` | Postgres connection | Optional |
| `PORT` | Server port | `8080` |
| `LOG_LEVEL` | Log level | `info` |

## License

MIT
