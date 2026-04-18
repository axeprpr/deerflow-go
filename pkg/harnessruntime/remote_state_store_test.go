package harnessruntime

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPRemoteStateServerRoundTrip(t *testing.T) {
	t.Parallel()

	serverStore := RuntimeStatePlane{
		Snapshots: NewInMemoryRunStore(),
		Events:    NewInMemoryRunEventStore(),
		Threads:   NewInMemoryThreadStateStore(),
	}
	stateServer := NewHTTPRemoteStateServer(serverStore, JSONRemoteStateProtocol{})
	httpServer := httptest.NewServer(stateServer.Handler())
	defer httpServer.Close()

	snapshots := NewRemoteRunSnapshotStore(httpServer.URL, nil, nil)
	events := NewRemoteRunEventStore(httpServer.URL, nil, nil)
	threads := NewRemoteThreadStateStore(httpServer.URL, nil, nil)

	record := RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "running"}
	snapshots.SaveRunSnapshot(RunSnapshot{Record: record})
	snapshot, ok := snapshots.LoadRunSnapshot("run-1")
	if !ok || snapshot.Record.RunID != "run-1" {
		t.Fatalf("LoadRunSnapshot() = %+v, %v", snapshot, ok)
	}

	events.AppendRunEvent("run-1", RunEvent{ID: "run-1:1", Event: "metadata", RunID: "run-1", ThreadID: "thread-1"})
	if got := events.NextRunEventIndex("run-1"); got != 2 {
		t.Fatalf("NextRunEventIndex() = %d", got)
	}
	loadedEvents := events.LoadRunEvents("run-1")
	if len(loadedEvents) != 1 || loadedEvents[0].Event != "metadata" {
		t.Fatalf("LoadRunEvents() = %#v", loadedEvents)
	}

	sub, unsubscribe := events.(RunEventFeed).SubscribeRunEvents("run-1", 2)
	defer unsubscribe()
	events.AppendRunEvent("run-1", RunEvent{ID: "run-1:2", Event: "end", RunID: "run-1", ThreadID: "thread-1"})
	select {
	case event := <-sub:
		if event.ID != "run-1:2" {
			t.Fatalf("event.ID = %q", event.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote event subscription")
	}

	threads.SaveThreadRuntimeState(ThreadRuntimeState{
		ThreadID: "thread-1",
		Status:   "busy",
		Metadata: map[string]any{"graph_id": "lead_agent"},
	})
	if !threads.HasThread("thread-1") {
		t.Fatal("HasThread() = false")
	}
	state, ok := threads.LoadThreadRuntimeState("thread-1")
	if !ok || state.Status != "busy" {
		t.Fatalf("LoadThreadRuntimeState() = %+v, %v", state, ok)
	}
	threads.MarkThreadStatus("thread-1", "idle")
	threads.SetThreadMetadata("thread-1", "assistant_id", "assistant-1")
	threads.ClearThreadMetadata("thread-1", "graph_id")
	state, ok = threads.LoadThreadRuntimeState("thread-1")
	if !ok || state.Status != "idle" {
		t.Fatalf("LoadThreadRuntimeState() after status = %+v, %v", state, ok)
	}
	if state.Metadata["assistant_id"] != "assistant-1" {
		t.Fatalf("assistant_id = %#v", state.Metadata["assistant_id"])
	}
	if _, exists := state.Metadata["graph_id"]; exists {
		t.Fatalf("graph_id still present: %#v", state.Metadata)
	}
}

func TestRuntimeNodeConfigBuildRemoteStateStores(t *testing.T) {
	t.Parallel()

	config := DefaultRuntimeNodeConfig("test", t.TempDir())
	config.State.Backend = RuntimeStateStoreBackendRemote
	config.State.SnapshotBackend = RuntimeStateStoreBackendRemote
	config.State.EventBackend = RuntimeStateStoreBackendRemote
	config.State.ThreadBackend = RuntimeStateStoreBackendRemote
	config.State.URL = "http://state.example"

	plane := config.BuildStatePlane()
	if _, ok := plane.Snapshots.(*remoteRunSnapshotStore); !ok {
		t.Fatalf("snapshot store type = %T", plane.Snapshots)
	}
	if _, ok := plane.Events.(*remoteRunEventStore); !ok {
		t.Fatalf("event store type = %T", plane.Events)
	}
	if _, ok := plane.Threads.(*remoteThreadStateStore); !ok {
		t.Fatalf("thread store type = %T", plane.Threads)
	}
}

func TestHTTPRemoteStateServerTryCancelStaleRunRoundTrip(t *testing.T) {
	t.Parallel()

	serverStore := RuntimeStatePlane{
		Snapshots: NewInMemoryRunStore(),
		Events:    NewInMemoryRunEventStore(),
		Threads:   NewInMemoryThreadStateStore(),
	}
	stateServer := NewHTTPRemoteStateServer(serverStore, JSONRemoteStateProtocol{})
	httpServer := httptest.NewServer(stateServer.Handler())
	defer httpServer.Close()

	snapshots := NewRemoteRunSnapshotStore(httpServer.URL, nil, nil)
	cancelStore, ok := snapshots.(RunSnapshotCancelStore)
	if !ok {
		t.Fatalf("snapshot store %T does not implement RunSnapshotCancelStore", snapshots)
	}

	now := time.Now().UTC().Add(-2 * time.Minute)
	snapshots.SaveRunSnapshot(RunSnapshot{
		Record: RunRecord{
			RunID:     "run-remote-cancel",
			ThreadID:  "thread-remote-cancel",
			Status:    "running",
			CreatedAt: now,
			UpdatedAt: now,
		},
	})

	record, changed := cancelStore.TryCancelStaleRun("run-remote-cancel", time.Now().UTC().Add(-30*time.Second))
	if !changed {
		t.Fatal("TryCancelStaleRun() changed=false, want true")
	}
	if record.Status != "interrupted" || !record.Outcome.Interrupted {
		t.Fatalf("record = %#v", record)
	}

	if _, changed := cancelStore.TryCancelStaleRun("run-remote-cancel", time.Now().UTC().Add(-30*time.Second)); changed {
		t.Fatal("second TryCancelStaleRun() changed=true, want false")
	}
}
