# TODO

## Runtime Architecture Roadmap

Goal:

- keep upstream DeerFlow UI and API contracts stable
- preserve single-node high concurrency as the default strength
- reach at least upstream-level extensibility and isolation
- evolve the backend toward a stronger Go-native harness core

Target architecture:

- `langgraphcompat`
  - only HTTP, SSE, protocol shaping, compatibility views
- `harness`
  - runtime assembly boundary
- `harnessruntime`
  - orchestration, policies, profiles, events, snapshots, coordination
- `agent`
  - execution loop
- externalized runtime state
  - run snapshots
  - event log
  - thread metadata
  - sandbox leases

### Phase 1: Freeze Boundaries

- keep upstream UI-facing API contracts stable
- keep compatibility logic out of runtime policy decisions
- keep runtime policy out of HTTP handlers
- treat `langgraphcompat -> harness -> harnessruntime -> agent` as the permanent dependency direction

### Phase 2: Externalize Runtime State

- move run snapshot storage behind a replaceable store interface
- move event log storage behind a replaceable append/replay interface
- move thread metadata and active-run selection behind runtime-side services
- remove long-term truth from process-local maps where possible

### Phase 3: Introduce Run Coordinator

- create a runtime-side coordinator for run submission, cancellation, join, replay, and recovery
- keep gateway as request decode + protocol encode only
- make worker execution callable through an internal coordination interface
- support both in-process and out-of-process dispatch

### Phase 4: Promote Sandbox to a Resource Service

- turn sandbox management into a lease-based resource boundary
- support acquire / heartbeat / release
- support per-thread reuse and idle eviction
- support both local single-node mode and pooled multi-instance mode

### Phase 5: Promote Memory to a Runtime Service

- keep memory injection/update entirely runtime-owned
- separate storage, update scheduling, and prompt injection
- remove gateway-centric memory ownership

### Phase 6: Preserve Two Deployment Modes

Single-node mode:

- one Go process
- in-process queue/dispatch
- local state/event store implementation
- local sandbox manager

Distributed mode:

- gateway nodes
- coordinator nodes
- worker nodes
- shared state/event store
- shared artifact storage
- sandbox manager as an independent service

### Phase 7: Strengthen Execution Modes

- keep public UI requests on the default compatible mode
- allow internal runtime profiles to shape:
  - tool runtime
  - sandbox runtime
  - run policy
  - recovery policy
- do not leak non-upstream request fields into public UI contracts

### Immediate Next Steps

1. design the run snapshot store interface
2. design the event log append/replay interface
3. design the coordinator boundary between gateway and worker runtime
4. design the sandbox lease service interface
5. keep all changes gated by UI contract tests
