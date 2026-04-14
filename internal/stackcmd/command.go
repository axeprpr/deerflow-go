package stackcmd

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

type CommandOptions struct {
	Stderr io.Writer
	Stdout io.Writer
	Args   []string
}

func RunCommand(fs *flag.FlagSet, build langgraphcmd.BuildInfo, options CommandOptions) error {
	prepared, err := PrepareCommand(fs, build, options)
	if err != nil {
		return err
	}
	return prepared.Run()
}

func PrepareCommand(fs *flag.FlagSet, build langgraphcmd.BuildInfo, options CommandOptions) (*commandrun.PreparedCommand, error) {
	if fs == nil {
		fs = flag.CommandLine
	}
	logger := log.New(commandrun.OutputWriter(options.Stderr), "[runtime-stack] ", log.LstdFlags)
	printManifest := fs.Bool("print-manifest", false, "print resolved runtime stack manifest and exit")
	writeBundle := fs.String("write-bundle", "", "write stack manifest and per-process specs to a directory, then exit")
	validateBundle := fs.String("validate-bundle", "", "validate an existing runtime bundle directory and exit")
	spawnBundle := fs.String("spawn-bundle", "", "launch stack using external processes from an existing bundle directory")
	bundleRestartPolicy := fs.String("bundle-restart-policy", string(bundleDefaultRestartPolicy), "host-plan restart policy for write-bundle: never|on-failure|always")
	bundleMaxRestarts := fs.Int("bundle-max-restarts", bundleDefaultMaxRestarts, "host-plan max restart attempts per process for write-bundle (<=0 means unlimited)")
	bundleRestartDelay := fs.Duration("bundle-restart-delay", bundleDefaultRestartDelay, "host-plan restart delay for write-bundle")
	bundleDependencyTimeout := fs.Duration("bundle-dependency-timeout", bundleDefaultDependencyTimeout, "host-plan dependency readiness timeout for write-bundle")
	bundleFailureIsolation := fs.Bool("bundle-failure-isolation", bundleDefaultFailureIsolation, "host-plan failure isolation hint for write-bundle orchestration")
	spawnProcesses := fs.Bool("spawn-processes", false, "launch stack using external processes from manifest binaries")
	processBinaryDir := fs.String("process-binary-dir", "", "directory used to resolve process binaries when spawning external processes")
	spawnRestartPolicy := fs.String("spawn-restart-policy", string(ProcessRestartOnFailure), "external process restart policy: never|on-failure|always")
	spawnMaxRestarts := fs.Int("spawn-max-restarts", 3, "max restart attempts per external process (<=0 means unlimited)")
	spawnRestartDelay := fs.Duration("spawn-restart-delay", 500*time.Millisecond, "delay before restarting an external process")
	spawnDependencyTimeout := fs.Duration("spawn-dependency-timeout", 60*time.Second, "timeout waiting for each dependency readiness target")
	spawnFailureIsolation := fs.Bool("spawn-failure-isolation", false, "allow other external processes to keep running when one process exits with terminal error")

	yolo := fs.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := DefaultConfig()
	binding := BindFlags(fs, cfg)
	if err := fs.Parse(commandrun.CommandArgs(fs, options.Args)); err != nil {
		return nil, err
	}
	cfg = binding.Config()
	cfg.Gateway.ApplyYoloEnvironment(*yolo)
	cfg.Gateway.ApplyProcessEnvironment()
	builder := NewBuilder(cfg)
	if *printManifest {
		return &commandrun.PreparedCommand{
			RunFunc: func() error {
				return commandrun.PrintJSON(options.Stdout, builder.Manifest())
			},
		}, nil
	}
	if strings.TrimSpace(*validateBundle) != "" {
		return &commandrun.PreparedCommand{
			RunFunc: func() error {
				if err := ValidateBundle(*validateBundle); err != nil {
					return err
				}
				_, err := io.WriteString(commandrun.StdoutWriter(options.Stdout), *validateBundle+"\n")
				return err
			},
		}, nil
	}
	if strings.TrimSpace(*writeBundle) != "" {
		restartPolicy, err := parseProcessRestartPolicy(*bundleRestartPolicy)
		if err != nil {
			return nil, err
		}
		return &commandrun.PreparedCommand{
			RunFunc: func() error {
				if err := WriteBundleWithOptions(*writeBundle, builder.Manifest(), BundleOptions{
					RestartPolicy:     restartPolicy,
					MaxRestarts:       *bundleMaxRestarts,
					RestartDelay:      *bundleRestartDelay,
					DependencyTimeout: *bundleDependencyTimeout,
					FailureIsolation:  *bundleFailureIsolation,
				}); err != nil {
					return err
				}
				_, err := io.WriteString(commandrun.StdoutWriter(options.Stdout), *writeBundle+"\n")
				return err
			},
		}, nil
	}
	if strings.TrimSpace(*spawnBundle) != "" {
		restartPolicy, err := parseProcessRestartPolicy(*spawnRestartPolicy)
		if err != nil {
			return nil, err
		}
		if err := ValidateBundle(*spawnBundle); err != nil {
			return nil, err
		}
		manifest, err := LoadBundleManifest(*spawnBundle)
		if err != nil {
			return nil, err
		}
		processLauncher, err := NewProcessLauncher(manifest.Processes, ProcessLaunchOptions{
			Stdout:            commandrun.StdoutWriter(options.Stdout),
			Stderr:            commandrun.OutputWriter(options.Stderr),
			BinaryDir:         strings.TrimSpace(*processBinaryDir),
			RestartPolicy:     restartPolicy,
			MaxRestarts:       *spawnMaxRestarts,
			RestartDelay:      *spawnRestartDelay,
			DependencyTimeout: *spawnDependencyTimeout,
			FailureIsolation:  *spawnFailureIsolation,
		})
		if err != nil {
			return nil, err
		}
		startup := []string{
			"Starting split runtime stack from bundle...",
			"  bundle_dir=" + strings.TrimSpace(*spawnBundle),
			"  launch_mode=external-processes-bundle",
		}
		if *spawnFailureIsolation {
			startup = append(startup, "  failure_isolation=enabled")
		} else {
			startup = append(startup, "  failure_isolation=disabled")
		}
		return &commandrun.PreparedCommand{
			Logger:          logger,
			Lifecycle:       processLauncher,
			StartupLines:    startup,
			ReadyLines:      manifest.ReadyLines(),
			Ready:           readyProbeForTargets(manifest.ReadyTargets()),
			ReadyTimeout:    15 * time.Second,
			ShutdownTimeout: 15 * time.Second,
			IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
		}, nil
	}
	if *spawnProcesses {
		restartPolicy, err := parseProcessRestartPolicy(*spawnRestartPolicy)
		if err != nil {
			return nil, err
		}
		processLauncher, err := NewProcessLauncher(builder.Manifest().Processes, ProcessLaunchOptions{
			Stdout:            commandrun.StdoutWriter(options.Stdout),
			Stderr:            commandrun.OutputWriter(options.Stderr),
			BinaryDir:         strings.TrimSpace(*processBinaryDir),
			RestartPolicy:     restartPolicy,
			MaxRestarts:       *spawnMaxRestarts,
			RestartDelay:      *spawnRestartDelay,
			DependencyTimeout: *spawnDependencyTimeout,
			FailureIsolation:  *spawnFailureIsolation,
		})
		if err != nil {
			return nil, err
		}
		startup := append([]string{}, builder.StartupLines(build, *yolo, strings.TrimSpace(os.Getenv("LOG_LEVEL")))...)
		startup = append(startup, "  launch_mode=external-processes")
		if *spawnFailureIsolation {
			startup = append(startup, "  failure_isolation=enabled")
		} else {
			startup = append(startup, "  failure_isolation=disabled")
		}
		return &commandrun.PreparedCommand{
			Logger:          logger,
			Lifecycle:       processLauncher,
			StartupLines:    startup,
			ReadyLines:      builder.ReadyLines(),
			Ready:           builder.ReadyProbe(),
			ReadyTimeout:    15 * time.Second,
			ShutdownTimeout: 15 * time.Second,
			IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
		}, nil
	}

	launcher, err := builder.BuildLauncher(context.Background())
	if err != nil {
		return nil, err
	}
	return &commandrun.PreparedCommand{
		Logger:          logger,
		Lifecycle:       launcher,
		StartupLines:    builder.StartupLines(build, *yolo, strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
		ReadyLines:      builder.ReadyLines(),
		Ready:           builder.ReadyProbe(),
		ReadyTimeout:    15 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		IgnoredErrors:   []error{http.ErrServerClosed, context.Canceled},
	}, nil
}

func readyProbeForTargets(targets []string) commandrun.ReadyFunc {
	filtered := make([]string, 0, len(targets))
	for _, target := range targets {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return commandrun.HTTPReadyProbe{
		Interval: 50 * time.Millisecond,
		Targets:  filtered,
	}.Wait
}
