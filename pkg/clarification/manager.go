package clarification

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Manager struct {
	mu             sync.RWMutex
	clarifications map[string]*Clarification
	pendingCh      chan *Clarification
}

var clarificationSeq uint64

func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = 16
	}
	return &Manager{
		clarifications: make(map[string]*Clarification),
		pendingCh:      make(chan *Clarification, buffer),
	}
}

func (m *Manager) Request(ctx context.Context, req ClarificationRequest) (*Clarification, error) {
	if m == nil {
		return nil, fmt.Errorf("clarification manager is nil")
	}

	kind := normalizeType(firstNonEmptyClarificationType(req.ClarificationType, req.Type), req.Options)
	clarificationType := normalizeClarificationType(firstNonEmptyClarificationType(req.ClarificationType, req.Type), kind, req.Options)
	options := normalizeOptions(req.Options)
	question := fallbackQuestion(strings.TrimSpace(req.Question), clarificationType, strings.TrimSpace(req.Context), options)

	now := time.Now().UTC()
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
		CreatedAt:         now,
	}

	m.mu.Lock()
	m.clarifications[item.ID] = clone(item)
	m.mu.Unlock()

	select {
	case m.pendingCh <- clone(item):
	default:
	}
	EmitEvent(ctx, item)
	return clone(item), nil
}

func (m *Manager) Resolve(id string, answer string) error {
	if m == nil {
		return fmt.Errorf("clarification manager is nil")
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("clarification id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.clarifications[id]
	if !ok {
		return fmt.Errorf("clarification %q not found", id)
	}

	item.Answer = strings.TrimSpace(answer)
	item.ResolvedAt = time.Now().UTC()
	return nil
}

func (m *Manager) Get(id string) (*Clarification, bool) {
	if m == nil {
		return nil, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	item, ok := m.clarifications[strings.TrimSpace(id)]
	if !ok {
		return nil, false
	}
	return clone(item), true
}

func (m *Manager) ListByThread(threadID string) []Clarification {
	if m == nil {
		return nil
	}

	threadID = strings.TrimSpace(threadID)
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]Clarification, 0)
	for _, item := range m.clarifications {
		if threadID == "" || item.ThreadID == threadID {
			items = append(items, *clone(item))
		}
	}
	slices.SortFunc(items, func(a, b Clarification) int {
		if cmp := a.CreatedAt.Compare(b.CreatedAt); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	})
	return items
}

func (m *Manager) Pending() <-chan *Clarification {
	if m == nil {
		return nil
	}
	return m.pendingCh
}

type threadIDContextKey struct{}
type eventSinkContextKey struct{}
type managerContextKey struct{}

type EventSink func(*Clarification)

func WithThreadID(ctx context.Context, threadID string) context.Context {
	if strings.TrimSpace(threadID) == "" {
		return ctx
	}
	return context.WithValue(ctx, threadIDContextKey{}, strings.TrimSpace(threadID))
}

func ThreadIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	threadID, _ := ctx.Value(threadIDContextKey{}).(string)
	return strings.TrimSpace(threadID)
}

func WithEventSink(ctx context.Context, sink EventSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, eventSinkContextKey{}, sink)
}

func WithManager(ctx context.Context, manager *Manager) context.Context {
	if manager == nil {
		return ctx
	}
	return context.WithValue(ctx, managerContextKey{}, manager)
}

func ManagerFromContext(ctx context.Context) *Manager {
	if ctx == nil {
		return nil
	}
	manager, _ := ctx.Value(managerContextKey{}).(*Manager)
	return manager
}

func EmitEvent(ctx context.Context, item *Clarification) {
	if ctx == nil || item == nil {
		return
	}
	sink, _ := ctx.Value(eventSinkContextKey{}).(EventSink)
	if sink != nil {
		sink(clone(item))
	}
}

func normalizeType(kind string, options []ClarificationOption) string {
	kind = strings.TrimSpace(kind)
	switch kind {
	case "choice", "text", "confirm":
		return kind
	case "approach_choice":
		return "choice"
	case "risk_confirmation":
		return "confirm"
	case "missing_info", "ambiguous_requirement", "suggestion":
		return "text"
	}
	if len(options) > 0 {
		return "choice"
	}
	return "text"
}

func normalizeClarificationType(rawType string, kind string, options []ClarificationOption) string {
	rawType = strings.TrimSpace(rawType)
	switch rawType {
	case "missing_info", "ambiguous_requirement", "approach_choice", "risk_confirmation", "suggestion":
		return rawType
	}

	switch kind {
	case "choice":
		return "approach_choice"
	case "confirm":
		return "risk_confirmation"
	case "text":
		if len(options) > 0 {
			return "approach_choice"
		}
		return "missing_info"
	default:
		if len(options) > 0 {
			return "approach_choice"
		}
		return "missing_info"
	}
}

func normalizeOptions(options []ClarificationOption) []ClarificationOption {
	if len(options) == 0 {
		return nil
	}

	out := make([]ClarificationOption, 0, len(options))
	for i, option := range options {
		label := strings.TrimSpace(option.Label)
		value := strings.TrimSpace(option.Value)
		if label == "" && value == "" {
			continue
		}
		if label == "" {
			label = value
		}
		if value == "" {
			value = label
		}
		id := strings.TrimSpace(option.ID)
		if id == "" {
			id = fmt.Sprintf("option_%d", i+1)
		}
		out = append(out, ClarificationOption{
			ID:    id,
			Label: label,
			Value: value,
		})
	}
	return out
}

func clone(item *Clarification) *Clarification {
	if item == nil {
		return nil
	}
	copyItem := *item
	if len(item.Options) > 0 {
		copyItem.Options = append([]ClarificationOption(nil), item.Options...)
	}
	return &copyItem
}

func firstNonEmptyClarificationType(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newClarificationID() string {
	seq := atomic.AddUint64(&clarificationSeq, 1)
	return fmt.Sprintf("clarify_%d_%d", time.Now().UTC().UnixNano(), seq)
}
