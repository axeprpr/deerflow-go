package harnessruntime

import (
	"sort"
	"strings"
)

type QueryRuntime interface {
	LoadRunRecord(runID string) (RunRecord, bool)
	ListRunRecords(threadID string) []RunRecord
	HasThread(threadID string) bool
}

type JoinSelection struct {
	Run         RunRecord
	HasRun      bool
	ThreadFound bool
}

type QueryService struct {
	runtime QueryRuntime
}

func NewQueryService(runtime QueryRuntime) QueryService {
	return QueryService{runtime: runtime}
}

func (s QueryService) Run(threadID, runID string) (RunRecord, bool) {
	if s.runtime == nil {
		return RunRecord{}, false
	}
	record, ok := s.runtime.LoadRunRecord(strings.TrimSpace(runID))
	if !ok {
		return RunRecord{}, false
	}
	if threadID != "" && record.ThreadID != strings.TrimSpace(threadID) {
		return RunRecord{}, false
	}
	return normalizeQueryRunRecord(record), true
}

func (s QueryService) ListThreadRuns(threadID string) []RunRecord {
	if s.runtime == nil {
		return nil
	}
	records := append([]RunRecord(nil), s.runtime.ListRunRecords(strings.TrimSpace(threadID))...)
	for i := range records {
		records[i] = normalizeQueryRunRecord(records[i])
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}

func (s QueryService) LatestRun(threadID string) (RunRecord, bool) {
	records := s.ListThreadRuns(threadID)
	if len(records) == 0 {
		return RunRecord{}, false
	}
	return records[0], true
}

func (s QueryService) LatestActiveRun(threadID string) (RunRecord, bool) {
	for _, record := range s.ListThreadRuns(threadID) {
		if isRunningStatus(record.Status) {
			return record, true
		}
	}
	return RunRecord{}, false
}

func (s QueryService) SelectJoinRun(threadID string) JoinSelection {
	threadID = strings.TrimSpace(threadID)
	if s.runtime == nil {
		return JoinSelection{}
	}

	if active, ok := s.LatestActiveRun(threadID); ok {
		return JoinSelection{
			Run:         active,
			HasRun:      true,
			ThreadFound: true,
		}
	}

	threadFound := s.runtime.HasThread(threadID)
	if !threadFound {
		if latest, ok := s.LatestRun(threadID); ok {
			return JoinSelection{
				Run:         latest,
				HasRun:      true,
				ThreadFound: true,
			}
		}
		return JoinSelection{}
	}

	return JoinSelection{ThreadFound: true}
}

func normalizeQueryRunRecord(record RunRecord) RunRecord {
	record.Outcome = NewOutcomeService().BindRecord(record, record.Outcome)
	return record
}
