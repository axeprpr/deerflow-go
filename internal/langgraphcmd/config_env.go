package langgraphcmd

import "os"

func init() {
	getenv = os.Getenv
}
