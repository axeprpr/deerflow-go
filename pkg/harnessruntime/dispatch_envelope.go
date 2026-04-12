package harnessruntime

import "time"

type WorkerDispatchEnvelope struct {
	RunID       string
	ThreadID    string
	SubmittedAt time.Time
	Attempt     int
	Payload     []byte
}

type DispatchEnvelopeCodec struct {
	Plans WorkerPlanMarshaler
}

func (c DispatchEnvelopeCodec) Encode(req DispatchRequest) (WorkerDispatchEnvelope, error) {
	plans := c.Plans
	if plans == nil {
		plans = WorkerPlanCodec{}
	}
	payload, err := plans.Encode(req.Plan)
	if err != nil {
		return WorkerDispatchEnvelope{}, err
	}
	return WorkerDispatchEnvelope{
		RunID:       req.Plan.RunID,
		ThreadID:    req.Plan.ThreadID,
		SubmittedAt: req.Plan.SubmittedAt,
		Attempt:     req.Plan.Attempt,
		Payload:     payload,
	}, nil
}

func (c DispatchEnvelopeCodec) Decode(env WorkerDispatchEnvelope) (DispatchRequest, error) {
	plans := c.Plans
	if plans == nil {
		plans = WorkerPlanCodec{}
	}
	plan, err := plans.Decode(env.Payload)
	if err != nil {
		return DispatchRequest{}, err
	}
	return DispatchRequest{Plan: plan}, nil
}
