# API Diff

## Summary

Against the current repository code, there is no confirmed frontend-blocking route gap for the main DeerFlow UI paths.

The compatibility target is:

- `/api/*` gateway-style endpoints
- `/api/langgraph/*` thread/run endpoints
- plain LangGraph-style `/threads/*` and `/runs/*` endpoints

The current code registers the upstream frontend-critical management and runtime paths, including:

- `/api/models`
- `/api/skills`
- `/api/agents`
- `/api/user-profile`
- `/api/mcp/config`
- `/api/memory`
- `/api/memory/export`
- `/api/memory/import`
- `/api/memory/facts`
- `/api/threads/{thread_id}/uploads`
- `/api/threads/{thread_id}/artifacts/{artifact_path...}`
- `/api/threads/{thread_id}/suggestions`
- `/api/langgraph/*`

## Important caveat

No route gap does not mean full upstream equivalence.

The remaining differences are mostly in:

- runtime behavior
- persistence semantics
- payload shaping details
- internal architecture

So the right conclusion is:

- route coverage for the upstream frontend is broadly in place
- full behavior parity is still an ongoing runtime problem, not a pure routing problem
