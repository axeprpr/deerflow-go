# Architecture

## Positioning

`deerflow-go` is a compatibility-oriented Go runtime for DeerFlow.

It keeps the upstream frontend and protocol shape, but it does not reuse the upstream Python gateway or LangGraph/LangChain harness. Instead, it implements a Go-native backend with its own runtime loop and compatibility layer.

## Upstream vs Go

Upstream DeerFlow is split into three backend-facing layers:

- `frontend`: Next.js UI
- `backend/app/gateway`: FastAPI gateway for `/api/*`
- `backend/packages/harness/deerflow`: LangGraph/LangChain runtime, middleware, memory, sandbox, skills, MCP

`deerflow-go` compresses the backend into one Go service:

- `cmd/langgraph`: bootstrap
- `pkg/langgraphcompat`: gateway compatibility and LangGraph-style thread/run API
- `pkg/agent`: custom ReAct-style runtime loop
- `pkg/llm`: provider and streaming adapter layer
- `pkg/tools`: built-in tool registry and execution
- `pkg/memory`: durable memory model and storage
- `pkg/sandbox`: execution isolation

## Layering

### 1. Compat HTTP layer

`pkg/langgraphcompat` is the boundary the frontend sees.

It exposes:

- `/api/*` gateway endpoints
- `/api/langgraph/*` and plain LangGraph-style thread/run endpoints
- SSE stream and reconnect/join semantics
- uploads, artifacts, suggestions, and compatibility state

This layer exists because upstream DeerFlow expects both a gateway API and a LangGraph-style runtime API.

### 2. Runtime layer

`pkg/agent` is the actual execution core.

It is not LangGraph. It is a custom Go loop that:

- assembles prompt + tools + thread context
- calls the model
- executes tool calls
- handles clarification/present-files/subagent flow
- emits runtime events back to the compatibility layer

This is the main structural difference from upstream.

### 3. Model / tool layer

`pkg/llm` and `pkg/tools` provide the runtime substrate:

- provider adapters and stream handling
- tool schemas and tool-call normalization
- built-in file, shell, presentation, clarification, ACP, and subagent tools

The runtime depends on this layer, but this layer is independent from the HTTP compatibility surface.

### 4. Execution services

`pkg/memory` and `pkg/sandbox` hold longer-lived execution concerns:

- memory extraction, persistence, and injection
- sandbox creation and command/file isolation

These are conceptually parallel to upstream harness subsystems, but implemented with different boundaries.

## Advantages

- One deployable backend instead of a Python gateway plus Python runtime stack
- Clearer ownership over runtime behavior, tool semantics, and streaming events
- Easier to align specific frontend-visible behaviors without depending on LangGraph internals
- Simpler self-hosting story

## Tradeoffs

- Behavior parity must be maintained manually instead of inherited from LangGraph/LangChain
- Upstream middleware semantics have to be re-modeled in custom runtime code
- Compatibility work concentrates in `pkg/langgraphcompat`, which needs active discipline to avoid becoming a monolith again

## Current structure after cleanup

The compatibility layer is now split by responsibility:

- `run_handlers.go` / `run_service.go`
- `thread_state_service.go` / `thread_query_service.go`
- `gateway_admin_handlers.go`
- `gateway_runtime_handlers.go`
- `gateway_catalog_service.go`
- `gateway_compat_fs_service.go`
- `memory_handlers.go` / `memory_service.go`
- `suggestions_handlers.go`

This is closer to upstream's router/service split, while still remaining a single Go process.

