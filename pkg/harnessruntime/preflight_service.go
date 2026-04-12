package harnessruntime

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/google/uuid"
)

type RunRecord struct {
	RunID           string
	ThreadID        string
	AssistantID     string
	Attempt         int
	ResumeFromEvent int
	ResumeReason    string
	Status          string
	Error           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SessionSnapshot struct {
	PresentFiles     *tools.PresentFileRegistry
	ExistingMessages []models.Message
}

type PreflightRuntime interface {
	PrepareSession(threadID string) SessionSnapshot
	MarkThreadStatus(threadID string, status string)
	SetThreadMetadata(threadID string, key string, value any)
	ClearThreadMetadata(threadID string, key string)
	SaveRunRecord(record RunRecord)
}

type PreflightInput struct {
	RouteThreadID      string
	RequestedThreadID  string
	RequestedThreadIDX string
	AssistantID        string
	AssistantIDX       string
	NewMessages        []models.Message
}

type PreflightResult struct {
	ThreadID         string
	AssistantID      string
	PresentFiles     *tools.PresentFileRegistry
	ExistingMessages []models.Message
	Messages         []models.Message
	Run              RunRecord
}

type PreflightService struct {
	runtime PreflightRuntime
	now     func() time.Time
	newID   func() string
}

func NewPreflightService(runtime PreflightRuntime) PreflightService {
	return PreflightService{
		runtime: runtime,
		now: func() time.Time {
			return time.Now().UTC()
		},
		newID: func() string {
			return uuid.New().String()
		},
	}
}

func (s PreflightService) Resolve(input PreflightInput) (threadID string, assistantID string) {
	threadID = strings.TrimSpace(input.RouteThreadID)
	if threadID == "" {
		threadID = firstNonEmpty(input.RequestedThreadID, input.RequestedThreadIDX)
	}
	if threadID == "" {
		threadID = s.newID()
	}
	assistantID = firstNonEmpty(input.AssistantID, input.AssistantIDX)
	return threadID, assistantID
}

func (s PreflightService) Prepare(input PreflightInput) PreflightResult {
	threadID, assistantID := s.Resolve(input)

	snapshot := SessionSnapshot{}
	if s.runtime != nil {
		snapshot = s.runtime.PrepareSession(threadID)
		s.runtime.MarkThreadStatus(threadID, "busy")
	}

	now := time.Now().UTC()
	if s.now != nil {
		now = s.now()
	}
	runID := ""
	if s.newID != nil {
		runID = s.newID()
	}
	record := RunRecord{
		RunID:       runID,
		ThreadID:    threadID,
		AssistantID: assistantID,
		Attempt:     1,
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.SetThreadMetadata(threadID, "assistant_id", assistantID)
		s.runtime.SetThreadMetadata(threadID, "graph_id", firstNonEmpty(assistantID, "lead_agent"))
		s.runtime.SetThreadMetadata(threadID, "run_id", runID)
		s.runtime.ClearThreadMetadata(threadID, "interrupts")
	}

	return PreflightResult{
		ThreadID:         threadID,
		AssistantID:      assistantID,
		PresentFiles:     snapshot.PresentFiles,
		ExistingMessages: append([]models.Message(nil), snapshot.ExistingMessages...),
		Messages:         append(append([]models.Message(nil), snapshot.ExistingMessages...), input.NewMessages...),
		Run:              record,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
