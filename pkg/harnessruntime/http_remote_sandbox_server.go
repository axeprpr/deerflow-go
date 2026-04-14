package harnessruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

type HTTPRemoteSandboxServer struct {
	config   SandboxManagerConfig
	protocol RemoteSandboxProtocol

	mu       sync.Mutex
	sessions map[string]sandbox.Session
}

func NewHTTPRemoteSandboxServer(config SandboxManagerConfig, protocol RemoteSandboxProtocol) *HTTPRemoteSandboxServer {
	return &HTTPRemoteSandboxServer{
		config:   config.Normalized(),
		protocol: defaultRemoteSandboxProtocol(protocol),
		sessions: map[string]sandbox.Session{},
	}
}

func (s *HTTPRemoteSandboxServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DefaultRemoteSandboxHealthPath, s.handleHealth)
	mux.HandleFunc(DefaultRemoteSandboxLeasePath, s.handleLeases)
	mux.HandleFunc(DefaultRemoteSandboxLeasePath+"/", s.handleLease)
	return mux
}

func (s *HTTPRemoteSandboxServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	activeLeases := len(s.sessions)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                "ok",
		"backend":               s.config.Backend,
		"active_leases":         activeLeases,
		"heartbeat_interval_ms": s.config.HeartbeatInterval.Milliseconds(),
		"idle_ttl_ms":           s.config.IdleTTL.Milliseconds(),
		"sweep_interval_ms":     s.config.SweepInterval.Milliseconds(),
		"paths": map[string]string{
			"health": DefaultRemoteSandboxHealthPath,
			"leases": DefaultRemoteSandboxLeasePath,
		},
	})
}

func (s *HTTPRemoteSandboxServer) handleLeases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	leaseID, session, err := s.createLease()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RemoteSandboxAcquireResponse{
		LeaseID:                leaseID,
		Dir:                    session.GetDir(),
		HeartbeatIntervalMilli: s.config.HeartbeatInterval.Milliseconds(),
	})
}

func (s *HTTPRemoteSandboxServer) handleLease(w http.ResponseWriter, r *http.Request) {
	leaseID, suffix := remoteSandboxLeasePathParts(r.URL.Path)
	if leaseID == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodDelete && suffix == "":
		s.releaseLease(leaseID)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && suffix == DefaultRemoteSandboxHeartbeatPath:
		if _, ok := s.loadLease(leaseID); !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && suffix == DefaultRemoteSandboxExecPath:
		s.handleLeaseExec(w, r, leaseID)
	case r.Method == http.MethodPut && suffix == DefaultRemoteSandboxFilePath:
		s.handleLeaseWriteFile(w, r, leaseID)
	case r.Method == http.MethodGet && suffix == DefaultRemoteSandboxFilePath:
		s.handleLeaseReadFile(w, r, leaseID)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *HTTPRemoteSandboxServer) handleLeaseExec(w http.ResponseWriter, r *http.Request, leaseID string) {
	session, ok := s.loadLease(leaseID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := defaultRemoteSandboxProtocol(s.protocol).DecodeExecRequest(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	timeout := time.Duration(req.TimeoutMillis) * time.Millisecond
	result, err := session.Exec(r.Context(), req.Cmd, timeout)
	if err != nil && result == nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	payload, encErr := defaultRemoteSandboxProtocol(s.protocol).EncodeExecResponse(result)
	if encErr != nil {
		http.Error(w, encErr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *HTTPRemoteSandboxServer) handleLeaseWriteFile(w http.ResponseWriter, r *http.Request, leaseID string) {
	session, ok := s.loadLease(leaseID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := session.WriteFile(path, data); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPRemoteSandboxServer) handleLeaseReadFile(w http.ResponseWriter, r *http.Request, leaseID string) {
	session, ok := s.loadLease(leaseID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	data, err := session.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_, _ = w.Write(data)
}

func (s *HTTPRemoteSandboxServer) createLease() (string, sandbox.Session, error) {
	leaseID := randomLeaseID()
	session, err := sandbox.New(leaseID, s.config.Root)
	if err != nil {
		return "", nil, err
	}
	s.mu.Lock()
	s.sessions[leaseID] = session
	s.mu.Unlock()
	return leaseID, session, nil
}

func (s *HTTPRemoteSandboxServer) loadLease(leaseID string) (sandbox.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[leaseID]
	return session, ok
}

func (s *HTTPRemoteSandboxServer) releaseLease(leaseID string) {
	s.mu.Lock()
	session, ok := s.sessions[leaseID]
	if ok {
		delete(s.sessions, leaseID)
	}
	s.mu.Unlock()
	if ok && session != nil {
		_ = session.Close()
	}
}

func randomLeaseID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf[:])
}

func remoteSandboxLeasePathParts(path string) (leaseID string, suffix string) {
	trimmed := strings.TrimPrefix(path, DefaultRemoteSandboxLeasePath+"/")
	parts := strings.SplitN(trimmed, "/", 2)
	leaseID = strings.TrimSpace(parts[0])
	if leaseID == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return leaseID, ""
	}
	return leaseID, "/" + strings.TrimSpace(parts[1])
}

func (s *HTTPRemoteSandboxServer) Close(ctx context.Context) error {
	s.mu.Lock()
	sessions := s.sessions
	s.sessions = map[string]sandbox.Session{}
	s.mu.Unlock()
	var closeErr error
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if err := session.Close(); err != nil && !errors.Is(err, context.Canceled) && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
