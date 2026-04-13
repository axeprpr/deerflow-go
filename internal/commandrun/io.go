package commandrun

import (
	"flag"
	"io"
	"os"
)

func OutputWriter(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return os.Stderr
}

func CommandArgs(fs *flag.FlagSet, explicit []string) []string {
	if explicit != nil {
		return explicit
	}
	if fs == flag.CommandLine {
		return os.Args[1:]
	}
	return nil
}
