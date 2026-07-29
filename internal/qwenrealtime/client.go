package qwenrealtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	DefaultModel = "qwen-audio-3.0-realtime-flash"
	DefaultVoice = "longanqian"
)

type Config struct {
	APIKey       string
	URL          string
	Model        string
	Voice        string
	Instructions string
	Dialer       *websocket.Dialer
}

type Client struct {
	config Config
}

type Session struct {
	conn *websocket.Conn
}

type TurnResult struct {
	Transcript   string `json:"transcript"`
	Reply        string `json:"reply"`
	FirstAudioMS int64  `json:"first_audio_ms"`
	TotalMS      int64  `json:"total_ms"`
	AudioBytes   int64  `json:"audio_bytes"`
}

func New(config Config) *Client {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.URL = strings.TrimSpace(config.URL)
	config.Model = defaultValue(config.Model, DefaultModel)
	config.Voice = defaultValue(config.Voice, DefaultVoice)
	return &Client{config: config}
}

func (c *Client) Connect(ctx context.Context) (*Session, error) {
	if c == nil || c.config.APIKey == "" {
		return nil, errors.New("DashScope API key is not configured")
	}
	endpoint, err := realtimeURL(c.config.URL, c.config.Model)
	if err != nil {
		return nil, err
	}
	dialer := c.config.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.config.APIKey)
	conn, _, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return nil, fmt.Errorf("connect Qwen-Audio Realtime: %w", err)
	}
	session := &Session{conn: conn}
	if err := session.configure(ctx, c.config); err != nil {
		conn.Close()
		return nil, err
	}
	return session, nil
}

func (s *Session) configure(ctx context.Context, config Config) error {
	if err := s.conn.WriteJSON(map[string]any{
		"event_id": eventID(),
		"type":     "session.update",
		"session": map[string]any{
			"modalities":          []string{"text", "audio"},
			"voice":               config.Voice,
			"instructions":        config.Instructions,
			"input_audio_format":  "pcm",
			"output_audio_format": "pcm",
			"turn_detection":      nil,
		},
	}); err != nil {
		return fmt.Errorf("configure Qwen-Audio Realtime: %w", err)
	}
	for {
		event, err := s.readEvent(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case "session.updated":
			return nil
		case "error", "session.failed":
			return eventError(event)
		}
	}
}

func (s *Session) RunTurn(ctx context.Context, pcm []byte) (TurnResult, error) {
	if s == nil || s.conn == nil {
		return TurnResult{}, errors.New("Qwen-Audio Realtime session is not connected")
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return TurnResult{}, errors.New("PCM input must contain 16-bit samples")
	}
	const chunkBytes = 3200
	for offset := 0; offset < len(pcm); offset += chunkBytes {
		end := min(offset+chunkBytes, len(pcm))
		if err := s.conn.WriteJSON(map[string]any{
			"event_id": eventID(),
			"type":     "input_audio_buffer.append",
			"audio":    base64.StdEncoding.EncodeToString(pcm[offset:end]),
		}); err != nil {
			return TurnResult{}, fmt.Errorf("send Qwen-Audio input: %w", err)
		}
	}
	started := time.Now()
	for _, eventType := range []string{"input_audio_buffer.commit", "response.create"} {
		if err := s.conn.WriteJSON(map[string]any{"event_id": eventID(), "type": eventType}); err != nil {
			return TurnResult{}, fmt.Errorf("start Qwen-Audio response: %w", err)
		}
	}

	result := TurnResult{}
	for {
		event, err := s.readEvent(ctx)
		if err != nil {
			return result, err
		}
		switch event.Type {
		case "conversation.item.input_audio_transcription.completed":
			result.Transcript = strings.TrimSpace(event.Transcript)
		case "response.audio_transcript.delta":
			result.Reply += event.Delta
		case "response.audio_transcript.done":
			if strings.TrimSpace(event.Transcript) != "" {
				result.Reply = event.Transcript
			}
		case "response.audio.delta":
			if result.FirstAudioMS == 0 {
				result.FirstAudioMS = time.Since(started).Milliseconds()
			}
			audio, err := base64.StdEncoding.DecodeString(event.Delta)
			if err != nil {
				return result, fmt.Errorf("decode Qwen-Audio output: %w", err)
			}
			result.AudioBytes += int64(len(audio))
		case "response.done":
			result.Reply = strings.TrimSpace(result.Reply)
			result.TotalMS = time.Since(started).Milliseconds()
			if result.Reply == "" {
				return result, errors.New("Qwen-Audio Realtime returned no reply transcript")
			}
			return result, nil
		case "error", "session.failed", "response.failed":
			return result, eventError(event)
		}
	}
}

func (s *Session) readEvent(ctx context.Context) (serverEvent, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetReadDeadline(deadline)
	} else {
		_ = s.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	}
	_, payload, err := s.conn.ReadMessage()
	if err != nil {
		return serverEvent{}, fmt.Errorf("read Qwen-Audio event: %w", err)
	}
	var event serverEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return serverEvent{}, fmt.Errorf("decode Qwen-Audio event: %w", err)
	}
	return event, nil
}

func (s *Session) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

type serverEvent struct {
	Type       string `json:"type"`
	Delta      string `json:"delta,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

func eventError(event serverEvent) error {
	message := strings.TrimSpace(event.Error.Message)
	if message == "" {
		message = strings.TrimSpace(event.Message)
	}
	if message == "" {
		message = event.Type
	}
	return fmt.Errorf("Qwen-Audio Realtime failed: %s", message)
}

func realtimeURL(base, model string) (string, error) {
	if base == "" {
		base = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" || parsed.Host == "" {
		return "", errors.New("Qwen-Audio Realtime URL must use ws or wss")
	}
	query := parsed.Query()
	query.Set("model", defaultValue(model, DefaultModel))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func eventID() string {
	return "event_" + strconvFormatInt(time.Now().UnixNano())
}

func strconvFormatInt(value int64) string {
	return fmt.Sprintf("%x", value)
}
