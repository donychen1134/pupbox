package openaiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	apiKey    string
	baseURL   string
	http      *http.Client
	chatModel string
	sttModel  string
	ttsModel  string
	ttsVoice  string
	ttsFormat string
	ttsPrompt string
	ttsSpeed  float64
}

type Config struct {
	APIKey    string
	BaseURL   string
	ChatModel string
	STTModel  string
	TTSModel  string
	TTSVoice  string
	TTSFormat string
	TTSPrompt string
	TTSSpeed  string
}

func NewFromEnv() *Client {
	return New(Config{
		APIKey:    os.Getenv("OPENAI_API_KEY"),
		BaseURL:   envDefault("OPENAI_BASE_URL", "https://api.openai.com"),
		ChatModel: envDefault("PUPBOX_CHAT_MODEL", "gpt-4o-mini"),
		STTModel:  envDefault("PUPBOX_STT_MODEL", "whisper-1"),
		TTSModel:  envDefault("PUPBOX_TTS_MODEL", "gpt-4o-mini-tts"),
		TTSVoice:  envDefault("PUPBOX_TTS_VOICE", "marin"),
		TTSFormat: envDefault("PUPBOX_TTS_FORMAT", "mp3"),
		TTSPrompt: envDefault("PUPBOX_TTS_PROMPT", defaultTTSPrompt),
		TTSSpeed:  envDefault("PUPBOX_TTS_SPEED", defaultTTSSpeedString),
	})
}

func New(cfg Config) *Client {
	return &Client{
		apiKey:    strings.TrimSpace(cfg.APIKey),
		baseURL:   strings.TrimRight(envDefaultValue(cfg.BaseURL, "https://api.openai.com"), "/"),
		http:      &http.Client{Timeout: 45 * time.Second},
		chatModel: envDefaultValue(cfg.ChatModel, "gpt-4o-mini"),
		sttModel:  envDefaultValue(cfg.STTModel, "whisper-1"),
		ttsModel:  envDefaultValue(cfg.TTSModel, "gpt-4o-mini-tts"),
		ttsVoice:  envDefaultValue(cfg.TTSVoice, "marin"),
		ttsFormat: envDefaultValue(cfg.TTSFormat, "mp3"),
		ttsPrompt: envDefaultValue(cfg.TTSPrompt, defaultTTSPrompt),
		ttsSpeed:  parseSpeechSpeed(envDefaultValue(cfg.TTSSpeed, defaultTTSSpeedString)),
	}
}

func (c *Client) Available() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) ChatModel() string {
	return c.chatModel
}

func (c *Client) STTModel() string {
	return c.sttModel
}

func (c *Client) TTSModel() string {
	return c.ttsModel
}

func (c *Client) TTSVoice() string {
	return c.ttsVoice
}

func (c *Client) TTSFormat() string {
	return c.ttsFormat
}

func (c *Client) TTSSpeed() float64 {
	return c.ttsSpeed
}

func (c *Client) CreateResponse(ctx context.Context, instructions, input string) (string, error) {
	if !c.Available() {
		return "", errors.New("openai api key is not configured")
	}

	payload := map[string]any{
		"model":             c.chatModel,
		"instructions":      instructions,
		"input":             input,
		"max_output_tokens": 220,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return "", err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/responses", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("responses api returned %s: %s", resp.Status, string(data))
	}

	var parsed responsePayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.OutputText) != "" {
		return strings.TrimSpace(parsed.OutputText), nil
	}

	text := extractOutputText(parsed.Output)
	if text == "" {
		return "", errors.New("responses api returned no output_text")
	}
	return text, nil
}

func (c *Client) Transcribe(ctx context.Context, audio []byte, filename, contentType string) (string, error) {
	if !c.Available() {
		return "", errors.New("openai api key is not configured")
	}
	if len(audio) == 0 {
		return "", errors.New("empty audio")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", c.sttModel); err != nil {
		return "", err
	}
	_ = writer.WriteField("language", "zh")

	part, err := createAudioPart(writer, filename, contentType)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("transcriptions api returned %s: %s", resp.Status, string(data))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return "", errors.New("transcription returned empty text")
	}
	return strings.TrimSpace(parsed.Text), nil
}

func (c *Client) Speak(ctx context.Context, text string) ([]byte, string, error) {
	if !c.Available() {
		return nil, "", errors.New("openai api key is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", errors.New("empty speech text")
	}

	payload := map[string]any{
		"model":           c.ttsModel,
		"voice":           c.ttsVoice,
		"input":           text,
		"response_format": c.ttsFormat,
		"instructions":    c.ttsPrompt,
		"speed":           c.ttsSpeed,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return nil, "", err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/audio/speech", &body)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("speech api returned %s: %s", resp.Status, string(data))
	}
	return data, audioMIME(c.ttsFormat), nil
}

const (
	defaultTTSSpeed       = 0.88
	defaultTTSSpeedString = "0.88"
	defaultTTSPrompt      = "你是一个藏在毛绒小狗玩具里的中文声音。声音要温暖、圆润、亲近、像在和三岁小女孩玩；语速偏慢，吐字清楚，句子之间有短停顿。不要播音腔，不要机械，不要严肃。"
)

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

type responsePayload struct {
	OutputText string           `json:"output_text"`
	Output     []responseOutput `json:"output"`
}

type responseOutput struct {
	Content []responseContent `json:"content"`
}

type responseContent struct {
	Text string `json:"text"`
}

func extractOutputText(output []responseOutput) string {
	var parts []string
	for _, item := range output {
		for _, content := range item.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func createAudioPart(writer *multipart.Writer, filename, contentType string) (io.Writer, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "/" || filename == "" {
		filename = "recording.webm"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filename)))
	header.Set("Content-Type", contentType)
	return writer.CreatePart(header)
}

func escapeQuotes(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(s)
}

func envDefault(key, fallback string) string {
	return envDefaultValue(os.Getenv(key), fallback)
}

func envDefaultValue(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func parseSpeechSpeed(value string) float64 {
	speed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || speed < 0.25 || speed > 4.0 {
		return defaultTTSSpeed
	}
	return speed
}

func audioMIME(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	default:
		return "audio/mpeg"
	}
}
