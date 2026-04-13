# deerflow-go

`deerflow-go` is a Go runtime that targets DeerFlow UI and LangGraph-style protocol compatibility while replacing the original Python gateway + LangGraph/LangChain harness with a self-hostable Go backend.

The goal is not a line-by-line port of upstream DeerFlow. The goal is a compatible external surface with a Go-native runtime core.

## Architecture

Upstream DeerFlow:

```mermaid
flowchart TD
    UI[Next.js Frontend]
    Gateway[FastAPI Gateway]
    API[Gateway API Layer<br/>/api/* + LangGraph-facing routes]
    Runtime[Python Harness Runtime]
    Factory[Agent Factory / create_agent]
    Workflow[LangGraph / LangChain Workflow]
    Middleware[Middleware Pipeline<br/>clarification / memory / title / summarization / loop detection]
    Memory[Memory Subsystem]
    Sandbox[SandboxProvider]
    Tools[Tools / Skills / MCP / Channels]
    LLM[Model Providers]
    State[Threads / Runs / Checkpoints / Artifacts]

    UI --> Gateway
    Gateway --> API
    API --> Runtime
    Gateway --> State
    Runtime --> Factory
    Factory --> Workflow
    Workflow --> Middleware
    Middleware --> Memory
    Middleware --> Sandbox
    Workflow --> Tools
    Workflow --> LLM
    Workflow --> State
```

`deerflow-go`:

```mermaid
flowchart TD
    UI[Upstream DeerFlow Frontend]
    Compat[pkg/langgraphcompat<br/>HTTP / SSE / DeerFlow-compatible API]
    Node[pkg/harnessruntime RuntimeNode<br/>state plane / sandbox manager / dispatch / remote worker]
    Runtime[pkg/harness + pkg/harnessruntime<br/>runtime assembly / coordinator / recovery / profiles]
    Agent[pkg/agent<br/>custom ReAct loop]
    State[Runtime State Plane<br/>snapshots / events / threads]
    Dispatch[Worker Transport<br/>direct / queued / remote]
    Remote[Remote Worker Node<br/>HTTP client / server / protocol]
    SandboxMgr[Sandbox Resource Service<br/>lease / heartbeat / eviction]
    ToolRuntime[Tool Runtime]
    Providers[pkg/llm]
    Tools[pkg/tools]
    Memory[pkg/memory]
    Sandbox[pkg/sandbox]

    UI --> Compat
    Compat --> Node
    Compat --> Runtime
    Node --> State
    Node --> Dispatch
    Node --> Remote
    Node --> SandboxMgr
    Runtime --> Agent
    Runtime --> ToolRuntime
    Runtime --> Memory
    Dispatch --> Runtime
    Agent --> Providers
    Agent --> Tools
    ToolRuntime --> Tools
    SandboxMgr --> Sandbox
```

## Layers

The repository is organized around six main layers:

- `cmd/langgraph`: process entrypoint and server bootstrap
- `pkg/langgraphcompat`: DeerFlow-compatible HTTP, gateway state, uploads, artifacts, threads/runs, and SSE
- `pkg/harness`: runtime assembly boundary, lifecycle/profile wiring, and agent-facing abstractions
- `pkg/harnessruntime`: runtime node, state plane, coordinator, dispatch, recovery, remote worker, sandbox manager, and bootstrap
- `pkg/agent`, `pkg/llm`, `pkg/tools`: Go-native agent loop, model adapters, and tool execution
- `pkg/memory`, `pkg/sandbox`: durable memory and execution isolation
- `TODO.md`: current architecture roadmap and remaining runtime work

Reference documents:

- [Architecture](docs/ARCHITECTURE.md)
- [API Diff](docs/API_DIFF.md)
