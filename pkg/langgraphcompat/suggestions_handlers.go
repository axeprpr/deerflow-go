package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		N          int    `json:"n"`
		Count      int    `json:"count"`
		ModelName  string `json:"model_name"`
		ModelNameX string `json:"modelName"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &raw)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}
	_, hasN := raw["n"]
	_, hasCount := raw["count"]
	countProvided := hasN || hasCount
	if req.N == 0 {
		req.N = req.Count
	}
	if req.N < 0 {
		req.N = 0
	}
	if !countProvided && req.N == 0 {
		req.N = 3
	}
	if req.N > 5 {
		req.N = 5
	}
	if req.N == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}

	lastUser := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			lastUser = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	if lastUser == "" {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []string{}})
		return
	}

	subject := compactSubject(lastUser)
	candidates := []string{
		"请基于以上内容给出一个可执行的分步计划。",
		"请总结关键结论，并标注不确定性。",
		"请给出 3 个下一步可选方案并比较利弊。",
	}
	if subject != "" {
		candidates[0] = "围绕“" + subject + "”给出一个可执行的分步计划。"
		candidates[1] = "继续深入“" + subject + "”：请总结关键结论并标注不确定性。"
	}
	if req.N < len(candidates) {
		candidates = candidates[:req.N]
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": candidates})
}
