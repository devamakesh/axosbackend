package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"axosbackend/internal/envfile"
	"axosbackend/internal/server"
)

func main() {
	envfile.Load()

	handler, err := server.NewAlienxServer(server.Options{
		Env: server.EnvMapFromProcess(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	host := strings.TrimSpace(os.Getenv("ALIENX_HOST"))
	if host == "" {
		host = "0.0.0.0"
	}
	port := 8787
	if rawPort := strings.TrimSpace(os.Getenv("ALIENX_PORT")); rawPort != "" {
		if p, err := strconv.Atoi(rawPort); err == nil && p > 0 {
			port = p
		}
	} else if rawPort := strings.TrimSpace(os.Getenv("PORT")); rawPort != "" {
		if p, err := strconv.Atoi(rawPort); err == nil && p > 0 {
			port = p
		}
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	server.StopStaleInstances()

	fmt.Printf("ALIEN X personal server listening on http://%s. Provider credentials remain on this server.\n", addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
