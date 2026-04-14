package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

const DefaultTaskLifecycleMetadataKey = "task_lifecycle"

type TaskLifecycleDescriptor struct {
	Status            string   `json:"status,omitempty"`
	PendingTasks      []string `json:"pending_tasks,omitempty"`
	ExpectedArtifacts []string `json:"expected_artifacts,omitempty"`
	VerifiedArtifacts []string `json:"verified_artifacts,omitempty"`
}

type TaskLifecycleService struct{}

func NewTaskLifecycleService() TaskLifecycleService {
	return TaskLifecycleService{}
}

func (TaskLifecycleService) Describe(outcome RunOutcome, taskState harness.TaskState, paused bool) TaskLifecycleDescriptor {
	status := outcome.RunStatus
	switch {
	case paused:
		status = "paused"
	case status == "":
		status = "success"
	}
	return TaskLifecycleDescriptor{
		Status:            status,
		PendingTasks:      append([]string(nil), taskState.PendingTexts()...),
		ExpectedArtifacts: append([]string(nil), taskState.MissingExpectedOutputs()...),
		VerifiedArtifacts: append([]string(nil), taskState.VerifiedOutputs...),
	}
}

func (d TaskLifecycleDescriptor) IsZero() bool {
	return d.Status == "" && len(d.PendingTasks) == 0 && len(d.ExpectedArtifacts) == 0 && len(d.VerifiedArtifacts) == 0
}

func (d TaskLifecycleDescriptor) Value() map[string]any {
	if d.IsZero() {
		return nil
	}
	out := map[string]any{
		"status": d.Status,
	}
	if len(d.PendingTasks) > 0 {
		out["pending_tasks"] = append([]string(nil), d.PendingTasks...)
	}
	if len(d.ExpectedArtifacts) > 0 {
		out["expected_artifacts"] = append([]string(nil), d.ExpectedArtifacts...)
	}
	if len(d.VerifiedArtifacts) > 0 {
		out["verified_artifacts"] = append([]string(nil), d.VerifiedArtifacts...)
	}
	return out
}
