package harnessruntime

import (
	"context"
	"strings"
	"time"
)

type CoordinationRuntime interface {
	LoadRunRecord(runID string) (RunRecord, bool)
	CancelRun(runID string) bool
}

type CoordinationService struct {
	runtime      CoordinationRuntime
	pollInterval time.Duration
}

func NewCoordinationService(runtime CoordinationRuntime) CoordinationService {
	return CoordinationService{
		runtime:      runtime,
		pollInterval: 100 * time.Millisecond,
	}
}

func (s CoordinationService) ThreadRun(threadID, runID string) (RunRecord, bool) {
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
	return record, true
}

func (s CoordinationService) Wait(ctx context.Context, threadID, runID string, cancelOnDisconnect bool) (RunRecord, bool, bool) {
	for {
		record, found := s.ThreadRun(threadID, runID)
		if !found {
			return RunRecord{}, false, false
		}
		if !isRunningStatus(record.Status) {
			return record, true, true
		}

		select {
		case <-ctx.Done():
			if cancelOnDisconnect && s.runtime != nil {
				s.runtime.CancelRun(runID)
			}
			return RunRecord{}, true, false
		case <-time.After(s.pollInterval):
		}
	}
}

func (s CoordinationService) Cancel(threadID, runID string) (map[string]any, bool, bool) {
	record, found := s.ThreadRun(threadID, runID)
	if !found {
		return nil, false, false
	}
	if !isRunningStatus(record.Status) {
		return nil, true, false
	}
	if s.runtime == nil || !s.runtime.CancelRun(runID) {
		return nil, true, false
	}
	return map[string]any{
		"run_id":    record.RunID,
		"thread_id": record.ThreadID,
		"status":    "interrupted",
	}, true, true
}

func isRunningStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "running", "queued", "busy":
		return true
	default:
		return false
	}
}
