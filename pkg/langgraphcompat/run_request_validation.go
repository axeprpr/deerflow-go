package langgraphcompat

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func isStrictLangGraphRunPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "/api/langgraph/runs/") || path == "/api/langgraph/runs/stream" {
		return true
	}
	return strings.HasPrefix(path, "/api/langgraph/threads/") && strings.Contains(path, "/runs")
}

func validateStrictLangGraphRunRequest(r *http.Request, routeThreadID string, req RunCreateRequest) error {
	if r == nil || !isStrictLangGraphRunPath(r.URL.Path) {
		return nil
	}
	if strings.TrimSpace(firstNonEmpty(req.AssistantID, req.AssistantIDX)) == "" {
		return fmt.Errorf("assistant_id required")
	}

	threadID := strings.TrimSpace(routeThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(firstNonEmpty(req.ThreadID, req.ThreadIDX))
	}
	if threadID == "" {
		return fmt.Errorf("thread_id required")
	}
	if _, err := uuid.Parse(threadID); err != nil {
		return fmt.Errorf("invalid thread_id")
	}
	return nil
}
