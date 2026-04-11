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

This package is now split by concern instead of centered on a few large files:

- HTTP handlers:
  - `thread_handlers.go`
  - `thread_state_handlers.go`
  - `thread_files_handlers.go`
  - `thread_search_handlers.go`
  - `run_handlers.go`
  - `gateway_admin_handlers.go`
  - `gateway_runtime_handlers.go`
  - `memory_handlers.go`
  - `clarification_handlers.go`
  - `health_handlers.go`
  - `suggestions_handlers.go`
- Compat services and mappers:
  - `thread_*_service.go`
  - `run_service.go`
  - `run_store.go`
  - `run_registry.go`
  - `run_stream_service.go`
  - `run_mapper.go`
  - `run_response_mapper.go`
  - `gateway_catalog_service.go`
  - `gateway_compat_fs_service.go`
  - `memory_service.go`
- Protocol / compat helpers:
  - `response_helpers.go`
  - `compat_value_helpers.go`
  - `thread_message_service.go`
  - `thread_payload_service.go`
  - `thread_view_service.go`
  - `thread_persistence_paths.go`
  - `thread_persistence_service.go`

The goal is that this layer owns protocol adaptation, response shaping, storage compatibility, and HTTP semantics, but not core runtime orchestration policy.

### 2. Harness / Runtime layer

`pkg/harness` owns runtime assembly and longer-lived runtime dependencies.

It groups:

- agent factory
- runtime feature assembly
- lifecycle hook composition
- durable memory runtime boundary
- sandbox provider abstraction

`pkg/agent` remains the actual execution core.

It is not LangGraph. It is a custom Go loop that:

- assembles prompt + tools + thread context
- calls the model
- executes tool calls
- handles clarification/present-files/subagent flow
- emits runtime events back to the compatibility layer

This remains the main structural difference from upstream, but the dependency
direction is now cleaner: compat depends on a harness runtime instead of owning
agent assembly, durable memory, and sandbox lifecycle as separate concerns.

### 3. Runtime services

`pkg/harnessruntime` now carries most runtime-side orchestration that used to leak into the compat package.

It includes:

- feature config and lifecycle config
- feature providers and provider adapters
- memory runtime construction
- preflight and context binding
- orchestrator and runner-facing execution planning
- completion / outcome / run-state services
- coordination and query services

This is the closest Go equivalent to the upstream `gateway -> harness runtime` split. The upstream implementation is still Python + LangGraph/LangChain, but the dependency shape is now similar:

- compat/http layer prepares requests
- harness/harnessruntime assemble runtime execution
- agent loop executes
- compat/http layer encodes responses and streams events

### 4. Model / tool layer

`pkg/llm` and `pkg/tools` provide the runtime substrate:

- provider adapters and stream handling
- tool schemas and tool-call normalization
- built-in file, shell, presentation, clarification, ACP, and subagent tools

The runtime depends on this layer, but this layer is independent from the HTTP compatibility surface.

### 5. Execution services

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

The current dependency flow is:

1. `pkg/langgraphcompat`
   Request decode, protocol compatibility, HTTP/SSE transport, compat persistence views
2. `pkg/harness`
   Runtime assembly boundary, feature builders, lifecycle wiring, sandbox/memory abstractions
3. `pkg/harnessruntime`
   Runtime services and adapters used by the compatibility layer and harness
4. `pkg/agent`
   Execution loop
5. `pkg/llm`, `pkg/tools`, `pkg/memory`, `pkg/sandbox`
   Provider, tools, storage, and isolation substrate

This is closer to upstream's router/service split and app/runtime dependency direction, while still remaining a single Go process.
