package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"axosbackend/internal/envfile"
	"axosbackend/internal/server"
)

func redact(msg, secret string) string {
	if secret == "" {
		return msg
	}
	out := strings.ReplaceAll(msg, secret, "[redacted]")
	if len(out) > 350 {
		return out[:350]
	}
	return out
}

// Run executes the provider smoke or diagnostic checks based on args.
func Run(args []string) int {
	envfile.Load()

	hasFlag := func(flag string) bool {
		for _, a := range args {
			if a == flag {
				return true
			}
		}
		return false
	}

	geminiKey := firstEnv("GEMINI_API_KEY", "GEMENI_API", "GEMINI_API")
	deepseekKey := firstEnv("DEEPSEEK_API_KEY", "DEEPSEEK_API")
	client := &http.Client{Timeout: 25 * time.Second}

	if hasFlag("--diagnose") {
		if geminiKey != "" {
			for _, model := range []string{"gemini-flash-latest", "gemini-3.1-flash-lite"} {
				payload, _ := json.Marshal(map[string]any{
					"contents": []any{
						map[string]any{"parts": []any{map[string]any{"text": "Say hello."}}},
					},
				})
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model), bytes.NewReader(payload))
				req.Header.Set("x-goog-api-key", geminiKey)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				cancel()
				if err != nil {
					fmt.Printf("%s: error %s\n", model, redact(err.Error(), geminiKey))
					continue
				}
				var data struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&data)
				_ = resp.Body.Close()
				suffix := ""
				if data.Error.Message != "" {
					suffix = " " + redact(data.Error.Message, geminiKey)
				}
				fmt.Printf("%s: %d%s\n", model, resp.StatusCode, suffix)
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					break
				}
			}
		}
		if deepseekKey != "" {
			dsModel := firstEnv("DEEPSEEK_MODEL")
			if dsModel == "" {
				dsModel = "deepseek-chat"
			}
			payload, _ := json.Marshal(map[string]any{
				"model": dsModel,
				"messages": []any{
					map[string]any{"role": "user", "content": "Say hello."},
				},
			})
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(payload))
			req.Header.Set("Authorization", "Bearer "+deepseekKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			cancel()
			if err != nil {
				fmt.Printf("deepseek: error %s\n", redact(err.Error(), deepseekKey))
			} else {
				var data struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&data)
				_ = resp.Body.Close()
				suffix := ""
				if data.Error.Message != "" {
					suffix = " " + redact(data.Error.Message, deepseekKey)
				}
				fmt.Printf("deepseek: %d%s\n", resp.StatusCode, suffix)
			}
		}
		return 0
	}

	if hasFlag("--models") {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://generativelanguage.googleapis.com/v1beta/models", nil)
		req.Header.Set("x-goog-api-key", geminiKey)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			status := 0
			if resp != nil {
				status = resp.StatusCode
				_ = resp.Body.Close()
			}
			fmt.Fprintf(os.Stderr, "Model list unavailable (%d)\n", status)
			return 1
		}
		var data struct {
			Models []struct {
				Name                       string   `json:"name"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		var names []string
		for _, m := range data.Models {
			supportsGen := false
			for _, method := range m.SupportedGenerationMethods {
				if method == "generateContent" {
					supportsGen = true
					break
				}
			}
			if supportsGen && strings.Contains(m.Name, "flash") {
				names = append(names, m.Name)
				if len(names) == 12 {
					break
				}
			}
		}
		fmt.Println(strings.Join(names, "\n"))
		return 0
	}

	srv, err := server.NewAlienxServer(server.Options{Env: server.EnvMapFromProcess()})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	token := firstEnv("ALIENX_SERVER_TOKEN", "COACH_SERVER_TOKEN")
	call := func(path string, body map[string]any) (map[string]any, error) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusOK {
			extra := ""
			if ps, ok := out["providerStatus"]; ok {
				extra += fmt.Sprintf(", upstream %v", ps)
			}
			if nc, ok := out["networkCode"]; ok {
				extra += fmt.Sprintf(", %v", nc)
			}
			return nil, fmt.Errorf("%s: provider check failed (%d%s)", path, rec.Code, extra)
		}
		return out, nil
	}

	failed := false
	res, err := call("/v1/chat", map[string]any{
		"message":  "Say a short hello in Tamil.",
		"context":  map[string]any{"profile": map[string]any{"language": "ta-IN"}},
		"history":  []any{},
		"provider": "auto",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		failed = true
	} else {
		prov, _ := res["provider"].(string)
		if prov == "" {
			prov = "connected"
		}
		fmt.Printf("AI conversation (%s): connected\n", prov)
	}

	if deepseekKey != "" {
		_, err := call("/v1/chat", map[string]any{
			"message":  "Say hello in one sentence.",
			"context":  map[string]any{"profile": map[string]any{"language": "en-IN"}},
			"history":  []any{},
			"provider": "deepseek",
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			failed = true
		} else {
			fmt.Println("DeepSeek conversation: connected")
		}
	}

	speech, err := call("/v1/speak", map[string]any{
		"text": "வணக்கம். எப்படி இருக்கிறீர்கள்?",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		failed = true
	} else {
		fmt.Println("Sarvam Tamil speech: connected")
		_, err := call("/v1/transcribe", map[string]any{
			"audio": speech["audio"],
			"mime":  "audio/wav",
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			failed = true
		} else {
			fmt.Println("Sarvam Tamil transcription: connected")
		}
	}

	if failed {
		return 1
	}
	return 0
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
