package main

import (
	"os"
	"path/filepath"

	"github.com/gameap/daemon/internal/app"
)

func main() {
	os.Args[0] = filepath.Base(os.Args[0])

	app.Run(os.Args)
}
