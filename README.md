# deerflow-go

`deerflow-go` is a Go runtime that targets DeerFlow UI and LangGraph-style protocol compatibility while replacing the original Python gateway + LangGraph/LangChain harness with a self-hostable Go backend.

The goal is not a line-by-line port of upstream DeerFlow. The goal is a compatible external surface with a Go-native runtime core.

## Architecture

Upstream DeerFlow is layered as:

- `frontend`: Next.js UI
- `gateway`: FastAPI `/api/*`
- `harness/runtime`: Python LangGraph/LangChain agent execution

`deerflow-go` collapses those backend layers into one Go service:

- `frontend`: still uses upstream DeerFlow UI
- `compat layer`: exposes `/api/*` and `/api/langgraph/*`
- `runtime`: custom Go agent loop, tool system, memory, checkpointing, and sandbox

This keeps the frontend and protocol shape close to upstream while removing the original multi-service Python runtime.

## Layers

The repository is organized around four main layers:

- `cmd/langgraph`: process entrypoint and server bootstrap
- `pkg/langgraphcompat`: DeerFlow-compatible HTTP, thread/run lifecycle, gateway state, uploads, artifacts, and SSE
- `pkg/agent`, `pkg/llm`, `pkg/tools`: Go-native runtime loop, model adapters, and tool execution
- `pkg/memory`, `pkg/sandbox`: durable memory and execution isolation

Reference documents:

- [Architecture](docs/ARCHITECTURE.md)
- [API Diff](docs/API_DIFF.md)
