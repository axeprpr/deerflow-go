package harness

import (
	"sync"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

// SandboxProvider mirrors upstream's provider boundary. The current local
// implementation keeps the existing singleton behavior, but runtime assembly no
// longer depends on compat-owned sandbox fields directly.
type SandboxProvider interface {
	Acquire() (*sandbox.Sandbox, error)
	Close() error
}

type LocalSandboxProvider struct {
	name string
	root string

	mu      sync.Mutex
	sandbox *sandbox.Sandbox
}

func NewLocalSandboxProvider(name, root string) *LocalSandboxProvider {
	return &LocalSandboxProvider{name: name, root: root}
}

func (p *LocalSandboxProvider) Acquire() (*sandbox.Sandbox, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sandbox != nil {
		return p.sandbox, nil
	}
	sb, err := sandbox.New(p.name, p.root)
	if err != nil {
		return nil, err
	}
	p.sandbox = sb
	return sb, nil
}

func (p *LocalSandboxProvider) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sandbox == nil {
		return nil
	}
	err := p.sandbox.Close()
	p.sandbox = nil
	return err
}

