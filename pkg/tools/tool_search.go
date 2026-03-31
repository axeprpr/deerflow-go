package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

const maxDeferredToolSearchResults = 5

type DeferredToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type DeferredToolRegistry struct {
	entries map[string]models.Tool
}

func NewDeferredToolRegistry(tools []models.Tool) *DeferredToolRegistry {
	registry := &DeferredToolRegistry{entries: make(map[string]models.Tool, len(tools))}
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		registry.entries[tool.Name] = tool
	}
	return registry
}

func (r *DeferredToolRegistry) Entries() []DeferredToolEntry {
	if r == nil || len(r.entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]DeferredToolEntry, 0, len(names))
	for _, name := range names {
		tool := r.entries[name]
		out = append(out, DeferredToolEntry{
			Name:        tool.Name,
			Description: strings.TrimSpace(tool.Description),
		})
	}
	return out
}

func (r *DeferredToolRegistry) Search(query string) []models.Tool {
	if r == nil || len(r.entries) == 0 {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	if strings.HasPrefix(query, "select:") {
		return r.selectByName(strings.TrimPrefix(query, "select:"))
	}
	if strings.HasPrefix(query, "+") {
		return r.searchRequiredName(strings.TrimPrefix(query, "+"))
	}
	return r.searchRegex(query)
}

func (r *DeferredToolRegistry) selectByName(raw string) []models.Tool {
	names := strings.Split(raw, ",")
	selected := make([]models.Tool, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tool, ok := r.entries[name]
		if !ok {
			continue
		}
		selected = append(selected, tool)
		if len(selected) >= maxDeferredToolSearchResults {
			break
		}
	}
	return selected
}

func (r *DeferredToolRegistry) searchRequiredName(raw string) []models.Tool {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return nil
	}
	required := strings.ToLower(parts[0])
	candidates := make([]models.Tool, 0)
	for _, tool := range r.entries {
		if strings.Contains(strings.ToLower(tool.Name), required) {
			candidates = append(candidates, tool)
		}
	}
	if len(parts) > 1 {
		pattern := strings.Join(parts[1:], " ")
		sort.SliceStable(candidates, func(i, j int) bool {
			left := regexScore(pattern, candidates[i])
			right := regexScore(pattern, candidates[j])
			if left == right {
				return candidates[i].Name < candidates[j].Name
			}
			return left > right
		})
	} else {
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	}
	if len(candidates) > maxDeferredToolSearchResults {
		candidates = candidates[:maxDeferredToolSearchResults]
	}
	return candidates
}

func (r *DeferredToolRegistry) searchRegex(query string) []models.Tool {
	regex := compileDeferredToolRegex(query)
	type scoredTool struct {
		score int
		tool  models.Tool
	}
	scored := make([]scoredTool, 0)
	for _, tool := range r.entries {
		searchable := tool.Name + " " + strings.TrimSpace(tool.Description)
		if !regex.MatchString(searchable) {
			continue
		}
		score := 1
		if regex.MatchString(tool.Name) {
			score = 2
		}
		scored = append(scored, scoredTool{score: score, tool: tool})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].tool.Name < scored[j].tool.Name
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > maxDeferredToolSearchResults {
		scored = scored[:maxDeferredToolSearchResults]
	}
	out := make([]models.Tool, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.tool)
	}
	return out
}

func compileDeferredToolRegex(pattern string) *regexp.Regexp {
	regex, err := regexp.Compile("(?i)" + pattern)
	if err == nil {
		return regex
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
}

func regexScore(pattern string, tool models.Tool) int {
	regex := compileDeferredToolRegex(pattern)
	return len(regex.FindAllString(tool.Name+" "+strings.TrimSpace(tool.Description), -1))
}

func DeferredToolSearchTool(search func(string) []models.Tool, onActivate func([]models.Tool)) models.Tool {
	return models.Tool{
		Name: "tool_search",
		Description: "Fetch full schema definitions for deferred tools so they can be called. " +
			"Use `select:name1,name2` for exact matches, plain keywords for search, or `+keyword rest` to require text in the tool name.",
		Groups: []string{"builtin"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Tool search query"},
			},
			"required": []any{"query"},
		},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			query, _ := call.Arguments["query"].(string)
			matched := search(strings.TrimSpace(query))
			if len(matched) == 0 {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusCompleted,
					Content:  "No tools found matching: " + strings.TrimSpace(query),
				}, nil
			}
			onActivate(matched)
			payload := make([]map[string]any, 0, len(matched))
			for _, tool := range matched {
				payload = append(payload, map[string]any{
					"name":        tool.Name,
					"description": strings.TrimSpace(tool.Description),
					"parameters":  tool.InputSchema,
				})
			}
			data, _ := json.MarshalIndent(payload, "", "  ")
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  string(data),
			}, nil
		},
	}
}
