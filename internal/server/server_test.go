package server

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessTokenDisabledByDefault(t *testing.T) {
	srv := New(Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAccessTokenProtectsAPIs(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "health", method: http.MethodGet, path: "/api/health"},
		{name: "activities", method: http.MethodGet, path: "/api/activities"},
		{name: "events", method: http.MethodGet, path: "/api/events"},
		{name: "chat", method: http.MethodPost, path: "/api/chat", body: `{"text":"嗯嗯"}`},
		{name: "speech", method: http.MethodPost, path: "/api/speech", body: `{"text":"汪。"}`},
		{name: "voice", method: http.MethodPost, path: "/api/voice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestAccessTokenAcceptsBearerHeader(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"豆豆讲故事"}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response chatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Source != "activity:story" {
		t.Fatalf("source = %q, want activity:story", response.Source)
	}
}

func TestAccessTokenAcceptsQueryToken(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health?token=secret-token", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["auth_required"] != true {
		t.Fatalf("auth_required = %v, want true", response["auth_required"])
	}
}

func TestEventLogRecordsChatAndReturnsRecentEvents(t *testing.T) {
	eventLogPath := filepath.Join(t.TempDir(), "events.jsonl")
	srv := New(Config{EventLogPath: eventLogPath})

	for _, text := range []string{"豆豆讲故事", "我想玩插座"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"`+text+`"}`))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("chat status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events?limit=1", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Events) != 1 {
		t.Fatalf("events len = %d, want 1", len(response.Events))
	}
	event := response.Events[0]
	if event.Transcript != "我想玩插座" {
		t.Fatalf("transcript = %q, want latest event", event.Transcript)
	}
	if event.Source != "safety" {
		t.Fatalf("source = %q, want safety", event.Source)
	}
	if !event.SafetyTriggered || event.SafetyCategory != "danger" {
		t.Fatalf("safety = (%v, %q), want danger trigger", event.SafetyTriggered, event.SafetyCategory)
	}
	if event.TraceID == "" || event.Time == "" {
		t.Fatalf("trace/time missing: %+v", event)
	}
}

func TestAudioDurationMSWAV(t *testing.T) {
	data := testWAV(16_000, 1, 16, 32_000)
	durationMS, ok := audioDurationMS(data, "recording.wav", "audio/wav")
	if !ok {
		t.Fatal("expected WAV duration to be detected")
	}
	if durationMS != 1000 {
		t.Fatalf("durationMS = %d, want 1000", durationMS)
	}
	if isTooShortAudio(data, "recording.wav", "audio/wav") {
		t.Fatal("did not expect 1s WAV to be treated as too short")
	}
}

func TestIsTooShortAudioForTinyWAV(t *testing.T) {
	data := testWAV(16_000, 1, 16, 1_600)
	if !isTooShortAudio(data, "recording.wav", "audio/wav") {
		t.Fatal("expected tiny WAV to be treated as too short")
	}
}

func testWAV(sampleRate uint32, channels, bitsPerSample uint16, dataBytes uint32) []byte {
	data := make([]byte, 44+dataBytes)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 36+dataBytes)
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], channels)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	bytesPerSecond := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	binary.LittleEndian.PutUint32(data[28:32], bytesPerSecond)
	blockAlign := channels * bitsPerSample / 8
	binary.LittleEndian.PutUint16(data[32:34], blockAlign)
	binary.LittleEndian.PutUint16(data[34:36], bitsPerSample)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], dataBytes)
	return data
}
