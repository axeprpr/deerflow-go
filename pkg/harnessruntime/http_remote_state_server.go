package harnessruntime

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type HTTPRemoteStateServer struct {
	state    RuntimeStatePlane
	protocol RemoteStateProtocol
}

func NewHTTPRemoteStateServer(state RuntimeStatePlane, protocol RemoteStateProtocol) *HTTPRemoteStateServer {
	return &HTTPRemoteStateServer{
		state:    state,
		protocol: defaultRemoteStateProtocol(protocol),
	}
}

func (s *HTTPRemoteStateServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DefaultRemoteStateHealthPath, s.handleHealth)
	mux.HandleFunc(DefaultRemoteStateSnapshotsPath, s.handleSnapshots)
	mux.HandleFunc(DefaultRemoteStateSnapshotsPath+"/", s.handleSnapshot)
	mux.HandleFunc(DefaultRemoteStateEventsPath+"/", s.handleEvents)
	mux.HandleFunc(DefaultRemoteStateThreadsPath, s.handleThreads)
	mux.HandleFunc(DefaultRemoteStateThreadsPath+"/", s.handleThread)
	return mux
}

func (s *HTTPRemoteStateServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *HTTPRemoteStateServer) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.state.Snapshots == nil {
		http.Error(w, "snapshot store is not configured", http.StatusServiceUnavailable)
		return
	}
	payload, err := s.protocol.EncodeRunSnapshots(s.state.Snapshots.ListRunSnapshots(strings.TrimSpace(r.URL.Query().Get("thread_id"))))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *HTTPRemoteStateServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, DefaultRemoteStateSnapshotsPath+"/"))
	if runID == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		snapshot, ok := s.state.Snapshots.LoadRunSnapshot(runID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		payload, err := s.protocol.EncodeRunSnapshot(snapshot)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	case http.MethodPut:
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		snapshot, err := s.protocol.DecodeRunSnapshot(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(snapshot.Record.RunID) == "" {
			snapshot.Record.RunID = runID
		}
		s.state.Snapshots.SaveRunSnapshot(snapshot)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *HTTPRemoteStateServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, DefaultRemoteStateEventsPath+"/"))
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(trimmed, "/", 2)
	runID := strings.TrimSpace(parts[0])
	suffix := ""
	if len(parts) == 2 {
		suffix = "/" + strings.TrimSpace(parts[1])
	}
	if runID == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodGet && suffix == "":
		payload, err := s.protocol.EncodeRunEvents(s.state.Events.LoadRunEvents(runID))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	case r.Method == http.MethodGet && suffix == "/next-index":
		payload, err := s.protocol.EncodeInt(s.state.Events.NextRunEventIndex(runID))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	case r.Method == http.MethodPost && suffix == "":
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		event, err := s.protocol.DecodeRunEvent(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.state.Events.AppendRunEvent(runID, event)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && suffix == "":
		replace, ok := s.state.Events.(RunEventReplaceStore)
		if !ok {
			http.Error(w, "event store does not support replace", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		events, err := s.protocol.DecodeRunEvents(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		replace.ReplaceRunEvents(runID, events)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *HTTPRemoteStateServer) handleThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	payload, err := s.protocol.EncodeThreadRuntimeStates(s.state.Threads.ListThreadRuntimeStates())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *HTTPRemoteStateServer) handleThread(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, DefaultRemoteStateThreadsPath+"/"))
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	threadID := strings.TrimSpace(parts[0])
	if threadID == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		state, ok := s.state.Threads.LoadThreadRuntimeState(threadID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		payload, err := s.protocol.EncodeThreadRuntimeState(state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	case len(parts) == 1 && r.Method == http.MethodPut:
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state, err := s.protocol.DecodeThreadRuntimeState(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(state.ThreadID) == "" {
			state.ThreadID = threadID
		}
		s.state.Threads.SaveThreadRuntimeState(state)
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodPost:
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value, err := s.protocol.DecodeAny(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		status, _ := value.(string)
		s.state.Threads.MarkThreadStatus(threadID, status)
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 3 && parts[1] == "metadata" && r.Method == http.MethodPut:
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value, err := s.protocol.DecodeAny(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.state.Threads.SetThreadMetadata(threadID, parts[2], value)
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 3 && parts[1] == "metadata" && r.Method == http.MethodDelete:
		s.state.Threads.ClearThreadMetadata(threadID, parts[2])
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
