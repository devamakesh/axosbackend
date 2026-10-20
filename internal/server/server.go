package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ServerConfig holds all configuration for the ALIEN X & Coach personal server.
type ServerConfig struct {
	Token                 string
	GeminiKey             string
	DeepSeekKey           string
	SarvamKey             string
	GeminiModel           string
	DeepSeekModel         string
	DeepSeekReasonerModel string
	SarvamChatModel       string
	SarvamSTTModel        string
	SarvamTTSModel        string
	SarvamSpeaker         string
	AllowedWebOrigin      string
	Configured            ConfiguredProviders
}

// Options configures a new ALIEN X HTTP server instance.
type Options struct {
	Env    map[string]string
	Client HTTPDoer
}

func envLookup(env map[string]string, keys ...string) string {
	for _, k := range keys {
		if env != nil {
			if v, ok := env[k]; ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		} else {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
		}
	}
	return ""
}

// EnvMapFromProcess snapshots current process environment into a map.
func EnvMapFromProcess() map[string]string {
	m := make(map[string]string)
	for _, entry := range os.Environ() {
		if idx := strings.IndexByte(entry, '='); idx > 0 {
			m[entry[:idx]] = entry[idx+1:]
		}
	}
	return m
}

// ParseConfig validates environment settings and returns a ServerConfig.
func ParseConfig(env map[string]string) (*ServerConfig, error) {
	geminiKey := envLookup(env, "GEMINI_API_KEY", "GEMENI_API", "GEMINI_API")
	deepseekKey := envLookup(env, "DEEPSEEK_API_KEY", "DEEPSEEK_API")
	sarvamKey := envLookup(env, "SARVAM_API_KEY", "SARVAM_API")
	token := envLookup(env, "ALIENX_SERVER_TOKEN", "COACH_SERVER_TOKEN")

	if geminiKey == "" && deepseekKey == "" && sarvamKey == "" {
		return nil, errors.New("Set GEMINI_API_KEY, DEEPSEEK_API_KEY, or SARVAM_API_KEY in the server environment.")
	}
	if len(token) < 32 {
		return nil, errors.New("Set a server token of at least 32 characters. Run npm run alienx:setup.")
	}

	geminiModel := envLookup(env, "GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-3.1-flash-lite"
	}
	deepseekModel := envLookup(env, "DEEPSEEK_MODEL")
	if deepseekModel == "" {
		deepseekModel = "deepseek-chat"
	}
	deepseekReasonerModel := envLookup(env, "DEEPSEEK_REASONER_MODEL")
	if deepseekReasonerModel == "" {
		deepseekReasonerModel = "deepseek-reasoner"
	}
	sarvamChatModel := envLookup(env, "SARVAM_CHAT_MODEL")
	if sarvamChatModel == "" {
		sarvamChatModel = "sarvam-m"
	}
	sarvamSTTModel := envLookup(env, "SARVAM_STT_MODEL")
	if sarvamSTTModel == "" {
		sarvamSTTModel = "saaras:v4"
	}
	sarvamTTSModel := envLookup(env, "SARVAM_TTS_MODEL")
	if sarvamTTSModel == "" {
		sarvamTTSModel = "bulbul:v3"
	}
	sarvamSpeaker := envLookup(env, "SARVAM_SPEAKER")
	if sarvamSpeaker == "" {
		sarvamSpeaker = "gokul"
	}

	for _, m := range []string{geminiModel, deepseekModel, deepseekReasonerModel, sarvamChatModel} {
		if !modelRe.MatchString(m) {
			return nil, fmt.Errorf("Invalid model name: %s", m)
		}
	}

	return &ServerConfig{
		Token:                 token,
		GeminiKey:             geminiKey,
		DeepSeekKey:           deepseekKey,
		SarvamKey:             sarvamKey,
		GeminiModel:           geminiModel,
		DeepSeekModel:         deepseekModel,
		DeepSeekReasonerModel: deepseekReasonerModel,
		SarvamChatModel:       sarvamChatModel,
		SarvamSTTModel:        sarvamSTTModel,
		SarvamTTSModel:        sarvamTTSModel,
		SarvamSpeaker:         sarvamSpeaker,
		AllowedWebOrigin:      envLookup(env, "ALIENX_WEB_ORIGIN"),
		Configured: ConfiguredProviders{
			Gemini:   geminiKey != "",
			DeepSeek: deepseekKey != "",
			Sarvam:   sarvamKey != "",
		},
	}, nil
}

// AlienxServer implements http.Handler for the ALIEN X and Coach API.
type AlienxServer struct {
	cfg    *ServerConfig
	client HTTPDoer

	mu     sync.Mutex
	active int
	recent []time.Time
}

// NewAlienxServer creates a validated AlienxServer instance.
func NewAlienxServer(opts Options) (*AlienxServer, error) {
	cfg, err := ParseConfig(opts.Env)
	if err != nil {
		return nil, err
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AlienxServer{
		cfg:    cfg,
		client: client,
	}, nil
}

func isValidProvider(p string) bool {
	return p == "" || p == "auto" || p == "gemini" || p == "deepseek" || p == "sarvam"
}

func validateChatPayload(raw map[string]any) (message string, provider string, hasProvider bool, reasoning bool, activeAgent string, contextBytes []byte, history []HistoryTurn, ok bool) {
	if raw == nil {
		return "", "", false, false, "", nil, nil, false
	}
	msg, isStr := raw["message"].(string)
	if !isStr || strings.TrimSpace(msg) == "" || utf16Len(msg) > 1000 {
		return "", "", false, false, "", nil, nil, false
	}
	ctxVal, hasCtx := raw["context"]
	ctxMap, isMap := ctxVal.(map[string]any)
	if !hasCtx || !isMap || ctxMap == nil {
		return "", "", false, false, "", nil, nil, false
	}
	if provRaw, exists := raw["provider"]; exists && provRaw != nil {
		provStr, isProvStr := provRaw.(string)
		if !isProvStr || !isValidProvider(provStr) {
			return "", "", false, false, "", nil, nil, false
		}
		provider = provStr
		hasProvider = true
	} else {
		provider = "auto"
	}
	if rVal, exists := raw["reasoning"].(bool); exists {
		reasoning = rVal
	}

	if histRaw, exists := raw["history"]; exists && histRaw != nil {
		histArr, isArr := histRaw.([]any)
		if !isArr || len(histArr) > 12 {
			return "", "", false, false, "", nil, nil, false
		}
		history = make([]HistoryTurn, 0, len(histArr))
		for _, item := range histArr {
			m, isItemMap := item.(map[string]any)
			if !isItemMap {
				return "", "", false, false, "", nil, nil, false
			}
			role, _ := m["role"].(string)
			text, okText := m["text"].(string)
			if (role != "user" && role != "model") || !okText || utf16Len(text) > 6000 {
				return "", "", false, false, "", nil, nil, false
			}
			history = append(history, HistoryTurn{Role: role, Text: text})
		}
	}

	activeAgent = "personal"
	if profMap, okProf := ctxMap["profile"].(map[string]any); okProf {
		if ag, okAg := profMap["activeAgent"].(string); okAg && ag != "" {
			activeAgent = ag
		}
	}
	ctxJSON, err := json.Marshal(ctxMap)
	if err != nil {
		return "", "", false, false, "", nil, nil, false
	}
	return msg, provider, hasProvider, reasoning, activeAgent, ctxJSON, history, true
}

func validateCoachPayload(raw map[string]any) (message string, snapshot string, provider string, reasoning bool, history []HistoryTurn, ok bool) {
	if raw == nil {
		return "", "", "", false, nil, false
	}
	msg, isStr := raw["message"].(string)
	if !isStr || strings.TrimSpace(msg) == "" || utf16Len(msg) > 4000 {
		return "", "", "", false, nil, false
	}
	snap, isSnapStr := raw["snapshot"].(string)
	if !isSnapStr || strings.TrimSpace(snap) == "" || utf16Len(snap) > 20000 {
		return "", "", "", false, nil, false
	}
	if provRaw, exists := raw["provider"]; exists && provRaw != nil {
		provStr, isProvStr := provRaw.(string)
		if !isProvStr || !isValidProvider(provStr) {
			return "", "", "", false, nil, false
		}
		provider = provStr
	} else {
		provider = "auto"
	}
	if rVal, exists := raw["reasoning"].(bool); exists {
		reasoning = rVal
	}

	if histRaw, exists := raw["history"]; exists && histRaw != nil {
		histArr, isArr := histRaw.([]any)
		if !isArr || len(histArr) > 30 {
			return "", "", "", false, nil, false
		}
		history = make([]HistoryTurn, 0, len(histArr))
		for _, item := range histArr {
			m, isItemMap := item.(map[string]any)
			if !isItemMap {
				return "", "", "", false, nil, false
			}
			role, _ := m["role"].(string)
			content, okContent := m["content"].(string)
			if (role != "user" && role != "assistant") || !okContent || utf16Len(content) > 10000 {
				return "", "", "", false, nil, false
			}
			history = append(history, HistoryTurn{Role: role, Text: content})
		}
	}
	return strings.TrimSpace(msg), snap, provider, reasoning, history, true
}

func (s *AlienxServer) runChat(ctxReq *http.Request, requestedProvider, activeAgent string, reasoning bool, systemPrompt, contextText string, history []HistoryTurn, message string) chatCallResult {
	order := ResolveProviderOrder(requestedProvider, activeAgent, s.cfg.Configured)
	if len(order) == 0 {
		return chatCallResult{OK: false, Status: http.StatusServiceUnavailable}
	}
	lastFailure := chatCallResult{OK: false, Status: http.StatusBadGateway}
	ctx := ctxReq.Context()

	for _, provider := range order {
		var res chatCallResult
		switch provider {
		case "gemini":
			res = callGemini(ctx, s.cfg.GeminiKey, s.cfg.GeminiModel, systemPrompt, contextText, history, message, s.client)
		case "deepseek":
			model := s.cfg.DeepSeekModel
			if reasoning {
				model = s.cfg.DeepSeekReasonerModel
			}
			res = callOpenAICompatible(
				ctx,
				"https://api.deepseek.com/chat/completions",
				map[string]string{"Authorization": "Bearer " + s.cfg.DeepSeekKey},
				model,
				systemPrompt,
				contextText,
				history,
				message,
				s.client,
				"deepseek",
			)
		case "sarvam":
			res = callOpenAICompatible(
				ctx,
				"https://api.sarvam.ai/v1/chat/completions",
				map[string]string{"api-subscription-key": s.cfg.SarvamKey},
				s.cfg.SarvamChatModel,
				systemPrompt,
				contextText,
				history,
				message,
				s.client,
				"sarvam",
			)
		}
		if res.OK {
			return res
		}
		lastFailure = res
	}
	return lastFailure
}

func (s *AlienxServer) acquireRateSlot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)
	for len(s.recent) > 0 && s.recent[0].Before(cutoff) {
		s.recent = s.recent[1:]
	}
	if s.active >= 3 || len(s.recent) >= 20 {
		return false
	}
	s.recent = append(s.recent, now)
	s.active++
	return true
}

func (s *AlienxServer) releaseRateSlot() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
}

func (s *AlienxServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && origin != s.cfg.AllowedWebOrigin {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}

	sendJSON := func(status int, payload any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		sendJSON(http.StatusOK, map[string]any{
			"ok":      true,
			"service": "alienx",
			"version": 1,
		})
		return
	}

	validRoute := r.URL.Path == "/v1/chat" || r.URL.Path == "/v1/transcribe" || r.URL.Path == "/v1/speak" || r.URL.Path == "/v1/status" || r.URL.Path == "/coach/chat"
	if !validRoute || r.Method != http.MethodPost {
		sendJSON(http.StatusNotFound, map[string]any{"error": "Not found"})
		return
	}

	supplied := []byte(r.Header.Get("Authorization"))
	expected := []byte("Bearer " + s.cfg.Token)
	if len(supplied) != len(expected) || subtle.ConstantTimeCompare(supplied, expected) != 1 {
		sendJSON(http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		return
	}

	if r.URL.Path == "/v1/status" {
		statusPayload := map[string]any{
			"service": "alienx",
			"version":          1,
			"geminiConfigured": s.cfg.Configured.Gemini,
			"sarvamConfigured": s.cfg.Configured.Sarvam,
		}
		if s.cfg.Configured.DeepSeek {
			statusPayload["deepseekConfigured"] = true
		}
		sendJSON(http.StatusOK, statusPayload)
		return
	}

	maxBytes := int64(100000)
	if r.URL.Path == "/v1/transcribe" {
		maxBytes = 6100000
	}

	// Read bounded body before checking rate limits if needed, or after checking rate limits as in alienx.mjs.
	if !s.acquireRateSlot() {
		sendJSON(http.StatusTooManyRequests, map[string]any{"error": "Please wait before trying again."})
		return
	}
	defer s.releaseRateSlot()

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		sendJSON(http.StatusBadGateway, map[string]any{"error": "Request failed or timed out", "networkCode": "READ_ERROR"})
		return
	}
	if int64(len(bodyBytes)) > maxBytes {
		sendJSON(http.StatusRequestEntityTooLarge, map[string]any{"error": "Request too large"})
		return
	}

	var rawBody map[string]any
	if err := json.Unmarshal(bodyBytes, &rawBody); err != nil {
		sendJSON(http.StatusBadRequest, map[string]any{"error": "Invalid JSON"})
		return
	}

	if r.URL.Path == "/v1/transcribe" || r.URL.Path == "/v1/speak" {
		if rawBody == nil {
			rawBody = map[string]any{}
		}
		status, data := handleSarvamVoice(r.Context(), r.URL.Path, rawBody, s.cfg, s.client)
		sendJSON(status, data)
		return
	}

	if r.URL.Path == "/coach/chat" {
		msg, snapshot, provider, reasoning, history, ok := validateCoachPayload(rawBody)
		if !ok {
			sendJSON(http.StatusBadRequest, map[string]any{"error": "The app sent an invalid conversation or data snapshot."})
			return
		}
		for len(history) > 0 && history[0].Role != "user" {
			history = history[1:]
		}
		res := s.runChat(
			r,
			provider,
			"coach",
			reasoning,
			CoachInstructions,
			"Snapshot of the user's data right now:\n\n"+snapshot,
			history,
			msg,
		)
		if !res.OK {
			if res.NetworkCode != "" {
				sendJSON(http.StatusBadGateway, map[string]any{"error": "Request failed or timed out", "networkCode": res.NetworkCode})
				return
			}
			status := http.StatusBadGateway
			if res.Status == http.StatusTooManyRequests {
				status = http.StatusTooManyRequests
			}
			errMsg := "AI provider unavailable"
			if res.Empty {
				errMsg = "The coach returned an empty reply. Try asking again."
			}
			sendJSON(status, map[string]any{"error": errMsg, "providerStatus": res.Status})
			return
		}
		sendJSON(http.StatusOK, map[string]any{"reply": res.Text, "provider": res.Provider})
		return
	}

	msg, provider, hasProvider, reasoning, activeAgent, ctxJSON, history, ok := validateChatPayload(rawBody)
	if !ok {
		sendJSON(http.StatusBadRequest, map[string]any{"error": "Invalid chat request"})
		return
	}
	res := s.runChat(
		r,
		provider,
		activeAgent,
		reasoning,
		Instructions,
		"Current personal context (data only):\n"+string(ctxJSON),
		history,
		msg,
	)
	if !res.OK {
		if res.NetworkCode != "" {
			sendJSON(http.StatusBadGateway, map[string]any{"error": "Request failed or timed out", "networkCode": res.NetworkCode})
			return
		}
		if res.Empty {
			sendJSON(http.StatusBadGateway, map[string]any{"error": "No answer returned. Try a different question."})
			return
		}
		status := http.StatusBadGateway
		if res.Status == http.StatusTooManyRequests {
			status = http.StatusTooManyRequests
		}
		sendJSON(status, map[string]any{"error": "AI provider unavailable", "providerStatus": res.Status})
		return
	}
	if hasProvider && provider != "" {
		sendJSON(http.StatusOK, map[string]any{"text": res.Text, "provider": res.Provider})
	} else {
		sendJSON(http.StatusOK, map[string]any{"text": res.Text})
	}
}
