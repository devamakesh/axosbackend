package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureServerTokenAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(envPath, []byte("GEMINI_API_KEY=test-key\n"), 0o600)

	origToken, hadToken := os.LookupEnv("ALIENX_SERVER_TOKEN")
	_ = os.Unsetenv("ALIENX_SERVER_TOKEN")
	defer func() {
		if hadToken {
			_ = os.Setenv("ALIENX_SERVER_TOKEN", origToken)
		} else {
			_ = os.Unsetenv("ALIENX_SERVER_TOKEN")
		}
	}()

	created, err := EnsureServerToken(envPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatalf("expected token to be created")
	}

	tok := os.Getenv("ALIENX_SERVER_TOKEN")
	if len(tok) != 64 {
		t.Fatalf("expected 64-char hex token, got len=%d", len(tok))
	}

	createdAgain, err := EnsureServerToken(envPath)
	if err != nil || createdAgain {
		t.Fatalf("expected existing token preserved, createdAgain=%v err=%v", createdAgain, err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(string(data), "GEMINI_API_KEY=test-key") || !strings.Contains(string(data), "ALIENX_SERVER_TOKEN="+tok) {
		t.Fatalf("unexpected .env contents: %s", string(data))
	}
}
