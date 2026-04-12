package harnessruntime

import (
	"context"
	"testing"
	"time"

	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type memoryUpdateExecutorStub struct {
	done chan MemoryUpdateJob
}

func (s memoryUpdateExecutorStub) Execute(_ context.Context, job MemoryUpdateJob) error {
	s.done <- job
	return nil
}

func TestInProcessMemoryUpdateQueueExecutesJobs(t *testing.T) {
	done := make(chan MemoryUpdateJob, 1)
	queue := NewInProcessMemoryUpdateQueue(memoryUpdateExecutorStub{done: done}, 1, time.Second)
	defer queue.Close()

	queue.Enqueue(MemoryUpdateJob{
		Scope: pkgmemory.AgentScope("planner"),
		Messages: []models.Message{{
			Role:    models.RoleAI,
			Content: "done",
		}},
	})

	select {
	case job := <-done:
		if job.Scope.Key() != "agent:planner" {
			t.Fatalf("scope key = %q, want %q", job.Scope.Key(), "agent:planner")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("memory update job was not executed")
	}
}
