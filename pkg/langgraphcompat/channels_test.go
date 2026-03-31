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
		ServiceRunning bool                   `json:"service_running"`
		Channels       map[string]channelInfo `json:"channels"`
	}
	if err := json.Unmarshal(statusResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.ServiceRunning {
		t.Fatalf("service_running=%v want true", payload.ServiceRunning)
	}
	if !payload.Channels["feishu"].Enabled {
		t.Fatalf("feishu=%#v want enabled", payload.Channels["feishu"])
	}
	if !payload.Channels["feishu"].Running {
		t.Fatalf("feishu=%#v want running", payload.Channels["feishu"])
	}
	if payload.Channels["slack"].Enabled {
		t.Fatalf("slack=%#v want disabled", payload.Channels["slack"])
	}
	if payload.Channels["slack"].Running {
		t.Fatalf("slack=%#v want stopped", payload.Channels["slack"])
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
	if !restartPayload.Success {
		t.Fatalf("success=%v want true", restartPayload.Success)
	}
	if !strings.Contains(restartPayload.Message, "restarted successfully") {
		t.Fatalf("message=%q want restart success", restartPayload.Message)
	}

	statusResp = performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status after restart=%d body=%s", statusResp.Code, statusResp.Body.String())
	}
	if err := json.Unmarshal(statusResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response after restart: %v", err)
	}
	if !payload.Channels["feishu"].Running {
		t.Fatalf("feishu after restart=%#v want running", payload.Channels["feishu"])
	}
}

func TestChannelsRestartRejectsDisabledChannel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  slack:
    enabled: false
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/channels/slack/restart", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("restart status=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Success {
		t.Fatalf("success=%v want false", payload.Success)
	}
	if !strings.Contains(payload.Message, "not enabled") {
		t.Fatalf("message=%q want disabled explanation", payload.Message)
	}
}

func TestGatewayChannelServiceStopClearsRunningState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  telegram:
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	svc := newGatewayChannelService()
	svc.start()
	if !svc.snapshot().Channels["telegram"].Running {
		t.Fatalf("telegram before stop=%#v want running", svc.snapshot().Channels["telegram"])
	}

	svc.stop()
	snap := svc.snapshot()
	if snap.ServiceRunning {
		t.Fatalf("service_running=%v want false", snap.ServiceRunning)
	}
	if !snap.Channels["telegram"].Enabled || snap.Channels["telegram"].Running {
		t.Fatalf("telegram after stop=%#v want enabled but stopped", snap.Channels["telegram"])
	}
}
