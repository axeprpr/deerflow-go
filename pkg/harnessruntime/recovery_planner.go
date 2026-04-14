package harnessruntime

type RecoveryPlanner struct {
	threads ThreadStateStore
}

func NewRecoveryPlanner() RecoveryPlanner {
	return RecoveryPlanner{}
}

func NewRecoveryPlannerWithThreads(threads ThreadStateStore) RecoveryPlanner {
	return RecoveryPlanner{threads: threads}
}

func (p RecoveryPlanner) NextRecord(record RunRecord, afterEvent int, reason string) RunRecord {
	if record.Attempt <= 0 {
		record.Attempt = 1
	}
	record.Attempt++
	record.ResumeFromEvent = afterEvent
	record.ResumeReason = reason
	record.Status = "running"
	record.Error = ""
	record.Outcome = NewOutcomeService().DescribeLiveRunning(record, p.threads)
	return record
}

func (p RecoveryPlanner) ResumePlan(plan RunPlan, record RunRecord, afterEvent int, reason string) RunPlan {
	record = p.NextRecord(record, afterEvent, reason)
	plan.RunID = record.RunID
	plan.ThreadID = record.ThreadID
	plan.AssistantID = record.AssistantID
	plan.Attempt = record.Attempt
	plan.ResumeFromEvent = record.ResumeFromEvent
	plan.ResumeReason = record.ResumeReason
	return plan
}
