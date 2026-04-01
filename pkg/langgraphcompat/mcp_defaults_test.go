package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMCPConfigGetReturnsEmptyServersByDefault(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload gatewayMCPConfig
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.MCPServers) != 0 {
		t.Fatalf("mcp_servers=%#v want empty", payload.MCPServers)
	}
}
