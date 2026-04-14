package stackcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

func WriteBundle(dir string, manifest StackManifest) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("bundle dir required")
	}
	if err := manifest.ValidateProcessGraph(); err != nil {
		return fmt.Errorf("invalid process graph: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := commandrun.WriteJSONFile(filepath.Join(dir, "stack-manifest.json"), manifest); err != nil {
		return err
	}
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o755); err != nil {
		return err
	}
	for _, process := range manifest.Processes {
		if err := commandrun.WriteJSONFile(filepath.Join(processDir, process.Name+".json"), process); err != nil {
			return err
		}
	}
	return nil
}
