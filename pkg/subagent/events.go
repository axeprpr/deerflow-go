package subagent

import "context"

type TaskEvent struct {
	Type          string `json:"type"`
	TaskID        string `json:"task_id"`
	RequestID     string `json:"request_id,omitempty"`
	Description   string `json:"description,omitempty"`
	Message       any    `json:"message,omitempty"`
	MessageIndex  int    `json:"message_index,omitempty"`
	TotalMessages int    `json:"total_messages,omitempty"`
	Result        string `json:"result,omitempty"`
	Error         string `json:"error,omitempty"`
}

type eventSinkContextKey struct{}
type concurrencyLimitContextKey struct{}

type EventSink func(TaskEvent)

func WithEventSink(ctx context.Context, sink EventSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, eventSinkContextKey{}, sink)
}

func EmitEvent(ctx context.Context, evt TaskEvent) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(eventSinkContextKey{}).(EventSink)
	if sink != nil {
		sink(evt)
	}
}

func WithConcurrencyLimit(ctx context.Context, max int) context.Context {
	if ctx == nil || max <= 0 {
		return ctx
	}
	return context.WithValue(ctx, concurrencyLimitContextKey{}, make(chan struct{}, max))
}

func concurrencySemaphoreFromContext(ctx context.Context) chan struct{} {
	if ctx == nil {
		return nil
	}
	sem, _ := ctx.Value(concurrencyLimitContextKey{}).(chan struct{})
	return sem
}
