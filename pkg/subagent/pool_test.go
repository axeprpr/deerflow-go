package subagent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

type fakeExecutor struct {
	execute func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error)
}

func (f fakeExecutor) Execute(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
	return f.execute(ctx, task, emit)
}

func TestPoolStartTaskCompletes(t *testing.T) {
	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			emit(TaskEvent{Type: "task_running", Message: "working"})
			return ExecutionResult{
				Result: "done",
				Messages: []models.Message{
					{ID: "m1", SessionID: task.ID, Role: models.RoleAI, Content: "done"},
				},
			}, nil
		},
	}, PoolConfig{Timeout: time.Second})

	var events []TaskEvent
	ctx := WithEventSink(context.Background(), func(evt TaskEvent) {
		events = append(events, evt)
	})

	task, err := pool.StartTask(ctx, "test task", "do work", SubagentConfig{Type: SubagentGeneralPurpose})
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	completed, err := pool.Wait(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if completed.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want %s", completed.Status, TaskStatusCompleted)
	}
	if completed.Result != "done" {
		t.Fatalf("result = %q, want %q", completed.Result, "done")
	}
	if completed.RequestID == "" {
		t.Fatal("RequestID = empty, want generated request id")
	}
	if len(completed.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(completed.Messages))
	}
	if len(events) < 3 {
		t.Fatalf("events = %d, want at least 3", len(events))
	}
	if events[0].Type != "task_started" {
		t.Fatalf("first event = %s, want task_started", events[0].Type)
	}
	if events[0].RequestID == "" {
		t.Fatal("first event missing request id")
	}
	if events[len(events)-1].Type != "task_completed" {
		t.Fatalf("last event = %s, want task_completed", events[len(events)-1].Type)
	}
}

func TestPoolStartTaskTimesOut(t *testing.T) {
	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			<-ctx.Done()
			return ExecutionResult{}, ctx.Err()
		},
	}, PoolConfig{Timeout: 20 * time.Millisecond})

	task, err := pool.StartTask(context.Background(), "timeout task", "sleep", SubagentConfig{Type: SubagentBash})
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	completed, err := pool.Wait(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if completed.Status != TaskStatusTimedOut {
		t.Fatalf("status = %s, want %s", completed.Status, TaskStatusTimedOut)
	}
	if completed.Error == "" {
		t.Fatalf("expected timeout error, got %q", completed.Error)
	}
}

func TestPoolWaitUnknownTask(t *testing.T) {
	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			return ExecutionResult{}, nil
		},
	}, PoolConfig{})

	if _, err := pool.Wait(context.Background(), "missing"); err == nil {
		t.Fatal("Wait() expected error for missing task")
	}
}

func TestPoolHonorsContextConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var current int32
	var maxSeen int32

	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			running := atomic.AddInt32(&current, 1)
			for {
				seen := atomic.LoadInt32(&maxSeen)
				if running <= seen || atomic.CompareAndSwapInt32(&maxSeen, seen, running) {
					break
				}
			}
			started <- struct{}{}
			defer atomic.AddInt32(&current, -1)
			<-release
			return ExecutionResult{Result: task.ID}, nil
		},
	}, PoolConfig{MaxConcurrent: 2, Timeout: time.Second})

	ctx := WithConcurrencyLimit(context.Background(), 1)
	task1, err := pool.StartTask(ctx, "task 1", "one", SubagentConfig{Type: SubagentGeneralPurpose})
	if err != nil {
		t.Fatalf("StartTask(task1) error = %v", err)
	}
	task2, err := pool.StartTask(ctx, "task 2", "two", SubagentConfig{Type: SubagentGeneralPurpose})
	if err != nil {
		t.Fatalf("StartTask(task2) error = %v", err)
	}

	<-started
	select {
	case <-started:
		t.Fatal("second task started before first completed; expected serialized execution")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	if _, err := pool.Wait(context.Background(), task1.ID); err != nil {
		t.Fatalf("Wait(task1) error = %v", err)
	}
	if _, err := pool.Wait(context.Background(), task2.ID); err != nil {
		t.Fatalf("Wait(task2) error = %v", err)
	}
	if got := atomic.LoadInt32(&maxSeen); got != 1 {
		t.Fatalf("max concurrent = %d, want 1", got)
	}
}

func TestPoolResolveConfigPreservesDisallowedTools(t *testing.T) {
	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			return ExecutionResult{}, nil
		},
	}, PoolConfig{
		Timeout: time.Second,
		Defaults: map[SubagentType]SubagentConfig{
			SubagentGeneralPurpose: {
				Type:            SubagentGeneralPurpose,
				MaxTurns:        6,
				Timeout:         time.Second,
				DisallowedTools: []string{"task", "ask_clarification"},
			},
		},
	})

	resolved := pool.resolveConfig(SubagentConfig{Type: SubagentGeneralPurpose})
	if len(resolved.DisallowedTools) != 2 {
		t.Fatalf("disallowed len=%d want 2", len(resolved.DisallowedTools))
	}
	if resolved.DisallowedTools[0] != "task" || resolved.DisallowedTools[1] != "ask_clarification" {
		t.Fatalf("disallowed=%v", resolved.DisallowedTools)
	}

	override := pool.resolveConfig(SubagentConfig{
		Type:            SubagentGeneralPurpose,
		DisallowedTools: []string{"task", "present_files"},
	})
	if len(override.DisallowedTools) != 2 {
		t.Fatalf("override disallowed len=%d want 2", len(override.DisallowedTools))
	}
	if override.DisallowedTools[0] != "task" || override.DisallowedTools[1] != "present_files" {
		t.Fatalf("override disallowed=%v", override.DisallowedTools)
	}
}

func TestPoolDefaultConfigsMatchUpstreamTurns(t *testing.T) {
	pool := NewPool(fakeExecutor{
		execute: func(ctx context.Context, task *Task, emit func(TaskEvent)) (ExecutionResult, error) {
			return ExecutionResult{}, nil
		},
	}, PoolConfig{Timeout: time.Second})

	general := pool.resolveConfig(SubagentConfig{Type: SubagentGeneralPurpose})
	if general.MaxTurns != defaultGeneralPurposeMaxTurns {
		t.Fatalf("general max turns=%d want=%d", general.MaxTurns, defaultGeneralPurposeMaxTurns)
	}

	bash := pool.resolveConfig(SubagentConfig{Type: SubagentBash})
	if bash.MaxTurns != defaultBashMaxTurns {
		t.Fatalf("bash max turns=%d want=%d", bash.MaxTurns, defaultBashMaxTurns)
	}
}
