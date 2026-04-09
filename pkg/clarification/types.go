package clarification

import "time"

type Clarification struct {
	ID                string                `json:"id"`
	ThreadID          string                `json:"thread_id,omitempty"`
	Type              string                `json:"type,omitempty"`
	ClarificationType string                `json:"clarification_type,omitempty"`
	Context           string                `json:"context,omitempty"`
	Question          string                `json:"question"`
	Options           []ClarificationOption `json:"options,omitempty"`
	Default           string                `json:"default,omitempty"`
	Required          bool                  `json:"required"`
	Answer            string                `json:"answer,omitempty"`
	ResolvedAt        time.Time             `json:"resolved_at,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
}

type ClarificationOption struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type ClarificationRequest struct {
	Type              string                `json:"type"`
	ClarificationType string                `json:"clarification_type,omitempty"`
	Context           string                `json:"context,omitempty"`
	Question          string                `json:"question"`
	Options           []ClarificationOption `json:"options,omitempty"`
	Default           string                `json:"default,omitempty"`
	Required          bool                  `json:"required"`
}
