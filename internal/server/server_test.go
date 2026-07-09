package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{name: "recordings", method: http.MethodGet, path: "/api/recordings/abcd"},
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
	stats, ok := audioStats(data, "recording.wav", "audio/wav")
	if !ok {
		t.Fatal("expected WAV stats")
	}
	if stats.Peak != 0 || stats.RMS != 0 {
		t.Fatalf("silent WAV stats = %+v, want zero levels", stats)
	}
}

func TestIsTooShortAudioForTinyWAV(t *testing.T) {
	data := testWAV(16_000, 1, 16, 1_600)
	if !isTooShortAudio(data, "recording.wav", "audio/wav") {
		t.Fatal("expected tiny WAV to be treated as too short")
	}
}

func TestRecordingStoreSaveFindAndPrune(t *testing.T) {
	dir := t.TempDir()
	store := NewRecordingStore(dir, 1)
	if _, err := store.Save("abcd-1", []byte("first"), "recording.wav", "audio/wav"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.Save("abcd-2", []byte("second"), "recording.wav", "audio/wav"); err != nil {
		t.Fatalf("save second: %v", err)
	}
	if _, _, err := store.Find("abcd-1"); !os.IsNotExist(err) {
		t.Fatalf("old recording err = %v, want not exist", err)
	}
	path, mimeType, err := store.Find("abcd-2")
	if err != nil {
		t.Fatalf("find second: %v", err)
	}
	if mimeType != "audio/wav" {
		t.Fatalf("mime = %q, want audio/wav", mimeType)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("recording data = %q, want second", string(data))
	}
}

func TestRecordingEndpointServesSavedAudio(t *testing.T) {
	dir := t.TempDir()
	srv := New(Config{RecordingDir: dir})
	if _, err := srv.recordings.Save("abcd-1", []byte("audio"), "recording.wav", "audio/wav"); err != nil {
		t.Fatalf("save recording: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/abcd-1", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "audio" {
		t.Fatalf("body = %q, want audio", rec.Body.String())
	}
}

func TestVoiceEventMarksDiagnosticRecording(t *testing.T) {
	eventLogPath := filepath.Join(t.TempDir(), "events.jsonl")
	recordingDir := t.TempDir()
	srv := New(Config{EventLogPath: eventLogPath, RecordingDir: recordingDir})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("audio", "recording.wav")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := file.Write(testWAV(16_000, 1, 16, 16_000)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/voice?tts=off", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("voice status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	events, err := srv.events.Recent(1)
	if err != nil {
		t.Fatalf("events recent: %v", err)
	}
	if len(events) != 1 || !events[0].HasRecording {
		t.Fatalf("event recording marker missing: %+v", events)
	}
	if events[0].Timings.AudioDurationMS == 0 {
		t.Fatalf("event duration missing: %+v", events[0].Timings)
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
