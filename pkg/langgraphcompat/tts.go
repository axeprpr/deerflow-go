package langgraphcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultTTSBaseURL = "https://api.openai.com/v1"
	defaultTTSModel   = "gpt-4o-mini-tts"
	defaultTTSVoice   = "alloy"
)

var gatewayTTSHTTPClient = &http.Client{Timeout: 2 * time.Minute}

type gatewayTTSRequest struct {
	Text         string  `json:"text"`
	Model        string  `json:"model,omitempty"`
	Voice        string  `json:"voice,omitempty"`
	Format       string  `json:"format,omitempty"`
	Instructions string  `json:"instructions,omitempty"`
	SpeedRatio   float64 `json:"speed_ratio,omitempty"`
	VolumeRatio  float64 `json:"volume_ratio,omitempty"`
	PitchRatio   float64 `json:"pitch_ratio,omitempty"`
	Input        string  `json:"input,omitempty"`
	ResponseFmt  string  `json:"response_format,omitempty"`
}

type gatewayTTSConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Voice   string
}

func (s *Server) handleTTS(w http.ResponseWriter, r *http.Request) {
	var req gatewayTTSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid request body"})
		return
	}

	input := strings.TrimSpace(firstNonEmpty(req.Text, req.Input))
	if input == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "text is required"})
		return
	}

	cfg, err := gatewayTTSConfigFromEnv()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": err.Error()})
		return
	}

	audio, format, err := synthesizeGatewaySpeech(r.Context(), cfg, req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": err.Error()})
		return
	}

	w.Header().Set("Content-Type", ttsContentType(format))
	w.Header().Set("Content-Disposition", contentDisposition("attachment", "speech."+format))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func gatewayTTSConfigFromEnv() (gatewayTTSConfig, error) {
	apiKey := strings.TrimSpace(firstNonEmpty(
		os.Getenv("TTS_API_KEY"),
		os.Getenv("OPENAI_API_KEY"),
	))
	if apiKey == "" {
		return gatewayTTSConfig{}, fmt.Errorf("tts is not configured: set TTS_API_KEY or OPENAI_API_KEY")
	}

	baseURL := strings.TrimSpace(firstNonEmpty(
		os.Getenv("TTS_API_BASE_URL"),
		os.Getenv("OPENAI_API_BASE_URL"),
		defaultTTSBaseURL,
	))
	model := strings.TrimSpace(firstNonEmpty(os.Getenv("TTS_MODEL"), defaultTTSModel))
	voice := strings.TrimSpace(firstNonEmpty(os.Getenv("TTS_VOICE"), defaultTTSVoice))
	return gatewayTTSConfig{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Voice:   voice,
	}, nil
}

func synthesizeGatewaySpeech(ctx context.Context, cfg gatewayTTSConfig, req gatewayTTSRequest) ([]byte, string, error) {
	format := normalizeTTSFormat(firstNonEmpty(req.Format, req.ResponseFmt))
	payload := map[string]any{
		"model":           firstNonEmpty(strings.TrimSpace(req.Model), cfg.Model),
		"voice":           firstNonEmpty(strings.TrimSpace(req.Voice), cfg.Voice),
		"input":           strings.TrimSpace(firstNonEmpty(req.Text, req.Input)),
		"response_format": format,
	}
	if instructions := strings.TrimSpace(req.Instructions); instructions != "" {
		payload["instructions"] = instructions
	}
	if req.SpeedRatio > 0 {
		payload["speed"] = req.SpeedRatio
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", ttsContentType(format))

	resp, err := gatewayTTSHTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("tts upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, format, nil
}

func normalizeTTSFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3", "opus", "aac", "flac", "wav", "pcm":
		return strings.ToLower(strings.TrimSpace(format))
	default:
		return "mp3"
	}
}

func ttsContentType(format string) string {
	switch normalizeTTSFormat(format) {
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "opus":
		return "audio/opus"
	case "pcm":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}
