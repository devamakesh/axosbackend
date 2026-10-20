package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StopStaleInstances terminates any lingering legacy node server/alienx.mjs
// or previous axosbackend server processes owned by the current user so the port can bind cleanly.
func StopStaleInstances() {
	selfPID := os.Getpid()
	parentPID := os.Getppid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	killedAny := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == selfPID || pid == parentPID {
			continue
		}
		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdlineBytes) == 0 {
			continue
		}
		cmdline := string(bytes.ReplaceAll(cmdlineBytes, []byte{0}, []byte{' '}))
		if strings.Contains(cmdline, "server/alienx.mjs") ||
			(strings.Contains(cmdline, "axosbackend") && !strings.Contains(cmdline, "go build") && !strings.Contains(cmdline, "go test")) {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				killedAny = true
			}
		}
	}
	if killedAny {
		time.Sleep(300 * time.Millisecond)
	}
}
