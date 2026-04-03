package clarification

import (
	"context"
	"testing"
	"time"
)

func TestManagerRequestResolveAndGet(t *testing.T) {
	manager := NewManager(1)

	item, err := manager.Request(WithThreadID(context.Background(), "thread-1"), ClarificationRequest{
		Question: "Which option?",
		Options: []ClarificationOption{
			{Label: "A", Value: "a"},
			{Label: "B", Value: "b"},
		},
		Required: true,
	})
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if item.ID == "" {
		t.Fatal("Request() returned empty ID")
	}
	if item.Type != "choice" {
		t.Fatalf("Request() type = %q, want choice", item.Type)
	}
	if item.ThreadID != "thread-1" {
		t.Fatalf("Request() thread_id = %q", item.ThreadID)
	}

	got, ok := manager.Get(item.ID)
	if !ok {
		t.Fatal("Get() did not find clarification")
	}
	if got.Question != item.Question {
		t.Fatalf("Get() question = %q, want %q", got.Question, item.Question)
	}

	if err := manager.Resolve(item.ID, "a"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	resolved, ok := manager.Get(item.ID)
	if !ok {
		t.Fatal("Get() after Resolve() did not find clarification")
	}
	if resolved.Answer != "a" {
		t.Fatalf("resolved answer = %q, want a", resolved.Answer)
	}
	if resolved.ResolvedAt.IsZero() {
		t.Fatal("resolved_at was not set")
	}
}

func TestManagerEmitsEventAndPending(t *testing.T) {
	manager := NewManager(1)

	events := make(chan *Clarification, 1)
	ctx := WithEventSink(WithThreadID(context.Background(), "thread-2"), func(item *Clarification) {
		events <- item
	})

	item, err := manager.Request(ctx, ClarificationRequest{Question: "Need more detail"})
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}

	select {
	case emitted := <-events:
		if emitted.ID != item.ID {
			t.Fatalf("emitted id = %q, want %q", emitted.ID, item.ID)
		}
	default:
		t.Fatal("expected clarification event to be emitted")
	}

	select {
	case pending := <-manager.Pending():
		if pending.ID != item.ID {
			t.Fatalf("pending id = %q, want %q", pending.ID, item.ID)
		}
	default:
		t.Fatal("expected clarification on pending channel")
	}
}

func TestManagerListByThreadSorted(t *testing.T) {
	manager := NewManager(1)
	now := time.Now().UTC()

	manager.clarifications["later"] = &Clarification{
		ID:        "later",
		ThreadID:  "thread-1",
		Question:  "Later",
		CreatedAt: now.Add(2 * time.Minute),
	}
	manager.clarifications["other"] = &Clarification{
		ID:        "other",
		ThreadID:  "thread-2",
		Question:  "Other",
		CreatedAt: now.Add(time.Minute),
	}
	manager.clarifications["first"] = &Clarification{
		ID:        "first",
		ThreadID:  "thread-1",
		Question:  "First",
		CreatedAt: now,
	}

	items := manager.ListByThread("thread-1")
	if len(items) != 2 {
		t.Fatalf("ListByThread() len = %d, want 2", len(items))
	}
	if items[0].ID != "first" || items[1].ID != "later" {
		t.Fatalf("ListByThread() order = [%s %s], want [first later]", items[0].ID, items[1].ID)
	}
}
