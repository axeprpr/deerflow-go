package langgraphcompat

import (
	"bytes"
	"html/template"
	"net/http"
	"sort"
	"strings"
)

func (s *Server) registerDocsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	mux.HandleFunc("GET /docs", s.handleSwaggerUI)
	mux.HandleFunc("GET /redoc", s.handleReDoc)
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, gatewayOpenAPISpec())
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	renderDocsPage(w, docsPageData{
		Title:    "DeerFlow API Gateway Docs",
		Subtitle: "Offline API reference generated from the built-in OpenAPI schema.",
		Groups:   gatewayDocsGroups(),
	})
}

func (s *Server) handleReDoc(w http.ResponseWriter, r *http.Request) {
	renderDocsPage(w, docsPageData{
		Title:    "DeerFlow API Gateway ReDoc",
		Subtitle: "Offline route index for the DeerFlow gateway and LangGraph-compatible endpoints.",
		Groups:   gatewayDocsGroups(),
	})
}

func gatewayOpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "DeerFlow API Gateway",
			"version":     "0.1.0",
			"description": "Gateway and LangGraph-compatible API for deerflow-go.",
		},
		"tags":  gatewayOpenAPITags(),
		"paths": gatewayOpenAPIPaths(),
	}
}

func gatewayOpenAPITags() []map[string]any {
	return []map[string]any{
		{"name": "tts", "description": "Convert generated text or reports into speech audio"},
		{"name": "models", "description": "Operations for querying available AI models and their configurations"},
		{"name": "mcp", "description": "Manage Model Context Protocol (MCP) server configurations"},
		{"name": "memory", "description": "Access and manage global memory data for personalized conversations"},
		{"name": "skills", "description": "Manage skills and their configurations"},
		{"name": "artifacts", "description": "Access and download thread artifacts and generated files"},
		{"name": "uploads", "description": "Upload and manage user files for threads"},
		{"name": "threads", "description": "Manage DeerFlow thread-local filesystem data"},
		{"name": "agents", "description": "Create and manage custom agents with per-agent config and prompts"},
		{"name": "suggestions", "description": "Generate follow-up question suggestions for conversations"},
		{"name": "channels", "description": "Manage IM channel integrations"},
		{"name": "health", "description": "Health check and system status endpoints"},
		{"name": "langgraph", "description": "LangGraph-compatible thread and run endpoints"},
	}
}

func gatewayOpenAPIPaths() map[string]any {
	return map[string]any{
		"/health": pathItem(map[string]any{
			"get": operation("health", "Health Check", "Service health check endpoint."),
		}),
		"/openapi.json": pathItem(map[string]any{
			"get": operation("docs", "OpenAPI Schema", "OpenAPI schema for the DeerFlow gateway."),
		}),
		"/docs": pathItem(map[string]any{
			"get": operation("docs", "Swagger UI", "Interactive Swagger UI for the gateway."),
		}),
		"/redoc": pathItem(map[string]any{
			"get": operation("docs", "ReDoc", "ReDoc-based API reference."),
		}),
		"/api/tts": pathItem(map[string]any{
			"post": operation("tts", "Text To Speech", "Convert text into speech audio via an OpenAI-compatible TTS backend."),
		}),
		"/api/models": pathItem(map[string]any{
			"get": operation("models", "List Models", "List configured models."),
		}),
		"/api/models/{model_name}": pathItem(map[string]any{
			"get": operation("models", "Get Model", "Get one configured model."),
		}),
		"/api/skills": pathItem(map[string]any{
			"get": operation("skills", "List Skills", "List available skills."),
		}),
		"/api/skills/{skill_name}": pathItem(map[string]any{
			"get": operation("skills", "Get Skill", "Get one skill."),
			"put": operation("skills", "Update Skill", "Enable or disable a skill."),
		}),
		"/api/skills/{skill_name}/enable": pathItem(map[string]any{
			"post": operation("skills", "Enable Skill", "Enable one skill."),
		}),
		"/api/skills/{skill_name}/disable": pathItem(map[string]any{
			"post": operation("skills", "Disable Skill", "Disable one skill."),
		}),
		"/api/skills/install": pathItem(map[string]any{
			"post": operation("skills", "Install Skill", "Install a skill from a .skill archive."),
		}),
		"/api/agents": pathItem(map[string]any{
			"get":  operation("agents", "List Agents", "List custom agents."),
			"post": operation("agents", "Create Agent", "Create a custom agent."),
		}),
		"/api/agents/check": pathItem(map[string]any{
			"get": operation("agents", "Check Agent Name", "Check whether an agent name is available."),
		}),
		"/api/agents/{name}": pathItem(map[string]any{
			"get":    operation("agents", "Get Agent", "Get a custom agent."),
			"put":    operation("agents", "Update Agent", "Update a custom agent."),
			"delete": operation("agents", "Delete Agent", "Delete a custom agent."),
		}),
		"/api/user-profile": pathItem(map[string]any{
			"get": operation("agents", "Get User Profile", "Get the global user profile."),
			"put": operation("agents", "Update User Profile", "Update the global user profile."),
		}),
		"/api/memory": pathItem(map[string]any{
			"get":    operation("memory", "Get Memory", "Get current memory data."),
			"put":    operation("memory", "Update Memory", "Replace the persisted memory snapshot."),
			"delete": operation("memory", "Clear Memory", "Delete all persisted memory."),
		}),
		"/api/memory/reload": pathItem(map[string]any{
			"post": operation("memory", "Reload Memory", "Reload memory data from storage."),
		}),
		"/api/memory/facts/{fact_id}": pathItem(map[string]any{
			"delete": operation("memory", "Delete Memory Fact", "Delete one memory fact."),
		}),
		"/api/memory/config": pathItem(map[string]any{
			"get": operation("memory", "Memory Config", "Get memory configuration."),
		}),
		"/api/memory/status": pathItem(map[string]any{
			"get": operation("memory", "Memory Status", "Get memory status and data."),
		}),
		"/api/channels": pathItem(map[string]any{
			"get": operation("channels", "Channels Status", "Get IM channel service status."),
		}),
		"/api/channels/{name}/restart": pathItem(map[string]any{
			"post": operation("channels", "Restart Channel", "Restart a configured IM channel."),
		}),
		"/api/mcp/config": pathItem(map[string]any{
			"get": operation("mcp", "Get MCP Config", "Get MCP configuration."),
			"put": operation("mcp", "Update MCP Config", "Update MCP configuration."),
		}),
		"/api/threads": pathItem(map[string]any{
			"get":  operation("threads", "List Threads", "List gateway threads with pagination and filtering."),
			"post": operation("threads", "Create Thread", "Create a gateway thread and return the thread envelope."),
		}),
		"/api/threads/search": pathItem(map[string]any{
			"post": operation("threads", "Search Threads", "Search gateway threads with structured filters."),
		}),
		"/api/threads/{thread_id}": pathItem(map[string]any{
			"get":    operation("threads", "Get Thread Data", "Get thread metadata, values, and config through the gateway prefix."),
			"patch":  operation("threads", "Update Thread Data", "Update thread metadata, values, and config through the gateway prefix."),
			"delete": operation("threads", "Delete Thread Data", "Delete thread-local gateway data."),
		}),
		"/api/threads/{thread_id}/state": pathItem(map[string]any{
			"get":   operation("threads", "Get Thread State", "Get thread state through the gateway prefix."),
			"post":  operation("threads", "Replace Thread State", "Replace thread state through the gateway prefix."),
			"patch": operation("threads", "Patch Thread State", "Patch thread state through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/history": pathItem(map[string]any{
			"get":  operation("threads", "Get Thread History", "Get thread history through the gateway prefix."),
			"post": operation("threads", "Get Thread History", "Get thread history with request body filters through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/runs": pathItem(map[string]any{
			"get":  operation("threads", "List Thread Runs", "List runs for a thread through the gateway prefix."),
			"post": operation("threads", "Create Thread Run", "Create a run bound to a thread through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/runs/stream": pathItem(map[string]any{
			"post": operation("threads", "Stream Thread Run", "Stream a run bound to a thread through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/runs/{run_id}": pathItem(map[string]any{
			"get": operation("threads", "Get Thread Run", "Get run metadata for a thread-scoped run through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/runs/{run_id}/cancel": pathItem(map[string]any{
			"post": operation("threads", "Cancel Thread Run", "Request cancellation for an in-flight thread run through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("threads", "Replay Thread Run Stream", "Replay a thread run event stream through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/stream": pathItem(map[string]any{
			"get": operation("threads", "Join Thread Stream", "Join the latest active thread stream through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/files": pathItem(map[string]any{
			"get": operation("threads", "List Thread Files", "List thread files across uploads, workspace, and outputs."),
		}),
		"/api/threads/{thread_id}/clarifications": pathItem(map[string]any{
			"get":  operation("threads", "List Clarifications", "List clarification requests for a thread through the gateway prefix."),
			"post": operation("threads", "Create Clarification", "Create a clarification request through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/clarifications/{id}": pathItem(map[string]any{
			"get": operation("threads", "Get Clarification", "Get a clarification request through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/clarifications/{id}/resolve": pathItem(map[string]any{
			"post": operation("threads", "Resolve Clarification", "Resolve a clarification request through the gateway prefix."),
		}),
		"/api/threads/{thread_id}/uploads": pathItem(map[string]any{
			"post": operation("uploads", "Upload Files", "Upload files to a thread."),
		}),
		"/api/threads/{thread_id}/uploads/list": pathItem(map[string]any{
			"get": operation("uploads", "List Uploads", "List uploaded files for a thread."),
		}),
		"/api/threads/{thread_id}/uploads/{filename}": pathItem(map[string]any{
			"get":    operation("uploads", "Get Upload", "Download or inline-view one uploaded file."),
			"head":   operation("uploads", "Probe Upload", "Fetch upload headers without returning the body."),
			"delete": operation("uploads", "Delete Upload", "Delete one uploaded file."),
		}),
		"/api/threads/{thread_id}/artifacts/{artifact_path}": pathItem(map[string]any{
			"get":  operation("artifacts", "Get Artifact", "Download or inline-view an artifact."),
			"head": operation("artifacts", "Probe Artifact", "Fetch artifact headers without returning the body."),
		}),
		"/api/threads/{thread_id}/suggestions": pathItem(map[string]any{
			"post": operation("suggestions", "Generate Suggestions", "Generate follow-up suggestions."),
		}),
		"/runs": pathItem(map[string]any{
			"post": operation("langgraph", "Create Run", "Create a non-streaming run."),
		}),
		"/runs/stream": pathItem(map[string]any{
			"post": operation("langgraph", "Stream Run", "Create a streaming run."),
		}),
		"/runs/{run_id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Run", "Get run metadata."),
		}),
		"/runs/{run_id}/cancel": pathItem(map[string]any{
			"post": operation("langgraph", "Cancel Run", "Request cancellation for an in-flight run."),
		}),
		"/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Run Stream", "Replay recorded run events."),
		}),
		"/threads": pathItem(map[string]any{
			"get":  operation("langgraph", "List Threads", "List threads."),
			"post": operation("langgraph", "Create Thread", "Create a thread."),
		}),
		"/threads/search": pathItem(map[string]any{
			"post": operation("langgraph", "Search Threads", "Search threads."),
		}),
		"/threads/{thread_id}": pathItem(map[string]any{
			"get":    operation("langgraph", "Get Thread", "Get a thread."),
			"patch":  operation("langgraph", "Update Thread", "Update a thread."),
			"delete": operation("langgraph", "Delete Thread", "Delete a thread."),
		}),
		"/threads/{thread_id}/files": pathItem(map[string]any{
			"get": operation("langgraph", "List Thread Files", "List presented files for a thread."),
		}),
		"/threads/{thread_id}/state": pathItem(map[string]any{
			"get":   operation("langgraph", "Get Thread State", "Get thread state."),
			"post":  operation("langgraph", "Replace Thread State", "Replace thread state."),
			"patch": operation("langgraph", "Patch Thread State", "Patch thread state."),
		}),
		"/threads/{thread_id}/history": pathItem(map[string]any{
			"get":  operation("langgraph", "Get Thread History", "Get thread history."),
			"post": operation("langgraph", "Get Thread History", "Get thread history with request body filters."),
		}),
		"/threads/{thread_id}/runs": pathItem(map[string]any{
			"get":  operation("langgraph", "List Thread Runs", "List runs for a thread."),
			"post": operation("langgraph", "Create Thread Run", "Create a run bound to a thread."),
		}),
		"/threads/{thread_id}/runs/{run_id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Thread Run", "Get run metadata for a thread-scoped run."),
		}),
		"/threads/{thread_id}/runs/{run_id}/cancel": pathItem(map[string]any{
			"post": operation("langgraph", "Cancel Thread Run", "Request cancellation for an in-flight thread-scoped run."),
		}),
		"/threads/{thread_id}/runs/stream": pathItem(map[string]any{
			"post": operation("langgraph", "Stream Thread Run", "Stream a run bound to a thread."),
		}),
		"/threads/{thread_id}/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Thread Run Stream", "Replay a thread run event stream."),
		}),
		"/threads/{thread_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Join Thread Stream", "Join the latest active thread stream."),
		}),
		"/threads/{thread_id}/clarifications": pathItem(map[string]any{
			"get":  operation("langgraph", "List Clarifications", "List clarification requests for a thread."),
			"post": operation("langgraph", "Create Clarification", "Create a clarification request."),
		}),
		"/threads/{thread_id}/clarifications/{id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Clarification", "Get a clarification request."),
		}),
		"/threads/{thread_id}/clarifications/{id}/resolve": pathItem(map[string]any{
			"post": operation("langgraph", "Resolve Clarification", "Resolve a clarification request."),
		}),
		"/api/langgraph/runs": pathItem(map[string]any{
			"post": operation("langgraph", "Create Run (Prefixed)", "Create a non-streaming run via the prefixed API."),
		}),
		"/api/langgraph/runs/stream": pathItem(map[string]any{
			"post": operation("langgraph", "Stream Run (Prefixed)", "Create a streaming run via the prefixed API."),
		}),
		"/api/langgraph/runs/{run_id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Run (Prefixed)", "Get run metadata via the prefixed API."),
		}),
		"/api/langgraph/runs/{run_id}/cancel": pathItem(map[string]any{
			"post": operation("langgraph", "Cancel Run (Prefixed)", "Request cancellation for an in-flight run via the prefixed API."),
		}),
		"/api/langgraph/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Run Stream (Prefixed)", "Replay run events via the prefixed API."),
		}),
		"/api/langgraph/threads": pathItem(map[string]any{
			"get":  operation("langgraph", "List Threads (Prefixed)", "List threads via the prefixed API."),
			"post": operation("langgraph", "Create Thread (Prefixed)", "Create a thread via the prefixed API."),
		}),
		"/api/langgraph/threads/search": pathItem(map[string]any{
			"post": operation("langgraph", "Search Threads (Prefixed)", "Search threads via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}": pathItem(map[string]any{
			"get":    operation("langgraph", "Get Thread (Prefixed)", "Get a thread via the prefixed API."),
			"patch":  operation("langgraph", "Update Thread (Prefixed)", "Update a thread via the prefixed API."),
			"delete": operation("langgraph", "Delete Thread (Prefixed)", "Delete a thread via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/files": pathItem(map[string]any{
			"get": operation("langgraph", "List Thread Files (Prefixed)", "List presented files for a thread via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/state": pathItem(map[string]any{
			"get":   operation("langgraph", "Get Thread State (Prefixed)", "Get thread state via the prefixed API."),
			"post":  operation("langgraph", "Replace Thread State (Prefixed)", "Replace thread state via the prefixed API."),
			"patch": operation("langgraph", "Patch Thread State (Prefixed)", "Patch thread state via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/history": pathItem(map[string]any{
			"get":  operation("langgraph", "Get Thread History (Prefixed)", "Get thread history via the prefixed API."),
			"post": operation("langgraph", "Get Thread History (Prefixed)", "Get thread history with request body filters via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/runs": pathItem(map[string]any{
			"get":  operation("langgraph", "List Thread Runs (Prefixed)", "List runs for a thread via the prefixed API."),
			"post": operation("langgraph", "Create Thread Run (Prefixed)", "Create a run bound to a thread via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/runs/{run_id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Thread Run (Prefixed)", "Get run metadata for a thread-scoped run via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/runs/{run_id}/cancel": pathItem(map[string]any{
			"post": operation("langgraph", "Cancel Thread Run (Prefixed)", "Request cancellation for an in-flight thread-scoped run via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/runs/stream": pathItem(map[string]any{
			"post": operation("langgraph", "Stream Thread Run (Prefixed)", "Stream a run bound to a thread via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Thread Run Stream (Prefixed)", "Replay a thread run event stream via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Join Thread Stream (Prefixed)", "Join the latest active thread stream via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/clarifications": pathItem(map[string]any{
			"get":  operation("langgraph", "List Clarifications (Prefixed)", "List clarification requests for a thread via the prefixed API."),
			"post": operation("langgraph", "Create Clarification (Prefixed)", "Create a clarification request via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/clarifications/{id}": pathItem(map[string]any{
			"get": operation("langgraph", "Get Clarification (Prefixed)", "Get a clarification request via the prefixed API."),
		}),
		"/api/langgraph/threads/{thread_id}/clarifications/{id}/resolve": pathItem(map[string]any{
			"post": operation("langgraph", "Resolve Clarification (Prefixed)", "Resolve a clarification request via the prefixed API."),
		}),
	}
}

func pathItem(operations map[string]any) map[string]any {
	item := map[string]any{}
	for method, operation := range operations {
		item[method] = operation
	}
	return item
}

func operation(tag, summary, description string) map[string]any {
	return map[string]any{
		"tags":        []string{tag},
		"summary":     summary,
		"description": description,
		"responses": map[string]any{
			"200": map[string]any{"description": "Successful response"},
		},
	}
}

type docsPageData struct {
	Title    string
	Subtitle string
	Groups   []docsGroup
}

type docsGroup struct {
	Name        string
	Description string
	Operations  []docsOperation
}

type docsOperation struct {
	Method      string
	Path        string
	Summary     string
	Description string
}

var docsPageTemplate = template.Must(template.New("gateway-docs").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4f1ea;
      --panel: #fffdf8;
      --ink: #1e252b;
      --muted: #56636d;
      --line: #d8d1c5;
      --accent: #0b6b8a;
      --method-bg: #e8f2f5;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Iowan Old Style", "Palatino Linotype", Georgia, serif;
      background: linear-gradient(180deg, #efe7da 0%, var(--bg) 100%);
      color: var(--ink);
    }
    main {
      max-width: 1100px;
      margin: 0 auto;
      padding: 32px 20px 56px;
    }
    h1 { margin: 0 0 10px; font-size: 2.2rem; }
    p.lead { margin: 0 0 20px; color: var(--muted); max-width: 72ch; }
    a.schema {
      color: var(--accent);
      text-decoration: none;
      font-weight: 600;
    }
    section {
      margin-top: 28px;
      background: rgba(255, 253, 248, 0.88);
      border: 1px solid var(--line);
      border-radius: 18px;
      padding: 22px 20px;
      box-shadow: 0 10px 30px rgba(46, 39, 27, 0.06);
    }
    h2 { margin: 0 0 6px; font-size: 1.35rem; }
    p.group { margin: 0 0 16px; color: var(--muted); }
    article {
      padding: 14px 0;
      border-top: 1px solid var(--line);
    }
    article:first-of-type { border-top: 0; padding-top: 0; }
    .row {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
      margin-bottom: 6px;
    }
    .method {
      display: inline-block;
      min-width: 64px;
      padding: 4px 10px;
      border-radius: 999px;
      background: var(--method-bg);
      color: var(--accent);
      font: 700 0.78rem/1.2 ui-monospace, SFMono-Regular, Menlo, monospace;
      text-align: center;
      letter-spacing: 0.04em;
    }
    code.path {
      font: 600 0.95rem/1.4 ui-monospace, SFMono-Regular, Menlo, monospace;
      word-break: break-word;
    }
    .summary { font-weight: 700; }
    .description {
      margin: 0;
      color: var(--muted);
      line-height: 1.5;
    }
  </style>
</head>
<body>
  <main>
    <h1>{{.Title}}</h1>
    <p class="lead">{{.Subtitle}}</p>
    <p><a class="schema" href="/openapi.json">Open raw OpenAPI schema</a></p>
    {{range .Groups}}
    <section>
      <h2>{{.Name}}</h2>
      <p class="group">{{.Description}}</p>
      {{range .Operations}}
      <article>
        <div class="row">
          <span class="method">{{.Method}}</span>
          <code class="path">{{.Path}}</code>
        </div>
        <div class="summary">{{.Summary}}</div>
        <p class="description">{{.Description}}</p>
      </article>
      {{end}}
    </section>
    {{end}}
  </main>
</body>
</html>
`))

func renderDocsPage(w http.ResponseWriter, data docsPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := docsPageTemplate.Execute(&buf, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func gatewayDocsGroups() []docsGroup {
	tagDescriptions := make(map[string]string)
	groupOrder := make([]string, 0)
	for _, tag := range gatewayOpenAPITags() {
		name := strings.TrimSpace(stringValue(tag["name"]))
		if name == "" {
			continue
		}
		tagDescriptions[name] = strings.TrimSpace(stringValue(tag["description"]))
		groupOrder = append(groupOrder, name)
	}

	groups := make(map[string][]docsOperation)
	for path, rawItem := range gatewayOpenAPIPaths() {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOperation := range item {
			op, ok := rawOperation.(map[string]any)
			if !ok {
				continue
			}
			tag := "other"
			if rawTags, ok := op["tags"].([]string); ok && len(rawTags) > 0 {
				tag = strings.TrimSpace(rawTags[0])
			} else if rawTags, ok := op["tags"].([]any); ok && len(rawTags) > 0 {
				tag = strings.TrimSpace(stringValue(rawTags[0]))
			}
			groups[tag] = append(groups[tag], docsOperation{
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     strings.TrimSpace(stringValue(op["summary"])),
				Description: strings.TrimSpace(stringValue(op["description"])),
			})
		}
	}

	out := make([]docsGroup, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, tag := range groupOrder {
		ops := groups[tag]
		if len(ops) == 0 {
			continue
		}
		sort.Slice(ops, func(i, j int) bool {
			if ops[i].Path == ops[j].Path {
				return ops[i].Method < ops[j].Method
			}
			return ops[i].Path < ops[j].Path
		})
		out = append(out, docsGroup{
			Name:        tag,
			Description: tagDescriptions[tag],
			Operations:  ops,
		})
		seen[tag] = struct{}{}
	}

	var extras []string
	for tag := range groups {
		if _, ok := seen[tag]; ok {
			continue
		}
		extras = append(extras, tag)
	}
	sort.Strings(extras)
	for _, tag := range extras {
		ops := groups[tag]
		sort.Slice(ops, func(i, j int) bool {
			if ops[i].Path == ops[j].Path {
				return ops[i].Method < ops[j].Method
			}
			return ops[i].Path < ops[j].Path
		})
		out = append(out, docsGroup{
			Name:        tag,
			Description: tagDescriptions[tag],
			Operations:  ops,
		})
	}

	return out
}
