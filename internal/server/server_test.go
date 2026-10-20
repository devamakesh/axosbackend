package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockDoer func(req *http.Request) (*http.Response, error)

func (m mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m(req)
}

func jsonHTTPResponse(status int, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(b)),
	}
}

func defaultTestEnv() (string, map[string]string) {
	token := strings.Repeat("test-token-", 5)
	return token, map[string]string{
		"GEMENI_API":          "test-provider-key",
		"SARVAM_API":          "test-voice-key",
		"ALIENX_SERVER_TOKEN": token,
	}
}

func callServer(t *testing.T, srv http.Handler, path string, body any, auth string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+auth)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestServerRequiresAuthenticationBeforeContactingProviders(t *testing.T) {
	_, env := defaultTestEnv()
	calls := 0
	srv, err := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			calls++
			return jsonHTTPResponse(200, map[string]any{}), nil
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, _ := callServer(t, srv, "/v1/chat", map[string]any{}, "invalid", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	status, _ = callServer(t, srv, "/v1/transcribe", map[string]any{}, "invalid", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if calls != 0 {
		t.Fatalf("expected 0 provider calls, got %d", calls)
	}
}

func TestGeminiReceivesBoundedConversationAndDoesNotLeakCredentials(t *testing.T) {
	token, env := defaultTestEnv()
	var capturedReq *http.Request
	var capturedBody []byte

	srv, err := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			capturedBody, _ = io.ReadAll(req.Body)
			return jsonHTTPResponse(200, map[string]any{
				"candidates": []any{
					map[string]any{
						"content": map[string]any{
							"parts": []any{map[string]any{"text": "வணக்கம்!"}},
						},
					},
				},
			}), nil
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status, body := callServer(t, srv, "/v1/chat", map[string]any{
		"message": "வணக்கம்",
		"context": map[string]any{"profile": map[string]any{"language": "ta-IN"}},
		"history": []any{},
	}, token, nil)
	if status != 200 || body["text"] != "வணக்கம்!" {
		t.Fatalf("unexpected response: %d %v", status, body)
	}
	if capturedReq.Header.Get("x-goog-api-key") != env["GEMENI_API"] {
		t.Fatalf("expected x-goog-api-key header, got %s", capturedReq.Header.Get("x-goog-api-key"))
	}
	if !strings.Contains(string(capturedBody), "Tamil") {
		t.Fatalf("expected systemInstruction to mention Tamil")
	}

	status, _ = callServer(t, srv, "/v1/chat", map[string]any{
		"message": strings.Repeat("x", 1001),
		"context": map[string]any{},
	}, token, nil)
	if status != 400 {
		t.Fatalf("expected 400 for >1000 char message, got %d", status)
	}

	failedSrv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New(env["GEMENI_API"])
		}),
	})
	failStatus, failBody := callServer(t, failedSrv, "/v1/chat", map[string]any{
		"message": "hi",
		"context": map[string]any{},
	}, token, nil)
	if failStatus != 502 {
		t.Fatalf("expected 502, got %d", failStatus)
	}
	rawFail, _ := json.Marshal(failBody)
	if strings.Contains(string(rawFail), "test-provider-key") {
		t.Fatalf("leaked provider key in error response: %s", string(rawFail))
	}
}

func TestTamilTranscriptionSarvam(t *testing.T) {
	token, env := defaultTestEnv()
	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.sarvam.ai/speech-to-text" {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("api-subscription-key") != env["SARVAM_API"] {
				t.Fatalf("missing sarvam key")
			}
			_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
			if err != nil {
				t.Fatalf("invalid content-type: %v", err)
			}
			mr := multipart.NewReader(req.Body, params["boundary"])
			fields := map[string]string{}
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("part read error: %v", err)
				}
				b, _ := io.ReadAll(p)
				fields[p.FormName()] = string(b)
			}
			if fields["language_code"] != "ta-IN" || fields["mode"] != "transcribe" {
				t.Fatalf("unexpected form fields: %v", fields)
			}
			return jsonHTTPResponse(200, map[string]any{"transcript": "இன்றைய திட்டம்"}), nil
		}),
	})

	status, body := callServer(t, srv, "/v1/transcribe", map[string]any{
		"audio": "UklGRg==",
		"mime":  "audio/mp4",
	}, token, nil)
	if status != 200 || body["text"] != "இன்றைய திட்டம்" {
		t.Fatalf("unexpected transcribe result: %d %v", status, body)
	}

	badStatus, _ := callServer(t, srv, "/v1/transcribe", map[string]any{
		"audio": "invalid!",
		"mime":  "audio/mp4",
	}, token, nil)
	if badStatus != 400 {
		t.Fatalf("expected 400 for invalid base64 audio, got %d", badStatus)
	}
}

func TestTamilSpeechSynthesisSarvam(t *testing.T) {
	token, env := defaultTestEnv()
	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.sarvam.ai/text-to-speech" {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			var payload map[string]any
			_ = json.NewDecoder(req.Body).Decode(&payload)
			if payload["language_code"] != "ta-IN" || payload["model"] != "bulbul:v3" || payload["output_audio_codec"] != "wav" {
				t.Fatalf("unexpected TTS payload: %v", payload)
			}
			return jsonHTTPResponse(200, map[string]any{"audios": []string{"UklGRg=="}}), nil
		}),
	})

	status, _ := callServer(t, srv, "/v1/speak", map[string]any{"text": "வணக்கம்"}, token, nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	overStatus, _ := callServer(t, srv, "/v1/speak", map[string]any{"text": strings.Repeat("x", 2501)}, token, nil)
	if overStatus != 400 {
		t.Fatalf("expected 400 for >2500 chars, got %d", overStatus)
	}
}

func TestUnapprovedBrowserOriginsRejected(t *testing.T) {
	token, env := defaultTestEnv()
	srv, _ := NewAlienxServer(Options{Env: env})
	status, _ := callServer(t, srv, "/v1/chat", map[string]any{}, token, map[string]string{"Origin": "https://unknown.example"})
	if status != 403 {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestTemporaryGeminiOverloadRetriesAndFallsBack(t *testing.T) {
	token, env := defaultTestEnv()
	var urls []string
	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			urls = append(urls, req.URL.String())
			if len(urls) < 3 {
				return jsonHTTPResponse(503, map[string]any{}), nil
			}
			return jsonHTTPResponse(200, map[string]any{
				"candidates": []any{
					map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "வணக்கம்"}}}},
				},
			}), nil
		}),
	})

	status, _ := callServer(t, srv, "/v1/chat", map[string]any{"message": "hello", "context": map[string]any{}}, token, nil)
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if len(urls) != 3 || !strings.Contains(urls[2], "gemini-flash-lite-latest") {
		t.Fatalf("unexpected retry sequence: %v", urls)
	}
}

func TestGeminiAttemptTimeoutsRetryNextBoundedAttempt(t *testing.T) {
	token, env := defaultTestEnv()
	attempts := 0
	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, &TimeoutError{Message: "timed out"}
			}
			return jsonHTTPResponse(200, map[string]any{
				"candidates": []any{
					map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "Hello"}}}},
				},
			}), nil
		}),
	})

	status, _ := callServer(t, srv, "/v1/chat", map[string]any{"message": "hello", "context": map[string]any{}}, token, nil)
	if status != 200 || attempts != 2 {
		t.Fatalf("expected 200 after 2 attempts, got status=%d attempts=%d", status, attempts)
	}
}

func TestConnectionStatusAuthenticatedAndReturnsOnlyMetadata(t *testing.T) {
	token, env := defaultTestEnv()
	calls := 0
	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			calls++
			return jsonHTTPResponse(200, map[string]any{}), nil
		}),
	})

	status, _ := callServer(t, srv, "/v1/status", map[string]any{}, "wrong", nil)
	if status != 401 {
		t.Fatalf("expected 401, got %d", status)
	}
	status, body := callServer(t, srv, "/v1/status", map[string]any{}, token, nil)
	if status != 200 || body["service"] != "alienx" || body["geminiConfigured"] != true || body["sarvamConfigured"] != true {
		t.Fatalf("unexpected status response: %d %v", status, body)
	}
	if calls != 0 {
		t.Fatalf("expected 0 provider calls, got %d", calls)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "test-provider-key") || strings.Contains(string(raw), "test-voice-key") || strings.Contains(string(raw), "test-token") {
		t.Fatalf("status leaked secret: %s", string(raw))
	}

	noVoiceEnv := map[string]string{
		"GEMENI_API":          "test-provider-key",
		"ALIENX_SERVER_TOKEN": token,
	}
	noVoiceSrv, _ := NewAlienxServer(Options{Env: noVoiceEnv})
	_, noVoiceBody := callServer(t, noVoiceSrv, "/v1/status", map[string]any{}, token, nil)
	if noVoiceBody["sarvamConfigured"] != false {
		t.Fatalf("expected sarvamConfigured=false, got %v", noVoiceBody["sarvamConfigured"])
	}

	dsEnv := map[string]string{
		"GEMENI_API":          "test-provider-key",
		"SARVAM_API":          "test-voice-key",
		"DEEPSEEK_API_KEY":    "ds-secret",
		"ALIENX_SERVER_TOKEN": token,
	}
	dsSrv, _ := NewAlienxServer(Options{Env: dsEnv})
	_, dsBody := callServer(t, dsSrv, "/v1/status", map[string]any{}, token, nil)
	if dsBody["deepseekConfigured"] != true {
		t.Fatalf("expected deepseekConfigured=true, got %v", dsBody["deepseekConfigured"])
	}
}

func TestDeepSeekRoutingStripsThinkTagsAndFailsOver(t *testing.T) {
	token, env := defaultTestEnv()
	env["DEEPSEEK_API_KEY"] = "ds-key-secret"
	var urls []string

	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			urls = append(urls, req.URL.String())
			if req.URL.String() == "https://api.deepseek.com/chat/completions" {
				if req.Header.Get("Authorization") != "Bearer ds-key-secret" {
					t.Fatalf("unexpected auth header: %s", req.Header.Get("Authorization"))
				}
				return jsonHTTPResponse(200, map[string]any{
					"choices": []any{
						map[string]any{
							"message": map[string]any{
								"content": "<think>internal reasoning</think>\nஇன்று இரண்டு முக்கிய பணிகளில் கவனம் செலுத்துங்கள்.",
							},
						},
					},
				}), nil
			}
			return nil, errors.New("unexpected URL: " + req.URL.String())
		}),
	})

	status, body := callServer(t, srv, "/v1/chat", map[string]any{
		"message":  "AX-CEO briefing",
		"provider": "auto",
		"context":  map[string]any{"profile": map[string]any{"activeAgent": "ax-ceo", "language": "ta-IN"}},
	}, token, nil)
	if status != 200 || body["text"] != "இன்று இரண்டு முக்கிய பணிகளில் கவனம் செலுத்துங்கள்." || body["provider"] != "deepseek" {
		t.Fatalf("unexpected deepseek response: %d %v", status, body)
	}
	if len(urls) == 0 || urls[0] != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("expected deepseek URL first, got %v", urls)
	}

	failoverSrv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "generativelanguage.googleapis.com") {
				return jsonHTTPResponse(429, map[string]any{"error": "quota"}), nil
			}
			if req.URL.String() == "https://api.deepseek.com/chat/completions" {
				return jsonHTTPResponse(200, map[string]any{
					"choices": []any{
						map[string]any{"message": map[string]any{"content": "Answered by DeepSeek fallback."}},
					},
				}), nil
			}
			return nil, errors.New("unexpected URL")
		}),
	})

	fbStatus, fbBody := callServer(t, failoverSrv, "/v1/chat", map[string]any{
		"message":  "hello",
		"provider": "auto",
		"context":  map[string]any{"profile": map[string]any{"activeAgent": "personal"}},
	}, token, nil)
	if fbStatus != 200 || fbBody["text"] != "Answered by DeepSeek fallback." || fbBody["provider"] != "deepseek" {
		t.Fatalf("unexpected failover response: %d %v", fbStatus, fbBody)
	}
}

func TestUnifiedCoachChatEndpointValidatesSnapshotsAndRoutes(t *testing.T) {
	token, env := defaultTestEnv()
	env["DEEPSEEK_API_KEY"] = "ds-key-secret"

	srv, _ := NewAlienxServer(Options{
		Env: env,
		Client: mockDoer(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.deepseek.com/chat/completions" {
				t.Fatalf("expected deepseek URL, got %s", req.URL.String())
			}
			var payload openAIRequest
			_ = json.NewDecoder(req.Body).Decode(&payload)
			if len(payload.Messages) < 2 || !strings.Contains(payload.Messages[1].Content, "Tasks: 2 open") {
				t.Fatalf("missing snapshot in messages: %v", payload.Messages)
			}
			return jsonHTTPResponse(200, map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]any{"content": "Start with your top priority task for 25 minutes."}},
				},
			}), nil
		}),
	})

	status, body := callServer(t, srv, "/coach/chat", map[string]any{
		"message":  "Plan my morning",
		"snapshot": "Tasks: 2 open",
		"history": []any{
			map[string]any{"role": "assistant", "content": "Welcome"},
			map[string]any{"role": "user", "content": "Hi"},
		},
	}, token, nil)
	if status != 200 || body["reply"] != "Start with your top priority task for 25 minutes." || body["provider"] != "deepseek" {
		t.Fatalf("unexpected coach response: %d %v", status, body)
	}
}
