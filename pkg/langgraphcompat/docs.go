package langgraphcompat

import "net/http"

func (s *Server) registerDocsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	mux.HandleFunc("GET /docs", s.handleSwaggerUI)
	mux.HandleFunc("GET /redoc", s.handleReDoc)
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, gatewayOpenAPISpec())
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DeerFlow API Gateway Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/openapi.json',
      dom_id: '#swagger-ui'
    });
  </script>
</body>
</html>
`))
}

func (s *Server) handleReDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DeerFlow API Gateway ReDoc</title>
</head>
<body>
  <redoc spec-url="/openapi.json"></redoc>
  <script src="https://unpkg.com/redoc@next/bundles/redoc.standalone.js"></script>
</body>
</html>
`))
}

func gatewayOpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "DeerFlow API Gateway",
			"version":     "0.1.0",
			"description": "Gateway and LangGraph-compatible API for deerflow-go.",
		},
		"tags": []map[string]any{
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
		},
		"paths": gatewayOpenAPIPaths(),
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
		"/api/threads/{thread_id}": pathItem(map[string]any{
			"delete": operation("threads", "Delete Thread Data", "Delete thread-local gateway data."),
		}),
		"/api/threads/{thread_id}/uploads": pathItem(map[string]any{
			"post": operation("uploads", "Upload Files", "Upload files to a thread."),
		}),
		"/api/threads/{thread_id}/uploads/list": pathItem(map[string]any{
			"get": operation("uploads", "List Uploads", "List uploaded files for a thread."),
		}),
		"/api/threads/{thread_id}/uploads/{filename}": pathItem(map[string]any{
			"delete": operation("uploads", "Delete Upload", "Delete one uploaded file."),
		}),
		"/api/threads/{thread_id}/artifacts/{artifact_path}": pathItem(map[string]any{
			"get": operation("artifacts", "Get Artifact", "Download or inline-view an artifact."),
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
		"/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Run Stream", "Replay recorded run events."),
		}),
		"/threads": pathItem(map[string]any{
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
		"/api/langgraph/runs/{run_id}/stream": pathItem(map[string]any{
			"get": operation("langgraph", "Replay Run Stream (Prefixed)", "Replay run events via the prefixed API."),
		}),
		"/api/langgraph/threads": pathItem(map[string]any{
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
