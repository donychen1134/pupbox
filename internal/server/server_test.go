package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
		{name: "speech stream", method: http.MethodPost, path: "/api/speech-stream", body: `{"text":"汪。"}`},
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

func TestSpeechCachesTTS(t *testing.T) {
	voice := &countingVoiceProvider{}
	srv := New(Config{Voice: voice})

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/speech", strings.NewReader(`{"text":"汪，豆豆在这里。"}`))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("speech status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	if calls := voice.speakCalls.Load(); calls != 1 {
		t.Fatalf("speakCalls = %d, want 1", calls)
	}
}

func TestSpeechStreamEmitsChunksAndPersistsCache(t *testing.T) {
	dir := t.TempDir()
	voice := &testStreamingVoiceProvider{chunks: [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}}}
	first := New(Config{Voice: voice, SpeechCacheDir: dir})
	firstEvents := requestSpeechStream(t, first, "豆豆流式说话")
	if len(firstEvents) != 3 || firstEvents[0].Type != "audio" || firstEvents[1].Type != "audio" || firstEvents[2].Type != "done" {
		t.Fatalf("first events = %#v", firstEvents)
	}
	if firstEvents[2].Cache != "miss" || firstEvents[2].Timings == nil {
		t.Fatalf("first done event = %#v", firstEvents[2])
	}
	if calls := voice.streamCalls.Load(); calls != 1 {
		t.Fatalf("stream calls = %d, want 1", calls)
	}

	restartedVoice := &testStreamingVoiceProvider{chunks: voice.chunks}
	restarted := New(Config{Voice: restartedVoice, SpeechCacheDir: dir})
	cachedEvents := requestSpeechStream(t, restarted, "豆豆流式说话")
	if len(cachedEvents) != 2 || cachedEvents[0].Type != "audio" || cachedEvents[1].Type != "done" {
		t.Fatalf("cached events = %#v", cachedEvents)
	}
	if cachedEvents[0].Cache != "stream" || cachedEvents[1].Cache != "stream" {
		t.Fatalf("cached event sources = %q, %q", cachedEvents[0].Cache, cachedEvents[1].Cache)
	}
	if calls := restartedVoice.streamCalls.Load(); calls != 0 {
		t.Fatalf("stream calls after restart = %d, want 0", calls)
	}
	want := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	got, err := base64.StdEncoding.DecodeString(cachedEvents[0].AudioBase64)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("cached audio = %v err=%v, want %v", got, err, want)
	}
}

func TestSpeechStreamFallsBackForNonStreamingProvider(t *testing.T) {
	voice := &countingVoiceProvider{}
	srv := New(Config{Voice: voice})
	events := requestSpeechStream(t, srv, "豆豆回退说话")
	if len(events) != 2 || events[0].Type != "audio" || events[1].Type != "done" {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Cache != "fallback" || events[1].Cache != "fallback" {
		t.Fatalf("fallback sources = %q, %q", events[0].Cache, events[1].Cache)
	}
	if calls := voice.speakCalls.Load(); calls != 1 {
		t.Fatalf("speak calls = %d, want 1", calls)
	}
}

func requestSpeechStream(t *testing.T, srv *Server, text string) []speechStreamEvent {
	t.Helper()
	body, err := json.Marshal(speechRequest{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/speech-stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("speech stream status = %d; body=%s", rec.Code, rec.Body.String())
	}
	decoder := json.NewDecoder(rec.Body)
	var events []speechStreamEvent
	for {
		var event speechStreamEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode stream event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

type testStreamingVoiceProvider struct {
	streamCalls atomic.Int32
	chunks      [][]byte
}

func (p *testStreamingVoiceProvider) Available() bool   { return true }
func (p *testStreamingVoiceProvider) Name() string      { return "streaming-voice" }
func (p *testStreamingVoiceProvider) STTModel() string  { return "test-stt" }
func (p *testStreamingVoiceProvider) TTSModel() string  { return "test-tts" }
func (p *testStreamingVoiceProvider) TTSVoice() string  { return "test-speaker" }
func (p *testStreamingVoiceProvider) TTSFormat() string { return "mp3" }
func (p *testStreamingVoiceProvider) TTSSpeed() float64 { return 1 }
func (p *testStreamingVoiceProvider) StreamSampleRate() int {
	return 24000
}
func (p *testStreamingVoiceProvider) Transcribe(context.Context, []byte, string, string) (string, error) {
	return "嗯嗯", nil
}
func (p *testStreamingVoiceProvider) Speak(_ context.Context, text string) ([]byte, string, error) {
	return []byte("complete:" + text), "audio/mpeg", nil
}
func (p *testStreamingVoiceProvider) StreamSpeak(_ context.Context, _ string, onChunk func([]byte) error) (string, int, error) {
	p.streamCalls.Add(1)
	for _, chunk := range p.chunks {
		if err := onChunk(chunk); err != nil {
			return "", 0, err
		}
	}
	return "audio/pcm", 24000, nil
}

type countingVoiceProvider struct {
	speakCalls atomic.Int32
}

func (p *countingVoiceProvider) Available() bool   { return true }
func (p *countingVoiceProvider) Name() string      { return "test-voice" }
func (p *countingVoiceProvider) STTModel() string  { return "test-stt" }
func (p *countingVoiceProvider) TTSModel() string  { return "test-tts" }
func (p *countingVoiceProvider) TTSVoice() string  { return "test-speaker" }
func (p *countingVoiceProvider) TTSFormat() string { return "wav" }
func (p *countingVoiceProvider) TTSSpeed() float64 { return 1 }
func (p *countingVoiceProvider) Transcribe(context.Context, []byte, string, string) (string, error) {
	return "嗯嗯", nil
}
func (p *countingVoiceProvider) Speak(_ context.Context, text string) ([]byte, string, error) {
	p.speakCalls.Add(1)
	return []byte("audio:" + text), "audio/wav", nil
}

func TestSpeechCoalescesConcurrentTTS(t *testing.T) {
	voice := &blockingVoiceProvider{started: make(chan struct{}), release: make(chan struct{})}
	srv := New(Config{Voice: voice})

	request := func(done chan<- struct{}) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/speech", strings.NewReader(`{"text":"汪，等等豆豆。"}`))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("speech status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		done <- struct{}{}
	}

	done := make(chan struct{}, 2)
	go request(done)
	<-voice.started
	go request(done)
	time.Sleep(20 * time.Millisecond)
	close(voice.release)
	<-done
	<-done
	if calls := voice.calls.Load(); calls != 1 {
		t.Fatalf("Speak calls = %d, want 1", calls)
	}
}

type blockingVoiceProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *blockingVoiceProvider) Available() bool   { return true }
func (p *blockingVoiceProvider) Name() string      { return "blocking-voice" }
func (p *blockingVoiceProvider) STTModel() string  { return "test-stt" }
func (p *blockingVoiceProvider) TTSModel() string  { return "test-tts" }
func (p *blockingVoiceProvider) TTSVoice() string  { return "test-speaker" }
func (p *blockingVoiceProvider) TTSFormat() string { return "wav" }
func (p *blockingVoiceProvider) TTSSpeed() float64 { return 1 }
func (p *blockingVoiceProvider) Transcribe(context.Context, []byte, string, string) (string, error) {
	return "嗯嗯", nil
}
func (p *blockingVoiceProvider) Speak(_ context.Context, text string) ([]byte, string, error) {
	if p.calls.Add(1) == 1 {
		close(p.started)
	}
	<-p.release
	return []byte("audio:" + text), "audio/wav", nil
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

func TestEventStoreRetainsConfiguredLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store := NewEventStore(path, 2)
	if err := store.Ensure(); err != nil {
		t.Fatalf("ensure event store: %v", err)
	}
	for _, transcript := range []string{"one", "two", "three"} {
		if err := store.Append(ConversationEvent{Transcript: transcript}); err != nil {
			t.Fatalf("append %q: %v", transcript, err)
		}
	}
	events, err := store.Recent(10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 2 || events[0].Transcript != "three" || events[1].Transcript != "two" {
		t.Fatalf("events = %+v, want newest two", events)
	}
	gotPath, ready, count, statusErr := store.Status()
	if gotPath != path || !ready || count != 2 || statusErr != nil {
		t.Fatalf("status = (%q, %v, %d, %v)", gotPath, ready, count, statusErr)
	}
}

func TestChatUsesRecentSessionContext(t *testing.T) {
	chat := &capturingChatProvider{}
	srv := New(Config{Chat: chat})

	for _, text := range []string{"云朵在哪里", "为什么呢"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"`+text+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(sessionHeader, "session-1234")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("chat status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	inputs := chat.Inputs()
	if len(inputs) != 2 {
		t.Fatalf("inputs = %d, want 2", len(inputs))
	}
	if strings.Contains(inputs[0], "最近的对话") {
		t.Fatalf("first input unexpectedly has history: %q", inputs[0])
	}
	if !strings.Contains(inputs[1], "小朋友：云朵在哪里") || !strings.Contains(inputs[1], "豆豆：测试回复 1") {
		t.Fatalf("second input missing history: %q", inputs[1])
	}
}

func TestChatRoutesNaturalConversationToModel(t *testing.T) {
	chat := &capturingChatProvider{}
	srv := New(Config{Chat: chat})

	for _, text := range []string{
		"豆豆，你今天开心吗",
		"我想玩积木",
		"我画了一辆红色汽车",
		"妈妈今天回家了",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"`+text+`"}`))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("chat status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var response chatResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.Source != "test-chat" || response.Activity != nil {
			t.Errorf("chat %q routed to source=%q activity=%#v, want test-chat", text, response.Source, response.Activity)
		}
	}

	if inputs := chat.Inputs(); len(inputs) != 4 {
		t.Fatalf("model inputs = %d, want 4", len(inputs))
	}
}

func TestChatKeepsExplicitActivityOutOfModel(t *testing.T) {
	chat := &capturingChatProvider{}
	srv := New(Config{Chat: chat})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"豆豆，讲个故事"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	var response chatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Source != "activity:story" || response.Activity == nil || response.Activity.ID != "story" {
		t.Fatalf("source=%q activity=%#v, want story activity", response.Source, response.Activity)
	}
	if inputs := chat.Inputs(); len(inputs) != 0 {
		t.Fatalf("model inputs = %d, want 0", len(inputs))
	}
}

type capturingChatProvider struct {
	mu     sync.Mutex
	inputs []string
}

func (p *capturingChatProvider) Available() bool   { return true }
func (p *capturingChatProvider) Name() string      { return "test-chat" }
func (p *capturingChatProvider) ChatModel() string { return "test-model" }
func (p *capturingChatProvider) CreateResponse(_ context.Context, _, input string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inputs = append(p.inputs, input)
	return fmt.Sprintf("测试回复 %d", len(p.inputs)), nil
}
func (p *capturingChatProvider) Inputs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.inputs...)
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
