package langgraphcmd

import "os"

func (c *Config) ApplyYoloEnvironment(enabled bool) {
	if c == nil || !enabled {
		return
	}
	os.Setenv("DEERFLOW_YOLO", "1")
	os.Setenv("ADDR", ":8080")
	os.Setenv("DEERFLOW_DATA_ROOT", "./data")
	os.Setenv("LOG_LEVEL", "info")
	c.ApplyYoloDefaults(true)
}

func (c Config) ApplyProcessEnvironment() {
	if c.AuthToken != "" {
		os.Setenv("DEERFLOW_AUTH_TOKEN", c.AuthToken)
	}
}
