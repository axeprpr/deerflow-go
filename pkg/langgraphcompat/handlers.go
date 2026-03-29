package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/google/uuid"
)

// handleRunsStream handles POST /runs/stream
// This is the main streaming endpoint that the frontend uses
func (s *Server) handleRunsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RunCreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Get or create thread ID
	threadID := req.ThreadID
	if threadID == "" {
		threadID = uuid.New().String()
	}

	// Get input messages from request
	input := req.Input
	if input == nil {
		input = make(map[string]any)
	}

	// Extract messages from input
	messages, ok := input["messages"].([]any)
	if !ok {
		messages = []any{}
	}

	// Convert to models.Message
	deerMessages := s.convertToMessages(threadID, messages)

	// Get existing session messages if thread exists
	s.sessionsMu.RLock()
	if session, exists := s.sessions[threadID]; exists {
		deerMessages = append(session.Messages, deerMessages...)
	}
	s.sessionsMu.RUnlock()

	// Set up SSE streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	runID := uuid.New().String()

	// Send thread creation event if new thread
	if req.ThreadID == "" {
		s.sendSSEEvent(w, flusher, "thread", map[string]any{
			"thread_id": threadID,
			"created_at": time.Now().Unix(),
		})
	}

	// Send run creation event
	s.sendSSEEvent(w, flusher, "metadata", map[string]any{
		"run_id":    runID,
		"thread_id": threadID,
	})

	// Run agent and stream events
	ctx := r.Context()
	result, err := s.agent.Run(ctx, threadID, deerMessages)

	if err != nil {
		s.sendSSEEvent(w, flusher, "error", map[string]any{
			"error": err.Error(),
		})
		return
	}

	// Stream each message update
	for _, msg := range result.Messages {
		// Send human message
		s.sendSSEEvent(w, flusher, "messages", map[string]any{
			"data": []Message{
				{
					Type:    "human",
					ID:      msg.ID,
					Content: msg.Content,
				},
			},
		})

		// Send AI message
		s.sendSSEEvent(w, flusher, "messages", map[string]any{
			"data": []Message{
				{
					Type:    "ai",
					ID:      msg.ID + "_ai",
					Content: msg.Content,
				},
			},
		})
	}

	// Save session
	s.saveSession(threadID, result.Messages)

	// Send completion event
	s.sendSSEEvent(w, flusher, "done", map[string]any{
		"run_id": runID,
	})

	flusher.Flush()
}

func (s *Server) convertToMessages(threadID string, input []any) []models.Message {
	messages := make([]models.Message, 0, len(input))
	msgSeq := uint64(time.Now().UnixNano())

	for _, m := range input {
		msgMap, ok := m.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content, _ := msgMap["content"].(string)

		if role == "" || content == "" {
			continue
		}

		msgSeq++
		msg := models.Message{
			ID:        fmt.Sprintf("msg_%d", msgSeq),
			SessionID: threadID,
			Role:      s.convertRole(role),
			Content:   content,
		}
		messages = append(messages, msg)
	}

	return messages
}

func (s *Server) convertRole(langchainRole string) models.Role {
	switch strings.ToLower(langchainRole) {
	case "human", "user":
		return models.RoleHuman
	case "ai", "assistant":
		return models.RoleAI
	case "system":
		return models.RoleSystem
	case "tool":
		return models.RoleTool
	default:
		return models.RoleHuman
	}
}

func (s *Server) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data map[string]any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

func (s *Server) saveSession(threadID string, messages []models.Message) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, exists := s.sessions[threadID]; exists {
		session.Messages = append(session.Messages, messages...)
		session.UpdatedAt = time.Now()
	} else {
		s.sessions[threadID] = &Session{
			ThreadID:  threadID,
			Messages:  messages,
			Metadata:  make(map[string]any),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}
}
