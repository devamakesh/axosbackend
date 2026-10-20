package main

import (
	"fmt"
	"os"

	"axosbackend/internal/envfile"
)

func main() {
	created, err := envfile.EnsureServerToken("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	if created {
		fmt.Println("Created a private server access token in .env. Copy it into Personalize ALIEN X on your device.")
	} else {
		fmt.Println("An ALIEN X server token is already configured; it was preserved.")
	}
}
