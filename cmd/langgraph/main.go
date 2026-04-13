package main

import (
	"log"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := langgraphcmd.RunCommand(nil, langgraphcmd.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
	}, langgraphcmd.CommandOptions{}); err != nil {
		log.Fatal(err)
	}
}
