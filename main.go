package main

import (
	"github.com/SciMate-AI/scicli/cmd"
	"github.com/SciMate-AI/scicli/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
