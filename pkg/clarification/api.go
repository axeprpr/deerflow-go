package clarification

import (
	"encoding/json"
	"net/http"
	"strings"
)

type API struct {
	Manager *Manager
}

func NewAPI(manager *Manager) *API {
	return &API{Manager: manager}
}

func (a *API) HandleCreate(w http.ResponseWriter, r *http.Request, threadID string) {
	if a == nil || a.Manager == nil {
		http.Error(w, "clarification manager is not configured", http.StatusInternalServerError)
		return
	}

	var raw map[string]any
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req := ClarificationRequest{
		Type:     strings.TrimSpace(firstNonEmpty(raw["type"], raw["clarification_type"], raw["clarificationType"])),
		Question: strings.TrimSpace(stringValue(raw["question"])),
		Default:  strings.TrimSpace(stringValue(raw["default"])),
		Required: boolValue(raw["required"]),
	}
	if rawOptions, ok := raw["options"].([]any); ok {
		req.Options = make([]ClarificationOption, 0, len(rawOptions))
		for _, rawOption := range rawOptions {
			optionMap, ok := rawOption.(map[string]any)
			if !ok {
				continue
			}
			req.Options = append(req.Options, ClarificationOption{
				ID:    strings.TrimSpace(stringValue(optionMap["id"])),
				Label: strings.TrimSpace(stringValue(optionMap["label"])),
				Value: strings.TrimSpace(stringValue(optionMap["value"])),
			})
		}
	}

	item, err := a.Manager.Request(WithThreadID(r.Context(), threadID), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

func (a *API) HandleList(w http.ResponseWriter, _ *http.Request, threadID string) {
	if a == nil || a.Manager == nil {
		http.Error(w, "clarification manager is not configured", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"clarifications": a.Manager.ListByThread(threadID),
	})
}

func (a *API) HandleGet(w http.ResponseWriter, _ *http.Request, threadID string, id string) {
	if a == nil || a.Manager == nil {
		http.Error(w, "clarification manager is not configured", http.StatusInternalServerError)
		return
	}

	item, ok := a.Manager.Get(id)
	if !ok || !matchesThread(item, threadID) {
		http.Error(w, "clarification not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func (a *API) HandleList(w http.ResponseWriter, _ *http.Request, threadID string) {
	if a == nil || a.Manager == nil {
		http.Error(w, "clarification manager is not configured", http.StatusInternalServerError)
		return
	}

	items := a.Manager.ListByThread(threadID)
	writeJSON(w, http.StatusOK, map[string]any{"clarifications": items})
}

func (a *API) HandleResolve(w http.ResponseWriter, r *http.Request, threadID string, id string) {
	if a == nil || a.Manager == nil {
		http.Error(w, "clarification manager is not configured", http.StatusInternalServerError)
		return
	}

	item, ok := a.Manager.Get(id)
	if !ok || !matchesThread(item, threadID) {
		http.Error(w, "clarification not found", http.StatusNotFound)
		return
	}

	var req struct {
		Answer string `json:"answer"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Answer) == "" && item.Required {
		http.Error(w, "answer is required", http.StatusBadRequest)
		return
	}

	if err := a.Manager.Resolve(id, req.Answer); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	updated, _ := a.Manager.Get(id)
	writeJSON(w, http.StatusOK, updated)
}

func matchesThread(item *Clarification, threadID string) bool {
	if item == nil {
		return false
	}
	return strings.TrimSpace(item.ThreadID) == strings.TrimSpace(threadID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func firstNonEmpty(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return text
		}
	}
	return ""
}
