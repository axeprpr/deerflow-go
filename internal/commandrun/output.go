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
	data, err := MarshalJSON(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(StdoutWriter(w), string(data))
	return err
}

func MarshalJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

func WriteJSONFile(path string, value any) error {
	data, err := MarshalJSON(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
