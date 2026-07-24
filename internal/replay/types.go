package replay

import "time"

const CorpusVersion = 1

type CorpusMetadata struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SourceURL  string    `json:"source_url"`
	EntryCount int       `json:"entry_count"`
	GroupGapMS int64     `json:"group_gap_ms"`
}

type CorpusEntry struct {
	ID                 string        `json:"id"`
	File               string        `json:"file"`
	SHA256             string        `json:"sha256"`
	MIME               string        `json:"mime"`
	RecordedAt         string        `json:"recorded_at"`
	Session            string        `json:"session"`
	Order              int           `json:"order"`
	OriginalTranscript string        `json:"original_transcript,omitempty"`
	OriginalReply      string        `json:"original_reply,omitempty"`
	OriginalSource     string        `json:"original_source,omitempty"`
	OriginalActivityID string        `json:"original_activity_id,omitempty"`
	OriginalSafety     string        `json:"original_safety_category,omitempty"`
	ParentFeedback     string        `json:"parent_feedback,omitempty"`
	Expected           ExpectedRoute `json:"expected,omitempty"`
}

type ExpectedRoute struct {
	Source         string `json:"source,omitempty"`
	ActivityID     string `json:"activity_id,omitempty"`
	SafetyCategory string `json:"safety_category,omitempty"`
}

type CollectResult struct {
	OutputDir string
	Collected int
	Skipped   int
}

type RunReport struct {
	Version    int         `json:"version"`
	RunID      string      `json:"run_id"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
	TargetURL  string      `json:"target_url"`
	CorpusDir  string      `json:"corpus_dir"`
	Results    []RunResult `json:"results"`
	Summary    RunSummary  `json:"summary"`
}

type RunResult struct {
	ID                   string        `json:"id"`
	Session              string        `json:"session"`
	Order                int           `json:"order"`
	Pass                 bool          `json:"pass"`
	NeedsReview          bool          `json:"needs_review,omitempty"`
	Issues               []string      `json:"issues,omitempty"`
	Error                string        `json:"error,omitempty"`
	OriginalTranscript   string        `json:"original_transcript,omitempty"`
	Transcript           string        `json:"transcript,omitempty"`
	TranscriptSimilarity float64       `json:"transcript_similarity,omitempty"`
	OriginalReply        string        `json:"original_reply,omitempty"`
	Reply                string        `json:"reply,omitempty"`
	OriginalSource       string        `json:"original_source,omitempty"`
	OriginalActivityID   string        `json:"original_activity_id,omitempty"`
	OriginalSafety       string        `json:"original_safety_category,omitempty"`
	RouteChanged         bool          `json:"route_changed,omitempty"`
	Expected             ExpectedRoute `json:"expected,omitempty"`
	Source               string        `json:"source,omitempty"`
	ActivityID           string        `json:"activity_id,omitempty"`
	SafetyCategory       string        `json:"safety_category,omitempty"`
	AIError              string        `json:"ai_error,omitempty"`
	TTSError             string        `json:"tts_error,omitempty"`
	HTTPMS               int64         `json:"http_ms"`
	Timings              TimingStats   `json:"timings"`
}

type RunSummary struct {
	Total                       int     `json:"total"`
	Passed                      int     `json:"passed"`
	Failed                      int     `json:"failed"`
	NeedsReview                 int     `json:"needs_review"`
	RouteChecks                 int     `json:"route_checks"`
	RouteMatches                int     `json:"route_matches"`
	TranscriptSamples           int     `json:"transcript_samples"`
	AverageTranscriptSimilarity float64 `json:"average_transcript_similarity"`
	STTP50MS                    int64   `json:"stt_p50_ms"`
	STTP90MS                    int64   `json:"stt_p90_ms"`
	ReplyP50MS                  int64   `json:"reply_p50_ms"`
	ReplyP90MS                  int64   `json:"reply_p90_ms"`
	TotalP50MS                  int64   `json:"total_p50_ms"`
	TotalP90MS                  int64   `json:"total_p90_ms"`
}

type TimingStats struct {
	TotalMS            int64   `json:"total_ms"`
	UploadMS           int64   `json:"upload_ms,omitempty"`
	STTMS              int64   `json:"stt_ms"`
	ReplyMS            int64   `json:"reply_ms"`
	TTSMS              int64   `json:"tts_ms"`
	AudioBytes         int64   `json:"audio_bytes,omitempty"`
	AudioDurationMS    int64   `json:"audio_duration_ms,omitempty"`
	STTAudioDurationMS int64   `json:"stt_audio_duration_ms,omitempty"`
	STTTrimmedMS       int64   `json:"stt_trimmed_ms,omitempty"`
	AudioPeak          float64 `json:"audio_peak,omitempty"`
	AudioRMS           float64 `json:"audio_rms,omitempty"`
}

type conversationEvent struct {
	Time           string `json:"time"`
	TraceID        string `json:"trace_id"`
	Endpoint       string `json:"endpoint"`
	Transcript     string `json:"transcript"`
	Reply          string `json:"reply"`
	Source         string `json:"source"`
	SafetyCategory string `json:"safety_category"`
	ActivityID     string `json:"activity_id"`
	HasRecording   bool   `json:"has_recording"`
	RecordingMIME  string `json:"recording_mime"`
	ParentFeedback string `json:"parent_feedback"`
}

type eventsResponse struct {
	Events []conversationEvent `json:"events"`
}

type voiceResponse struct {
	TraceID    string       `json:"trace_id"`
	Transcript string       `json:"transcript"`
	Reply      string       `json:"reply"`
	Source     string       `json:"source"`
	AIError    string       `json:"ai_error"`
	TTSError   string       `json:"tts_error"`
	Safety     safetyResult `json:"safety"`
	Activity   *activity    `json:"activity"`
	Timings    TimingStats  `json:"timings"`
}

type safetyResult struct {
	Category string `json:"category"`
}

type activity struct {
	ID string `json:"id"`
}
