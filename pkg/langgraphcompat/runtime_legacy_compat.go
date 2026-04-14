package langgraphcompat

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"gopkg.in/yaml.v3"
)

func configuredGatewayModels(defaultModel string) []gatewayModel {
	if models := configuredGatewayModelsFromJSONEnv(); len(models) > 0 {
		return models
	}
	if models := configuredGatewayModelsFromListEnv(); len(models) > 0 {
		return models
	}
	if models := configuredGatewayModelsFromConfig(); len(models) > 0 {
		return models
	}
	if strings.TrimSpace(defaultModel) == "" {
		return nil
	}
	return []gatewayModel{newGatewayDefaultModel(defaultModel)}
}

func configuredGatewayModelsFromJSONEnv() []gatewayModel {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_MODELS_JSON"))
	if raw == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	models := make([]gatewayModel, 0, len(items))
	for _, item := range items {
		models = append(models, gatewayModelFromMap(item))
	}
	return normalizeGatewayModels(models)
}

func configuredGatewayModelsFromListEnv() []gatewayModel {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_MODELS"))
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		return gatewayModelsFromEnv(raw)
	}
	parts := strings.Split(raw, ",")
	models := make([]gatewayModel, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "=") {
			name, model, _ := strings.Cut(part, "=")
			models = append(models, gatewayModel{
				Name:  strings.TrimSpace(name),
				Model: strings.TrimSpace(model),
			})
			continue
		}
		models = append(models, newGatewayDefaultModel(part))
	}
	return normalizeGatewayModels(models)
}

func configuredGatewayModelsFromConfig() []gatewayModel {
	path, ok := resolveGatewayConfigPath()
	if !ok {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Models []map[string]any `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	models := make([]gatewayModel, 0, len(raw.Models))
	for _, item := range raw.Models {
		models = append(models, gatewayModelFromMap(item))
	}
	return normalizeGatewayModels(models)
}

func (s *Server) resolveRunConfig(cfg runConfig, runtimeContext map[string]any) (harness.AgentSpec, error) {
	runtimeContext = runtimeContextWithRunConfig(cfg, runtimeContext)
	resolvedModel, catalogModel := s.resolveConfiguredModel(cfg.ModelName)
	agentSpec := harness.AgentSpec{
		AgentType:              cfg.AgentType,
		ExecutionMode:          cfg.ExecutionMode,
		MaxTurns:               s.maxTurns,
		MaxConcurrentSubagents: subagentLimitFromRunConfig(cfg),
		Model:                  resolvedModel,
		ReasoningEffort:        cfg.ReasoningEffort,
		Temperature:            cfg.Temperature,
		MaxTokens:              cfg.MaxTokens,
	}
	if cfg.MaxTurns != nil && *cfg.MaxTurns > 0 {
		agentSpec.MaxTurns = *cfg.MaxTurns
	}
	if catalogModel != nil && !catalogModel.SupportsReasoningEffort {
		agentSpec.ReasoningEffort = ""
	}
	if catalogModel != nil && catalogModel.RequestTimeoutSeconds > 0 {
		agentSpec.RequestTimeout = time.Duration(catalogModel.RequestTimeoutSeconds * float64(time.Second))
	}

	if agentSpec.AgentType == "" {
		agentSpec.AgentType = agent.AgentTypeGeneral
	}
	basePrompt := agent.GetAgentTypeConfig(agentSpec.AgentType).SystemPrompt
	if profile := strings.TrimSpace(s.userProfile); profile != "" {
		basePrompt = strings.TrimSpace(basePrompt + "\n\nUSER.md:\n" + profile)
	}
	agentSpec.SystemPrompt = strings.TrimSpace(basePrompt + "\n\n" + s.environmentPrompt(runtimeContext))
	return agentSpec, nil
}

func runtimeContextWithRunConfig(cfg runConfig, runtimeContext map[string]any) map[string]any {
	merged := make(map[string]any, len(runtimeContext)+2)
	for key, value := range runtimeContext {
		merged[key] = value
	}
	if cfg.SubagentEnabled != nil {
		merged["subagent_enabled"] = *cfg.SubagentEnabled
	}
	if limit := subagentLimitFromRunConfig(cfg); limit > 0 {
		merged["max_concurrent_subagents"] = limit
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func subagentEnabledFromRunConfig(cfg runConfig) bool {
	return cfg.SubagentEnabled != nil && *cfg.SubagentEnabled
}

func subagentLimitFromRunConfig(cfg runConfig) int {
	if cfg.MaxConcurrentSubagents != nil && *cfg.MaxConcurrentSubagents > 0 {
		return *cfg.MaxConcurrentSubagents
	}
	if subagentEnabledFromRunConfig(cfg) {
		return defaultGatewaySubagentMaxConcurrent
	}
	return 0
}

func (s *Server) resolveConfiguredModel(name string) (string, *gatewayModel) {
	resolved := strings.TrimSpace(firstNonEmpty(name, s.defaultModel))
	if resolved == "" {
		return "", nil
	}

	s.uiStateMu.RLock()
	model, ok := s.findModelLocked(resolved)
	s.uiStateMu.RUnlock()
	if !ok {
		return resolved, nil
	}

	providerModel := strings.TrimSpace(firstNonEmpty(model.Model, model.Name, model.ID, resolved))
	return providerModel, &model
}

func (s *Server) threadHistory(threadID string) []ThreadState {
	return s.loadThreadHistory(threadID)
}

func uploadArtifactURL(threadID, filename string) string {
	return "/api/threads/" + strings.TrimSpace(threadID) + "/artifacts/mnt/user-data/uploads/" + sanitizeFilename(filename)
}

func validateThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("invalid thread_id")
	}
	if strings.Contains(threadID, ".") || strings.Contains(threadID, " ") ||
		strings.Contains(threadID, "/") || strings.Contains(threadID, "\\") ||
		strings.Contains(threadID, "..") {
		return fmt.Errorf("invalid thread_id")
	}
	return nil
}

func resolveAgentToolRegistry(registry *tools.Registry, groups []string) *tools.Registry {
	if registry == nil {
		return tools.NewRegistry()
	}
	allowed := map[string]struct{}{}
	add := func(names ...string) {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" {
				allowed[name] = struct{}{}
			}
		}
	}
	for _, group := range groups {
		switch strings.ToLower(strings.TrimSpace(group)) {
		case "file":
			add("ask_clarification", "ls", "present_files", "read_file", "glob", "grep", "str_replace", "write_file")
		case "file:read":
			add("ask_clarification", "ls", "read_file", "glob", "grep")
		case "file:write":
			add("ask_clarification", "present_files", "str_replace", "write_file")
		}
	}
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	return registry.Restrict(names)
}

func waitForRunSubscriber(t interface{ Fatalf(string, ...any) }, s *Server, runID string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.runSubscriberCount(runID) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for subscriber on %q", runID)
}
