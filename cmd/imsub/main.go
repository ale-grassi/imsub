package main

import (
	"log"
	"os"

	"imsub/internal/app"
)

var (
	runServer       = app.Run
	runAdminCommand = app.RunAdminCommand
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("imsub failed: %v", err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "admin" {
		return runAdminCommand(args[1:], os.Stdout)
	}
	return runServer()
}
