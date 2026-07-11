package server

import (
	"bytes"
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
	trimSTT     bool
	sessions    *SessionStore
	speechMu    sync.Mutex
	speechCache map[string]cachedSpeech
	speechDisk  *SpeechDiskCache
	speechCalls map[string]*speechCall
	warmRunning atomic.Bool
	warmTotal   atomic.Int64
	warmDone    atomic.Int64
	warmErrors  atomic.Int64
	traceSeq    atomic.Uint64
	log         *slog.Logger
}

type cachedSpeech struct {
	audio []byte
	mime  string
}

type speechCall struct {
	done  chan struct{}
	audio []byte
	mime  string
	err   error
}

const maxSpeechCacheEntries = 128

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

type StreamingVoiceProvider interface {
	StreamSampleRate() int
	StreamSpeak(ctx context.Context, text string, onChunk func([]byte) error) (string, int, error)
}

type Config struct {
	Chat             ChatProvider
	Voice            VoiceProvider
	StaticDir        string
	AccessToken      string
	EventLogPath     string
	EventLogLimit    int
	RecordingDir     string
	RecordingLimit   int
	TrimSTTSilence   bool
	SpeechCacheDir   string
	SpeechCacheLimit int
	Logger           *slog.Logger
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
	events := NewEventStore(cfg.EventLogPath, cfg.EventLogLimit)
	if events != nil {
		if err := events.Ensure(); err != nil {
			logger.Warn("event log is not ready", "error", err)
		}
	}
	speechDisk := NewSpeechDiskCache(cfg.SpeechCacheDir, cfg.SpeechCacheLimit)
	if speechDisk != nil {
		if err := speechDisk.Ensure(); err != nil {
			logger.Warn("speech cache is not ready", "error", err)
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
		events:      events,
		recordings:  NewRecordingStore(cfg.RecordingDir, cfg.RecordingLimit),
		trimSTT:     cfg.TrimSTTSilence,
		sessions:    NewSessionStore(128, 6, 15*time.Minute),
		speechCache: make(map[string]cachedSpeech),
		speechDisk:  speechDisk,
		speechCalls: make(map[string]*speechCall),
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
	s.mux.HandleFunc("POST /api/speech-stream", s.requireAccess(s.handleSpeechStream))
	s.mux.HandleFunc("POST /api/turn-metrics", s.requireAccess(s.handleTurnMetrics))
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
	eventLogPath, eventLogReady, eventCount, eventLogErr := s.events.Status()
	speechCacheDir, speechCacheReady, speechCacheEntries, speechCacheErr := s.speechDisk.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"auth_required":     s.accessToken != "",
		"event_log":         s.events != nil,
		"event_log_ready":   eventLogReady,
		"event_log_path":    eventLogPath,
		"event_log_events":  eventCount,
		"event_log_error":   errorString(eventLogErr),
		"tts_cache":         s.speechDisk != nil,
		"tts_cache_ready":   speechCacheReady,
		"tts_cache_dir":     speechCacheDir,
		"tts_cache_entries": speechCacheEntries,
		"tts_cache_error":   errorString(speechCacheErr),
		"tts_warm_running":  s.warmRunning.Load(),
		"tts_warm_total":    s.warmTotal.Load(),
		"tts_warm_done":     s.warmDone.Load(),
		"tts_warm_errors":   s.warmErrors.Load(),
		"recordings":        s.recordings != nil,
		"mode":              s.mode(),
		"dog":               dog.Name,
		"chat_provider":     s.chatProvider(),
		"voice_provider":    s.voiceProvider(),
		"chat_model":        s.modelName("chat"),
		"stt_model":         s.modelName("stt"),
		"stt_trim_silence":  s.trimSTT,
		"tts_model":         s.modelName("tts"),
		"tts_voice":         s.modelName("voice"),
		"tts_format":        s.modelName("format"),
		"tts_speed":         s.ttsSpeed(),
		"tts_streaming":     s.streamingVoiceAvailable(),
		"server_time":       time.Now().Format(time.RFC3339),
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
	traceID := s.nextTraceID()
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
	sessionID := requestSessionID(r)
	history := s.sessions.History(sessionID)
	replyStarted := time.Now()
	reply, safety, activity, source, aiErr := s.reply(r.Context(), text, history)
	timings.ReplyMS = elapsedMS(replyStarted)
	ttsStarted := time.Now()
	audio, mime, ttsErr := s.speak(r, reply)
	timings.TTSMS = elapsedMS(ttsStarted)
	timings.TotalMS = elapsedMS(started)
	response := chatResponse{
		TraceID:     traceID,
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
	s.sessions.Append(sessionID, text, reply)
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

func (s *Server) handleSpeechStream(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var req speechRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	text := dog.ClampReply(strings.TrimSpace(req.Text), 90)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if !s.useVoice {
		writeError(w, http.StatusServiceUnavailable, "server voice mode is not enabled")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	writeEvent := func(event speechStreamEvent) error {
		if err := encoder.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if audio, mime, cache, ok := s.cachedSpeechForStream(text); ok {
		firstAudioMS := elapsedMS(started)
		chunkSize := len(audio)
		if cache.kind == "stream" && mime == "audio/pcm" {
			chunkSize = streamAudioChunkBytes
		}
		for offset := 0; offset < len(audio); offset += chunkSize {
			end := min(offset+chunkSize, len(audio))
			if err := writeEvent(speechStreamEvent{
				Type:        "audio",
				AudioBase64: encodeAudio(audio[offset:end]),
				AudioMIME:   mime,
				SampleRate:  cache.sampleRate,
				Cache:       cache.kind,
			}); err != nil {
				return
			}
		}
		_ = writeEvent(speechStreamEvent{
			Type:  "done",
			Cache: cache.kind,
			Timings: &TimingStats{
				TotalMS:         elapsedMS(started),
				TTSMS:           elapsedMS(started),
				TTSFirstAudioMS: firstAudioMS,
			},
		})
		return
	}

	streaming, ok := s.voice.(StreamingVoiceProvider)
	if !ok {
		s.writeCompleteSpeechFallback(r.Context(), text, started, writeEvent)
		return
	}

	streamKey := s.streamSpeechCacheKey(text, streaming.StreamSampleRate())
	var audio bytes.Buffer
	cacheable := true
	firstAudioMS := int64(-1)
	mime, sampleRate, err := streaming.StreamSpeak(r.Context(), text, func(chunk []byte) error {
		if firstAudioMS < 0 {
			firstAudioMS = elapsedMS(started)
		}
		if cacheable && audio.Len()+len(chunk) <= maxCachedSpeechBytes {
			_, _ = audio.Write(chunk)
		} else {
			cacheable = false
		}
		return writeEvent(speechStreamEvent{
			Type:        "audio",
			AudioBase64: encodeAudio(chunk),
			AudioMIME:   "audio/pcm",
			SampleRate:  streaming.StreamSampleRate(),
			Cache:       "miss",
		})
	})
	if err != nil {
		if firstAudioMS < 0 {
			s.log.Warn("streaming speech failed before audio", "error", err)
			s.writeCompleteSpeechFallback(r.Context(), text, started, writeEvent)
			return
		}
		s.log.Warn("streaming speech failed after audio", "error", err)
		_ = writeEvent(speechStreamEvent{Type: "error", Error: "streaming speech ended early"})
		return
	}
	if mime == "" {
		mime = "audio/pcm"
	}
	if sampleRate <= 0 {
		sampleRate = streaming.StreamSampleRate()
	}
	if cacheable && audio.Len() > 0 {
		if cacheErr := s.speechDisk.Put(streamKey, mime, audio.Bytes()); cacheErr != nil {
			s.log.Warn("failed to write streaming speech cache", "error", cacheErr)
		}
	}
	timings := TimingStats{
		TotalMS:         elapsedMS(started),
		TTSMS:           elapsedMS(started),
		TTSFirstAudioMS: firstAudioMS,
	}
	_ = writeEvent(speechStreamEvent{Type: "done", Cache: "miss", Timings: &timings})
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
	timings := TimingStats{UploadMS: elapsedMS(started), AudioBytes: int64(len(data))}
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
		sttData := data
		if s.trimSTT {
			if trimmed, trim, ok := trimWAVSilence(data); ok {
				timings.STTAudioDurationMS = trim.InputDurationMS
				timings.STTTrimmedMS = trim.TrimmedMS
				sttData = trimmed
			}
		}
		if timings.STTAudioDurationMS == 0 {
			timings.STTAudioDurationMS = timings.AudioDurationMS
		}
		sttStarted := time.Now()
		transcript, sttErr = s.voice.Transcribe(r.Context(), sttData, filename, contentType)
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

	sessionID := requestSessionID(r)
	history := s.sessions.History(sessionID)
	replyStarted := time.Now()
	reply, safety, activity, source, aiErr := s.reply(r.Context(), transcript, history)
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
	s.sessions.Append(sessionID, transcript, reply)
	s.recordConversation("voice", response, recording, eventErrors{Chat: errorString(aiErr), TTS: errorString(ttsErr), Recording: errorString(recordingErr)})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleTurnMetrics(w http.ResponseWriter, r *http.Request) {
	var req turnMetricsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.TraceID = strings.TrimSpace(req.TraceID)
	if req.TraceID == "" || len(req.TraceID) > 128 {
		writeError(w, http.StatusBadRequest, "valid trace_id is required")
		return
	}
	if s.events == nil {
		writeError(w, http.StatusServiceUnavailable, "event log is not enabled")
		return
	}
	if !validClientTiming(req.VoiceResponseMS) || !validClientTiming(req.TTSFirstAudioMS) ||
		!validClientTiming(req.TTSMS) || !validClientTiming(req.PlaybackMS) || !validClientTiming(req.TurnTotalMS) ||
		!validClientTiming(req.AudioUnderrunMS) || req.AudioUnderruns < 0 || req.AudioUnderruns > 1000 {
		writeError(w, http.StatusBadRequest, "invalid timing value")
		return
	}
	cache := strings.TrimSpace(req.TTSCache)
	if len(cache) > 32 {
		writeError(w, http.StatusBadRequest, "invalid tts_cache value")
		return
	}
	playbackError := truncateText(strings.TrimSpace(req.PlaybackError), 200)
	updated, err := s.events.Update(req.TraceID, func(event *ConversationEvent) {
		event.Timings.VoiceResponseMS = req.VoiceResponseMS
		event.Timings.TTSFirstAudioMS = req.TTSFirstAudioMS
		event.Timings.TTSMS = req.TTSMS
		event.Timings.PlaybackMS = req.PlaybackMS
		event.Timings.TurnTotalMS = req.TurnTotalMS
		event.Timings.AudioUnderruns = req.AudioUnderruns
		event.Timings.AudioUnderrunMS = req.AudioUnderrunMS
		event.TTSCache = cache
		if playbackError != "" {
			if event.Errors == nil {
				event.Errors = &EventErrors{}
			}
			event.Errors.Playback = playbackError
		}
	})
	if err != nil {
		s.log.Warn("failed to update turn metrics", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update turn metrics")
		return
	}
	if !updated {
		writeError(w, http.StatusNotFound, "conversation event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
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
		Transcript:      truncateText(response.Transcript, 500),
		Reply:           truncateText(response.Reply, 500),
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

func (s *Server) reply(ctx context.Context, text string, history []dog.Turn) (string, dog.SafetyResult, *dog.Activity, string, error) {
	safety := dog.CheckSafety(text)
	if safety.Triggered {
		return safety.Reply, safety, nil, "safety", nil
	}

	if activity, ok := dog.PlanActivityWithHistory(text, history); ok {
		activity.Reply = dog.SpeechOnlyReply(activity.Reply)
		return activity.Reply, safety, &activity, "activity:" + activity.ID, nil
	}

	if s.useChat {
		reply, err := s.chat.CreateResponse(ctx, dog.Instructions(), contextualInput(history, text))
		if err != nil {
			fallback := dog.SpeechOnlyReply(dog.MockReply(text))
			return fallback, safety, nil, "mock_fallback", err
		}
		return dog.SpeechOnlyReply(dog.ClampReply(reply, 90)), safety, nil, s.chat.Name(), nil
	}

	return dog.SpeechOnlyReply(dog.MockReply(text)), safety, nil, "mock", nil
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
	if call, ok := s.speechCalls[key]; ok {
		s.speechMu.Unlock()
		select {
		case <-call.done:
			return append([]byte(nil), call.audio...), call.mime, call.err
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	call := &speechCall{done: make(chan struct{})}
	s.speechCalls[key] = call
	s.speechMu.Unlock()

	if audio, mime, ok, cacheErr := s.speechDisk.Get(key); ok {
		s.completeSpeechCall(key, call, audio, mime, nil)
		return audio, mime, nil
	} else if cacheErr != nil {
		s.log.Warn("failed to read speech cache", "error", cacheErr)
	}

	audio, mime, err := s.voice.Speak(ctx, text)
	if err == nil {
		if cacheErr := s.speechDisk.Put(key, mime, audio); cacheErr != nil {
			s.log.Warn("failed to write speech cache", "error", cacheErr)
		}
	}
	s.completeSpeechCall(key, call, audio, mime, err)
	return audio, mime, err
}

type speechStreamCache struct {
	kind       string
	sampleRate int
}

const streamAudioChunkBytes = 8 << 10

func (s *Server) cachedSpeechForStream(text string) ([]byte, string, speechStreamCache, bool) {
	if streaming, ok := s.voice.(StreamingVoiceProvider); ok {
		sampleRate := streaming.StreamSampleRate()
		streamKey := s.streamSpeechCacheKey(text, sampleRate)
		if audio, mime, found, err := s.speechDisk.Get(streamKey); found {
			return audio, mime, speechStreamCache{kind: "stream", sampleRate: sampleRate}, true
		} else if err != nil {
			s.log.Warn("failed to read streaming speech cache", "error", err)
		}
		// A complete MP3 cannot be decoded progressively in Safari. On a stream cache
		// miss, synthesize PCM so playback can start before the whole reply arrives.
		return nil, "", speechStreamCache{}, false
	}

	key := s.speechCacheKey(text)
	s.speechMu.Lock()
	if cached, ok := s.speechCache[key]; ok {
		audio := append([]byte(nil), cached.audio...)
		s.speechMu.Unlock()
		return audio, cached.mime, speechStreamCache{kind: "complete"}, true
	}
	s.speechMu.Unlock()

	if audio, mime, ok, err := s.speechDisk.Get(key); ok {
		s.speechMu.Lock()
		if len(s.speechCache) >= maxSpeechCacheEntries {
			for cachedKey := range s.speechCache {
				delete(s.speechCache, cachedKey)
				break
			}
		}
		s.speechCache[key] = cachedSpeech{audio: append([]byte(nil), audio...), mime: mime}
		s.speechMu.Unlock()
		return audio, mime, speechStreamCache{kind: "complete"}, true
	} else if err != nil {
		s.log.Warn("failed to read complete speech cache for stream", "error", err)
	}

	return nil, "", speechStreamCache{}, false
}

func (s *Server) writeCompleteSpeechFallback(
	ctx context.Context,
	text string,
	started time.Time,
	writeEvent func(speechStreamEvent) error,
) {
	audio, mime, err := s.synthesizeSpeech(ctx, text)
	if err != nil {
		_ = writeEvent(speechStreamEvent{Type: "error", Error: "speech synthesis failed"})
		return
	}
	firstAudioMS := elapsedMS(started)
	if err := writeEvent(speechStreamEvent{
		Type:        "audio",
		AudioBase64: encodeAudio(audio),
		AudioMIME:   mime,
		Cache:       "fallback",
	}); err != nil {
		return
	}
	_ = writeEvent(speechStreamEvent{
		Type:  "done",
		Cache: "fallback",
		Timings: &TimingStats{
			TotalMS:         elapsedMS(started),
			TTSMS:           elapsedMS(started),
			TTSFirstAudioMS: firstAudioMS,
		},
	})
}

func (s *Server) completeSpeechCall(key string, call *speechCall, audio []byte, mime string, err error) {
	s.speechMu.Lock()
	if err == nil {
		if len(s.speechCache) >= maxSpeechCacheEntries {
			for cachedKey := range s.speechCache {
				delete(s.speechCache, cachedKey)
				break
			}
		}
		s.speechCache[key] = cachedSpeech{audio: append([]byte(nil), audio...), mime: mime}
	}
	call.audio = append([]byte(nil), audio...)
	call.mime = mime
	call.err = err
	delete(s.speechCalls, key)
	close(call.done)
	s.speechMu.Unlock()
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

func (s *Server) streamSpeechCacheKey(text string, sampleRate int) string {
	return s.speechCacheKey(text) + "\x00stream-pcm\x00" + strconv.Itoa(sampleRate)
}

func (s *Server) streamingVoiceAvailable() bool {
	if !s.useVoice {
		return false
	}
	_, ok := s.voice.(StreamingVoiceProvider)
	return ok
}

func (s *Server) PrewarmSpeech(ctx context.Context, texts []string) {
	if !s.useVoice || !s.warmRunning.CompareAndSwap(false, true) {
		return
	}
	defer s.warmRunning.Store(false)

	unique := make([]string, 0, len(texts))
	seen := make(map[string]bool, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		unique = append(unique, text)
	}
	s.warmTotal.Store(int64(len(unique)))
	s.warmDone.Store(0)
	s.warmErrors.Store(0)
	if len(unique) == 0 {
		return
	}

	s.log.Info("speech cache warmup started", "items", len(unique))
	for _, text := range unique {
		if ctx.Err() != nil {
			break
		}
		if _, _, err := s.synthesizeSpeech(ctx, text); err != nil {
			s.warmErrors.Add(1)
			s.log.Warn("speech cache warmup item failed", "error", err)
		}
		s.warmDone.Add(1)
	}
	s.log.Info(
		"speech cache warmup finished",
		"completed", s.warmDone.Load(),
		"errors", s.warmErrors.Load(),
	)
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

type turnMetricsRequest struct {
	TraceID         string `json:"trace_id"`
	VoiceResponseMS int64  `json:"voice_response_ms"`
	TTSFirstAudioMS int64  `json:"tts_first_audio_ms"`
	TTSMS           int64  `json:"tts_ms"`
	PlaybackMS      int64  `json:"playback_ms"`
	TurnTotalMS     int64  `json:"turn_total_ms"`
	AudioUnderruns  int64  `json:"audio_underruns"`
	AudioUnderrunMS int64  `json:"audio_underrun_ms"`
	TTSCache        string `json:"tts_cache"`
	PlaybackError   string `json:"playback_error"`
}

type speechResponse struct {
	AudioBase64 string      `json:"audio_base64,omitempty"`
	AudioMIME   string      `json:"audio_mime,omitempty"`
	Mode        string      `json:"mode"`
	TTSError    string      `json:"tts_error,omitempty"`
	Timings     TimingStats `json:"timings"`
}

type speechStreamEvent struct {
	Type        string       `json:"type"`
	AudioBase64 string       `json:"audio_base64,omitempty"`
	AudioMIME   string       `json:"audio_mime,omitempty"`
	SampleRate  int          `json:"sample_rate,omitempty"`
	Cache       string       `json:"cache,omitempty"`
	Error       string       `json:"error,omitempty"`
	Timings     *TimingStats `json:"timings,omitempty"`
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
	TotalMS            int64   `json:"total_ms"`
	UploadMS           int64   `json:"upload_ms,omitempty"`
	STTMS              int64   `json:"stt_ms"`
	ReplyMS            int64   `json:"reply_ms"`
	TTSMS              int64   `json:"tts_ms"`
	TTSFirstAudioMS    int64   `json:"tts_first_audio_ms,omitempty"`
	VoiceResponseMS    int64   `json:"voice_response_ms,omitempty"`
	PlaybackMS         int64   `json:"playback_ms,omitempty"`
	TurnTotalMS        int64   `json:"turn_total_ms,omitempty"`
	AudioUnderruns     int64   `json:"audio_underruns,omitempty"`
	AudioUnderrunMS    int64   `json:"audio_underrun_ms,omitempty"`
	AudioBytes         int64   `json:"audio_bytes,omitempty"`
	AudioDurationMS    int64   `json:"audio_duration_ms,omitempty"`
	STTAudioDurationMS int64   `json:"stt_audio_duration_ms,omitempty"`
	STTTrimmedMS       int64   `json:"stt_trimmed_ms,omitempty"`
	AudioPeak          float64 `json:"audio_peak,omitempty"`
	AudioRMS           float64 `json:"audio_rms,omitempty"`
}

func validClientTiming(value int64) bool {
	return value >= 0 && value <= 10*60*1000
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
	wav, ok := parsePCMWAV(data)
	if !ok {
		return AudioStats{}, false
	}
	bytesPerSecond := int64(wav.SampleRate) * int64(wav.Channels) * int64(wav.BitsPerSample) / 8
	if bytesPerSecond <= 0 {
		return AudioStats{}, false
	}
	stats := AudioStats{
		DurationMS: int64(len(wav.PCM)) * 1000 / bytesPerSecond,
	}
	if wav.BitsPerSample != 16 {
		return stats, true
	}
	var sumSquares float64
	var samples int
	for offset := 0; offset+2 <= len(wav.PCM); offset += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(wav.PCM[offset:offset+2]))) / 32768
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
