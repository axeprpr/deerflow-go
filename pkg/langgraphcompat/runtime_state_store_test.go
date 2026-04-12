package langgraphcompat

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestLocalRunSnapshotStorePersistsAndLoadsSnapshots(t *testing.T) {
	server := &Server{
		runs:     map[string]*Run{},
		dataRoot: t.TempDir(),
	}
	server.snapshotStore = newLocalRunSnapshotStore(server)

	snapshot := harnessruntime.RunSnapshot{
		Record: harnessruntime.RunRecord{
			RunID:       "run-1",
			ThreadID:    "thread-1",
			AssistantID: "lead_agent",
			Status:      "running",
			CreatedAt:   time.Now().UTC().Add(-time.Minute),
			UpdatedAt:   time.Now().UTC().Add(-time.Minute),
		},
		Events: []harnessruntime.RunEvent{{
			ID:       "run-1:1",
			Event:    "metadata",
			RunID:    "run-1",
			ThreadID: "thread-1",
		}},
	}

	server.snapshotStore.SaveRunSnapshot(snapshot)

	loaded, ok := server.snapshotStore.LoadRunSnapshot("run-1")
	if !ok {
		t.Fatal("LoadRunSnapshot() missing saved snapshot")
	}
	if loaded.Record.RunID != "run-1" || loaded.Record.ThreadID != "thread-1" {
		t.Fatalf("loaded record = %#v", loaded.Record)
	}
	if len(loaded.Events) != 1 || loaded.Events[0].Event != "metadata" {
		t.Fatalf("loaded events = %#v", loaded.Events)
	}

	data, err := os.ReadFile(server.runStatePath("run-1"))
	if err != nil {
		t.Fatalf("ReadFile(runStatePath) error = %v", err)
	}
	if !strings.Contains(string(data), `"run_id": "run-1"`) {
		t.Fatalf("persisted run file missing run id: %s", string(data))
	}
}

func TestLocalRunEventStoreAppendsEventsAndUpdatesSnapshot(t *testing.T) {
	server := &Server{
		runs:     map[string]*Run{},
		dataRoot: t.TempDir(),
	}
	server.snapshotStore = newLocalRunSnapshotStore(server)
	server.eventStore = newLocalRunEventStore(server.snapshotStore)

	server.snapshotStore.SaveRunSnapshot(harnessruntime.RunSnapshot{
		Record: harnessruntime.RunRecord{
			RunID:       "run-1",
			ThreadID:    "thread-1",
			AssistantID: "lead_agent",
			Status:      "running",
			CreatedAt:   time.Now().UTC().Add(-time.Minute),
			UpdatedAt:   time.Now().UTC().Add(-time.Minute),
		},
	})

	server.eventStore.AppendRunEvent("run-1", harnessruntime.RunEvent{
		ID:       "run-1:1",
		Event:    "values",
		RunID:    "run-1",
		ThreadID: "thread-1",
	})

	if got := server.eventStore.NextRunEventIndex("run-1"); got != 2 {
		t.Fatalf("NextRunEventIndex() = %d, want 2", got)
	}

	run := server.getRun("run-1")
	if run == nil {
		t.Fatal("getRun() returned nil after AppendRunEvent")
	}
	if len(run.Events) != 1 || run.Events[0].Event != "values" {
		t.Fatalf("run events = %#v", run.Events)
	}
	if run.UpdatedAt.IsZero() {
		t.Fatal("run UpdatedAt was not updated")
	}
}

func TestLocalThreadStateStoreUpdatesSessionMetadataAndStatus(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
	}
	server.ensureSession("thread-1", nil)

	store := server.ensureThreadStateStore()
	store.MarkThreadStatus("thread-1", "busy")
	store.SetThreadMetadata("thread-1", "assistant_id", "lead_agent")
	store.ClearThreadMetadata("thread-1", "assistant_id")

	if !store.HasThread("thread-1") {
		t.Fatal("HasThread() = false, want true")
	}
	session := server.ensureSession("thread-1", nil)
	if session.Status != "busy" {
		t.Fatalf("session status = %q, want busy", session.Status)
	}
	if _, ok := session.Metadata["assistant_id"]; ok {
		t.Fatalf("assistant_id metadata = %#v, want deleted", session.Metadata["assistant_id"])
	}
}
