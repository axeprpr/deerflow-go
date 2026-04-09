package clarification

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func AskClarificationTool(manager *Manager) models.Tool {
	return models.Tool{
		Name:        "ask_clarification",
		Description: "Ask the user for clarification when you need more information to proceed.\n\nUse this tool when you encounter situations where you cannot proceed without user input:\n\n- Missing information: Required details not provided.\n- Ambiguous requirements: Multiple valid interpretations exist.\n- Approach choices: Several valid approaches exist and you need user preference.\n- Risky operations: Destructive actions that need explicit confirmation.\n- Suggestions: You have a recommendation but want user approval before proceeding.\n\nThe execution will be interrupted and the question will be presented to the user.\nWait for the user's response before continuing.\n\nWhen to use ask_clarification:\n- You need information that was not provided in the user's request.\n- The requirement can be interpreted in multiple ways.\n- Multiple valid implementation approaches exist.\n- You are about to perform a potentially dangerous operation.\n- You have a recommendation but need user approval.\n\nBest practices:\n- Ask ONE clarification at a time for clarity.\n- Be specific and clear in your question.\n- Do not make assumptions when clarification is needed.\n- For risky operations, ALWAYS ask for confirmation.\n- After calling this tool, execution will be interrupted automatically.",
		Groups:      []string{"builtin", "interaction"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"clarification_type": map[string]any{
					"type":        "string",
					"enum":        []any{"missing_info", "ambiguous_requirement", "approach_choice", "risk_confirmation", "suggestion"},
					"description": "The type of clarification needed.",
				},
				"question": map[string]any{
					"type":        "string",
					"description": "The clarification question to ask the user. Ask one clear question at a time.",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Optional context explaining why clarification is needed.",
				},
				"options": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":        "string",
						"description": "A user-facing option label.",
					},
					"description": "Optional list of choices for approach_choice or suggestion clarifications.",
				},
			},
			"required": []any{"question", "clarification_type"},
		},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			if manager == nil {
				err := fmt.Errorf("clarification manager is required")
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}

			req, err := parseRequest(call.Arguments)
			if err != nil {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}

			item, err := manager.Request(ctx, req)
			if err != nil {
				return models.ToolResult{
					CallID:   call.ID,
					ToolName: call.Name,
					Status:   models.CallStatusFailed,
					Error:    err.Error(),
				}, err
			}

			content := formatClarificationMessage(item)
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Status:   models.CallStatusCompleted,
				Content:  content,
				Data: map[string]any{
					"id":                 item.ID,
					"thread_id":          item.ThreadID,
					"type":               item.Type,
					"clarification_type": item.ClarificationType,
					"context":            item.Context,
					"question":           item.Question,
					"options":            item.Options,
					"default":            item.Default,
					"required":           item.Required,
					"created_at":         item.CreatedAt.Format(time.RFC3339Nano),
				},
			}, nil
		},
	}
}

func InterceptToolCall(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	req, err := parseRequest(call.Arguments)
	if err != nil {
		return models.ToolResult{
			CallID:   call.ID,
			ToolName: call.Name,
			Status:   models.CallStatusFailed,
			Error:    err.Error(),
		}, err
	}

	item, err := createClarification(ctx, req)
	if err != nil {
		return models.ToolResult{
			CallID:   call.ID,
			ToolName: call.Name,
			Status:   models.CallStatusFailed,
			Error:    err.Error(),
		}, err
	}

	return buildToolResult(call, item), nil
}

func formatClarificationMessage(item *Clarification) string {
	if item == nil {
		return ""
	}

	question := strings.TrimSpace(item.Question)
	clarificationType := strings.TrimSpace(item.ClarificationType)
	if clarificationType == "" {
		clarificationType = "missing_info"
	}

	typeIcons := map[string]string{
		"missing_info":          "❓",
		"ambiguous_requirement": "🤔",
		"approach_choice":       "🔀",
		"risk_confirmation":     "⚠️",
		"suggestion":            "💡",
	}
	icon := typeIcons[clarificationType]
	if icon == "" {
		icon = "❓"
	}

	parts := make([]string, 0, 2+len(item.Options))
	if context := strings.TrimSpace(item.Context); context != "" {
		parts = append(parts, icon+" "+context)
		parts = append(parts, "\n"+question)
	} else {
		parts = append(parts, icon+" "+question)
	}

	for i, option := range item.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			label = strings.TrimSpace(option.Value)
		}
		if label == "" {
			continue
		}
		if len(parts) == 1 {
			parts = append(parts, "")
		}
		parts = append(parts, "  "+strconv.Itoa(i+1)+". "+label)
	}
	return strings.Join(parts, "\n")
}

func createClarification(ctx context.Context, req ClarificationRequest) (*Clarification, error) {
	if manager := ManagerFromContext(ctx); manager != nil {
		return manager.Request(ctx, req)
	}

	kind := normalizeType(firstNonEmptyClarificationType(req.ClarificationType, req.Type), req.Options)
	clarificationType := normalizeClarificationType(firstNonEmptyClarificationType(req.ClarificationType, req.Type), kind, req.Options)
	options := normalizeOptions(req.Options)
	question := fallbackQuestion(strings.TrimSpace(req.Question), clarificationType, strings.TrimSpace(req.Context), options)

	item := &Clarification{
		ID:                newClarificationID(),
		ThreadID:          ThreadIDFromContext(ctx),
		Type:              kind,
		ClarificationType: clarificationType,
		Context:           strings.TrimSpace(req.Context),
		Question:          question,
		Options:           options,
		Default:           strings.TrimSpace(req.Default),
		Required:          req.Required,
		CreatedAt:         time.Now().UTC(),
	}
	EmitEvent(ctx, item)
	return clone(item), nil
}

func buildToolResult(call models.ToolCall, item *Clarification) models.ToolResult {
	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  formatClarificationMessage(item),
		Data: map[string]any{
			"id":                 item.ID,
			"thread_id":          item.ThreadID,
			"type":               item.Type,
			"clarification_type": item.ClarificationType,
			"context":            item.Context,
			"question":           item.Question,
			"options":            item.Options,
			"default":            item.Default,
			"required":           item.Required,
			"created_at":         item.CreatedAt.Format(time.RFC3339Nano),
		},
	}
}

func fallbackQuestion(question string, clarificationType string, context string, options []ClarificationOption) string {
	question = strings.TrimSpace(question)
	if question != "" {
		return question
	}

	switch strings.TrimSpace(clarificationType) {
	case "approach_choice":
		if len(options) > 0 {
			return "Please choose one of the available options."
		}
		return "Please choose the approach you want me to take."
	case "risk_confirmation":
		return "Please confirm whether I should proceed."
	case "suggestion":
		return "Would you like me to proceed with this suggestion?"
	case "ambiguous_requirement":
		if context != "" {
			return "Please clarify the requirement so I can continue."
		}
		return "Please clarify what you want me to do."
	default:
		return "Please provide the missing information needed to continue."
	}
}

func parseRequest(args map[string]any) (ClarificationRequest, error) {
	req := ClarificationRequest{
		Type: strings.TrimSpace(stringValue(args["type"])),
		ClarificationType: strings.TrimSpace(firstNonEmptyString(
			args["clarification_type"],
			args["clarificationType"],
			args["type"],
		)),
		Context:  strings.TrimSpace(stringValue(args["context"])),
		Question: strings.TrimSpace(stringValue(args["question"])),
		Default:  strings.TrimSpace(stringValue(args["default"])),
		Required: boolValue(args["required"]),
	}

	if rawOptions, ok := args["options"].([]any); ok {
		req.Options = make([]ClarificationOption, 0, len(rawOptions))
		for _, raw := range rawOptions {
			switch option := raw.(type) {
			case string:
				option = strings.TrimSpace(option)
				if option == "" {
					continue
				}
				req.Options = append(req.Options, ClarificationOption{
					Label: option,
					Value: option,
				})
			case map[string]any:
				req.Options = append(req.Options, ClarificationOption{
					ID:    strings.TrimSpace(stringValue(option["id"])),
					Label: strings.TrimSpace(stringValue(option["label"])),
					Value: strings.TrimSpace(stringValue(option["value"])),
				})
			}
		}
	}

	return req, nil
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return text
		}
	}
	return ""
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}
