package replay

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var validTraceID = regexp.MustCompile(`^[a-f0-9-]{4,80}$`)

type CollectOptions struct {
	ServerURL string
	Token     string
	OutputDir string
	Limit     int
	Feedback  string
	GroupGap  time.Duration
	Client    *http.Client
	Now       func() time.Time
	Log       io.Writer
}

func Collect(ctx context.Context, options CollectOptions) (CollectResult, error) {
	base, err := normalizeBaseURL(options.ServerURL)
	if err != nil {
		return CollectResult{}, err
	}
	if options.Limit <= 0 || options.Limit > 200 {
		return CollectResult{}, errors.New("limit must be between 1 and 200")
	}
	if options.GroupGap <= 0 {
		options.GroupGap = 5 * time.Minute
	}
	if err := validateFeedbackFilter(options.Feedback); err != nil {
		return CollectResult{}, err
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if strings.TrimSpace(options.OutputDir) == "" {
		return CollectResult{}, errors.New("output directory is required")
	}

	var payload eventsResponse
	eventsURL := endpointURL(base, "/api/events") + "?limit=" + strconv.Itoa(options.Limit)
	if err := getJSON(ctx, options.Client, eventsURL, options.Token, &payload); err != nil {
		return CollectResult{}, fmt.Errorf("fetch events: %w", err)
	}

	events := make([]conversationEvent, 0, len(payload.Events))
	for _, event := range payload.Events {
		if event.Endpoint != "voice" || !event.HasRecording || !feedbackMatches(event.ParentFeedback, options.Feedback) {
			continue
		}
		if !validTraceID.MatchString(event.TraceID) {
			logf(options.Log, "skip invalid trace ID\n")
			continue
		}
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return eventTime(events[i]).Before(eventTime(events[j]))
	})
	if len(events) == 0 {
		return CollectResult{}, errors.New("no matching diagnostic recordings were found")
	}

	outDir, err := filepath.Abs(options.OutputDir)
	if err != nil {
		return CollectResult{}, fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Stat(outDir); err == nil {
		return CollectResult{}, fmt.Errorf("output directory already exists: %s", outDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return CollectResult{}, fmt.Errorf("inspect output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outDir), 0o700); err != nil {
		return CollectResult{}, fmt.Errorf("create output parent: %w", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(outDir), "."+filepath.Base(outDir)+".tmp-")
	if err != nil {
		return CollectResult{}, fmt.Errorf("create temporary corpus: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		return CollectResult{}, err
	}
	audioDir := filepath.Join(tmpDir, "audio")
	if err := os.Mkdir(audioDir, 0o700); err != nil {
		return CollectResult{}, err
	}

	entries := make([]CorpusEntry, 0, len(events))
	skipped := 0
	group := 0
	order := 0
	var previous time.Time
	for _, event := range events {
		recordedAt := eventTime(event)
		if group == 0 || previous.IsZero() || recordedAt.IsZero() || recordedAt.Sub(previous) > options.GroupGap {
			group++
			order = 0
		}
		previous = recordedAt

		data, contentType, err := downloadRecording(ctx, options.Client, base, options.Token, event.TraceID)
		if err != nil {
			skipped++
			logf(options.Log, "skip %s: %v\n", event.TraceID, err)
			continue
		}
		order++
		mimeType := normalizedMIME(event.RecordingMIME, contentType)
		relativeFile := filepath.Join("audio", event.TraceID+extensionForMIME(mimeType))
		if err := os.WriteFile(filepath.Join(tmpDir, relativeFile), data, 0o600); err != nil {
			return CollectResult{}, fmt.Errorf("write recording %s: %w", event.TraceID, err)
		}
		sum := sha256.Sum256(data)
		entry := CorpusEntry{
			ID:                 event.TraceID,
			File:               filepath.ToSlash(relativeFile),
			SHA256:             hex.EncodeToString(sum[:]),
			MIME:               mimeType,
			RecordedAt:         event.Time,
			Session:            fmt.Sprintf("session-%03d", group),
			Order:              order,
			OriginalTranscript: event.Transcript,
			OriginalReply:      event.Reply,
			OriginalSource:     event.Source,
			OriginalActivityID: event.ActivityID,
			OriginalSafety:     event.SafetyCategory,
			ParentFeedback:     event.ParentFeedback,
		}
		if event.ParentFeedback == "good" || event.ParentFeedback == "too_long" {
			entry.Expected = ExpectedRoute{
				Source:         event.Source,
				ActivityID:     event.ActivityID,
				SafetyCategory: event.SafetyCategory,
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return CollectResult{}, errors.New("matching events exist, but none of their recordings could be downloaded")
	}
	if err := writeManifest(filepath.Join(tmpDir, "manifest.jsonl"), entries); err != nil {
		return CollectResult{}, err
	}
	metadata := CorpusMetadata{
		Version:    CorpusVersion,
		CreatedAt:  options.Now().UTC(),
		SourceURL:  base.String(),
		EntryCount: len(entries),
		GroupGapMS: options.GroupGap.Milliseconds(),
	}
	if err := writePrivateJSON(filepath.Join(tmpDir, "corpus.json"), metadata); err != nil {
		return CollectResult{}, err
	}
	if err := os.Rename(tmpDir, outDir); err != nil {
		return CollectResult{}, fmt.Errorf("publish corpus: %w", err)
	}
	return CollectResult{OutputDir: outDir, Collected: len(entries), Skipped: skipped}, nil
}

func downloadRecording(ctx context.Context, client *http.Client, base *url.URL, token, traceID string) ([]byte, string, error) {
	target := endpointURL(base, "/api/recordings/") + url.PathEscape(traceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", err
	}
	addAuth(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	contentType := resp.Header.Get("Content-Type")
	data, err := readResponse(resp)
	return data, contentType, err
}

func writeManifest(path string, entries []CorpusEntry) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			file.Close()
			return fmt.Errorf("encode manifest: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func writePrivateJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func eventTime(event conversationEvent) time.Time {
	value, _ := time.Parse(time.RFC3339Nano, event.Time)
	return value
}

func normalizedMIME(eventMIME, responseMIME string) string {
	for _, value := range []string{eventMIME, responseMIME} {
		value = strings.TrimSpace(strings.Split(value, ";")[0])
		if strings.HasPrefix(value, "audio/") {
			return value
		}
	}
	return "audio/wav"
}

func extensionForMIME(mimeType string) string {
	switch mimeType {
	case "audio/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav", "audio/vnd.wave":
		return ".wav"
	}
	if extensions, _ := mime.ExtensionsByType(mimeType); len(extensions) > 0 {
		return extensions[0]
	}
	return ".bin"
}

func validateFeedbackFilter(value string) error {
	switch strings.TrimSpace(value) {
	case "", "all", "rated", "good", "missed", "too_long":
		return nil
	default:
		return errors.New("feedback must be all, rated, good, missed, or too_long")
	}
}

func feedbackMatches(feedback, filter string) bool {
	switch strings.TrimSpace(filter) {
	case "", "all":
		return true
	case "rated":
		return feedback != ""
	default:
		return feedback == filter
	}
}

func logf(writer io.Writer, format string, args ...any) {
	if writer != nil {
		fmt.Fprintf(writer, format, args...)
	}
}
