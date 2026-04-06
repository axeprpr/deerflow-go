package tools

import (
	"os"
	"path/filepath"
	"strings"
)

func DataRootFromEnv() string {
	for _, key := range []string{"DEERFLOW_DATA_ROOT", "DEER_FLOW_HOME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return filepath.Join(os.TempDir(), "deerflow-go-data")
}
