package main

import (
	"log"

	"imsub/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("imsub failed: %v", err)
	}
}
