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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := commandrun.WriteJSONFile(filepath.Join(dir, "stack-manifest.json"), manifest); err != nil {
		return err
	}
	for _, process := range manifest.Processes {
		scriptPath := filepath.Join(dir, process.Name+".sh")
		if err := os.WriteFile(scriptPath, []byte(process.Script()), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (p ProcessManifest) Script() string {
	parts := append([]string{shellQuote(p.Binary)}, quoteArgs(p.Args)...)
	return "#!/usr/bin/env bash\nset -euo pipefail\nexec " + strings.Join(parts, " ") + "\n"
}

func quoteArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, shellQuote(arg))
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
