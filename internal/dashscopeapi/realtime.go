package dashscopeapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultRealtimeSTTModel = "qwen3-asr-flash-realtime"
	defaultRealtimeSilence  = 650
	opusOutputFrameMS       = 60
)

// StreamTranscribeOpus wraps raw mono Opus packets in a streaming Ogg container
// for Qwen realtime ASR. The provider-side VAD calls onTranscript for each
// completed utterance.
func (c *Client) StreamTranscribeOpus(
	ctx context.Context,
	audio <-chan []byte,
	sampleRate int,
	frameDurationMS int,
	onTranscript func(text, emotion string) error,
) error {
	if !c.Available() {
		return errors.New("dashscope api key is not configured")
	}
	if onTranscript == nil {
		return errors.New("transcript callback is required")
	}
	if sampleRate != 8000 && sampleRate != 16000 {
		return fmt.Errorf("unsupported realtime Opus sample rate: %d", sampleRate)
	}
	if frameDurationMS <= 0 {
		return errors.New("realtime Opus frame duration is required")
	}

	endpoint, err := c.realtimeEndpoint()
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	headers.Set("OpenAI-Beta", "realtime=v1")
	dialer := *websocket.DefaultDialer
	if c.forceIPv4 || c.tcpMaxSegment > 0 {
		netDialer := dashscopeNetDialer(c.tcpMaxSegment)
		dialer.NetDialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
			if c.forceIPv4 {
				network = "tcp4"
			}
			return netDialer.DialContext(ctx, network, address)
		}
	}
	conn, _, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return fmt.Errorf("connect dashscope realtime ASR: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"event_id": realtimeEventID(),
		"type":     "session.update",
		"session": map[string]any{
			"modalities":         []string{"text"},
			"input_audio_format": "opus",
			"sample_rate":        sampleRate,
			"turn_detection": map[string]any{
				"type":                "server_vad",
				"threshold":           0.2,
				"silence_duration_ms": realtimeSilenceMS(),
			},
		},
	}); err != nil {
		return fmt.Errorf("configure dashscope realtime ASR: %w", err)
	}

	ogg := newOggOpusWriter(sampleRate, 1, frameDurationMS)
	for _, page := range ogg.Headers() {
		if err := appendRealtimeAudio(conn, page); err != nil {
			return err
		}
	}

	events := make(chan realtimeASREvent, 32)
	readErrors := make(chan error, 1)
	go readRealtimeEvents(conn, events, readErrors)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErrors:
			return err
		case event := <-events:
			switch event.Type {
			case "conversation.item.input_audio_transcription.completed":
				text := strings.TrimSpace(event.Transcript)
				if text == "" {
					text = strings.TrimSpace(event.Text)
				}
				if text != "" {
					if err := onTranscript(text, strings.TrimSpace(event.Emotion)); err != nil {
						return err
					}
				}
			case "error", "session.failed":
				if event.Error.Message == "" {
					event.Error.Message = event.Message
				}
				return fmt.Errorf("dashscope realtime ASR failed: %s", event.Error.Message)
			case "session.finished":
				return nil
			}
		case packet, ok := <-audio:
			if !ok {
				_ = conn.WriteJSON(map[string]any{
					"event_id": realtimeEventID(),
					"type":     "session.finish",
				})
				return nil
			}
			if len(packet) == 0 {
				continue
			}
			page, err := ogg.WritePacket(packet)
			if err != nil {
				return err
			}
			if err := appendRealtimeAudio(conn, page); err != nil {
				return err
			}
		}
	}
}

func appendRealtimeAudio(conn *websocket.Conn, data []byte) error {
	if err := conn.WriteJSON(map[string]any{
		"event_id": realtimeEventID(),
		"type":     "input_audio_buffer.append",
		"audio":    base64.StdEncoding.EncodeToString(data),
	}); err != nil {
		return fmt.Errorf("send audio to dashscope realtime ASR: %w", err)
	}
	return nil
}

func (c *Client) OpusOutputSampleRate() int {
	if c == nil || c.sampleRate <= 0 {
		return 24000
	}
	return c.sampleRate
}

func (c *Client) OpusOutputFrameDuration() int {
	return opusOutputFrameMS
}

// StreamSpeakOpus streams CosyVoice Ogg/Opus output as raw Opus packets that
// the Xiaozhi firmware can decode directly.
func (c *Client) StreamSpeakOpus(
	ctx context.Context,
	text string,
	onPacket func([]byte) error,
) error {
	if !c.Available() {
		return errors.New("dashscope api key is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty speech text")
	}
	if onPacket == nil {
		return errors.New("Opus packet callback is required")
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(c.speechPayload(text, "opus")); err != nil {
		return err
	}
	req, err := c.newJSONRequest(ctx, "/api/v1/services/audio/tts/SpeechSynthesizer", &body)
	if err != nil {
		return err
	}
	req.Header.Set("X-DashScope-SSE", "enable")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("dashscope streaming Opus speech api returned %s: %s", resp.Status, string(data))
	}

	parser := newOggPacketParser()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	packets := 0
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
			return fmt.Errorf("decode dashscope Opus speech stream: %w", err)
		}
		if event.Output.FinishReason == "stop" {
			finished = true
		}
		if event.Output.Audio.Data == "" {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(event.Output.Audio.Data)
		if err != nil {
			return fmt.Errorf("decode dashscope Opus audio: %w", err)
		}
		if err := parser.Feed(chunk, func(packet []byte) error {
			packets++
			return onPacket(packet)
		}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if packets == 0 {
		return errors.New("dashscope streaming Opus speech returned no audio")
	}
	if !finished {
		return errors.New("dashscope streaming Opus speech ended before completion")
	}
	return nil
}

type realtimeASREvent struct {
	Type       string `json:"type"`
	Transcript string `json:"transcript,omitempty"`
	Text       string `json:"text,omitempty"`
	Emotion    string `json:"emotion,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

func readRealtimeEvents(conn *websocket.Conn, events chan<- realtimeASREvent, errs chan<- error) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			errs <- fmt.Errorf("read dashscope realtime ASR: %w", err)
			return
		}
		var event realtimeASREvent
		if err := json.Unmarshal(payload, &event); err != nil {
			continue
		}
		events <- event
	}
}

func (c *Client) realtimeEndpoint() (string, error) {
	endpoint := strings.TrimSpace(os.Getenv("PUPBOX_DASHSCOPE_REALTIME_URL"))
	if endpoint == "" {
		base := strings.TrimRight(c.baseURL, "/")
		base = strings.TrimPrefix(base, "https://")
		base = strings.TrimPrefix(base, "http://")
		endpoint = "wss://" + base + "/api-ws/v1/realtime"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid dashscope realtime URL: %w", err)
	}
	query := parsed.Query()
	query.Set("model", envDefault("PUPBOX_DASHSCOPE_REALTIME_STT_MODEL", defaultRealtimeSTTModel))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func realtimeSilenceMS() int {
	value := strings.TrimSpace(os.Getenv("PUPBOX_XIAOZHI_SILENCE_MS"))
	if value == "" {
		return defaultRealtimeSilence
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 300 || parsed > 2000 {
		return defaultRealtimeSilence
	}
	return parsed
}

func realtimeEventID() string {
	return "event_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

type oggPacketParser struct {
	buffer []byte
	packet []byte
}

func newOggPacketParser() *oggPacketParser {
	return &oggPacketParser{}
}

func (p *oggPacketParser) Feed(chunk []byte, onPacket func([]byte) error) error {
	p.buffer = append(p.buffer, chunk...)
	for {
		if len(p.buffer) < 27 {
			return nil
		}
		if string(p.buffer[:4]) != "OggS" || p.buffer[4] != 0 {
			return errors.New("invalid Ogg/Opus stream")
		}
		segments := int(p.buffer[26])
		headerSize := 27 + segments
		if len(p.buffer) < headerSize {
			return nil
		}
		bodySize := 0
		for _, size := range p.buffer[27:headerSize] {
			bodySize += int(size)
		}
		pageSize := headerSize + bodySize
		if len(p.buffer) < pageSize {
			return nil
		}

		offset := headerSize
		for _, sizeByte := range p.buffer[27:headerSize] {
			size := int(sizeByte)
			p.packet = append(p.packet, p.buffer[offset:offset+size]...)
			offset += size
			if size < 255 {
				packet := append([]byte(nil), p.packet...)
				p.packet = p.packet[:0]
				if bytes.HasPrefix(packet, []byte("OpusHead")) {
					if len(packet) >= 16 {
						_ = int(binary.LittleEndian.Uint32(packet[12:16]))
					}
				} else if !bytes.HasPrefix(packet, []byte("OpusTags")) && len(packet) > 0 {
					if err := onPacket(packet); err != nil {
						return err
					}
				}
			}
		}
		p.buffer = append(p.buffer[:0], p.buffer[pageSize:]...)
	}
}
