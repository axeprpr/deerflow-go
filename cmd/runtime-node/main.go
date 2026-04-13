package main

import (
	"log"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
)

func main() {
	if err := runtimecmd.RunCommand(nil, runtimecmd.CommandOptions{
		LogPrefix: "[runtime-node] ",
	}); err != nil {
		log.Fatal(err)
	}
}
