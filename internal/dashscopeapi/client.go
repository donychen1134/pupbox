package dashscopeapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	apiKey     string
	baseURL    string
	http       *http.Client
	chatModel  string
	sttModel   string
	ttsModel   string
	ttsVoice   string
	ttsFormat  string
	ttsPrompt  string
	ttsSpeed   float64
	sampleRate int
}

type Config struct {
	APIKey     string
	BaseURL    string
	ChatModel  string
	STTModel   string
	TTSModel   string
	TTSVoice   string
	TTSFormat  string
	TTSPrompt  string
	TTSSpeed   string
	SampleRate string
}

func NewFromEnv() *Client {
	return New(Config{
		APIKey:     envAny("CHAT_ARCHIVE_QWEN_API_KEY", "DASHSCOPE_API_KEY"),
		BaseURL:    envDefault("PUPBOX_DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com"),
		ChatModel:  envDefault("PUPBOX_DASHSCOPE_CHAT_MODEL", "qwen-plus-character"),
		STTModel:   envDefault("PUPBOX_DASHSCOPE_STT_MODEL", "qwen3-asr-flash"),
		TTSModel:   envDefault("PUPBOX_DASHSCOPE_TTS_MODEL", "cosyvoice-v3-flash"),
		TTSVoice:   envDefault("PUPBOX_DASHSCOPE_TTS_VOICE", "longhuhu_v3"),
		TTSFormat:  envDefault("PUPBOX_DASHSCOPE_TTS_FORMAT", envDefault("PUPBOX_TTS_FORMAT", "mp3")),
		TTSPrompt:  os.Getenv("PUPBOX_DASHSCOPE_TTS_PROMPT"),
		TTSSpeed:   envDefault("PUPBOX_DASHSCOPE_TTS_SPEED", envDefault("PUPBOX_TTS_SPEED", defaultTTSSpeedString)),
		SampleRate: envDefault("PUPBOX_DASHSCOPE_TTS_SAMPLE_RATE", "24000"),
	})
}

func New(cfg Config) *Client {
	return &Client{
		apiKey:     strings.TrimSpace(cfg.APIKey),
		baseURL:    strings.TrimRight(envDefaultValue(cfg.BaseURL, "https://dashscope.aliyuncs.com"), "/"),
		http:       &http.Client{Timeout: 60 * time.Second},
		chatModel:  envDefaultValue(cfg.ChatModel, "qwen-plus-character"),
		sttModel:   envDefaultValue(cfg.STTModel, "qwen3-asr-flash"),
		ttsModel:   envDefaultValue(cfg.TTSModel, "cosyvoice-v3-flash"),
		ttsVoice:   envDefaultValue(cfg.TTSVoice, "longhuhu_v3"),
		ttsFormat:  envDefaultValue(cfg.TTSFormat, "mp3"),
		ttsPrompt:  strings.TrimSpace(cfg.TTSPrompt),
		ttsSpeed:   parseSpeechRate(envDefaultValue(cfg.TTSSpeed, defaultTTSSpeedString)),
		sampleRate: parseSampleRate(envDefaultValue(cfg.SampleRate, "24000")),
	}
}

func (c *Client) Available() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) Name() string {
	return "dashscope"
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

func (c *Client) StreamSampleRate() int {
	return c.sampleRate
}

func (c *Client) CreateResponse(ctx context.Context, instructions, input string) (string, error) {
	return c.createResponse(ctx, instructions, input, false)
}

func (c *Client) CreateStructuredResponse(ctx context.Context, instructions, input string) (string, error) {
	return c.createResponse(ctx, instructions, input, true)
}

func (c *Client) createResponse(ctx context.Context, instructions, input string, structured bool) (string, error) {
	if !c.Available() {
		return "", errors.New("dashscope api key is not configured")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("empty chat input")
	}

	messages := []map[string]string{
		{"role": "user", "content": input},
	}
	if strings.TrimSpace(instructions) != "" {
		messages = append([]map[string]string{{"role": "system", "content": instructions}}, messages...)
	}
	payload := map[string]any{
		"model":       c.chatModel,
		"messages":    messages,
		"temperature": chatTemperature(c.chatModel),
		"max_tokens":  180,
	}
	if structured {
		payload["response_format"] = map[string]string{"type": "json_object"}
		payload["temperature"] = 0.2
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return "", err
	}

	req, err := c.newJSONRequest(ctx, "/compatible-mode/v1/chat/completions", &body)
	if err != nil {
		return "", err
	}

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
		return "", fmt.Errorf("dashscope chat api returned %s: %s", resp.Status, string(data))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", errors.New("dashscope chat returned no content")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func chatTemperature(model string) float64 {
	if strings.Contains(strings.ToLower(model), "character") {
		return 0.92
	}
	return 0.7
}

func (c *Client) Transcribe(ctx context.Context, audio []byte, filename, contentType string) (string, error) {
	if !c.Available() {
		return "", errors.New("dashscope api key is not configured")
	}
	if len(audio) == 0 {
		return "", errors.New("empty audio")
	}

	dataURL := "data:" + normalizeAudioMIME(filename, contentType) + ";base64," + base64.StdEncoding.EncodeToString(audio)
	payload := map[string]any{
		"model": c.sttModel,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_audio",
						"input_audio": map[string]string{
							"data": dataURL,
						},
					},
				},
			},
		},
		"stream": false,
		"asr_options": map[string]any{
			"language":   "zh",
			"enable_itn": false,
		},
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return "", err
	}

	req, err := c.newJSONRequest(ctx, "/compatible-mode/v1/chat/completions", &body)
	if err != nil {
		return "", err
	}

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
		return "", fmt.Errorf("dashscope transcriptions api returned %s: %s", resp.Status, string(data))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", errors.New("dashscope transcription returned no content")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func (c *Client) Speak(ctx context.Context, text string) ([]byte, string, error) {
	if !c.Available() {
		return nil, "", errors.New("dashscope api key is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", errors.New("empty speech text")
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(c.speechPayload(text, c.ttsFormat)); err != nil {
		return nil, "", err
	}

	req, err := c.newJSONRequest(ctx, "/api/v1/services/audio/tts/SpeechSynthesizer", &body)
	if err != nil {
		return nil, "", err
	}

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
		return nil, "", fmt.Errorf("dashscope speech api returned %s: %s", resp.Status, string(data))
	}

	var parsed speechResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, "", err
	}
	if parsed.Output.Audio.Data != "" {
		audio, err := base64.StdEncoding.DecodeString(parsed.Output.Audio.Data)
		if err != nil {
			return nil, "", err
		}
		return audio, audioMIME(c.ttsFormat), nil
	}
	if strings.TrimSpace(parsed.Output.Audio.URL) == "" {
		return nil, "", errors.New("dashscope speech returned no audio")
	}
	audio, err := c.downloadAudio(ctx, parsed.Output.Audio.URL)
	if err != nil {
		return nil, "", err
	}
	return audio, audioMIME(c.ttsFormat), nil
}

func (c *Client) StreamSpeak(ctx context.Context, text string, onChunk func([]byte) error) (string, int, error) {
	if !c.Available() {
		return "", 0, errors.New("dashscope api key is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", 0, errors.New("empty speech text")
	}
	if onChunk == nil {
		return "", 0, errors.New("speech stream callback is required")
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(c.speechPayload(text, "pcm")); err != nil {
		return "", 0, err
	}
	req, err := c.newJSONRequest(ctx, "/api/v1/services/audio/tts/SpeechSynthesizer", &body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("X-DashScope-SSE", "enable")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", 0, fmt.Errorf("dashscope streaming speech api returned %s: %s", resp.Status, string(data))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	chunks := 0
	finished := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "" || line == "[DONE]" {
			continue
		}
		var event streamSpeechResponse
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return "", 0, fmt.Errorf("decode dashscope speech stream: %w", err)
		}
		if event.Output.FinishReason == "stop" {
			finished = true
		}
		if event.Output.Audio.Data == "" {
			continue
		}
		audio, err := base64.StdEncoding.DecodeString(event.Output.Audio.Data)
		if err != nil {
			return "", 0, fmt.Errorf("decode dashscope speech audio: %w", err)
		}
		if len(audio) == 0 {
			continue
		}
		if err := onChunk(audio); err != nil {
			return "", 0, err
		}
		chunks++
	}
	if err := scanner.Err(); err != nil {
		return "", 0, err
	}
	if chunks == 0 {
		return "", 0, errors.New("dashscope streaming speech returned no audio")
	}
	if !finished {
		return "", 0, errors.New("dashscope streaming speech ended before completion")
	}
	return "audio/pcm", c.sampleRate, nil
}

func (c *Client) speechPayload(text, format string) map[string]any {
	input := map[string]any{
		"text":        text,
		"voice":       c.ttsVoice,
		"format":      format,
		"sample_rate": c.sampleRate,
		"rate":        c.ttsSpeed,
	}
	if c.ttsPrompt != "" {
		input["instruction"] = c.ttsPrompt
	}
	return map[string]any{
		"model": c.ttsModel,
		"input": input,
	}
}

func (c *Client) downloadAudio(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dashscope audio download returned %s: %s", resp.Status, string(data))
	}
	if len(data) == 0 {
		return nil, errors.New("dashscope audio download returned empty body")
	}
	return data, nil
}

func (c *Client) newJSONRequest(ctx context.Context, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type speechResponse struct {
	Output struct {
		Audio struct {
			Data string `json:"data"`
			URL  string `json:"url"`
		} `json:"audio"`
	} `json:"output"`
}

type streamSpeechResponse struct {
	Output struct {
		FinishReason string `json:"finish_reason"`
		Audio        struct {
			Data string `json:"data"`
		} `json:"audio"`
	} `json:"output"`
}

const (
	defaultTTSSpeed       = 0.88
	defaultTTSSpeedString = "0.88"
)

func normalizeAudioMIME(filename, contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType != "" {
		return canonicalAudioMIME(contentType)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".webm":
		return "audio/webm"
	}
	if guessed := mime.TypeByExtension(ext); guessed != "" {
		return canonicalAudioMIME(strings.Split(guessed, ";")[0])
	}
	return "audio/wav"
}

func canonicalAudioMIME(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "audio/x-wav":
		return "audio/wav"
	case "audio/mp3":
		return "audio/mpeg"
	default:
		return value
	}
}

func envAny(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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

func parseSpeechRate(value string) float64 {
	speed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || speed < 0.5 || speed > 2.0 {
		return defaultTTSSpeed
	}
	return speed
}

func parseSampleRate(value string) int {
	rate, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 24000
	}
	switch rate {
	case 8000, 16000, 22050, 24000, 44100, 48000:
		return rate
	default:
		return 24000
	}
}

func audioMIME(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "pcm":
		return "audio/L16"
	default:
		return "audio/mpeg"
	}
}
