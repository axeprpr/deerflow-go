package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelsStatusWithoutConfigReturnsServiceStoppedAndEmptyChannels(t *testing.T) {
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
	if len(payload.Channels) != 0 {
		t.Fatalf("channels=%#v want empty", payload.Channels)
	}
}

func TestChannelsRestartReturns503WithoutConfig(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/channels/feishu/restart", nil, nil)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("restart status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Channel service is not running") {
		t.Fatalf("body=%q want service unavailable message", resp.Body.String())
	}
}

func TestChannelsStatusReadsConfigAndRestartExplainsState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  feishu:
    enabled: true
    app_id: cli_xxx
    app_secret: sec_xxx
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
	if restartPayload.Message != "Channel feishu restarted successfully" {
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

func TestChannelsStatusReadsConfigFromModernEnvName(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  telegram:
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)

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
	if !payload.ServiceRunning {
		t.Fatalf("service_running=%v want true", payload.ServiceRunning)
	}
	if !payload.Channels["telegram"].Enabled || !payload.Channels["telegram"].Running {
		t.Fatalf("telegram=%#v want enabled and running", payload.Channels["telegram"])
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
	if payload.Message != "Failed to restart channel slack" {
		t.Fatalf("message=%q want failed restart message", payload.Message)
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

	svc := newGatewayChannelService(nil)
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

func TestChannelsStatusReloadsConfigWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  feishu:
    enabled: true
    app_id: cli_xxx
    app_secret: sec_xxx
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	_, handler := newCompatTestServer(t)

	initial := performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial status=%d body=%s", initial.Code, initial.Body.String())
	}

	if err := os.WriteFile(configPath, []byte(`
channels:
  slack:
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("reloaded status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		ServiceRunning bool                   `json:"service_running"`
		Channels       map[string]channelInfo `json:"channels"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.ServiceRunning {
		t.Fatalf("service_running=%v want true", payload.ServiceRunning)
	}
	if payload.Channels["feishu"].Enabled || payload.Channels["feishu"].Running {
		t.Fatalf("feishu=%#v want disabled+stopped after reload", payload.Channels["feishu"])
	}
	if !payload.Channels["slack"].Enabled || !payload.Channels["slack"].Running {
		t.Fatalf("slack=%#v want enabled+running after reload", payload.Channels["slack"])
	}
}

func TestChannelsStatusMarksFeishuStoppedWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  feishu:
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

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
	if !payload.ServiceRunning {
		t.Fatalf("service_running=%v want true", payload.ServiceRunning)
	}
	if !payload.Channels["feishu"].Enabled {
		t.Fatalf("feishu=%#v want enabled", payload.Channels["feishu"])
	}
	if payload.Channels["feishu"].Running {
		t.Fatalf("feishu=%#v want stopped without credentials", payload.Channels["feishu"])
	}
}

func TestChannelsRestartReportsFeishuCredentialError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
channels:
  feishu:
    enabled: true
    app_id: cli_xxx
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/channels/feishu/restart", nil, nil)
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
	if payload.Message != "Failed to restart channel feishu" {
		t.Fatalf("message=%q want failed restart message", payload.Message)
	}
}

func TestChannelRestartReloadsNewlyEnabledConfig(t *testing.T) {
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

	svc := newGatewayChannelService(nil)
	svc.start()

	if ok, msg := svc.restart("slack"); ok || !strings.Contains(msg, "not enabled") {
		t.Fatalf("first restart ok=%v msg=%q want disabled failure", ok, msg)
	}

	if err := os.WriteFile(configPath, []byte(`
channels:
  slack:
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	ok, msg := svc.restart("slack")
	if !ok {
		t.Fatalf("restart ok=%v msg=%q want success", ok, msg)
	}
	snap := svc.snapshot()
	if !snap.ServiceRunning {
		t.Fatalf("service_running=%v want true", snap.ServiceRunning)
	}
	if !snap.Channels["slack"].Enabled || !snap.Channels["slack"].Running {
		t.Fatalf("slack=%#v want enabled+running", snap.Channels["slack"])
	}
}
