# deerflow-go

`deerflow-go` is a Go runtime that targets DeerFlow UI and LangGraph-style protocol compatibility while replacing the original Python gateway + LangGraph/LangChain harness with a self-hostable Go backend.

The goal is not a line-by-line port of upstream DeerFlow. The goal is a compatible external surface with a Go-native runtime core.

## Architecture

`deerflow-go` keeps the upstream UI/API contract, but replaces the Python + LangGraph/LangChain runtime internals with a Go-native runtime core.

UI/API compatibility boundary:

```mermaid
flowchart LR
    UI[Upstream DeerFlow UI]
    Compat[pkg/langgraphcompat<br/>HTTP + SSE compatibility]
    APIA[API routes: /api/*]
    APIB[Thread/Run stream routes]

    UI --> Compat
    Compat --> APIA
    Compat --> APIB
```

Runtime dependency direction (single-node fast path):

```mermaid
flowchart TD
    Compat[pkg/langgraphcompat]
    Harness[pkg/harness<br/>runtime assembly boundary]
    RuntimeNode[pkg/harnessruntime<br/>coordinator + dispatch + recovery]
    Agent[pkg/agent<br/>ReAct loop]
    LLM[pkg/llm]
    Tools[pkg/tools]
    Memory[pkg/memory]
    Sandbox[pkg/sandbox]
    State[Runtime state plane<br/>threads/runs/snapshots/events]

    Compat --> Harness
    Harness --> RuntimeNode
    RuntimeNode --> Agent
    Agent --> LLM
    Agent --> Tools
    RuntimeNode --> Memory
    RuntimeNode --> Sandbox
    RuntimeNode --> State
```

Split-process production topology (gateway/worker/state/sandbox):

```mermaid
flowchart LR
    UI[Upstream DeerFlow UI]
    GatewayProc[Process: gateway or langgraph<br/>compat HTTP/SSE]
    WorkerProc[Process: runtime-node<br/>execution worker]
    StateProc[Process: runtime-state<br/>shared run/thread state]
    SandboxProc[Process: runtime-sandbox<br/>lease-based sandbox service]
    Store[(Shared state/event storage)]
    Artifacts[(Artifact storage)]
    Models[(LLM providers)]

    UI --> GatewayProc
    GatewayProc --> WorkerProc
    GatewayProc --> StateProc
    WorkerProc --> StateProc
    WorkerProc --> SandboxProc
    StateProc --> Store
    GatewayProc --> Artifacts
    WorkerProc --> Artifacts
    WorkerProc --> Models
```

## Runtime Stack Manifest

`cmd/runtime-stack` resolves deployment topology into a portable manifest instead of shell scripts:

- `-print-manifest`: print full `stack-manifest.json` to stdout
- `-validate-bundle=<dir>`: validate an existing bundle directory (`manifest + host-plan + host assets`)
- `-write-bundle=<dir>`: write
  - `stack-manifest.json`
  - `host-plan.json` (systemd/Electron host orchestration plan)
  - `processes/gateway.json`
  - `processes/worker.json`
  - `processes/state.json`
  - `processes/sandbox.json`
  - `host/systemd/*.service` (rendered units for Linux host orchestration)
  - `host/electron/runtime-processes.json` (Electron host process plan)
- `-bundle-restart-policy=never|on-failure|always`: set `host-plan.json` restart policy
- `-bundle-max-restarts=<n>`: set `host-plan.json` max restart count (`<=0` means unlimited)
- `-bundle-restart-delay=<duration>`: set `host-plan.json` restart delay
- `-bundle-dependency-timeout=<duration>`: set `host-plan.json` dependency readiness timeout
- `-bundle-failure-isolation`: set `host-plan.json` failure-isolation hint for host orchestrators
- `-spawn-processes`: launch the manifest as real OS processes (no `.sh`), using per-process `binary + args`
- `-spawn-bundle=<dir>`: launch external processes directly from an existing bundle directory
- `-spawn-bundle-use-host-plan`: use `host-plan.json` runtime policy from bundle for spawn-bundle mode
- `-process-binary-dir=<dir>`: resolve process binaries from a specific directory in external-process mode
- `-spawn-restart-policy=never|on-failure|always`: restart strategy for external-process mode
- `-spawn-max-restarts=<n>`: per-process restart limit (`<=0` means unlimited)
- `-spawn-restart-delay=<duration>`: delay before restarting a failed process
- `-spawn-dependency-timeout=<duration>`: max wait time for each dependency readiness endpoint
- `-spawn-failure-isolation`: keep other processes running when one process exits with terminal error
- `-sandbox-max-active-leases=<n>`: set remote sandbox lease concurrency limit (`<=0` means unlimited); for dedicated sandbox service preset this value is inherited by `runtime-sandbox`
- `-sandbox-service-max-active-leases=<n>`: explicit override for dedicated `runtime-sandbox` lease concurrency limit

Each process spec includes component identity, bind address, readiness target, startup dependencies, binary, and args so orchestration can stay cross-platform (Linux/macOS/Windows/Electron-managed runtime).

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
- [Electron Local Runtime](desktop/electron/README.md)

## Remaining Large Workstreams

- Deployment split hardening:
  - keep single-node fast path
  - continue hardening gateway/worker/state/sandbox multi-process production topology
- Shared-state recovery baseline:
  - continue cross-instance recovery/join/replay coverage and operational diagnostics
  - run responses now include recovery metadata (`attempt`, `resume_from_event`, `resume_reason`, `outcome`) for debugging
- Sandbox backend completion:
  - keep `local-linux` default, continue strengthening `container`, `remote`, and `windows-restricted` backends
