package main

import (
	"flag"
	"log"
	"os"

	"github.com/axeprpr/deerflow-go/internal/statecmd"
)

func main() {
	fs := flag.NewFlagSet("runtime-state", flag.ExitOnError)
	if err := statecmd.RunCommand(fs, statecmd.CommandOptions{
		Stderr: os.Stderr,
		Args:   os.Args[1:],
	}); err != nil {
		log.Fatal(err)
	}
}
