package harnessruntime

import (
	"strings"
	"time"
)

func canCancelDetachedRecord(record RunRecord, staleBefore time.Time) bool {
	if strings.TrimSpace(record.RunID) == "" {
		return false
	}
	if !isRunningStatus(record.Status) {
		return false
	}
	if staleBefore.IsZero() {
		return true
	}
	if record.UpdatedAt.IsZero() {
		return false
	}
	return !record.UpdatedAt.After(staleBefore)
}

func applyDetachedCancel(record RunRecord, now time.Time) RunRecord {
	record.Status = "interrupted"
	record.Error = ""
	record.UpdatedAt = now.UTC()
	record.Outcome = NewOutcomeService().BindRecord(record, RunOutcomeDescriptor{
		RunStatus:   "interrupted",
		Interrupted: true,
	})
	return record
}
