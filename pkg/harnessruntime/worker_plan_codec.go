package harnessruntime

import "encoding/json"

type WorkerPlanCodec struct{}

func (WorkerPlanCodec) Encode(plan WorkerExecutionPlan) ([]byte, error) {
	return json.Marshal(plan)
}

func (WorkerPlanCodec) Decode(data []byte) (WorkerExecutionPlan, error) {
	var plan WorkerExecutionPlan
	err := json.Unmarshal(data, &plan)
	return plan, err
}
