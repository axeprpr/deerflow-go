package harnessruntime

type RunSnapshot struct {
	Record RunRecord
	Events []RunEvent
}

type SnapshotRuntime interface {
	LoadRunSnapshot(runID string) (RunSnapshot, bool)
	ListRunSnapshots(threadID string) []RunSnapshot
	SaveRunSnapshot(snapshot RunSnapshot)
}

type SnapshotStoreService struct {
	runtime SnapshotRuntime
}

func NewSnapshotStoreService(runtime SnapshotRuntime) SnapshotStoreService {
	return SnapshotStoreService{runtime: runtime}
}

func (s SnapshotStoreService) SaveRecord(record RunRecord) {
	if s.runtime == nil {
		return
	}
	snapshot, ok := s.runtime.LoadRunSnapshot(record.RunID)
	if !ok {
		snapshot = RunSnapshot{}
	}
	snapshot.Record = record
	s.runtime.SaveRunSnapshot(snapshot)
}

func (s SnapshotStoreService) LoadRecord(runID string) (RunRecord, bool) {
	if s.runtime == nil {
		return RunRecord{}, false
	}
	snapshot, ok := s.runtime.LoadRunSnapshot(runID)
	if !ok {
		return RunRecord{}, false
	}
	return snapshot.Record, true
}

func (s SnapshotStoreService) ListRecords(threadID string) []RunRecord {
	if s.runtime == nil {
		return nil
	}
	snapshots := s.runtime.ListRunSnapshots(threadID)
	records := make([]RunRecord, 0, len(snapshots))
	for _, snapshot := range snapshots {
		records = append(records, snapshot.Record)
	}
	return records
}

func (s SnapshotStoreService) NextEventIndex(runID string) int {
	if s.runtime == nil {
		return 1
	}
	snapshot, ok := s.runtime.LoadRunSnapshot(runID)
	if !ok {
		return 1
	}
	return len(snapshot.Events) + 1
}

func (s SnapshotStoreService) AppendEvent(runID string, event RunEvent) {
	if s.runtime == nil {
		return
	}
	snapshot, ok := s.runtime.LoadRunSnapshot(runID)
	if !ok {
		snapshot = RunSnapshot{}
	}
	snapshot.Events = append(snapshot.Events, event)
	s.runtime.SaveRunSnapshot(snapshot)
}

func (s SnapshotStoreService) LoadEvents(runID string) []RunEvent {
	if s.runtime == nil {
		return nil
	}
	snapshot, ok := s.runtime.LoadRunSnapshot(runID)
	if !ok {
		return nil
	}
	return append([]RunEvent(nil), snapshot.Events...)
}
