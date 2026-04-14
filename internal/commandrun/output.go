package commandrun

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func StdoutWriter(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return os.Stdout
}

func PrintJSON(w io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(StdoutWriter(w), string(data))
	return err
}
