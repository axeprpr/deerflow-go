package main

import (
	"log"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/stackcmd"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := stackcmd.RunCommand(nil, langgraphcmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
	}, stackcmd.CommandOptions{}); err != nil {
		log.Fatal(err)
	}
}
