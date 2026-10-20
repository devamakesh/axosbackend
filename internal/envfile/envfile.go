package envfile

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads environment variables from candidate .env files without overriding
// variables already present in the process environment.
func Load(paths ...string) {
	if len(paths) == 0 {
		paths = []string{".env", filepath.Join("..", ".env")}
	}
	for _, p := range paths {
		_ = loadFile(p)
	}
}

func loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

// ResolveEnvPath returns the target .env file path to update during setup.
// If ../.env exists and .env does not exist in the current directory, it uses ../.env.
func ResolveEnvPath() string {
	if _, err := os.Stat(".env"); err == nil {
		return ".env"
	}
	parentEnv := filepath.Join("..", ".env")
	if _, err := os.Stat(parentEnv); err == nil {
		return parentEnv
	}
	return ".env"
}

// EnsureServerToken ensures ALIENX_SERVER_TOKEN is configured in the environment or target .env file.
func EnsureServerToken(targetPath string) (created bool, err error) {
	Load(".env", filepath.Join("..", ".env"))
	if targetPath == "" {
		targetPath = ResolveEnvPath()
	}
	if tok := strings.TrimSpace(os.Getenv("ALIENX_SERVER_TOKEN")); tok != "" {
		return false, nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(buf)

	f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "\nALIENX_SERVER_TOKEN=%s\n", token); err != nil {
		return false, err
	}
	_ = os.Setenv("ALIENX_SERVER_TOKEN", token)
	return true, nil
}
