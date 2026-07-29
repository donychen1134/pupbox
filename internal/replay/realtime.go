package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
	"github.com/donychen1134/pupbox/internal/qwenrealtime"
)

type RealtimeOptions struct {
	APIKey     string
	URL        string
	Model      string
	Voice      string
	CorpusDir  string
	Report     string
	Limit      int
	RedactText bool
	Now        func() time.Time
	Log        io.Writer
}

type RealtimeReport struct {
	Version    int              `json:"version"`
	RunID      string           `json:"run_id"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Model      string           `json:"model"`
	Voice      string           `json:"voice"`
	Warning    string           `json:"warning"`
	CorpusDir  string           `json:"corpus_dir"`
	Results    []RealtimeResult `json:"results"`
	Summary    RealtimeSummary  `json:"summary"`
}

type RealtimeResult struct {
	ID                   string   `json:"id"`
	Session              string   `json:"session"`
	Order                int      `json:"order"`
	OriginalTranscript   string   `json:"original_transcript,omitempty"`
	Transcript           string   `json:"transcript,omitempty"`
	TranscriptSimilarity float64  `json:"transcript_similarity,omitempty"`
	OriginalReply        string   `json:"original_reply,omitempty"`
	Reply                string   `json:"reply,omitempty"`
	FirstAudioMS         int64    `json:"first_audio_ms,omitempty"`
	TotalMS              int64    `json:"total_ms,omitempty"`
	AudioBytes           int64    `json:"audio_bytes,omitempty"`
	Issues               []string `json:"issues,omitempty"`
	Error                string   `json:"error,omitempty"`
}

type RealtimeSummary struct {
	Total                       int     `json:"total"`
	Succeeded                   int     `json:"succeeded"`
	Failed                      int     `json:"failed"`
	TranscriptSamples           int     `json:"transcript_samples"`
	AverageTranscriptSimilarity float64 `json:"average_transcript_similarity"`
	FirstAudioP50MS             int64   `json:"first_audio_p50_ms"`
	FirstAudioP90MS             int64   `json:"first_audio_p90_ms"`
	TotalP50MS                  int64   `json:"total_p50_ms"`
	TotalP90MS                  int64   `json:"total_p90_ms"`
}

func RunRealtime(ctx context.Context, options RealtimeOptions) (RealtimeReport, string, error) {
	if strings.TrimSpace(options.APIKey) == "" {
		return RealtimeReport{}, "", errors.New("DashScope API key is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Limit <= 0 {
		options.Limit = 5
	}
	if options.Limit > 50 {
		return RealtimeReport{}, "", errors.New("limit must not exceed 50")
	}
	corpusDir, err := filepath.Abs(strings.TrimSpace(options.CorpusDir))
	if err != nil || strings.TrimSpace(options.CorpusDir) == "" {
		return RealtimeReport{}, "", errors.New("valid corpus directory is required")
	}
	entries, err := loadManifest(filepath.Join(corpusDir, "manifest.jsonl"))
	if err != nil {
		return RealtimeReport{}, "", err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Session == entries[j].Session {
			return entries[i].Order < entries[j].Order
		}
		return entries[i].Session < entries[j].Session
	})
	if len(entries) > options.Limit {
		entries = entries[:options.Limit]
	}
	runID, err := newRunID(options.Now())
	if err != nil {
		return RealtimeReport{}, "", err
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = qwenrealtime.DefaultModel
	}
	voice := strings.TrimSpace(options.Voice)
	if voice == "" {
		voice = qwenrealtime.DefaultVoice
	}
	report := RealtimeReport{
		Version: 1, RunID: runID, StartedAt: options.Now().UTC(),
		Model: model, Voice: voice, CorpusDir: corpusDir,
		Warning: "experimental direct audio path; Pupbox safety and deterministic activity routing are bypassed",
		Results: make([]RealtimeResult, 0, len(entries)),
	}
	client := qwenrealtime.New(qwenrealtime.Config{
		APIKey: options.APIKey, URL: options.URL, Model: model, Voice: voice,
		Instructions: dog.Instructions(),
	})
	summaryResults := make([]RealtimeResult, 0, len(entries))

	var session *qwenrealtime.Session
	var activeGroup string
	defer func() {
		if session != nil {
			_ = session.Close()
		}
	}()
	for _, entry := range entries {
		result := RealtimeResult{
			ID: entry.ID, Session: entry.Session, Order: entry.Order,
			OriginalTranscript: entry.OriginalTranscript, OriginalReply: entry.OriginalReply,
		}
		if session == nil || activeGroup != entry.Session {
			if session != nil {
				_ = session.Close()
			}
			session, err = client.Connect(ctx)
			if err != nil {
				result.Error = err.Error()
				report.Results = append(report.Results, redactRealtimeResult(result, options.RedactText))
				summaryResults = append(summaryResults, result)
				logf(options.Log, "%s: realtime connect error: %v\n", entry.ID, err)
				activeGroup = ""
				continue
			}
			activeGroup = entry.Session
		}
		pcm, loadErr := loadReplayPCM(corpusDir, entry)
		if loadErr != nil {
			result.Error = loadErr.Error()
		} else {
			turn, turnErr := session.RunTurn(ctx, pcm)
			if turnErr != nil {
				result.Error = turnErr.Error()
				_ = session.Close()
				session = nil
				activeGroup = ""
			} else {
				result.Transcript = turn.Transcript
				result.Reply = turn.Reply
				result.FirstAudioMS = turn.FirstAudioMS
				result.TotalMS = turn.TotalMS
				result.AudioBytes = turn.AudioBytes
				if result.OriginalTranscript != "" && result.Transcript != "" {
					result.TranscriptSimilarity = textSimilarity(result.OriginalTranscript, result.Transcript)
				}
				if len([]rune(result.Reply)) > 160 {
					result.Issues = append(result.Issues, "reply exceeds 160 characters")
				}
			}
		}
		logf(options.Log, "%s: realtime first_audio=%dms total=%dms error=%s\n", entry.ID, result.FirstAudioMS, result.TotalMS, result.Error)
		summaryResults = append(summaryResults, result)
		report.Results = append(report.Results, redactRealtimeResult(result, options.RedactText))
	}
	report.FinishedAt = options.Now().UTC()
	report.Summary = summarizeRealtime(summaryResults)

	reportPath := strings.TrimSpace(options.Report)
	if reportPath == "" {
		reportPath = filepath.Join(corpusDir, "realtime-report-"+runID+".json")
	}
	reportPath, err = filepath.Abs(reportPath)
	if err != nil {
		return report, "", err
	}
	if err := writePrivateJSON(reportPath, report); err != nil {
		return report, "", err
	}
	return report, reportPath, nil
}

func loadReplayPCM(corpusDir string, entry CorpusEntry) ([]byte, error) {
	path, err := corpusFile(corpusDir, entry.File)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("recording must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(entry.SHA256)) {
		return nil, errors.New("recording SHA256 does not match manifest")
	}
	return qwenrealtime.DecodeWAV16KMono(data)
}

func summarizeRealtime(results []RealtimeResult) RealtimeSummary {
	summary := RealtimeSummary{Total: len(results)}
	var firstAudio, total []int64
	var similarityTotal float64
	for _, result := range results {
		if result.Error == "" {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
		if result.OriginalTranscript != "" && result.Transcript != "" {
			summary.TranscriptSamples++
			similarityTotal += result.TranscriptSimilarity
		}
		if result.FirstAudioMS > 0 {
			firstAudio = append(firstAudio, result.FirstAudioMS)
		}
		if result.TotalMS > 0 {
			total = append(total, result.TotalMS)
		}
	}
	if summary.TranscriptSamples > 0 {
		summary.AverageTranscriptSimilarity = similarityTotal / float64(summary.TranscriptSamples)
	}
	summary.FirstAudioP50MS, summary.FirstAudioP90MS = percentiles(firstAudio)
	summary.TotalP50MS, summary.TotalP90MS = percentiles(total)
	return summary
}

func redactRealtimeResult(result RealtimeResult, redact bool) RealtimeResult {
	if redact {
		result.OriginalTranscript = ""
		result.Transcript = ""
		result.OriginalReply = ""
		result.Reply = ""
	}
	return result
}
