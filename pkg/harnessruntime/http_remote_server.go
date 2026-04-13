package harnessruntime

import (
	"encoding/json"
	"net/http"
)

const (
	DefaultRemoteWorkerDispatchPath = "/dispatch"
	DefaultRemoteWorkerHealthPath   = "/health"
)

type HTTPRemoteWorkerServer struct {
	mux *http.ServeMux
}

func NewHTTPRemoteWorkerServer(transport WorkerTransport, protocol RemoteWorkerProtocol) *HTTPRemoteWorkerServer {
	dispatch := NewHTTPRemoteWorkerHandler(transport, protocol)
	mux := http.NewServeMux()
	mux.Handle(DefaultRemoteWorkerDispatchPath, dispatch)
	mux.HandleFunc(DefaultRemoteWorkerHealthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	return &HTTPRemoteWorkerServer{mux: mux}
}

func (s *HTTPRemoteWorkerServer) Handler() http.Handler {
	if s == nil || s.mux == nil {
		return http.NewServeMux()
	}
	return s.mux
}
