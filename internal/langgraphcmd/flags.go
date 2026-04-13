package langgraphcmd

import (
	"flag"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
)

type FlagBinding struct {
	defaults  Config
	authToken *string
	addr      *string
	database  *string
	provider  *string
	model     *string
	runtime   *runtimecmd.NodeFlagBinding
}

func BindFlags(fs *flag.FlagSet, defaults Config) *FlagBinding {
	if fs == nil {
		fs = flag.CommandLine
	}
	return &FlagBinding{
		defaults:  defaults,
		authToken: fs.String("auth-token", defaults.AuthToken, "Bearer token for API auth (env: DEERFLOW_AUTH_TOKEN)"),
		addr:      fs.String("addr", defaults.Addr, "Server address"),
		database:  fs.String("db", defaults.DatabaseURL, "Database URL (postgres or sqlite)"),
		provider:  fs.String("provider", defaults.Provider, "Default LLM provider"),
		model:     fs.String("model", defaults.Model, "Default LLM model"),
		runtime:   runtimecmd.BindFlags(fs, defaults.Runtime, "runtime-", "Runtime "),
	}
}

func (b *FlagBinding) Config() Config {
	if b == nil {
		return Config{}
	}
	return Config{
		AuthToken:   valueOrEmpty(b.authToken),
		Addr:        valueOrEmpty(b.addr),
		DatabaseURL: valueOrEmpty(b.database),
		Provider:    valueOrEmpty(b.provider),
		Model:       valueOrEmpty(b.model),
		Runtime:     b.runtime.Config(),
	}
}

func valueOrEmpty(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
