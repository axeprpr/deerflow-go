package main

import (
	"flag"
	"log"
	"os"

	"github.com/axeprpr/deerflow-go/internal/sandboxcmd"
)

func main() {
	fs := flag.NewFlagSet("runtime-sandbox", flag.ExitOnError)
	if err := sandboxcmd.RunCommand(fs, sandboxcmd.CommandOptions{
		Stderr: os.Stderr,
		Args:   os.Args[1:],
	}); err != nil {
		log.Fatal(err)
	}
}
