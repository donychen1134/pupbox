package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
)

type Server struct {
	mux         *http.ServeMux
	chat        ChatProvider
	voice       VoiceProvider
	useChat     bool
	useVoice    bool
	staticDir   string
	accessToken string
	events      *EventStore
	recordings  *RecordingStore
	speechMu    sync.Mutex
	speechCache map[string]cachedSpeech
	traceSeq    atomic.Uint64
	log         *slog.Logger
}

type cachedSpeech struct {
	audio []byte
	mime  string
}

type ChatProvider interface {
	Available() bool
	Name() string
	ChatModel() string
	CreateResponse(ctx context.Context, instructions, input string) (string, error)
}

type VoiceProvider interface {
	Available() bool
	Name() string
	STTModel() string
	TTSModel() string
	TTSVoice() string
	TTSFormat() string
	TTSSpeed() float64
	Transcribe(ctx context.Context, audio []byte, filename, contentType string) (string, error)
	Speak(ctx context.Context, text string) ([]byte, string, error)
}

type Config struct {
	Chat           ChatProvider
	Voice          VoiceProvider
	StaticDir      string
	AccessToken    string
	EventLogPath   string
	RecordingDir   string
	RecordingLimit int
	Logger         *slog.Logger
}

func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	forceMock := strings.EqualFold(os.Getenv("PUPBOX_MODE"), "mock")
	voice := cfg.Voice
	if voice == nil {
		if provider, ok := cfg.Chat.(VoiceProvider); ok {
			voice = provider
		}
	}
	s := &Server{
		mux:         http.NewServeMux(),
		chat:        cfg.Chat,
		voice:       voice,
		useChat:     chatEnabled(cfg.Chat, forceMock),
		useVoice:    voice != nil && voice.Available() && !forceMock,
		staticDir:   cfg.StaticDir,
		accessToken: strings.TrimSpace(cfg.AccessToken),
		events:      NewEventStore(cfg.EventLogPath),
		recordings:  NewRecordingStore(cfg.RecordingDir, cfg.RecordingLimit),
		speechCache: make(map[string]cachedSpeech),
		log:         logger,
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.requireAccess(s.handleHealth))
	s.mux.HandleFunc("GET /api/activities", s.requireAccess(s.handleActivities))
	s.mux.HandleFunc("GET /api/events", s.requireAccess(s.handleEvents))
	s.mux.HandleFunc("GET /api/recordings/", s.requireAccess(s.handleRecording))
	s.mux.HandleFunc("POST /api/chat", s.requireAccess(s.handleChat))
	s.mux.HandleFunc("POST /api/speech", s.requireAccess(s.handleSpeech))
	s.mux.HandleFunc("POST /api/voice", s.requireAccess(s.handleVoice))
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) requireAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.accessToken == "" || s.validAccessToken(r) {
			next(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="pupbox"`)
		writeError(w, http.StatusUnauthorized, "access token required")
	}
}

func (s *Server) validAccessToken(r *http.Request) bool {
	token := requestAccessToken(r)
	if token == "" {
		return false
	}
	want := sha256.Sum256([]byte(s.accessToken))
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

func requestAccessToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if scheme, value, ok := strings.Cut(auth, " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(value)
	}
	if token := strings.TrimSpace(r.Header.Get("X-Pupbox-Access-Token")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"auth_required":  s.accessToken != "",
		"event_log":      s.events != nil,
		"recordings":     s.recordings != nil,
		"mode":           s.mode(),
		"dog":            dog.Name,
		"chat_provider":  s.chatProvider(),
		"voice_provider": s.voiceProvider(),
		"chat_model":     s.modelName("chat"),
		"stt_model":      s.modelName("stt"),
		"tts_model":      s.modelName("tts"),
		"tts_voice":      s.modelName("voice"),
		"tts_format":     s.modelName("format"),
		"tts_speed":      s.ttsSpeed(),
		"server_time":    time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := parseEventLimit(r.URL.Query().Get("limit"))
	if s.events == nil {
		writeJSON(w, http.StatusOK, eventsResponse{Events: []ConversationEvent{}})
		return
	}
	events, err := s.events.Recent(limit)
	if err != nil {
		s.log.Warn("failed to read event log", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read event log")
		return
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: events})
}

func (s *Server) handleRecording(w http.ResponseWriter, r *http.Request) {
	traceID := strings.TrimPrefix(r.URL.Path, "/api/recordings/")
	traceID = strings.TrimSpace(traceID)
	path, mimeType, err := s.recordings.Find(traceID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

func (s *Server) handleActivities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"activities": dog.Activities(),
	})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	timings := TimingStats{}
	replyStarted := time.Now()
	reply, safety, activity, source, aiErr := s.reply(r.Context(), text)
	timings.ReplyMS = elapsedMS(replyStarted)
	ttsStarted := time.Now()
	audio, mime, ttsErr := s.speak(r, reply)
	timings.TTSMS = elapsedMS(ttsStarted)
	timings.TotalMS = elapsedMS(started)
	response := chatResponse{
		Transcript:  text,
		Reply:       reply,
		AudioBase64: encodeAudio(audio),
		AudioMIME:   mime,
		Safety:      safety,
		Activity:    activity,
		Mode:        s.mode(),
		Source:      source,
		AIError:     errorString(aiErr),
		TTSError:    errorString(ttsErr),
		Timings:     timings,
	}
	s.recordConversation("chat", response, nil, eventErrors{Chat: errorString(aiErr), TTS: errorString(ttsErr)})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSpeech(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var req speechRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if !s.useVoice {
		writeJSON(w, http.StatusOK, speechResponse{
			Mode:     s.mode(),
			TTSError: "server voice mode is not enabled",
			Timings:  TimingStats{TotalMS: elapsedMS(started)},
		})
		return
	}

	ttsStarted := time.Now()
	audio, mime, err := s.synthesizeSpeech(r.Context(), dog.ClampReply(text, 90))
	timings := TimingStats{TTSMS: elapsedMS(ttsStarted), TotalMS: elapsedMS(started)}
	writeJSON(w, http.StatusOK, speechResponse{
		AudioBase64: encodeAudio(audio),
		AudioMIME:   mime,
		Mode:        s.mode(),
		TTSError:    errorString(err),
		Timings:     timings,
	})
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	traceID := s.nextTraceID()
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart audio upload")
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "audio file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read audio")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "audio file is empty")
		return
	}

	filename := header.Filename
	contentType := header.Header.Get("Content-Type")
	timings := TimingStats{AudioBytes: int64(len(data))}
	if stats, ok := audioStats(data, filename, contentType); ok {
		timings.AudioDurationMS = stats.DurationMS
		timings.AudioPeak = stats.Peak
		timings.AudioRMS = stats.RMS
	}
	recording, recordingErr := s.recordings.Save(traceID, data, filename, contentType)
	if recordingErr != nil {
		s.log.Warn("failed to save diagnostic recording", "error", recordingErr)
	}
	if isTooShortAudio(data, filename, contentType) {
		timings.TotalMS = elapsedMS(started)
		response := chatResponse{
			TraceID:    traceID,
			Transcript: "",
			Reply:      "豆豆刚才没有听清楚。你可以再说一遍吗？",
			Mode:       s.mode(),
			Source:     "stt_short_audio",
			Timings:    timings,
		}
		s.recordConversation("voice", response, recording, eventErrors{Recording: errorString(recordingErr)})
		writeJSON(w, http.StatusOK, response)
		return
	}

	transcript := "我想听一个小狗故事"
	var sttErr error
	if s.useVoice {
		sttStarted := time.Now()
		transcript, sttErr = s.voice.Transcribe(r.Context(), data, filename, contentType)
		timings.STTMS = elapsedMS(sttStarted)
		if sttErr != nil {
			timings.TotalMS = elapsedMS(started)
			response := chatResponse{
				TraceID:    traceID,
				Transcript: "",
				Reply:      "豆豆刚才没有听清楚。你可以再说一遍吗？",
				Mode:       s.mode(),
				Source:     "stt_error",
				AIError:    sttErr.Error(),
				Timings:    timings,
			}
			s.recordConversation("voice", response, recording, eventErrors{STT: sttErr.Error(), Recording: errorString(recordingErr)})
			writeJSON(w, http.StatusOK, response)
			return
		}
	}

	replyStarted := time.Now()
	reply, safety, activity, source, aiErr := s.reply(r.Context(), transcript)
	timings.ReplyMS = elapsedMS(replyStarted)
	ttsStarted := time.Now()
	audio, mime, ttsErr := s.speak(r, reply)
	timings.TTSMS = elapsedMS(ttsStarted)
	timings.TotalMS = elapsedMS(started)
	response := chatResponse{
		TraceID:     traceID,
		Transcript:  transcript,
		Reply:       reply,
		AudioBase64: encodeAudio(audio),
		AudioMIME:   mime,
		Safety:      safety,
		Activity:    activity,
		Mode:        s.mode(),
		Source:      source,
		AIError:     errorString(aiErr),
		TTSError:    errorString(ttsErr),
		Timings:     timings,
	}
	s.recordConversation("voice", response, recording, eventErrors{Chat: errorString(aiErr), TTS: errorString(ttsErr), Recording: errorString(recordingErr)})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) recordConversation(endpoint string, response chatResponse, recording *RecordingMeta, errors eventErrors) {
	if s.events == nil {
		return
	}
	traceID := response.TraceID
	if traceID == "" {
		traceID = s.nextTraceID()
	}
	event := ConversationEvent{
		Time:            time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:         traceID,
		Endpoint:        endpoint,
		Transcript:      response.Transcript,
		Reply:           response.Reply,
		Mode:            response.Mode,
		Source:          response.Source,
		SafetyTriggered: response.Safety.Triggered,
		SafetyCategory:  response.Safety.Category,
		Timings:         response.Timings,
	}
	if recording != nil {
		event.HasRecording = true
		event.RecordingMIME = recording.MIME
	}
	if response.Activity != nil {
		event.ActivityID = response.Activity.ID
		event.ActivityLabel = response.Activity.Label
	}
	if errors.STT != "" || errors.Chat != "" || errors.TTS != "" || errors.Recording != "" {
		event.Errors = &EventErrors{
			STT:       errors.STT,
			Chat:      errors.Chat,
			TTS:       errors.TTS,
			Recording: errors.Recording,
		}
	}
	if err := s.events.Append(event); err != nil {
		s.log.Warn("failed to append event log", "error", err)
	}
}

func (s *Server) nextTraceID() string {
	seq := s.traceSeq.Add(1)
	return fmt.Sprintf("%x-%x", time.Now().UnixNano(), seq)
}

func (s *Server) reply(ctx context.Context, text string) (string, dog.SafetyResult, *dog.Activity, string, error) {
	safety := dog.CheckSafety(text)
	if safety.Triggered {
		return safety.Reply, safety, nil, "safety", nil
	}

	if activity, ok := dog.PlanActivity(text); ok {
		return activity.Reply, safety, &activity, "activity:" + activity.ID, nil
	}

	if s.useChat {
		reply, err := s.chat.CreateResponse(ctx, dog.Instructions(), text)
		if err != nil {
			fallback := dog.MockReply(text)
			return fallback, safety, nil, "mock_fallback", err
		}
		return dog.ClampReply(reply, 90), safety, nil, s.chat.Name(), nil
	}

	return dog.MockReply(text), safety, nil, "mock", nil
}

func (s *Server) speak(r *http.Request, text string) ([]byte, string, error) {
	if !s.useVoice || strings.EqualFold(r.URL.Query().Get("tts"), "off") {
		return nil, "", nil
	}
	return s.synthesizeSpeech(r.Context(), text)
}

func (s *Server) synthesizeSpeech(ctx context.Context, text string) ([]byte, string, error) {
	if !s.useVoice {
		return nil, "", nil
	}
	key := s.speechCacheKey(text)
	s.speechMu.Lock()
	if cached, ok := s.speechCache[key]; ok {
		audio := append([]byte(nil), cached.audio...)
		s.speechMu.Unlock()
		return audio, cached.mime, nil
	}
	s.speechMu.Unlock()

	audio, mime, err := s.voice.Speak(ctx, text)
	if err != nil {
		return nil, "", err
	}
	s.speechMu.Lock()
	s.speechCache[key] = cachedSpeech{audio: append([]byte(nil), audio...), mime: mime}
	s.speechMu.Unlock()
	return audio, mime, nil
}

func (s *Server) speechCacheKey(text string) string {
	return strings.Join([]string{
		s.voiceProvider(),
		s.modelName("tts"),
		s.modelName("voice"),
		s.modelName("format"),
		strconv.FormatFloat(s.ttsSpeed(), 'f', 3, 64),
		text,
	}, "\x00")
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	staticDir := s.staticDir
	if staticDir == "" {
		staticDir = "web/static"
	}

	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == "." || path == "" {
		path = "index.html"
	}

	fullPath := filepath.Join(staticDir, path)
	if !strings.HasPrefix(fullPath, filepath.Clean(staticDir)+string(os.PathSeparator)) && filepath.Clean(fullPath) != filepath.Clean(staticDir) {
		http.NotFound(w, r)
		return
	}

	if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, fullPath)
}

func (s *Server) mode() string {
	if s.useVoice {
		return s.voice.Name()
	}
	if s.useChat {
		return s.chat.Name() + "-chat"
	}
	return "mock"
}

func (s *Server) chatProvider() string {
	if s.useChat {
		return s.chat.Name()
	}
	return "mock"
}

func (s *Server) voiceProvider() string {
	if s.useVoice {
		return s.voice.Name()
	}
	return "mock"
}

func (s *Server) modelName(kind string) string {
	switch kind {
	case "chat":
		if s.chat == nil {
			return ""
		}
		return s.chat.ChatModel()
	case "stt":
		if s.voice == nil {
			return ""
		}
		return s.voice.STTModel()
	case "tts":
		if s.voice == nil {
			return ""
		}
		return s.voice.TTSModel()
	case "voice":
		if s.voice == nil {
			return ""
		}
		return s.voice.TTSVoice()
	case "format":
		if s.voice == nil {
			return ""
		}
		return s.voice.TTSFormat()
	default:
		return ""
	}
}

func (s *Server) ttsSpeed() float64 {
	if s.voice == nil {
		return 0
	}
	return s.voice.TTSSpeed()
}

func chatEnabled(chat ChatProvider, forceMock bool) bool {
	if chat == nil || !chat.Available() || forceMock {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PUPBOX_CHAT_PROVIDER"))) {
	case "mock", "local", "off", "none":
		return false
	default:
		return true
	}
}

func parseEventLimit(value string) int {
	limit := 50
	if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
		limit = parsed
	}
	if limit > 200 {
		return 200
	}
	return limit
}

type chatRequest struct {
	Text string `json:"text"`
}

type speechRequest struct {
	Text string `json:"text"`
}

type speechResponse struct {
	AudioBase64 string      `json:"audio_base64,omitempty"`
	AudioMIME   string      `json:"audio_mime,omitempty"`
	Mode        string      `json:"mode"`
	TTSError    string      `json:"tts_error,omitempty"`
	Timings     TimingStats `json:"timings"`
}

type chatResponse struct {
	TraceID     string           `json:"trace_id,omitempty"`
	Transcript  string           `json:"transcript"`
	Reply       string           `json:"reply"`
	AudioBase64 string           `json:"audio_base64,omitempty"`
	AudioMIME   string           `json:"audio_mime,omitempty"`
	Safety      dog.SafetyResult `json:"safety"`
	Activity    *dog.Activity    `json:"activity,omitempty"`
	Mode        string           `json:"mode"`
	Source      string           `json:"source"`
	AIError     string           `json:"ai_error,omitempty"`
	TTSError    string           `json:"tts_error,omitempty"`
	Timings     TimingStats      `json:"timings"`
}

type TimingStats struct {
	TotalMS         int64   `json:"total_ms"`
	STTMS           int64   `json:"stt_ms"`
	ReplyMS         int64   `json:"reply_ms"`
	TTSMS           int64   `json:"tts_ms"`
	AudioBytes      int64   `json:"audio_bytes,omitempty"`
	AudioDurationMS int64   `json:"audio_duration_ms,omitempty"`
	AudioPeak       float64 `json:"audio_peak,omitempty"`
	AudioRMS        float64 `json:"audio_rms,omitempty"`
}

func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

func isTooShortAudio(data []byte, filename, contentType string) bool {
	if len(data) < 512 {
		return true
	}
	stats, ok := audioStats(data, filename, contentType)
	return ok && stats.DurationMS < 220
}

func audioDurationMS(data []byte, filename, contentType string) (int64, bool) {
	stats, ok := audioStats(data, filename, contentType)
	return stats.DurationMS, ok
}

type AudioStats struct {
	DurationMS int64
	Peak       float64
	RMS        float64
}

func audioStats(data []byte, filename, contentType string) (AudioStats, bool) {
	if !looksLikeWAV(filename, contentType, data) {
		return AudioStats{}, false
	}
	return wavAudioStats(data)
}

func looksLikeWAV(filename, contentType string, data []byte) bool {
	contentType = strings.ToLower(strings.Split(contentType, ";")[0])
	return strings.Contains(contentType, "wav") ||
		strings.HasSuffix(strings.ToLower(filename), ".wav") ||
		(len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE")
}

func wavDurationMS(data []byte) (int64, bool) {
	stats, ok := wavAudioStats(data)
	return stats.DurationMS, ok
}

func wavAudioStats(data []byte) (AudioStats, bool) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return AudioStats{}, false
	}

	var audioFormat, channels, bitsPerSample uint16
	var sampleRate uint32
	var pcm []byte
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		body := offset + 8
		if body+chunkSize > len(data) {
			return AudioStats{}, false
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				audioFormat = binary.LittleEndian.Uint16(data[body : body+2])
				channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
				sampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
				bitsPerSample = binary.LittleEndian.Uint16(data[body+14 : body+16])
			}
		case "data":
			pcm = data[body : body+chunkSize]
		}
		offset = body + chunkSize
		if offset%2 == 1 {
			offset++
		}
	}
	if audioFormat != 1 || channels == 0 || bitsPerSample == 0 || sampleRate == 0 || len(pcm) == 0 {
		return AudioStats{}, false
	}
	bytesPerSecond := int64(sampleRate) * int64(channels) * int64(bitsPerSample) / 8
	if bytesPerSecond <= 0 {
		return AudioStats{}, false
	}
	stats := AudioStats{
		DurationMS: int64(len(pcm)) * 1000 / bytesPerSecond,
	}
	if bitsPerSample != 16 {
		return stats, true
	}
	var sumSquares float64
	var samples int
	for offset := 0; offset+2 <= len(pcm); offset += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[offset:offset+2]))) / 32768
		abs := math.Abs(sample)
		if abs > stats.Peak {
			stats.Peak = abs
		}
		sumSquares += sample * sample
		samples++
	}
	if samples > 0 {
		stats.RMS = math.Sqrt(sumSquares / float64(samples))
	}
	return stats, true
}

func encodeAudio(audio []byte) string {
	if len(audio) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(audio)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
