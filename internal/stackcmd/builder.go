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
	manifest   StackManifest
}

func NewBuilder(config Config) Builder {
	resolved := config.withDefaults()
	return Builder{
		input:      config,
		config:     resolved,
		deployment: resolved.DeploymentSpec(),
		manifest:   resolved.Manifest(),
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

func (b Builder) Manifest() StackManifest {
	return b.manifest
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
	launcher.manifest = b.manifest
	return launcher, nil
}

func (b Builder) StartupLines(build langgraphcmd.BuildInfo, yolo bool, logLevel string) []string {
	return append(b.config.Gateway.StartupLines(build, yolo, logLevel), b.manifest.StartupLines(build, yolo, logLevel)...)
}

func (b Builder) ReadyLines() []string {
	return append(b.config.Gateway.ReadyLines(), b.manifest.ReadyLines()...)
}

func (b Builder) ReadyProbe() commandrun.ReadyFunc {
	return b.config.ReadyProbe()
}
