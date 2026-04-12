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
    Compat[pkg/langgraphcompat<br/>HTTP / SSE protocol compatibility]
    CompatState[Compat State / Persistence View]
    Harness[pkg/harness<br/>factory / runner / lifecycle / boundaries]
    Runtime[pkg/harnessruntime<br/>orchestrator / completion / query / event / snapshot / profile]
    Agent[pkg/agent<br/>custom ReAct loop]
    ToolRuntime[Tool Runtime]
    SandboxRuntime[Sandbox Runtime]
    Providers[pkg/llm]
    Tools[pkg/tools]
    Memory[pkg/memory]
    Sandbox[pkg/sandbox]

    UI --> Compat
    Compat <--> CompatState
    Compat --> Harness
    Harness --> Runtime
    Harness --> Agent
    Runtime --> ToolRuntime
    Runtime --> SandboxRuntime
    Runtime --> CompatState
    Agent --> Providers
    Agent --> Tools
    ToolRuntime --> Tools
    Runtime --> Memory
    SandboxRuntime --> Sandbox
```

## Layers

The repository is organized around five main layers:

- `cmd/langgraph`: process entrypoint and server bootstrap
- `pkg/langgraphcompat`: DeerFlow-compatible HTTP, thread/run lifecycle, gateway state, uploads, artifacts, and SSE
- `pkg/harness`, `pkg/harnessruntime`: runtime assembly, lifecycle, profiles, orchestration, events, and snapshots
- `pkg/agent`, `pkg/llm`, `pkg/tools`: Go-native agent loop, model adapters, and tool execution
- `pkg/memory`, `pkg/sandbox`: durable memory and execution isolation

Reference documents:

- [Architecture](docs/ARCHITECTURE.md)
- [API Diff](docs/API_DIFF.md)
