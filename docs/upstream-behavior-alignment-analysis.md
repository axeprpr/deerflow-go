# Upstream DeerFlow Behavior Alignment Analysis

This document records the current behavior-level comparison between upstream DeerFlow and `deerflow-go`, based on direct source reading of the upstream runtime, prompt, middleware, and built-in tools.

The goal is not broad API compatibility. The goal is behavior parity for the lead agent.

## Scope

Compared upstream sources:

- `third_party/deerflow-ui/backend/packages/harness/deerflow/agents/lead_agent/prompt.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/agents/lead_agent/agent.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/agents/factory.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/agents/middlewares/clarification_middleware.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/agents/middlewares/loop_detection_middleware.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/tools/builtins/clarification_tool.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/tools/builtins/present_file_tool.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/sandbox/tools.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/tools/tools.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/models/factory.py`
- `third_party/deerflow-ui/backend/packages/harness/deerflow/skills/loader.py`
- `third_party/deerflow-ui/config.example.yaml`

Compared Go sources:

- `pkg/langgraphcompat/runtime_prompt.go`
- `pkg/langgraphcompat/runtime_legacy_compat.go`
- `pkg/langgraphcompat/compat.go`
- `pkg/langgraphcompat/skills_discovery.go`
- `pkg/agent/react.go`
- `pkg/agent/types_config.go`
- `pkg/clarification/tool.go`
- `pkg/tools/builtin/file.go`
- `pkg/tools/present.go`

## High-Confidence Findings

### 1. Clarification prompt text is not the root cause

Upstream has a strong clarification policy. It is not looser than the Go version.

Upstream `prompt.py` includes:

- `PRIORITY CHECK: If anything is unclear, missing, or has multiple interpretations, you MUST ask for clarification FIRST`
- a full `<clarification_system>` block
- explicit mandatory clarification scenarios
- `Clarification First` in `<critical_reminders>`

Go `runtime_prompt.go` contains nearly the same structure:

- `thinkingStylePrompt`
- `clarificationSystemPrompt`
- `criticalRemindersPrompt`

Conclusion:

- The remaining behavior gap is not explained by “Go added clarification rules that upstream does not have”.
- Weakening clarification text blindly is the wrong direction.

### 2. Upstream clarification middleware is passive

Upstream `ClarificationMiddleware` only intercepts an already-chosen `ask_clarification` tool call and turns it into a `ToolMessage` plus `goto=END`.

It does not decide whether the model should clarify.

Go behaves similarly:

- `pkg/agent/react.go` intercepts `ask_clarification`
- `shouldPauseAfterToolCall()` stops the run after a successful clarification result

Conclusion:

- The true decision boundary is earlier than middleware.
- The important layers are prompt composition, tool surface, tool descriptions, message history, and loop semantics.

### 3. Upstream does not say “always load skills” for every task

Upstream prompt wording is narrower:

- `Skill First: Always load the relevant skill before starting complex tasks.`

That nuance matters. It is not a blanket instruction to load a skill before every request.

Go currently mirrors this wording in `criticalRemindersPrompt`, which is good.

## High-Impact Remaining Deltas

These are the most likely sources of remaining behavior differences.

### A. Tool surface and tool order are still not exact

Upstream lead agent tool exposure is assembled as:

1. configured tools from `config.yaml` in config order
2. built-in tools from `tools.py`
3. MCP tools
4. ACP tools

From upstream `config.example.yaml`, the default file/bash tool order is:

1. `ls`
2. `read_file`
3. `glob`
4. `grep`
5. `write_file`
6. `str_replace`
7. `bash`

Then upstream built-ins are appended from `tools.py`:

1. `present_files`
2. `ask_clarification`

If subagent mode is enabled, `task` is appended through `builtin_tools.extend(SUBAGENT_TOOLS)`.

Current Go default order in `compat.go` plus `cloneRegistryWithPresentFileTool()` is effectively:

1. `ls`
2. `read_file`
3. `write_file`
4. `str_replace`
5. `glob`
6. `bash`
7. `ask_clarification`
8. `task`
9. `present_file`
10. `present_files`

Important differences:

- `glob` is in the wrong position
- `grep` is missing from the visible default surface
- `present_files` is appended later than upstream
- Go also exposes a `present_file` alias that upstream does not actually bind as a tool

Why this matters:

- Tool order changes model logits for function selection.
- Missing `grep` changes the search space for file-centric tasks.
- `present_files` placement affects when the model decides it is “done enough” to present outputs.

### B. `present_files` schema and description differ materially

Upstream `present_files`:

- name exposed to model: `present_files`
- argument shape: only `filepaths`
- description explains:
  - when to use it
  - when not to use it
  - only outputs files can be presented
  - it should be called after creating files

Go `present_files`:

- adds optional `description`
- adds optional `mime_type`
- short description is only `Register generated output files so the UI can display them.`

Why this matters:

- This tool is part of the model’s termination behavior.
- Upstream teaches the model that `present_files` is a user-facing delivery step.
- Go currently presents a thinner and different contract.

### C. `read_file` and `write_file` are close, but not exact

Aligned:

- `description` first
- absolute paths
- `write_file` requires `description`, `path`, `content`

Remaining differences:

- Go `read_file` exposes an extra `limit` argument
- Upstream `read_file` does not expose `limit`
- Upstream truncates `read_file` output to `50000` chars by default
- Go currently does not apply upstream-style default truncation

Why this matters:

- After reading a skill or a large upload, the next model decision is conditioned on tool output size and shape.
- Upstream’s default truncation is part of the behavioral contract.

### D. Prompt assembly is close, but still not byte-level aligned

Upstream final prompt includes:

- role block
- soul block if present
- memory block if present
- `<thinking_style>`
- `<clarification_system>`
- `{skills_section}`
- `{deferred_tools_section}`
- `{subagent_section}`
- `<working_directory>`
- `<response_style>`
- `<citations>`
- `<critical_reminders>`
- `<current_date>YYYY-MM-DD, Weekday</current_date>`

Go prompt assembly today:

- agent-type base prompt contributes `<role>`
- `environmentPrompt()` adds the main runtime sections
- `pkg/agent/react.go` then appends an extra runtime line:
  - `You are running in a ReAct-style loop. Think step by step, call tools when necessary, and stop when you have a complete answer.`

Important differences:

- Go does not append upstream-style `<current_date>`
- Go adds an extra ReAct-specific system sentence that upstream does not add
- Upstream skills section includes skill mutability labels like `[built-in]` / `[custom, editable]`
- Upstream skills section can include skill self-evolution instructions

Why this matters:

- The remaining mismatch is now subtle. Small prompt-shape differences matter more once model, stream behavior, and clarification handling are largely aligned.

### E. Skill discovery behavior is similar, but not identical

Upstream skills loading:

- scans `public` and `custom`
- deduplicates by skill name
- custom can override public
- returns a list sorted by skill name

Go skill discovery:

- stores by `(category, name)` key
- can keep both public and custom variants under different storage keys
- prompt rendering sorts by category then name

Why this matters:

- If duplicate skill names exist, upstream and Go do not present the same available skill set to the model.
- Even without duplicates, ordering is not guaranteed to match upstream.

## Runtime / Loop Observations

### 1. Loop detection is conceptually aligned

Both sides:

- hash repeated tool call sets
- warn after repeated identical calls
- hard stop after a higher threshold

Go and upstream are already close here.

### 2. Clarification stop semantics are conceptually aligned

Both sides stop after a successful `ask_clarification`.

This is not the highest-priority remaining delta.

### 3. The biggest runtime gap is still custom ReAct vs LangGraph `create_agent`

Upstream uses LangGraph `create_agent(...)` and middleware hooks.
Go uses a custom loop in `pkg/agent/react.go`.

Even with the same prompt and tool list, these can still differ in:

- exact message history fed back after tool calls
- how warnings are injected
- how empty or malformed tool calls are repaired
- how terminal text is requested after tool usage

This layer should be investigated only after the prompt/tool surface is made closer to upstream, otherwise too many variables change at once.

## Recommended Alignment Order

Do not change all layers at once. Apply changes in this order.

### Phase 1: Tool surface parity

1. Match upstream default visible tool order exactly for the lead agent.
2. Add missing `grep` support or explicitly document and test that it is intentionally absent.
3. Revisit whether `present_file` alias should remain visible to the model.
   - Upstream prompt text references `present_file`
   - upstream bound tool name is `present_files`
   - this is an upstream inconsistency, so any Go change here must be deliberate

### Phase 2: Tool contract parity

1. Align `present_files` description/schema with upstream
2. Align `read_file` and `write_file` descriptions and argument surface more closely
3. Implement upstream-style default `read_file` truncation

### Phase 3: Prompt parity

1. Append upstream-style `<current_date>`
2. Remove or justify the extra ReAct system sentence
3. Align skill list ordering and labeling more closely
4. Align any remaining prompt-template wording deltas that materially affect decisions

### Phase 4: Loop/message-history parity

Only after phases 1-3:

1. Compare tool-result history formatting after each tool turn
2. Compare retry/repair prompts for malformed tool calls
3. Compare final-answer turn behavior after `present_files`

## What Not To Do

- Do not loosen clarification rules blindly.
- Do not treat middleware as the decision source.
- Do not change multiple behavior layers at once and then attribute the result to one cause.

## Current Best Hypothesis

The remaining behavior gap is most likely driven by a combination of:

1. non-exact tool surface and order
2. `present_files` contract differences
3. prompt assembly differences such as the extra ReAct sentence and missing `<current_date>`
4. `read_file` output size differences after skill loading

Those are higher-probability causes than the clarification block itself.
