package harnessruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

type remoteSandboxLeaseService struct {
	endpoint string
	client   *HTTPRemoteSandboxClient
}

func NewRemoteSandboxLeaseService(endpoint string, client *HTTPRemoteSandboxClient) SandboxLeaseService {
	if client == nil {
		client = NewHTTPRemoteSandboxClient(nil, nil)
	}
	return &remoteSandboxLeaseService{
		endpoint: strings.TrimSpace(endpoint),
		client:   client,
	}
}

func (s *remoteSandboxLeaseService) Provider() harness.SandboxProvider { return nil }

func (s *remoteSandboxLeaseService) AcquireLease(harness.AgentRequest) (SandboxLease, error) {
	if s == nil || s.client == nil {
		return SandboxLease{}, fmt.Errorf("remote sandbox backend is not configured")
	}
	info, err := s.client.Acquire(context.Background(), s.endpoint)
	if err != nil {
		return SandboxLease{}, err
	}
	handle := &remoteSandboxLeaseHandle{
		endpoint: s.endpoint,
		leaseID:  info.LeaseID,
		dir:      info.Dir,
		client:   s.client,
	}
	return SandboxLease{
		Sandbox:           handle,
		Heartbeat:         handle.Heartbeat,
		Release:           handle.Release,
		HeartbeatInterval: time.Duration(info.HeartbeatIntervalMilli) * time.Millisecond,
	}, nil
}

func (s *remoteSandboxLeaseService) Close() error { return nil }

type remoteSandboxLeaseHandle struct {
	endpoint string
	leaseID  string
	dir      string
	client   *HTTPRemoteSandboxClient
	once     sync.Once
}

func (h *remoteSandboxLeaseHandle) Exec(ctx context.Context, cmd string, timeout time.Duration) (*sandbox.Result, error) {
	return h.client.Exec(ctx, h.endpoint, h.leaseID, cmd, timeout)
}

func (h *remoteSandboxLeaseHandle) WriteFile(path string, data []byte) error {
	return h.client.WriteFile(context.Background(), h.endpoint, h.leaseID, path, data)
}

func (h *remoteSandboxLeaseHandle) ReadFile(path string) ([]byte, error) {
	return h.client.ReadFile(context.Background(), h.endpoint, h.leaseID, path)
}

func (h *remoteSandboxLeaseHandle) Close() error {
	return h.Release()
}

func (h *remoteSandboxLeaseHandle) GetDir() string {
	return h.dir
}

func (h *remoteSandboxLeaseHandle) Heartbeat() error {
	return h.client.Heartbeat(context.Background(), h.endpoint, h.leaseID)
}

func (h *remoteSandboxLeaseHandle) Release() error {
	var err error
	h.once.Do(func() {
		err = h.client.Release(context.Background(), h.endpoint, h.leaseID)
	})
	return err
}
