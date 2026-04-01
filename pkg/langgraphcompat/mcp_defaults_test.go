package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMCPConfigGetReturnsDefaultServers(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload gatewayMCPConfig
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.MCPServers) != 3 {
		t.Fatalf("mcp_servers=%#v want bundled defaults", payload.MCPServers)
	}
	if payload.MCPServers["github"].Command != "npx" {
		t.Fatalf("github=%#v want npx command", payload.MCPServers["github"])
	}
	if payload.MCPServers["filesystem"].Enabled {
		t.Fatalf("filesystem=%#v want disabled", payload.MCPServers["filesystem"])
	}
}

func TestMCPConfigPutRetainsBundledDefaultCatalog(t *testing.T) {
	_, handler := newCompatTestServer(t)

	body := `{"mcp_servers":{"github":{"enabled":true,"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"],"description":"GitHub tools"}}}`
	resp := performCompatRequest(t, handler, http.MethodPut, "/api/mcp/config", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload gatewayMCPConfig
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.MCPServers["github"].Enabled {
		t.Fatalf("github=%#v want enabled", payload.MCPServers["github"])
	}
	if _, ok := payload.MCPServers["filesystem"]; !ok {
		t.Fatalf("filesystem missing from %#v", payload.MCPServers)
	}
	if _, ok := payload.MCPServers["postgres"]; !ok {
		t.Fatalf("postgres missing from %#v", payload.MCPServers)
	}
}
