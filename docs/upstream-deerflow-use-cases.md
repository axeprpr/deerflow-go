# DeerFlow Upstream Regression Use Cases

This document defines a practical regression suite derived from the upstream DeerFlow documentation and code in `third_party/deerflow-ui/`. The goal is not to exhaustively mirror every implementation detail, but to cover the user-visible and API-visible contracts that upstream DeerFlow clearly exposes.

## Source Basis

Primary upstream references used to derive this suite:

- [third_party/deerflow-ui/README.md](/root/deerflow-go-clone/third_party/deerflow-ui/README.md)
- [third_party/deerflow-ui/backend/docs/CONFIGURATION.md](/root/deerflow-go-clone/third_party/deerflow-ui/backend/docs/CONFIGURATION.md)
- [third_party/deerflow-ui/backend/docs/MCP_SERVER.md](/root/deerflow-go-clone/third_party/deerflow-ui/backend/docs/MCP_SERVER.md)
- [third_party/deerflow-ui/backend/app/gateway/routers/thread_runs.py](/root/deerflow-go-clone/third_party/deerflow-ui/backend/app/gateway/routers/thread_runs.py)
- [third_party/deerflow-ui/backend/app/gateway/routers/artifacts.py](/root/deerflow-go-clone/third_party/deerflow-ui/backend/app/gateway/routers/artifacts.py)
- [third_party/deerflow-ui/backend/packages/harness/deerflow/client.py](/root/deerflow-go-clone/third_party/deerflow-ui/backend/packages/harness/deerflow/client.py)

## Use This Suite

Recommended execution layers:

- `UI`: browser flow through `/workspace/chats/...`
- `Gateway API`: `/api/*` and `/api/langgraph/*`
- `Thread Runs API`: `/api/threads/{thread_id}/runs*` and `/threads/{thread_id}/runs*`
- `Embedded client`: `DeerFlowClient`

Priority levels:

- `P0`: release gate, must pass
- `P1`: important parity and operability
- `P2`: extended compatibility and management surface

For every case, record:

- exact model used
- sandbox mode
- thread id / run id
- final thread status
- artifact paths
- last assistant reply

## Coverage Map

| Area | Covered By |
| --- | --- |
| Basic chat and run lifecycle | DF-P0-01 to DF-P0-04 |
| Clarification / direct execution behavior | DF-P0-05 to DF-P0-06 |
| Filesystem and artifact generation | DF-P0-07 to DF-P0-10 |
| Uploads | DF-P1-01 to DF-P1-03 |
| Skills and `.skill` archives | DF-P1-04 to DF-P1-06 |
| Models, memory, user profile | DF-P1-07 to DF-P1-10 |
| Agents, MCP, channels | DF-P2-01 to DF-P2-04 |
| Sandbox and security boundaries | DF-P2-05 to DF-P2-08 |
| Embedded client parity | DF-P2-09 to DF-P2-10 |

## P0 Cases

### DF-P0-01 Flash Chat Completes a Simple One-Shot Request

- Goal: verify the baseline `lead_agent` run path works end to end without planning or subagents.
- Upstream basis: `README.md` documents flash mode and `client.py` exposes `chat()` / `stream()`.
- Preconditions:
  - a working default model is configured
  - Gateway and LangGraph-compatible endpoints are reachable
- Input:
  - user message: `用三句话解释什么是 DeerFlow`
  - context: `thinking_enabled=false`, `is_plan_mode=false`, `subagent_enabled=false`
- Steps:
  1. Create a new thread.
  2. Submit the message through `POST /api/langgraph/threads/{thread_id}/runs/stream`.
  3. Wait for `/threads/{thread_id}/runs/{run_id}/join`.
- Assertions:
  - run finishes with `success`
  - thread reaches `idle`
  - final message type is `ai`
  - no `ask_clarification` tool is emitted
  - no artifact is created

### DF-P0-02 Stream Disconnect Continue and Join Resume

- Goal: verify upstream run lifecycle semantics, especially `on_disconnect=continue` plus `join`.
- Upstream basis: `thread_runs.py` defines `/runs/stream`, `/join`, `on_disconnect`.
- Preconditions:
  - same as DF-P0-01
- Input:
  - user message: `写一段 300 字的产品简介`
  - request body includes `on_disconnect=continue`
- Steps:
  1. Start `runs/stream`.
  2. Read metadata and capture `run_id`.
  3. Intentionally close the first SSE connection.
  4. Call `GET /threads/{thread_id}/runs/{run_id}/join`.
- Assertions:
  - run is still active after the first stream disconnect
  - joined stream eventually completes
  - final thread state contains the assistant reply

### DF-P0-03 Run Cancel Interrupts a Long Task

- Goal: verify explicit cancellation works and produces a cancellable runtime.
- Upstream basis: `thread_runs.py` cancel endpoint with `action=interrupt|rollback`.
- Input:
  - user message: `分 20 个部分写一份超详细行业研究提纲`
- Steps:
  1. Start a run.
  2. Immediately call `POST /api/threads/{thread_id}/runs/{run_id}/cancel?action=interrupt`.
  3. If needed, poll the run record.
- Assertions:
  - cancel endpoint returns `202` or `204`
  - run leaves `running`
  - thread does not keep producing new assistant content after cancellation

### DF-P0-04 Thread Continuation Preserves Conversation State

- Goal: verify multi-turn behavior on the same thread.
- Upstream basis: `client.py` notes thread-based persistence with a checkpointer.
- Input:
  - turn 1: `我叫 Alice，我用 Go 和 React`
  - turn 2: `总结一下我的技术栈`
- Steps:
  1. Submit turn 1 on a fresh thread.
  2. Submit turn 2 on the same thread.
- Assertions:
  - second reply mentions `Go` and `React`
  - second run succeeds without requiring restatement
  - thread message history contains both user turns and both assistant turns

### DF-P0-05 Direct Deliverable Generation Without Unnecessary Clarification

- Goal: verify upstream-style execution for concrete page generation requests.
- Upstream basis: DeerFlow ships web-page/front-end oriented skills and progressively loads them.
- Input:
  - user message: `帮我生成一个小鱼游泳的页面`
  - context: flash mode
- Steps:
  1. Start a fresh thread via UI or API.
  2. Let the run finish naturally.
- Assertions:
  - the first tool phase should move toward execution, typically `read_file` on the front-end skill or direct file generation
  - the run should not stop only on an aesthetic clarification question
  - an output artifact such as `/mnt/user-data/outputs/index.html` or similar is produced
  - final assistant reply references the created page

### DF-P0-06 Clarification Is Used Only When the Request Is Actually Underspecified

- Goal: prove clarification still exists, but is reserved for ambiguous requests.
- Upstream basis: lead-agent clarification middleware exists; behavior is decision-driven, not unconditional.
- Input:
  - user message: `帮我做一个页面`
- Steps:
  1. Start a fresh thread.
  2. Observe the first tool or assistant decision.
- Assertions:
  - the run may emit `ask_clarification` or ask a plain-text question
  - clarification should request a missing requirement such as purpose/style/content
  - no final deliverable is claimed before the ambiguity is resolved

### DF-P0-07 HTML Artifact Is Generated, Surfaced, and Finalized

- Goal: verify the canonical `write_file -> present_files -> final ai` path.
- Upstream basis: artifact behavior is central in README and historical upstream runs; `artifacts.py` defines serving semantics.
- Input:
  - user message: `生成一个单文件的产品介绍页，保存为 index.html`
- Steps:
  1. Start a new run.
  2. Wait until completion.
  3. Fetch the thread state.
- Assertions:
  - at least one artifact path exists in `thread.values.artifacts`
  - `index.html` exists under `/mnt/user-data/outputs/`
  - the final assistant reply comes after file presentation, not before

### DF-P0-08 HTML Artifact Route Returns the File Without Redirect Loops

- Goal: verify generated HTML artifacts are retrievable in a stable way.
- Upstream basis: `artifacts.py` serves files directly; active content is download-safe.
- Preconditions:
  - thread has a generated `index.html` artifact
- Steps:
  1. Request `/api/threads/{thread_id}/artifacts/mnt/user-data/outputs/index.html`.
  2. Request the same path with `?download=true`.
- Assertions:
  - both requests return `200`
  - neither response produces a redirect loop
  - `download=true` includes `Content-Disposition: attachment`

### DF-P0-09 Skill Archive Preview Works by Default

- Goal: verify `.skill` files preview as extracted `SKILL.md`.
- Upstream basis: `artifacts.py` explicitly extracts `SKILL.md` from `.skill` archives.
- Preconditions:
  - a valid `.skill` archive is present in the thread outputs or uploads
- Steps:
  1. Request `/api/threads/{thread_id}/artifacts/.../sample.skill`.
- Assertions:
  - response is `200`
  - response body is the extracted markdown preview
  - body is not the raw zip bytes

### DF-P0-10 Active HTML Content Is Forced to Download Safely

- Goal: verify the XSS-safety behavior documented upstream.
- Upstream basis: README security note and `artifacts.py` active content handling.
- Preconditions:
  - thread has `index.html`
- Steps:
  1. Call the artifact route directly for `index.html`.
- Assertions:
  - response includes attachment-style `Content-Disposition`
  - HTML is not silently executed under the app origin

## P1 Cases

### DF-P1-01 Upload a User File and List It

- Goal: verify uploads lifecycle starts correctly.
- Upstream basis: README and `client.py` mention file upload and analysis.
- Steps:
  1. `POST /api/threads/{thread_id}/uploads` with a sample file.
  2. `GET /api/threads/{thread_id}/uploads/list`.
- Assertions:
  - upload returns `success=true`
  - uploaded file has a virtual path under `/mnt/user-data/uploads/`
  - uploads list includes the new file

### DF-P1-02 Uploaded File Can Be Read and Used by the Agent

- Goal: verify uploaded files are actually available in the run filesystem.
- Input:
  - upload a short CSV or markdown file
  - user message: `读取我上传的文件并总结三点`
- Assertions:
  - the run uses `read_file` or equivalent file-reading behavior on `/mnt/user-data/uploads/...`
  - final assistant answer reflects the uploaded content

### DF-P1-03 Delete an Uploaded File

- Goal: verify upload cleanup.
- Steps:
  1. Upload a file.
  2. `DELETE /api/threads/{thread_id}/uploads/{filename}`.
  3. List uploads again.
- Assertions:
  - delete returns success
  - file no longer appears in uploads list

### DF-P1-04 List Skills and Read a Known Built-In Skill

- Goal: verify Gateway skill discovery.
- Upstream basis: README documents built-in public skills and progressive loading.
- Steps:
  1. `GET /api/skills`
  2. `GET /api/skills/frontend-design`
- Assertions:
  - built-in skills are listed
  - known skill has name, description, category, enabled flag

### DF-P1-05 Enable or Disable a Skill

- Goal: verify skill state management.
- Upstream basis: Gateway skill endpoints and docs around skill state.
- Steps:
  1. Pick a skill from `/api/skills`.
  2. `PUT /api/skills/{skill_name}` with `{"enabled": false}`.
  3. Fetch it again.
- Assertions:
  - updated skill reflects the new enabled state
  - state persists across a subsequent fetch

### DF-P1-06 Install a Valid `.skill` Archive Through the Gateway

- Goal: verify custom skill installation path.
- Upstream basis: README and Gateway skill installer behavior.
- Preconditions:
  - a valid `.skill` archive exists in thread artifacts or uploads
- Steps:
  1. `POST /api/skills/install` with `thread_id` and `path`.
  2. `GET /api/skills`.
- Assertions:
  - install returns `success=true`
  - installed skill appears as a custom skill
  - optional metadata such as `version`, `author`, `compatibility` does not cause rejection

### DF-P1-07 Models List and Model Lookup Match Configured Catalog

- Goal: verify model catalog exposure.
- Upstream basis: README and `CONFIGURATION.md` model configuration.
- Steps:
  1. `GET /api/models`
  2. `GET /api/models/{model_name}`
- Assertions:
  - configured models are listed with `name`, `display_name`, and provider model mapping
  - the selected model can be fetched individually

### DF-P1-08 User Profile Read and Update

- Goal: verify persistent user profile storage.
- Steps:
  1. `GET /api/user-profile`
  2. `PUT /api/user-profile` with a short profile blob
  3. `GET /api/user-profile` again
- Assertions:
  - updated profile is persisted and returned

### DF-P1-09 Memory Read, Clear, and Fact Dedupe

- Goal: verify memory management and duplicate suppression.
- Upstream basis: README says duplicate facts should not accumulate endlessly.
- Steps:
  1. Use the system enough to write a repeated preference, or directly seed memory in test setup.
  2. `GET /api/memory`
  3. `DELETE /api/memory`
  4. `GET /api/memory` again
- Assertions:
  - memory endpoints respond with the documented structure
  - duplicate fact entries are not multiplied without bound
  - clear resets the stored memory

### DF-P1-10 Memory Status and Config Are Reachable

- Goal: verify management visibility around memory.
- Steps:
  1. `GET /api/memory/status`
  2. `GET /api/memory/config`
- Assertions:
  - both endpoints return structured data
  - config reflects the current storage path and runtime behavior

## P2 Cases

### DF-P2-01 Agent CRUD Through Gateway

- Goal: verify the custom-agent management surface.
- Steps:
  1. `POST /api/agents` to create a simple agent with a description and optional model override.
  2. `GET /api/agents`
  3. `GET /api/agents/{name}`
  4. `PUT /api/agents/{name}`
  5. `DELETE /api/agents/{name}`
- Assertions:
  - agent appears in list after creation
  - updated fields are reflected on fetch
  - agent is removed after deletion

### DF-P2-02 MCP Config Read and Write

- Goal: verify extension configuration persistence.
- Upstream basis: `MCP_SERVER.md`.
- Steps:
  1. `GET /api/mcp/config`
  2. `PUT /api/mcp/config` with a sample disabled HTTP MCP server entry
  3. `GET /api/mcp/config` again
- Assertions:
  - config round-trips
  - server definitions, env vars, and OAuth fields remain structurally intact

### DF-P2-03 Channels Status Endpoint Works

- Goal: verify IM-channel management surface exists even if channels are disabled.
- Upstream basis: README IM Channels section.
- Steps:
  1. `GET /api/channels`
- Assertions:
  - endpoint returns service/channel status structure
  - disabled channels are represented cleanly rather than failing

### DF-P2-04 Suggestions Endpoint Produces Follow-Up Suggestions

- Goal: verify Gateway suggestions normalization works.
- Upstream basis: README notes rich-content normalization for suggestions.
- Steps:
  1. `POST /api/threads/{thread_id}/suggestions` with a short prior user conversation.
- Assertions:
  - response contains a suggestion array
  - the endpoint tolerates provider-specific content wrappers

### DF-P2-05 Local Sandbox Rejects Host Bash by Default

- Goal: verify the security boundary documented upstream.
- Upstream basis: `CONFIGURATION.md` says `allow_host_bash: false` by default.
- Preconditions:
  - local sandbox mode is active
- Steps:
  1. Request a task that requires `bash`.
- Assertions:
  - host bash is blocked by default
  - failure mode is explicit, not silent corruption

### DF-P2-06 Docker Sandbox Allows Safe Bash-Based Workflows

- Goal: verify bash works in isolated sandbox mode.
- Preconditions:
  - Docker sandbox is active
- Input:
  - `创建一个 txt 文件并用 bash 输出当前目录文件列表`
- Assertions:
  - bash tool can run
  - filesystem side effects are limited to the thread sandbox

### DF-P2-07 Skills Path Is Readable but Not Writable

- Goal: verify sandbox path permissions.
- Upstream basis: skills are loaded progressively but should not be overwritten by file-write tools.
- Steps:
  1. Ask the system to write into `/mnt/skills/public/...`
- Assertions:
  - write attempt is rejected
  - error clearly states skill paths are not writable

### DF-P2-08 Artifact Path Traversal Is Denied

- Goal: verify artifact serving is thread-scoped and safe.
- Steps:
  1. Request an artifact path containing traversal patterns or a foreign path.
- Assertions:
  - access is denied with `404` or `403`
  - files outside the thread root are not exposed

### DF-P2-09 Embedded Client `chat()` and `stream()` Match Gateway Semantics

- Goal: verify in-process client parity.
- Upstream basis: `client.py`.
- Steps:
  1. Use `DeerFlowClient.chat("hello", thread_id="...")`.
  2. Use `DeerFlowClient.stream("hello", thread_id="...")`.
- Assertions:
  - `chat()` returns a final response
  - `stream()` emits LangGraph-style `values`, `messages-tuple`, `end`

### DF-P2-10 Embedded Client Management Methods Match Gateway Schemas

- Goal: verify list/update helpers stay schema-aligned.
- Upstream basis: `client.py` promises Gateway-aligned dicts.
- Steps:
  1. Call `list_models()`, `list_skills()`, `upload_files()`.
- Assertions:
  - returned shapes match the HTTP Gateway response structures
  - file uploads land under the thread uploads area

## Recommended Smoke Subset

Run this subset on every deployment:

- DF-P0-01
- DF-P0-02
- DF-P0-05
- DF-P0-07
- DF-P0-08
- DF-P1-01
- DF-P1-04
- DF-P1-07
- DF-P1-09
- DF-P2-05 or DF-P2-06, depending on sandbox mode

## Recommended Goldens

Keep reusable prompts and fixtures for these canonical scenarios:

- `帮我生成一个小鱼游泳的页面`
- `读取我上传的文件并总结三点`
- `生成一个单文件的产品介绍页，保存为 index.html`
- `用三句话解释什么是 DeerFlow`
- `帮我做一个页面`

Keep reusable fixtures for:

- one small CSV upload
- one markdown upload
- one valid `.skill` archive
- one HTML artifact fixture

## Notes for Automation

- Prefer thread ids with stable prefixes like `smoke-...`, `artifact-...`, `memory-...`.
- Always capture `run_id` from `metadata` SSE before disconnecting.
- For artifact assertions, verify both API direct access and UI-visible artifact listing.
- Separate model-behavior assertions from runtime-contract assertions. If a case depends on planning/subagent behavior, pin the exact model and context flags.
- Do not treat exact prose as the golden unless the contract is textual. Prefer assertions on status, tool path shape, artifact existence, and endpoint semantics.
