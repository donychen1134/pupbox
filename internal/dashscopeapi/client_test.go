package dashscopeapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	client := New(Config{})

	if got, want := client.Name(), "dashscope"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
	if got, want := client.ChatModel(), "qwen-plus-character"; got != want {
		t.Fatalf("ChatModel() = %q, want %q", got, want)
	}
	if got, want := client.STTModel(), "qwen3-asr-flash"; got != want {
		t.Fatalf("STTModel() = %q, want %q", got, want)
	}
	if got, want := client.TTSModel(), "cosyvoice-v3-flash"; got != want {
		t.Fatalf("TTSModel() = %q, want %q", got, want)
	}
	if got, want := client.TTSVoice(), "longhuhu_v3"; got != want {
		t.Fatalf("TTSVoice() = %q, want %q", got, want)
	}
	if got, want := client.TTSFormat(), "mp3"; got != want {
		t.Fatalf("TTSFormat() = %q, want %q", got, want)
	}
	if got, want := client.TTSSpeed(), 0.88; got != want {
		t.Fatalf("TTSSpeed() = %v, want %v", got, want)
	}
	if got, want := client.ttsPrompt, ""; got != want {
		t.Fatalf("ttsPrompt = %q, want empty", got)
	}
}

func TestCharacterModelUsesRolePlayTemperature(t *testing.T) {
	if got, want := chatTemperature("qwen-plus-character"), 0.92; got != want {
		t.Fatalf("temperature = %v, want %v", got, want)
	}
	if got, want := chatTemperature("qwen-turbo"), 0.7; got != want {
		t.Fatalf("fallback temperature = %v, want %v", got, want)
	}
}

func TestParseSpeechRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want float64
	}{
		{name: "custom", in: "0.75", want: 0.75},
		{name: "minimum", in: "0.5", want: 0.5},
		{name: "maximum", in: "2.0", want: 2.0},
		{name: "too slow", in: "0.25", want: defaultTTSSpeed},
		{name: "too fast", in: "2.1", want: defaultTTSSpeed},
		{name: "invalid", in: "slow", want: defaultTTSSpeed},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseSpeechRate(tt.in); got != tt.want {
				t.Fatalf("parseSpeechRate(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSampleRate(t *testing.T) {
	t.Parallel()

	if got, want := parseSampleRate("16000"), 16000; got != want {
		t.Fatalf("parseSampleRate(16000) = %v, want %v", got, want)
	}
	if got, want := parseSampleRate("12345"), 24000; got != want {
		t.Fatalf("parseSampleRate(12345) = %v, want %v", got, want)
	}
}

func TestNormalizeAudioMIME(t *testing.T) {
	t.Parallel()

	if got, want := normalizeAudioMIME("recording.wav", ""), "audio/wav"; got != want {
		t.Fatalf("normalizeAudioMIME(wav) = %q, want %q", got, want)
	}
	if got, want := normalizeAudioMIME("recording.bin", "audio/webm;codecs=opus"), "audio/webm"; got != want {
		t.Fatalf("normalizeAudioMIME(content type) = %q, want %q", got, want)
	}
}

func TestStreamSpeakParsesSSEAudioChunks(t *testing.T) {
	t.Parallel()

	chunks := [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/audio/tts/SpeechSynthesizer" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-DashScope-SSE") != "enable" {
			t.Errorf("SSE header = %q", r.Header.Get("X-DashScope-SSE"))
		}
		var payload struct {
			Input struct {
				Format string `json:"format"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Input.Format != "pcm" {
			t.Errorf("format = %q, want pcm", payload.Input.Format)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("event: result\n"))
			_, _ = w.Write([]byte(`data: {"output":{"type":"sentence-synthesis","audio":{"data":"` + base64.StdEncoding.EncodeToString(chunk) + `"}}}` + "\n\n"))
		}
		_, _ = w.Write([]byte(`data: {"output":{"finish_reason":"stop","audio":{"data":""}}}` + "\n\n"))
	}))
	defer upstream.Close()

	client := New(Config{APIKey: "test-key", BaseURL: upstream.URL, SampleRate: "24000"})
	var got bytes.Buffer
	mime, sampleRate, err := client.StreamSpeak(context.Background(), "豆豆说话", func(chunk []byte) error {
		_, writeErr := got.Write(chunk)
		return writeErr
	})
	if err != nil {
		t.Fatalf("StreamSpeak: %v", err)
	}
	if mime != "audio/pcm" || sampleRate != 24000 {
		t.Fatalf("stream format = (%q, %d)", mime, sampleRate)
	}
	if want := bytes.Join(chunks, nil); !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("audio = %v, want %v", got.Bytes(), want)
	}
}

func TestStreamSpeakRejectsTruncatedSSE(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
		_, _ = w.Write([]byte(`data: {"output":{"type":"sentence-synthesis","audio":{"data":"` + chunk + `"}}}` + "\n\n"))
	}))
	defer upstream.Close()

	client := New(Config{APIKey: "test-key", BaseURL: upstream.URL, SampleRate: "24000"})
	var got bytes.Buffer
	_, _, err := client.StreamSpeak(context.Background(), "豆豆说话", func(chunk []byte) error {
		_, writeErr := got.Write(chunk)
		return writeErr
	})
	if err == nil || !strings.Contains(err.Error(), "ended before completion") {
		t.Fatalf("StreamSpeak error = %v, want incomplete stream error", err)
	}
	if got.Len() == 0 {
		t.Fatal("expected the partial chunk to reach the playback callback")
	}
}
