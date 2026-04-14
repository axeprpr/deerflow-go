package langgraphcmd

import "github.com/axeprpr/deerflow-go/internal/runtimecmd"

type ServerManifest struct {
	Addr        string                  `json:"addr"`
	DatabaseURL string                  `json:"database_url"`
	Provider    string                  `json:"provider"`
	Model       string                  `json:"model"`
	AuthEnabled bool                    `json:"auth_enabled"`
	Runtime     runtimecmd.NodeManifest `json:"runtime"`
}

func (c Config) Manifest() ServerManifest {
	return ServerManifest{
		Addr:        c.Addr,
		DatabaseURL: c.DatabaseURL,
		Provider:    c.Provider,
		Model:       c.Model,
		AuthEnabled: c.AuthToken != "",
		Runtime:     c.Runtime.Manifest(),
	}
}
