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

Memory scope model:

- `session`
  - thread-scoped working memory for the current conversation
- `user`
  - durable cross-thread memory for one user
- `group`
  - shared project/workspace memory across related threads
- `agent`
  - optional agent-scoped memory for specialized assistants
- separate `ephemeral`, `durable`, and `derived` records
- store memory under explicit `scope_type + scope_id + namespace`

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

Windows and Electron strategy:

- support Electron as a first-class client
- keep full public API compatibility so Electron can reuse the same gateway contract
- prefer `Windows client + remote Linux runtime` as the first supported Windows path
- add an optional Windows local restricted mode later
- do not block Electron delivery on a full-strength Windows sandbox

### Phase 7: Strengthen Execution Modes

- keep public UI requests on the default compatible mode
- allow internal runtime profiles to shape:
  - tool runtime
  - sandbox runtime
  - run policy
  - recovery policy
- do not leak non-upstream request fields into public UI contracts

Sandbox backend plan:

- `local-linux`
  - fast single-node backend for development and self-hosted servers
- `container`
  - stronger isolation backend for multi-tenant server execution
- `remote`
  - lease-based sandbox service for distributed worker fleets
- `windows-restricted`
  - limited local backend for Electron/Windows scenarios
- expose a common boundary:
  - `acquire`
  - `exec`
  - `read/write`
  - `heartbeat`
  - `release`

## Remaining Large TODOs

1. Finish runtime-owned refresh/rebuild of `harness.Runtime`
   - reduce `langgraphcompat.runtimeView()` rebuilding logic
   - move more runtime refresh rules behind `RuntimeNode` / `harnessruntime`
   - keep profile/lifecycle injection compatible with upstream UI behavior

2. Make deployment split first-class
   - add real gateway-node / worker-node startup modes
   - wire remote worker server/client through process entrypoints, not only library helpers
   - keep single-node mode as the default fast path

3. Promote state backends beyond local memory/file
   - add store/provider wiring for shared snapshot/event/thread backends
   - support true multi-instance run recovery and join/replay
   - avoid reintroducing process-local truth in compat maps

4. Finish sandbox backend implementations
   - `container`
   - `remote`
   - `windows-restricted`
   - keep `local-linux` as the fast default backend

5. Finish memory scope rollout
   - move from session/agent groundwork to real `user` / `group` shared memory
   - define persistence, injection, and update rules per scope
   - keep gateway memory APIs consistent with runtime-owned memory state

6. Keep all changes gated by upstream UI contract tests
   - UI-visible route/status/body/SSE compatibility remains non-negotiable

## P0 Execution Board (Current)

Done:

- P0 deterministic regression baseline is now CI-gated as a prerequisite job (`p0-regression`)
- P0 coverage includes:
  - skill discover/trigger
  - tool-call continuity
  - long-chain compaction no-repeat baseline
  - model resolution diagnostics
  - web tool surface + diagnosable errors

Pending (next P0 focus):

- cross-instance recovery/join/replay/cancel consistency stress under higher concurrency
- deploy split production hardening (restart/failure-isolation/dependency-order) in host orchestration
- memory scope productionization (`session/user/group` persistence + injection/update policy baseline)
- sandbox backend production boundaries (`container/remote/windows-restricted`) with quota/TTL/observability baseline
