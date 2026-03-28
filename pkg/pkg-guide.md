# deerflow-go Package Guide

Methodology: I checked current GitHub repo metadata and recent activity via the public GitHub API, and checked `pkg.go.dev` search/result pages for package visibility. `gh search` was requested, but `gh` is unauthenticated in this environment, so it could not query GitHub directly.

## 1. LLM Clients
**Recommended: `cloudwego/eino-ext`**

- GitHub: https://github.com/cloudwego/eino-ext
- Why it's the best choice:
  - It is the strongest current Go option I found for a single multi-provider client layer that already has OpenAI and Claude integrations, plus streaming and tool-calling support.
  - The OpenAI adapter supports a custom `BaseURL`, which makes OpenAI-compatible providers like SiliconFlow practical without forking the client layer.
  - It fits deerflow-go better than thin single-vendor SDKs because your project already needs multiple providers.
  - The Eino ecosystem is active and production-oriented, while most other unified Go LLM SDKs I found were much smaller or too new.
- Evidence checked:
  - `cloudwego/eino-ext` README lists ChatModel support for OpenAI and Claude.
  - `components/model/openai` supports streaming, tool calling, and `BaseURL`.
  - `components/model/claude` supports streaming and tool calling.
- Current maintenance status:
  - Owner: `cloudwego`
  - Stars: 640
  - Last push: 2026-03-26
- Concerns / caveats:
  - This is an ecosystem package, not a tiny SDK. You are buying into Eino abstractions.
  - If you wanted the thinnest possible vendor SDKs, `openai/openai-go` and `anthropics/anthropic-sdk-go` are stronger per-provider choices, but they do not solve the unified multi-provider problem cleanly.

## 2. Agent Framework
**Recommended: `cloudwego/eino`**

- GitHub: https://github.com/cloudwego/eino
- Why it's the best choice:
  - It is the most convincing production-grade Go agent framework I found that is both active and widely adopted.
  - It has first-class agent/tool abstractions, workflow composition, streaming, multi-agent patterns, and MCP integration in the same ecosystem.
  - It looks materially more production-ready than the small Go agent projects that surfaced in GitHub search.
  - It is a better bet than `langchaingo` for a new production codebase if you want stronger Go-native patterns and a more active current ecosystem.
- Evidence checked:
  - README shows ADK-style agents, tool use, graph composition, streaming, human-in-the-loop, and sub-agent patterns.
  - GitHub search and package search both surfaced Eino repeatedly around Go AI framework queries.
- Current maintenance status:
  - Owner: `cloudwego`
  - Stars: 10,298
  - Last push: 2026-03-27
- Concerns / caveats:
  - It is younger than Cobra/pgx-class infrastructure libraries, so expect some API movement as the ecosystem matures.
  - Documentation is solid, but parts of the ecosystem are still more visible in Chinese-language docs/examples than in older Western Go AI projects.

## 3. Sandbox / Exec
**Recommended: `opencontainers/runc/libcontainer`**

- GitHub: https://github.com/opencontainers/runc/tree/main/libcontainer
- Why it's the best choice:
  - For real process isolation with namespaces, cgroups, capabilities, and filesystem controls, this is the most mature Go foundation I found.
  - It is far more serious than lightweight “safe exec” wrappers because it is the library behind container-style isolation rather than just `exec.Command` sugar.
  - It is the only option I found that clearly addresses the hard parts of sandboxing in a production-grade way.
- Evidence checked:
  - `libcontainer` README explicitly covers namespaces, cgroups, capabilities, and filesystem access controls.
  - `pkg.go.dev` search for `libcontainer` points to `opencontainers/runc/libcontainer`.
- Current maintenance status:
  - Owner: `opencontainers`
  - Stars: 13,150 on `opencontainers/runc`
  - Last push: 2026-03-28
- Concerns / caveats:
  - This is heavier than a simple exec helper and is Linux/container oriented.
  - If deerflow-go only needs timeouts and basic `RLIMIT` handling, stdlib `os/exec` plus `golang.org/x/sys/unix` is simpler.
  - There is not a single lightweight Go library I found that cleanly gives timeout, memory limit, syscall filtering, and filesystem isolation all together.

## 4. MCP Client
**Recommended: `mark3labs/mcp-go`**

- GitHub: https://github.com/mark3labs/mcp-go
- Why it's the best choice:
  - It currently looks like the most mature practical Go MCP implementation for client work.
  - It is ahead of the official Go SDK on GitHub stars and ahead of it in the `pkg.go.dev` search results I checked for MCP Go packages.
  - It supports stdio, SSE, and streamable HTTP transports and is already broadly used in the Go MCP ecosystem.
- Evidence checked:
  - `pkg.go.dev` search for `model context protocol go` showed `mark3labs/mcp-go/mcp` before `modelcontextprotocol/go-sdk/mcp`.
  - README documents stdio, SSE, and streamable HTTP transports.
- Current maintenance status:
  - Owner: `mark3labs`
  - Stars: 8,476
  - Last push: 2026-03-26
- Concerns / caveats:
  - The official SDK, `modelcontextprotocol/go-sdk`, is the spec-adjacent choice and is improving quickly.
  - If you want maximum alignment with upstream MCP semantics over ecosystem maturity, the official SDK is the main alternative to watch.

## 5. Postgres
**Recommended: `jackc/pgx/v5`**

- GitHub: https://github.com/jackc/pgx
- Why it's the best choice:
  - It is the standard serious Go answer for Postgres now.
  - It gives native Postgres support, better performance characteristics, first-class pooling via `pgxpool`, and better access to Postgres-specific features than `sqlx`.
  - `sqlx` is still useful as a helper layer, but it is not a Postgres driver/pool strategy by itself.
- Evidence checked:
  - `pkg.go.dev` search for `pgx` and `postgres pgx` shows `github.com/jackc/pgx/v5` at the top.
  - `sqlx` has more historical stars, but materially older recent activity.
- Current maintenance status:
  - Owner: `jackc`
  - Stars: 13,532
  - Last push: 2026-03-22
- Concerns / caveats:
  - `pgx` is lower level than an ORM and you will write more explicit SQL-facing code.
  - If you want some `database/sql` niceties, you can still layer `sqlx` on top of `pgx/stdlib`, but I would not choose `sqlx` as the core dependency.

## 6. SSE / HTTP Streaming
**Recommended: Go standard library `net/http`**

- GitHub: https://github.com/golang/go/tree/master/src/net/http
- Why it's the best choice:
  - For server-side SSE in Go, the best approach is usually no extra dependency: plain `net/http`, correct headers, and flushing.
  - It is the most battle-tested HTTP stack in the Go ecosystem by a wide margin.
  - For deerflow-go, this keeps the streaming path simple, observable, and dependency-light.
- Evidence checked:
  - `pkg.go.dev/net/http` shows it as standard library, published 2026-03-06, imported by 1,769,979 packages.
  - Package search for SSE also surfaced several `net/http`-based SSE helpers, but none looked stronger than using the stdlib directly for a server.
- Current maintenance status:
  - Owner: `golang`
  - Stars: 133,185 on `golang/go`
  - Last push: 2026-03-28
- Concerns / caveats:
  - This is an approach recommendation, not a third-party SSE framework.
  - If you also need an SSE client library or a broker abstraction, `r3labs/sse/v2` is the main package I would consider next, but I would not add it by default for a simple server stream.

## 7. CLI Framework
**Recommended: `spf13/cobra`**

- GitHub: https://github.com/spf13/cobra
- Why it's the best choice:
  - It remains the safest production default for a serious Go CLI with subcommands, help generation, shell completion, and long-term familiarity.
  - It has the deepest ecosystem adoption and the most reusable examples.
  - `urfave/cli` is good and still active, but Cobra remains the default “boring choice” for larger operator/developer CLIs.
- Evidence checked:
  - `pkg.go.dev` search for `cobra` shows `github.com/spf13/cobra` first.
  - `pkg.go.dev` search for `cli framework` showed `urfave/cli/v3` first, but that query is broader; for production conventions and ecosystem gravity, Cobra still wins.
- Current maintenance status:
  - Owner: `spf13`
  - Stars: 43,524
  - Last push: 2025-12-10
- Concerns / caveats:
  - Cobra is heavier than minimalist flag-based CLIs.
  - If deerflow-go ends up with only a very small command surface, stdlib `flag` or `urfave/cli` could be leaner.

## 8. Config / Env
**Recommended: `caarlos0/env/v11`**

- GitHub: https://github.com/caarlos0/env
- Why it's the best choice:
  - For env-first config, it is the cleanest focused library I found: small, zero-dependency, actively maintained, and designed specifically for struct-based env parsing.
  - It is a better fit than `viper` when you do not need a giant multi-source config system.
  - It is a better current choice than older `envconfig` packages because maintenance is fresher and the API is straightforward.
- Evidence checked:
  - `pkg.go.dev` search for `caarlos0 env` shows `github.com/caarlos0/env/v11` first.
  - `pkg.go.dev` search for `envconfig` still surfaces `kelseyhightower/envconfig`, but I am not recommending it because `caarlos0/env` is more actively maintained and more focused for new code.
- Current maintenance status:
  - Owner: `caarlos0`
  - Stars: 6,067
  - Last push: 2026-03-01
- Concerns / caveats:
  - This is env-only by design.
  - If you later want layered config from files, flags, env, and remote stores, `koanf` is the better expansion path.

## Bottom Line

If I were wiring deerflow-go today, I would choose this stack:

- LLM clients: `cloudwego/eino-ext`
- Agent framework: `cloudwego/eino`
- Sandbox: `opencontainers/runc/libcontainer`
- MCP client: `mark3labs/mcp-go`
- Postgres: `jackc/pgx/v5`
- SSE: `net/http`
- CLI: `spf13/cobra`
- Config/env: `caarlos0/env/v11`
