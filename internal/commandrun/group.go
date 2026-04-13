package commandrun

import "context"

type LifecycleGroup struct {
	items []Lifecycle
}

func NewLifecycleGroup(items ...Lifecycle) *LifecycleGroup {
	filtered := make([]Lifecycle, 0, len(items))
	for _, item := range items {
		if item != nil {
			filtered = append(filtered, item)
		}
	}
	return &LifecycleGroup{items: filtered}
}

func (g *LifecycleGroup) Start() error {
	if g == nil || len(g.items) == 0 {
		return nil
	}
	errCh := make(chan error, len(g.items))
	for _, item := range g.items {
		lifecycle := item
		go func() {
			errCh <- lifecycle.Start()
		}()
	}
	for range g.items {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func (g *LifecycleGroup) Close(ctx context.Context) error {
	if g == nil {
		return nil
	}
	var closeErr error
	for i := len(g.items) - 1; i >= 0; i-- {
		if err := g.items[i].Close(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
