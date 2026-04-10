# DeerFlow Upstream Automation Coverage

This document maps the upstream-oriented use cases in [upstream-deerflow-use-cases.md](/root/deerflow-go-clone/docs/upstream-deerflow-use-cases.md) to the current Go-repo automated tests.

Status meanings:

- `covered`: there is already a direct automated test for the contract
- `partial`: pieces are covered, but not the full upstream behavior
- `gap`: no meaningful automated coverage yet
- `out-of-scope`: the upstream contract belongs to Python-only surfaces that do not currently exist in this Go repo

## Summary

| Area | Status | Notes |
| --- | --- | --- |
| Run lifecycle and SSE semantics | mostly covered | strong LangGraph/Gateway replay and reconnect coverage already exists |
| Uploads, skills, agents, memory, MCP, channels | covered | Gateway compatibility tests are broad here |
| Artifact download / preview | covered | `.skill` preview, redirect-free routing, and forced active-content attachment are now automated |
| Clarification vs direct execution decisions | covered | opt-in live behavior regressions now verify concrete requests execute and ambiguous requests ask for more detail |
| Full end-to-end deliverable completion | mostly covered | canonical `read_file -> write_file -> present_files -> final ai` chain now has deterministic coverage |
| Embedded Python client parity | out-of-scope | upstream Python client does not have a Go-side equivalent test surface |

## Coverage Dashboard

### By Total Count

| Metric | Count |
| --- | --- |
| Total upstream use cases tracked | 30 |
| `covered` | 21 |
| `partial` | 7 |
| `gap` | 0 |
| `out-of-scope` | 2 |

### By Effective Coverage

Out-of-scope items are excluded from the effective Go-repo automation denominator.

| Metric | Value |
| --- | --- |
| Effective in-scope use cases | 28 |
| Fully covered | 21 / 28 |
| Covered + partial | 28 / 28 |
| Full-cover ratio | 75.0% |
| Broad-cover ratio (`covered + partial`) | 100.0% |

### By Priority

| Priority | Covered | Partial | Gap | Out-of-scope |
| --- | --- | --- | --- | --- |
| `P0` | 9 | 1 | 0 | 0 |
| `P1` | 7 | 3 | 0 | 0 |
| `P2` | 5 | 3 | 0 | 2 |

### Interpretation

- The repo already has strong broad coverage on management contracts and runtime transport semantics.
- The largest remaining risks are no longer hard `gap`s; they are now concentrated in a small number of high-value `partial` contracts.
- In other words, the repo now has broad coverage across all in-scope upstream use cases, and the remaining work is about tightening release-grade confidence.

## Detailed Mapping

| Use Case | Status | Existing Automated Coverage | Notes |
| --- | --- | --- | --- |
| DF-P0-01 Flash Chat Completes a Simple One-Shot Request | partial | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestThreadRunsCreateReturnsFinalState`; [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestThreadRunStreamAcceptsTopLevelMessages` | verifies run creation/streaming, but not a stable “flash one-shot” golden |
| DF-P0-02 Stream Disconnect Continue and Join Resume | covered | [run_stream_reconnect_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/run_stream_reconnect_test.go), `TestRunsStreamContinuesAfterClientCancellationAndSupportsJoin`; [run_stream_reconnect_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/run_stream_reconnect_test.go), `TestThreadRunJoinWaitsForCompletion` | strong upstream-aligned lifecycle coverage |
| DF-P0-03 Run Cancel Interrupts a Long Task | covered | [run_stream_reconnect_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/run_stream_reconnect_test.go), `TestRunsStreamCancelsAbandonedRunWhenDisconnectModeIsCancel`; [run_stream_reconnect_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/run_stream_reconnect_test.go), `TestThreadRunJoinCanCancelRunOnDisconnect` | covers cancel semantics through runtime paths |
| DF-P0-04 Thread Continuation Preserves Conversation State | covered | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestThreadRunsCreatePersistsMessagesAcrossReload`; [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestThreadRunStreamPersistsMessagesAcrossReload` | strong persistence coverage |
| DF-P0-05 Direct Deliverable Generation Without Unnecessary Clarification | covered | [live_behavior_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/live_behavior_test.go), `TestLiveBehaviorConcretePromptExecutesWithoutClarification` | opt-in live regression verifies the real concrete prompt path on a running gateway |
| DF-P0-06 Clarification Is Used Only When the Request Is Actually Underspecified | covered | [live_behavior_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/live_behavior_test.go), `TestLiveBehaviorAmbiguousPromptRequestsMoreDetail`; [agent_test.go](/root/deerflow-go-clone/pkg/agent/agent_test.go), `TestAgentRunStopsAfterClarificationRequest` | decision boundary is now behaviorally covered with a real ambiguous prompt |
| DF-P0-07 HTML Artifact Is Generated, Surfaced, and Finalized | covered | [handlers_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/handlers_test.go), artifact update tests; [agent_test.go](/root/deerflow-go-clone/pkg/agent/agent_test.go), `TestAgentRunCompletesArtifactWorkflowWithFinalAssistantReply` | now has a deterministic end-to-end workflow chain |
| DF-P0-08 HTML Artifact Route Returns the File Without Redirect Loops | covered | [artifact_get_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/artifact_get_test.go), `TestArtifactGetIndexHTMLServesFileWithoutRedirect`; [artifact_get_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/artifact_get_test.go), `TestArtifactGetIndexHTMLDownloadDoesNotRedirect` | directly covers the recently fixed routing issue |
| DF-P0-09 Skill Archive Preview Works by Default | covered | [skill_archive_preview_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/skill_archive_preview_test.go), `TestArtifactGetSkillArchiveDefaultsToSkillMarkdownPreview` | direct upstream parity |
| DF-P0-10 Active HTML Content Is Forced to Download Safely | covered | [artifact_get_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/artifact_get_test.go), `TestArtifactGetIndexHTMLServesFileWithoutRedirect`; [gateway.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway.go), forced attachment behavior | upstream active-content attachment behavior is now enforced and tested |
| DF-P1-01 Upload a User File and List It | covered | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestUploadsAndArtifactsEndpoints`; [upload_gateway_text_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/upload_gateway_text_test.go), `TestUploadPlainTextDocumentCreatesMarkdownCompanion` | direct coverage |
| DF-P1-02 Uploaded File Can Be Read and Used by the Agent | covered | [uploads_context_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/uploads_context_test.go), multiple `TestConvertToMessages...`; [file_test.go](/root/deerflow-go-clone/pkg/tools/builtin/file_test.go), upload markdown companion tests | strong context and file-read coverage |
| DF-P1-03 Delete an Uploaded File | covered | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestUploadsAndArtifactsEndpoints` | included in endpoint lifecycle coverage |
| DF-P1-04 List Skills and Read a Known Built-In Skill | covered | [skills_discovery_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/skills_discovery_test.go); [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestModelsSkillsMCPConfigEndpoints` | strong discovery coverage |
| DF-P1-05 Enable or Disable a Skill | partial | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestModelsSkillsMCPConfigEndpoints` | generic endpoint coverage exists, but a dedicated skill-state persistence test would be clearer |
| DF-P1-06 Install a Valid `.skill` Archive Through the Gateway | covered | [skill_install_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/skill_install_test.go); [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestSkillInstallFromArchive`; [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestSkillInstallAcceptsCamelCaseThreadID` | solid coverage |
| DF-P1-07 Models List and Model Lookup Match Configured Catalog | covered | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestModelsSkillsMCPConfigEndpoints`; [runtime_model_resolution_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/runtime_model_resolution_test.go) | good endpoint plus runtime mapping coverage |
| DF-P1-08 User Profile Read and Update | partial | [gateway_compat_fs_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_compat_fs_test.go), compat-file loading coverage | file-backed loading is covered; direct GET/PUT endpoint tests should be added |
| DF-P1-09 Memory Read, Clear, and Fact Dedupe | partial | [memory_gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/memory_gateway_test.go); [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestMemoryFilePersistAndLoad`; [memory_test.go](/root/deerflow-go-clone/pkg/memory/memory_test.go) | memory shape and dedupe logic are covered; endpoint-level clear/reload flow is not fully explicit |
| DF-P1-10 Memory Status and Config Are Reachable | covered | [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), `TestAgentsAndMemoryEndpoints`; [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), memory-config defaults tests | endpoint coverage present |
| DF-P2-01 Agent CRUD Through Gateway | covered | [agent_persistence_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/agent_persistence_test.go), `TestAgentCreateUpdateDeleteSyncsFiles`; [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), agent endpoint tests | strong coverage |
| DF-P2-02 MCP Config Read and Write | covered | [mcp_defaults_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/mcp_defaults_test.go); [gateway_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/gateway_test.go), MCP config tests; [mcp_runtime_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/mcp_runtime_test.go) | very strong coverage |
| DF-P2-03 Channels Status Endpoint Works | covered | [channels_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/channels_test.go) | good endpoint coverage |
| DF-P2-04 Suggestions Endpoint Produces Follow-Up Suggestions | covered | [suggestions_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/suggestions_test.go); [suggestions_fallback_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/suggestions_fallback_test.go) | covered |
| DF-P2-05 Local Sandbox Rejects Host Bash by Default | partial | current coverage exists mainly in config/runtime behavior, not as a dedicated end-to-end contract test | should add an explicit runtime contract test |
| DF-P2-06 Docker Sandbox Allows Safe Bash-Based Workflows | partial | artifact emission after bash is covered in [handlers_test.go](/root/deerflow-go-clone/pkg/langgraphcompat/handlers_test.go), `TestForwardAgentEventEmitsArtifactUpdatesWhenBashCompletes` | sandbox-mode specific workflow test is still missing |
| DF-P2-07 Skills Path Is Readable but Not Writable | covered | [file_test.go](/root/deerflow-go-clone/pkg/tools/builtin/file_test.go), `TestWriteFileHandlerBlocksSkillsWrites` | direct contract coverage |
| DF-P2-08 Artifact Path Traversal Is Denied | partial | current artifact tests validate thread-scoped serving and recovered paths | explicit traversal/escape test should be added |
| DF-P2-09 Embedded Client `chat()` and `stream()` Match Gateway Semantics | out-of-scope | none in Go repo | belongs to upstream Python harness, not current Go codebase |
| DF-P2-10 Embedded Client Management Methods Match Gateway Schemas | out-of-scope | none in Go repo | same as above |

## Highest-Value Gaps to Automate Next

### 1. Concrete vs ambiguous execution decisions

Priority: resolved in current wave

Status:

- opt-in live behavior regressions now cover both:
  - `帮我生成一个小鱼游泳的页面`
  - `帮我做一个页面`

### 2. End-to-end deliverable completion

Priority: resolved in current wave

Status:

- deterministic workflow coverage now exists for `read_file -> write_file -> present_files -> final ai`

### 3. Upstream active-content attachment behavior

Priority: resolved in current wave

Status:

- upstream active-content forced attachment behavior is now covered and enforced

### 4. User profile endpoint lifecycle

Priority: medium

Missing coverage:

- explicit `GET/PUT/GET` endpoint flow

### 5. Artifact traversal / escape rejection

Priority: medium

Missing coverage:

- direct test for malicious artifact paths

## Proposed Next Automation Batch

If we continue immediately, the most pragmatic batch is:

1. add a user-profile endpoint lifecycle test
2. add an artifact traversal rejection test
3. add a memory clear/reload endpoint regression
4. add a sandbox security-mode regression
5. optionally convert the live behavior pair into a lightweight release smoke harness

## Recommended Batch Plan

To keep the work broad-first rather than overfitting to one path too early:

### Wave 1: Close P0 Runtime Gaps

Status: completed for the decision-boundary pair

Closed items:

- DF-P0-05 direct deliverable generation without unnecessary clarification
- DF-P0-06 clarification only for truly underspecified requests

Outcome:

- the last major user-visible decision-path gap is now behaviorally guarded
- runtime parity has moved from broad coverage to behavior-level coverage

### Wave 2: Close P1 Contract Partials

- DF-P1-05 skill enable/disable persistence
- DF-P1-08 user-profile endpoint lifecycle
- DF-P1-09 memory clear/reload/dedupe endpoint flow

Expected outcome:

- Gateway management behavior becomes release-gate safe, not just broadly smoke-tested

### Wave 3: Close P2 Security and Sandbox Contracts

- DF-P2-05 local sandbox rejects host bash by default
- DF-P2-06 docker sandbox permits safe bash workflows
- DF-P2-08 artifact traversal rejection

Expected outcome:

- deployment-mode and security-boundary expectations are explicitly automated

### Wave 4: Optional Deep Goldens

- full “small fish page” golden
- one upload-analysis golden
- one skill-install golden with subsequent usage

Expected outcome:

- easy-to-demo canonical regressions for release verification and CI smoke runs
