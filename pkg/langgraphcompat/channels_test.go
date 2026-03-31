package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelsStatusIncludesSupportedChannelsWithoutConfig(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		ServiceRunning bool                   `json:"service_running"`
		Channels       map[string]channelInfo `json:"channels"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.ServiceRunning {
		t.Fatalf("service_running=%v want=false", payload.ServiceRunning)
	}
	if len(payload.Channels) != len(supportedGatewayChannels) {
		t.Fatalf("channels=%#v", payload.Channels)
	}
	for _, name := range supportedGatewayChannels {
		info, ok := payload.Channels[name]
		if !ok {
			t.Fatalf("missing channel %q in %#v", name, payload.Channels)
		}
		if info.Enabled || info.Running {
			t.Fatalf("channel %q info=%#v want disabled+stopped", name, info)
		}
	}
}

func TestChannelsStatusReadsConfigAndRestartExplainsState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  feishu:
    enabled: true
  slack:
    enabled: false
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	_, handler := newCompatTestServer(t)

	statusResp := performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", statusResp.Code, statusResp.Body.String())
	}
	var payload struct {
		Channels map[string]channelInfo `json:"channels"`
	}
	if err := json.Unmarshal(statusResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.Channels["feishu"].Enabled {
		t.Fatalf("feishu=%#v want enabled", payload.Channels["feishu"])
	}
	if payload.Channels["slack"].Enabled {
		t.Fatalf("slack=%#v want disabled", payload.Channels["slack"])
	}
	if payload.Channels["telegram"].Enabled {
		t.Fatalf("telegram=%#v want disabled", payload.Channels["telegram"])
	}

	restartResp := performCompatRequest(t, handler, http.MethodPost, "/api/channels/feishu/restart", nil, nil)
	if restartResp.Code != http.StatusOK {
		t.Fatalf("restart status=%d body=%s", restartResp.Code, restartResp.Body.String())
	}
	var restartPayload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(restartResp.Body.Bytes(), &restartPayload); err != nil {
		t.Fatalf("unmarshal restart response: %v", err)
	}
	if restartPayload.Success {
		t.Fatalf("success=%v want false", restartPayload.Success)
	}
	if !strings.Contains(restartPayload.Message, "runtime is not implemented") {
		t.Fatalf("message=%q want runtime explanation", restartPayload.Message)
	}
}
