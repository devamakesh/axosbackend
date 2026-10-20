package main

import (
	"os"

	"axosbackend/internal/smoke"
)

func main() {
	os.Exit(smoke.Run(os.Args[1:]))
}
