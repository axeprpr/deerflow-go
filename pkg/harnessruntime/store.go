package harnessruntime

import "time"

// RunSnapshotStore persists run records together with their replayable event history.
// Implementations may be in-memory, file-backed, or remote.
type RunSnapshotStore interface {
	LoadRunSnapshot(runID string) (RunSnapshot, bool)
	ListRunSnapshots(threadID string) []RunSnapshot
	SaveRunSnapshot(snapshot RunSnapshot)
}

// RunSnapshotCancelStore provides an optional atomic transition for detached
// stale-run cancellation under concurrent cross-instance requests.
type RunSnapshotCancelStore interface {
	TryCancelStaleRun(runID string, staleBefore time.Time) (RunRecord, bool)
}

// RunEventStore persists ordered events for one run.
// It is intentionally narrow so runtime event services do not depend on compat state.
type RunEventStore interface {
	NextRunEventIndex(runID string) int
	AppendRunEvent(runID string, event RunEvent)
	LoadRunEvents(runID string) []RunEvent
}

type RunEventReplaceStore interface {
	ReplaceRunEvents(runID string, events []RunEvent)
}

// RunEventRecorder is the minimal append/index boundary needed by EventLogService.
type RunEventRecorder interface {
	NextRunEventIndex(runID string) int
	AppendRunEvent(runID string, event RunEvent)
}

// RunEventFeed provides live subscriptions for appended events.
type RunEventFeed interface {
	SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func())
}

// RunEventReplayFeed provides replay + live feed access without exposing append operations.
type RunEventReplayFeed interface {
	LoadRunEvents(runID string) []RunEvent
	SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func())
}

type ThreadRuntimeState struct {
	ThreadID  string
	Status    string
	Metadata  map[string]any
	CreatedAt string
	UpdatedAt string
}

type ThreadRuntimeStateStore interface {
	LoadThreadRuntimeState(threadID string) (ThreadRuntimeState, bool)
	ListThreadRuntimeStates() []ThreadRuntimeState
	SaveThreadRuntimeState(state ThreadRuntimeState)
}

// ThreadStateStore persists thread existence, status, and runtime metadata.
type ThreadStateStore interface {
	ThreadRuntimeStateStore
	HasThread(threadID string) bool
	MarkThreadStatus(threadID string, status string)
	SetThreadMetadata(threadID string, key string, value any)
	ClearThreadMetadata(threadID string, key string)
}
