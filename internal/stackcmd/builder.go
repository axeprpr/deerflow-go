package stackcmd

import (
	"context"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

type Builder struct {
	input      Config
	config     Config
	deployment DeploymentSpec
}

func NewBuilder(config Config) Builder {
	resolved := config.withDefaults()
	return Builder{
		input:      config,
		config:     resolved,
		deployment: resolved.DeploymentSpec(),
	}
}

func (b Builder) Input() Config {
	return b.input
}

func (b Builder) Config() Config {
	return b.config
}

func (b Builder) DeploymentSpec() DeploymentSpec {
	return b.deployment
}

func (b Builder) Validate() error {
	return b.config.Validate()
}

func (b Builder) BuildComponents(ctx context.Context) ([]LaunchComponent, error) {
	return b.config.BuildComponents(ctx)
}

func (b Builder) BuildLauncher(ctx context.Context) (*Launcher, error) {
	launcher, err := b.config.BuildLauncher(ctx)
	if err != nil {
		return nil, err
	}
	launcher.deploymentSpec = b.deployment
	return launcher, nil
}

func (b Builder) StartupLines(build langgraphcmd.BuildInfo, yolo bool, logLevel string) []string {
	return b.config.StartupLines(build, yolo, logLevel)
}

func (b Builder) ReadyLines() []string {
	return b.config.ReadyLines()
}

func (b Builder) ReadyProbe() commandrun.ReadyFunc {
	return b.config.ReadyProbe()
}
