package harnessruntime

type RunSnapshot struct {
	Record RunRecord
	Events []RunEvent
}

type SnapshotStoreService struct {
	store RunSnapshotStore
}

func NewSnapshotStoreService(store RunSnapshotStore) SnapshotStoreService {
	return SnapshotStoreService{store: store}
}

func (s SnapshotStoreService) SaveRecord(record RunRecord) {
	if s.store == nil {
		return
	}
	snapshot, ok := s.store.LoadRunSnapshot(record.RunID)
	if !ok {
		snapshot = RunSnapshot{}
	}
	snapshot.Record = record
	s.store.SaveRunSnapshot(snapshot)
}

func (s SnapshotStoreService) LoadRecord(runID string) (RunRecord, bool) {
	if s.store == nil {
		return RunRecord{}, false
	}
	snapshot, ok := s.store.LoadRunSnapshot(runID)
	if !ok {
		return RunRecord{}, false
	}
	return snapshot.Record, true
}

func (s SnapshotStoreService) ListRecords(threadID string) []RunRecord {
	if s.store == nil {
		return nil
	}
	snapshots := s.store.ListRunSnapshots(threadID)
	records := make([]RunRecord, 0, len(snapshots))
	for _, snapshot := range snapshots {
		records = append(records, snapshot.Record)
	}
	return records
}

func (s SnapshotStoreService) NextEventIndex(runID string) int {
	if s.store == nil {
		return 1
	}
	snapshot, ok := s.store.LoadRunSnapshot(runID)
	if !ok {
		return 1
	}
	return len(snapshot.Events) + 1
}

func (s SnapshotStoreService) AppendEvent(runID string, event RunEvent) {
	if s.store == nil {
		return
	}
	snapshot, ok := s.store.LoadRunSnapshot(runID)
	if !ok {
		snapshot = RunSnapshot{}
	}
	snapshot.Events = append(snapshot.Events, event)
	s.store.SaveRunSnapshot(snapshot)
}

func (s SnapshotStoreService) LoadEvents(runID string) []RunEvent {
	if s.store == nil {
		return nil
	}
	snapshot, ok := s.store.LoadRunSnapshot(runID)
	if !ok {
		return nil
	}
	return append([]RunEvent(nil), snapshot.Events...)
}
