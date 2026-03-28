# OSS Research for Deerflow-Go

Checked: 2026-03-28

Method:
- Used live GitHub REST API data for stars and recent activity.
- Read current upstream READMEs and public API/docs for MCP Registry and DeerFlow.
- Goal was not to list every project, but to identify the strongest current OSS option for each Deerflow-Go component area.

## 1. MCP Ecosystem

### Current state

The MCP ecosystem is now split across three distinct layers:

1. Reference servers: the steering-group-maintained examples in [`modelcontextprotocol/servers`](https://github.com/modelcontextprotocol/servers).
2. Real discovery/catalog: the official MCP Registry at [`modelcontextprotocol/registry`](https://github.com/modelcontextprotocol/registry) and `https://registry.modelcontextprotocol.io/`.
3. SDKs: multiple language SDKs, with Go currently led by [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) for practical adoption and [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) for official spec alignment.

The key nuance: [`modelcontextprotocol/servers`](https://github.com/modelcontextprotocol/servers) is explicitly a reference repo, not the production catalog. Its README now points users to the Registry for published servers.

### Best solutions

| Project | GitHub URL | What it does | Stars / maintenance | Use directly |
|---|---|---|---|---|
| modelcontextprotocol/servers | https://github.com/modelcontextprotocol/servers | Official reference MCP servers and examples. Includes current reference servers such as Everything, Fetch, Filesystem, Git, Memory, Sequential Thinking, and Time. | 82,330 stars; updated 2026-03-28; very active | PARTIAL |
| modelcontextprotocol/registry | https://github.com/modelcontextprotocol/registry | Official community registry for published MCP servers. This is the closest thing to an MCP app store / tools catalog. Registry README says API is in v0.1 freeze; live API is at `registry.modelcontextprotocol.io`. | 6,598 stars; updated 2026-03-28; active | YES |
| mark3labs/mcp-go | https://github.com/mark3labs/mcp-go | Popular Go MCP implementation with server/client support and practical transports. README documents stdio and higher-level server ergonomics. | 8,476 stars; updated 2026-03-28; active | YES |
| modelcontextprotocol/go-sdk | https://github.com/modelcontextprotocol/go-sdk | Official Go SDK for MCP servers and clients. README maps SDK versions to MCP spec versions and positions it as the spec-aligned implementation. | 4,246 stars; updated 2026-03-28; active | YES |

### Recommendation

Best overall answer for Deerflow-Go:
- MCP servers/catalog: use the official Registry as the discovery source.
- Best Go MCP implementation today: `mark3labs/mcp-go`.

Why:
- `mcp-go` currently has stronger ecosystem traction than the official Go SDK.
- The official registry is now the real catalog; the `servers` repo is no longer the authoritative list.

Recommendation for Deerflow-Go:
- Build the MCP client/server integration on top of `mark3labs/mcp-go`.
- Track `modelcontextprotocol/go-sdk` closely in case you want tighter long-term spec alignment.

## 2. ACP Protocol

### Current state

DeerFlow 2.0 is not defining a proprietary "DeerFlow ACP" that you can simply import into Go.

What the upstream repo currently shows:
- DeerFlow's Python harness depends on `agent-client-protocol>=0.4.0`.
- DeerFlow's ACP tool imports `from acp import ...` and spawns ACP-compatible subprocess agents.
- DeerFlow's frontend still talks to LangGraph via `@langchain/langgraph-sdk`, not ACP.

So the right interpretation is:
- DeerFlow uses upstream ACP for external coding-agent subprocesses.
- DeerFlow does not expose a standalone Go-importable ACP implementation of its own.

### Best solutions

| Project | GitHub URL | What it does | Stars / maintenance | Use directly |
|---|---|---|---|---|
| agentclientprotocol/agent-client-protocol | https://github.com/agentclientprotocol/agent-client-protocol | The upstream ACP spec and protocol home. Official libraries listed are Kotlin, Java, Python, Rust, and TypeScript. | 2,601 stars; updated 2026-03-28; active | PARTIAL |
| coder/acp-go-sdk | https://github.com/coder/acp-go-sdk | Community Go SDK for ACP. README shows typed requests/responses plus client and agent examples. | 121 stars; updated 2026-03-27; active but still early | PARTIAL |
| bytedance/deer-flow | https://github.com/bytedance/deer-flow | DeerFlow 2.0 itself. Useful as compatibility reference, but its ACP integration is Python-side and tied to its own harness. | 50,742 stars; updated 2026-03-28; very active | NO |

### Recommendation

Best answer for Deerflow-Go:
- You cannot directly "import DeerFlow's ACP" into Go in any meaningful reusable way.
- You can implement ACP compatibility in Go, but that means targeting the upstream ACP protocol, not reusing DeerFlow's Python code.

Use directly:
- `agentclientprotocol/agent-client-protocol`: PARTIAL. It is the protocol definition, not a Go SDK.
- `coder/acp-go-sdk`: PARTIAL. It is the best current Go entry point, but still much smaller and less proven than MCP Go libraries.

Recommendation for Deerflow-Go:
- Do not plan around importing DeerFlow ACP code.
- If ACP compatibility matters, build a thin ACP adapter around `coder/acp-go-sdk` and keep the boundary isolated behind an interface.
- Treat ACP as optional, not core-path, until the Go ACP ecosystem matures further.

## 3. Agent Runtime Frameworks in Go

### Current state

Outside Eino, there are now real Go options, but they fall into different buckets:
- general LLM framework: `tmc/langchaingo`
- agent-focused toolkit/runtime: `google/adk-go`
- newer agent-native frameworks: `trpc-group/trpc-agent-go`

### Best solutions

| Project | GitHub URL | What it does | Stars / maintenance | Use directly |
|---|---|---|---|---|
| google/adk-go | https://github.com/google/adk-go | Code-first Go toolkit for building, evaluating, and deploying AI agents. README emphasizes multi-agent composition and deployment. | 7,262 stars; updated 2026-03-28; active | YES |
| tmc/langchaingo | https://github.com/tmc/langchaingo | The long-running LangChain port for Go. Broadest historical ecosystem, but more of a general LLM app framework than a focused agent runtime. | 8,966 stars; updated 2026-03-28; active | PARTIAL |
| trpc-group/trpc-agent-go | https://github.com/trpc-group/trpc-agent-go | Go agent framework with planner/orchestration positioning and stronger agent-specific framing than LangChainGo. | 1,040 stars; updated 2026-03-28; active | PARTIAL |

### Recommendation

Best non-Eino runtime candidate today: `google/adk-go`.

Why:
- It is the cleanest agent-runtime alternative, not just an LLM wrapper.
- It is active, backed by Google, and explicitly targets building and deploying sophisticated agents.

Recommendation for Deerflow-Go:
- If you want to stay close to your current design and own the runtime, keep Eino.
- If you want the strongest external runtime alternative beyond Eino, evaluate `google/adk-go` first.
- Keep `tmc/langchaingo` as a fallback only if you want breadth of integrations more than an opinionated agent runtime.

## 4. Sandbox Libraries

### Current state

There is no single Go library that cleanly gives Deerflow-Go all of:
- lightweight process sandboxing
- filesystem isolation
- network restrictions
- cross-platform behavior

The current OSS landscape is best understood as three different answers:
- Landlock bindings for lightweight Linux-native restriction
- WebAssembly runtimes for plugin-style sandboxing
- gVisor for strong process/container isolation

### Best solutions

| Project | GitHub URL | What it does | Stars / maintenance | Use directly |
|---|---|---|---|---|
| landlock-lsm/go-landlock | https://github.com/landlock-lsm/go-landlock | Go bindings for Linux Landlock. README shows filesystem restrictions and notes some network support in the kernel model. | 291 stars; updated 2026-03-25; active but niche | PARTIAL |
| wazero/wazero | https://github.com/wazero/wazero | Zero-dependency Go WebAssembly runtime. Best answer when you can run user code as Wasm instead of native processes. | 6,032 stars; updated 2026-03-28; active | YES |
| google/gvisor | https://github.com/google/gvisor | Strong sandboxing boundary for containers via `runsc`. Much heavier than Landlock, but materially stronger for untrusted code execution. | 17,992 stars; updated 2026-03-27; very active | PARTIAL |

### Recommendation

Best overall sandbox answer for Deerflow-Go: `google/gvisor`, if the goal is running untrusted commands safely.

Best lightweight Linux-native answer: `landlock-lsm/go-landlock`.

Best in-process extension sandbox: `wazero`.

Recommendation for Deerflow-Go:
- If you want secure command execution similar to coding-agent sandboxes, Landlock alone is not enough. Use gVisor or another container-level isolation layer for the actual unsafe path.
- Use Landlock only as a lightweight Linux optimization or defense-in-depth layer.
- Use Wasm only for intentionally constrained extension/tool code, not arbitrary shell sessions.

## 5. Full Agent Platforms in Go

### Current state

True Dify/Flowise-class Go-native platforms still lag the Python/TypeScript leaders. The closest current options are not perfect drop-in equivalents:
- `UnicomAI/wanwu`: closest full enterprise AI platform
- `YaoApp/yao`: strong single-binary autonomous-agent runtime
- `kagent-dev/kagent`: Kubernetes-native agent platform, more infra-oriented than Dify-like

### Best solutions

| Project | GitHub URL | What it does | Stars / maintenance | Use directly |
|---|---|---|---|---|
| UnicomAI/wanwu | https://github.com/UnicomAI/wanwu | Enterprise-grade multi-tenant AI agent platform with workflows, RAG, MCP, model management, and APIs. This is the closest current Go-native analogue to Dify-class product scope. | 4,286 stars; updated 2026-03-28; active | PARTIAL |
| YaoApp/yao | https://github.com/YaoApp/yao | Single-binary runtime for autonomous agents with triggers, orchestration, MCP support, and built-in data/API/runtime pieces. | 7,523 stars; updated 2026-03-27; active | PARTIAL |
| kagent-dev/kagent | https://github.com/kagent-dev/kagent | Kubernetes-native framework/platform for AI agents with UI, controller, engine, and MCP tool servers. Best when the target environment is Kubernetes operations. | 2,457 stars; updated 2026-03-28; active | PARTIAL |

### Recommendation

Best current Go-native full platform to study: `UnicomAI/wanwu`.

Why:
- It is the only one in this set that clearly positions itself as a multi-tenant, workflow/RAG/agent/MCP platform comparable in scope to Dify.

Practical caveat:
- None of these are clean component dependencies for Deerflow-Go.
- They are better used as product references or integration targets than as embeddable libraries.

Recommendation for Deerflow-Go:
- Do not depend on a full platform as a core component.
- Use `wanwu` and `yao` as product/architecture references, not as importable building blocks.

## Bottom Line

If Deerflow-Go wants the strongest OSS choices per area right now:

- MCP ecosystem: `mark3labs/mcp-go` for implementation, `modelcontextprotocol/registry` for discovery.
- ACP protocol: do not import DeerFlow code directly; if needed, build an adapter around `coder/acp-go-sdk`.
- Agent runtime beyond Eino: `google/adk-go`.
- Sandbox: `google/gvisor` for strong isolation, with `go-landlock` only as a lightweight Linux layer.
- Full platform reference: `UnicomAI/wanwu`.

## Source Notes

Primary sources used:
- GitHub REST API repo metadata for stars and recent activity.
- MCP Registry repo and live API: `https://github.com/modelcontextprotocol/registry`, `https://registry.modelcontextprotocol.io/`.
- DeerFlow upstream repo and current source files: `https://github.com/bytedance/deer-flow`.
- Official or primary project READMEs for each project listed above.
