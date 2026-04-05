package webui

import (
	"embed"
	"io/fs"
)

// FS contains the generated embedded web UI assets.
// Populate internal/webui/dist before building to include the frontend in the binary.
//
//go:embed dist
var embedded embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
