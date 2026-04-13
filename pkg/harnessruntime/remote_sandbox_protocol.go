package harnessruntime

import (
	"encoding/json"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

const (
	DefaultRemoteSandboxHealthPath    = "/sandbox/health"
	DefaultRemoteSandboxLeasePath     = "/sandbox/leases"
	DefaultRemoteSandboxHeartbeatPath = "/heartbeat"
	DefaultRemoteSandboxExecPath      = "/exec"
	DefaultRemoteSandboxFilePath      = "/file"
)

type RemoteSandboxAcquireResponse struct {
	LeaseID                string `json:"lease_id"`
	Dir                    string `json:"dir"`
	HeartbeatIntervalMilli int64  `json:"heartbeat_interval_ms"`
}

type RemoteSandboxExecRequest struct {
	Cmd           string `json:"cmd"`
	TimeoutMillis int64  `json:"timeout_ms"`
}

type RemoteSandboxExecResponse struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exit_code"`
	DurationMillis int64  `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
}

type RemoteSandboxProtocol interface {
	EncodeExecRequest(RemoteSandboxExecRequest) ([]byte, error)
	DecodeExecRequest([]byte) (RemoteSandboxExecRequest, error)
	EncodeExecResponse(*sandbox.Result) ([]byte, error)
	DecodeExecResponse([]byte) (*sandbox.Result, error)
}

type JSONRemoteSandboxProtocol struct{}

func defaultRemoteSandboxProtocol(protocol RemoteSandboxProtocol) RemoteSandboxProtocol {
	if protocol != nil {
		return protocol
	}
	return JSONRemoteSandboxProtocol{}
}

func (JSONRemoteSandboxProtocol) EncodeExecRequest(req RemoteSandboxExecRequest) ([]byte, error) {
	return json.Marshal(req)
}

func (JSONRemoteSandboxProtocol) DecodeExecRequest(data []byte) (RemoteSandboxExecRequest, error) {
	var req RemoteSandboxExecRequest
	err := json.Unmarshal(data, &req)
	return req, err
}

func (JSONRemoteSandboxProtocol) EncodeExecResponse(result *sandbox.Result) ([]byte, error) {
	resp := RemoteSandboxExecResponse{}
	if result != nil {
		resp.Stdout = result.Stdout()
		resp.Stderr = result.Stderr()
		resp.ExitCode = result.ExitCode()
		resp.DurationMillis = result.Duration().Milliseconds()
		if err := result.Error(); err != nil {
			resp.Error = err.Error()
		}
	}
	return json.Marshal(resp)
}

func (JSONRemoteSandboxProtocol) DecodeExecResponse(data []byte) (*sandbox.Result, error) {
	var resp RemoteSandboxExecResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	var err error
	if resp.Error != "" {
		err = sandboxError(resp.Error)
	}
	return sandbox.NewResult(resp.Stdout, resp.Stderr, resp.ExitCode, time.Duration(resp.DurationMillis)*time.Millisecond, err), nil
}

type sandboxError string

func (e sandboxError) Error() string { return string(e) }
