package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
	"github.com/donychen1134/pupbox/internal/openaiapi"
)

type Server struct {
	mux       *http.ServeMux
	ai        *openaiapi.Client
	useOpenAI bool
	staticDir string
	log       *slog.Logger
}

type Config struct {
	AI        *openaiapi.Client
	StaticDir string
	Logger    *slog.Logger
}

func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	forceMock := strings.EqualFold(os.Getenv("PUPBOX_MODE"), "mock")
	s := &Server{
		mux:       http.NewServeMux(),
		ai:        cfg.AI,
		useOpenAI: cfg.AI != nil && cfg.AI.Available() && !forceMock,
		staticDir: cfg.StaticDir,
		log:       logger,
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/activities", s.handleActivities)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)
	s.mux.HandleFunc("POST /api/speech", s.handleSpeech)
	s.mux.HandleFunc("POST /api/voice", s.handleVoice)
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"mode":        s.mode(),
		"dog":         dog.Name,
		"chat_model":  s.modelName("chat"),
		"stt_model":   s.modelName("stt"),
		"tts_model":   s.modelName("tts"),
		"tts_voice":   s.modelName("voice"),
		"tts_format":  s.modelName("format"),
		"tts_speed":   s.ttsSpeed(),
		"server_time": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleActivities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"activities": dog.Activities(),
	})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
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

	reply, safety, activity, source, aiErr := s.reply(r.Context(), text)
	audio, mime, ttsErr := s.speak(r, reply)
	writeJSON(w, http.StatusOK, chatResponse{
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
	})
}

func (s *Server) handleSpeech(w http.ResponseWriter, r *http.Request) {
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
	if !s.useOpenAI {
		writeJSON(w, http.StatusOK, speechResponse{
			Mode:     s.mode(),
			TTSError: "openai mode is not enabled",
		})
		return
	}

	audio, mime, err := s.ai.Speak(r.Context(), dog.ClampReply(text, 90))
	writeJSON(w, http.StatusOK, speechResponse{
		AudioBase64: encodeAudio(audio),
		AudioMIME:   mime,
		Mode:        s.mode(),
		TTSError:    errorString(err),
	})
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
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

	transcript := "我想听一个小狗故事"
	var sttErr error
	if s.useOpenAI {
		transcript, sttErr = s.ai.Transcribe(r.Context(), data, filename, contentType)
		if sttErr != nil {
			writeJSON(w, http.StatusOK, chatResponse{
				Transcript: "",
				Reply:      "豆豆刚才没有听清楚。你可以再说一遍吗？",
				Mode:       s.mode(),
				Source:     "stt_error",
				AIError:    sttErr.Error(),
			})
			return
		}
	}

	reply, safety, activity, source, aiErr := s.reply(r.Context(), transcript)
	audio, mime, ttsErr := s.speak(r, reply)
	writeJSON(w, http.StatusOK, chatResponse{
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
	})
}

func (s *Server) reply(ctx context.Context, text string) (string, dog.SafetyResult, *dog.Activity, string, error) {
	safety := dog.CheckSafety(text)
	if safety.Triggered {
		return safety.Reply, safety, nil, "safety", nil
	}

	if activity, ok := dog.PlanActivity(text); ok {
		return activity.Reply, safety, &activity, "activity:" + activity.ID, nil
	}

	if s.useOpenAI {
		reply, err := s.ai.CreateResponse(ctx, dog.Instructions(), text)
		if err != nil {
			fallback := dog.MockReply(text)
			return fallback, safety, nil, "mock_fallback", err
		}
		return dog.ClampReply(reply, 90), safety, nil, "openai", nil
	}

	return dog.MockReply(text), safety, nil, "mock", nil
}

func (s *Server) speak(r *http.Request, text string) ([]byte, string, error) {
	if !s.useOpenAI || strings.EqualFold(r.URL.Query().Get("tts"), "off") {
		return nil, "", nil
	}
	audio, mime, err := s.ai.Speak(r.Context(), text)
	if err != nil {
		return nil, "", err
	}
	return audio, mime, nil
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
	if s.useOpenAI {
		return "openai"
	}
	return "mock"
}

func (s *Server) modelName(kind string) string {
	if s.ai == nil {
		return ""
	}
	switch kind {
	case "chat":
		return s.ai.ChatModel()
	case "stt":
		return s.ai.STTModel()
	case "tts":
		return s.ai.TTSModel()
	case "voice":
		return s.ai.TTSVoice()
	case "format":
		return s.ai.TTSFormat()
	default:
		return ""
	}
}

func (s *Server) ttsSpeed() float64 {
	if s.ai == nil {
		return 0
	}
	return s.ai.TTSSpeed()
}

type chatRequest struct {
	Text string `json:"text"`
}

type speechRequest struct {
	Text string `json:"text"`
}

type speechResponse struct {
	AudioBase64 string `json:"audio_base64,omitempty"`
	AudioMIME   string `json:"audio_mime,omitempty"`
	Mode        string `json:"mode"`
	TTSError    string `json:"tts_error,omitempty"`
}

type chatResponse struct {
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
