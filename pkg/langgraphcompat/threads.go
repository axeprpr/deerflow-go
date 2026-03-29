package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

// handleThreadGet handles GET /threads/{thread_id}
func (s *Server) handleThreadGet(w http.ResponseWriter, r *http.Request) {
	threadID := extractThreadID(r)
	if threadID == "" {
		http.Error(w, "Thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()

	if !exists {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}

	values := map[string]any{
		"messages": s.messagesToLangChain(session.Messages),
		"title":     session.Metadata["title"],
	}

	response := map[string]any{
		"thread_id":   threadID,
		"created_at":   session.CreatedAt.Unix(),
		"updated_at":   session.UpdatedAt.Unix(),
		"values":       values,
		"metadata":     session.Metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleThreadCreate handles POST /threads
func (s *Server) handleThreadCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	threadID := uuid.New().String()
	metadata := make(map[string]any)

	// Try to parse body for metadata
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()

		if len(body) > 0 {
			var req map[string]any
			if json.Unmarshal(body, &req) == nil {
				if m, ok := req["metadata"].(map[string]any); ok {
					metadata = m
				}
			}
		}
	}

	s.sessionsMu.Lock()
	s.sessions[threadID] = &Session{
		ThreadID:  threadID,
		Messages:  []models.Message{},
		Metadata:  metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.sessionsMu.Unlock()

	response := map[string]any{
		"thread_id":   threadID,
		"created_at":   time.Now().Unix(),
		"updated_at":   time.Now().Unix(),
		"values":       map[string]any{},
		"metadata":     metadata,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// handleThreadUpdate handles PUT /threads/{thread_id}
func (s *Server) handleThreadUpdate(w http.ResponseWriter, r *http.Request) {
	threadID := extractThreadID(r)
	if threadID == "" {
		http.Error(w, "Thread ID required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	session, exists := s.sessions[threadID]
	if !exists {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}

	// Update values
	if values, ok := req["values"].(map[string]any); ok {
		if title, ok := values["title"].(string); ok {
			session.Metadata["title"] = title
		}
	}

	// Update metadata
	if metadata, ok := req["metadata"].(map[string]any); ok {
		for k, v := range metadata {
			session.Metadata[k] = v
		}
	}

	session.UpdatedAt = time.Now()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"thread_id":   threadID,
		"updated_at":   session.UpdatedAt.Unix(),
	})
}

// handleThreadDelete handles DELETE /threads/{thread_id}
func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	threadID := extractThreadID(r)
	if threadID == "" {
		http.Error(w, "Thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.Lock()
	delete(s.sessions, threadID)
	s.sessionsMu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// handleThreadHistory handles GET /threads/{thread_id}/history
func (s *Server) handleThreadHistory(w http.ResponseWriter, r *http.Request) {
	threadID := extractThreadID(r)
	if threadID == "" {
		http.Error(w, "Thread ID required", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[threadID]
	s.sessionsMu.RUnlock()

	if !exists {
		http.Error(w, "Thread not found", http.StatusNotFound)
		return
	}

	messages := s.messagesToLangChain(session.Messages)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// handleThreadList handles GET /threads
func (s *Server) handleThreadList(w http.ResponseWriter, r *http.Request) {
	s.sessionsMu.RLock()
	threads := make([]map[string]any, 0, len(s.sessions))

	for _, session := range s.sessions {
		threads = append(threads, map[string]any{
			"thread_id":   session.ThreadID,
			"created_at":   session.CreatedAt.Unix(),
			"updated_at":   session.UpdatedAt.Unix(),
			"values": map[string]any{
				"title": session.Metadata["title"],
			},
			"metadata": session.Metadata,
		})
	}
	s.sessionsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(threads)
}

// handleRunGet handles GET /runs/{run_id}
func (s *Server) handleRunGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"run_id": "placeholder",
		"status": "completed",
	})
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

func (s *Server) messagesToLangChain(messages []models.Message) []Message {
	result := make([]Message, 0, len(messages))

	for _, msg := range messages {
		msgType := "human"
		role := "human"

		switch msg.Role {
		case models.RoleAI:
			msgType = "ai"
			role = "assistant"
		case models.RoleHuman:
			msgType = "human"
			role = "human"
		case models.RoleSystem:
			msgType = "system"
			role = "system"
		case models.RoleTool:
			msgType = "tool"
			role = "tool"
		}

		result = append(result, Message{
			Type:    msgType,
			ID:      msg.ID,
			Role:    role,
			Content: msg.Content,
		})
	}

	return result
}

func extractThreadID(r *http.Request) string {
	// Extract thread_id from path /threads/{thread_id}
	path := r.URL.Path
	if strings.HasPrefix(path, "/threads/") {
		id := strings.TrimPrefix(path, "/threads/")
		// Remove /history, /state, etc.
		if idx := strings.Index(id, "/"); idx > 0 {
			id = id[:idx]
		}
		return id
	}
	return ""
}
