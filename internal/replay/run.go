package replay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type RunOptions struct {
	ServerURL  string
	Token      string
	CorpusDir  string
	Report     string
	RedactText bool
	Client     *http.Client
	Now        func() time.Time
	Log        io.Writer
}

func Run(ctx context.Context, options RunOptions) (RunReport, string, error) {
	base, err := normalizeBaseURL(options.ServerURL)
	if err != nil {
		return RunReport{}, "", err
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 90 * time.Second}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	corpusDir, err := filepath.Abs(strings.TrimSpace(options.CorpusDir))
	if err != nil || strings.TrimSpace(options.CorpusDir) == "" {
		return RunReport{}, "", errors.New("valid corpus directory is required")
	}
	entries, err := loadManifest(filepath.Join(corpusDir, "manifest.jsonl"))
	if err != nil {
		return RunReport{}, "", err
	}
	if len(entries) == 0 {
		return RunReport{}, "", errors.New("manifest contains no recordings")
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Session == entries[j].Session {
			return entries[i].Order < entries[j].Order
		}
		return entries[i].Session < entries[j].Session
	})

	runID, err := newRunID(options.Now())
	if err != nil {
		return RunReport{}, "", err
	}
	report := RunReport{
		Version:   1,
		RunID:     runID,
		StartedAt: options.Now().UTC(),
		TargetURL: base.String(),
		CorpusDir: corpusDir,
		Results:   make([]RunResult, 0, len(entries)),
	}
	summaryResults := make([]RunResult, 0, len(entries))
	for _, entry := range entries {
		result := runEntry(ctx, options.Client, base, options.Token, corpusDir, runID, entry)
		summaryResults = append(summaryResults, result)
		if options.RedactText {
			result.OriginalTranscript = ""
			result.Transcript = ""
			result.OriginalReply = ""
			result.Reply = ""
		}
		report.Results = append(report.Results, result)
		if result.Error != "" {
			logf(options.Log, "%s: error: %s\n", entry.ID, result.Error)
		} else {
			logf(options.Log, "%s: pass=%t source=%s stt=%dms total=%dms\n",
				entry.ID, result.Pass, result.Source, result.Timings.STTMS, result.Timings.TotalMS)
		}
	}
	report.FinishedAt = options.Now().UTC()
	report.Summary = summarizeRun(summaryResults)

	reportPath := strings.TrimSpace(options.Report)
	if reportPath == "" {
		reportPath = filepath.Join(corpusDir, "report-"+runID+".json")
	}
	reportPath, err = filepath.Abs(reportPath)
	if err != nil {
		return report, "", fmt.Errorf("resolve report path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o700); err != nil {
		return report, "", fmt.Errorf("create report directory: %w", err)
	}
	if err := writePrivateJSON(reportPath, report); err != nil {
		return report, "", fmt.Errorf("write report: %w", err)
	}
	return report, reportPath, nil
}

func runEntry(ctx context.Context, client *http.Client, base *url.URL, token, corpusDir, runID string, entry CorpusEntry) RunResult {
	result := RunResult{
		ID:                 entry.ID,
		Session:            entry.Session,
		Order:              entry.Order,
		OriginalTranscript: entry.OriginalTranscript,
		OriginalReply:      entry.OriginalReply,
		OriginalSource:     entry.OriginalSource,
		OriginalActivityID: entry.OriginalActivityID,
		OriginalSafety:     entry.OriginalSafety,
		Expected:           entry.Expected,
		NeedsReview:        entry.ParentFeedback == "missed",
	}
	path, err := corpusFile(corpusDir, entry.File)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	info, err := os.Lstat(path)
	if err != nil {
		result.Error = fmt.Sprintf("inspect recording: %v", err)
		return result
	}
	if !info.Mode().IsRegular() {
		result.Error = "recording must be a regular file"
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.Error = fmt.Sprintf("read recording: %v", err)
		return result
	}
	if len(data) > maxResponseBytes {
		result.Error = "recording exceeds 32 MiB"
		return result
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(entry.SHA256)) {
		result.Error = "recording SHA256 does not match manifest"
		return result
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="audio"; filename="%s"`, escapeQuotes(filepath.Base(path))))
	partHeader.Set("Content-Type", entry.MIME)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if _, err := part.Write(data); err != nil {
		result.Error = err.Error()
		return result
	}
	if err := writer.Close(); err != nil {
		result.Error = err.Error()
		return result
	}

	target := endpointURL(base, "/api/voice") + "?tts=off"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &body)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	addAuth(req, token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Pupbox-Session-ID", replaySessionID(runID, entry.Session))

	started := time.Now()
	resp, err := client.Do(req)
	result.HTTPMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = fmt.Sprintf("send recording: %v", err)
		return result
	}
	responseData, err := readResponse(resp)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var payload voiceResponse
	if err := json.Unmarshal(responseData, &payload); err != nil {
		result.Error = fmt.Sprintf("decode voice response: %v", err)
		return result
	}
	result.Transcript = payload.Transcript
	result.Reply = payload.Reply
	result.Source = payload.Source
	result.AIError = payload.AIError
	result.TTSError = payload.TTSError
	result.Timings = payload.Timings
	result.SafetyCategory = payload.Safety.Category
	if payload.Activity != nil {
		result.ActivityID = payload.Activity.ID
	}
	result.RouteChanged = result.Source != entry.OriginalSource ||
		result.ActivityID != entry.OriginalActivityID ||
		result.SafetyCategory != entry.OriginalSafety
	if entry.OriginalTranscript != "" && payload.Transcript != "" {
		result.TranscriptSimilarity = textSimilarity(entry.OriginalTranscript, payload.Transcript)
	}
	result.Issues = evaluateResult(entry, result)
	result.Pass = len(result.Issues) == 0
	return result
}

func evaluateResult(entry CorpusEntry, result RunResult) []string {
	var issues []string
	if strings.TrimSpace(result.Transcript) == "" {
		issues = append(issues, "empty transcript")
	}
	if strings.TrimSpace(result.Reply) == "" {
		issues = append(issues, "empty reply")
	}
	if result.AIError != "" {
		issues = append(issues, "chat provider error")
	}
	if entry.Expected.Source != "" && result.Source != entry.Expected.Source {
		issues = append(issues, fmt.Sprintf("source mismatch: want %s", entry.Expected.Source))
	}
	if entry.Expected.ActivityID != "" && result.ActivityID != entry.Expected.ActivityID {
		issues = append(issues, fmt.Sprintf("activity mismatch: want %s", entry.Expected.ActivityID))
	}
	if entry.Expected.SafetyCategory != "" && result.SafetyCategory != entry.Expected.SafetyCategory {
		issues = append(issues, fmt.Sprintf("safety mismatch: want %s", entry.Expected.SafetyCategory))
	}
	if utf8.RuneCountInString(result.Reply) > 160 {
		issues = append(issues, "reply exceeds 160 characters")
	}
	return issues
}

func loadManifest(path string) ([]CorpusEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	var entries []CorpusEntry
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		var entry CorpusEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("decode manifest line %d: %w", line, err)
		}
		if entry.ID == "" || entry.File == "" || entry.Session == "" || entry.Order <= 0 || entry.SHA256 == "" {
			return nil, fmt.Errorf("manifest line %d is missing required fields", line)
		}
		if seen[entry.ID] {
			return nil, fmt.Errorf("duplicate recording ID %q", entry.ID)
		}
		seen[entry.ID] = true
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return entries, nil
}

func corpusFile(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", errors.New("recording path must be relative")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("recording path escapes corpus directory")
	}
	return path, nil
}

func replaySessionID(runID, session string) string {
	var builder strings.Builder
	builder.WriteString("replay-")
	builder.WriteString(runID)
	builder.WriteByte('-')
	for _, r := range session {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	value := builder.String()
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}

func newRunID(now time.Time) (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create run ID: %w", err)
	}
	return now.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(random), nil
}

func summarizeRun(results []RunResult) RunSummary {
	summary := RunSummary{Total: len(results)}
	var stt, reply, total []int64
	var similarities float64
	for _, result := range results {
		if result.Pass {
			summary.Passed++
		} else {
			summary.Failed++
		}
		if result.NeedsReview {
			summary.NeedsReview++
		}
		if result.Expected.Source != "" || result.Expected.ActivityID != "" || result.Expected.SafetyCategory != "" {
			summary.RouteChecks++
			if !hasRouteMismatch(result.Issues) {
				summary.RouteMatches++
			}
		}
		if result.OriginalTranscript != "" && result.Transcript != "" {
			summary.TranscriptSamples++
			similarities += result.TranscriptSimilarity
		}
		if result.Timings.STTMS > 0 {
			stt = append(stt, result.Timings.STTMS)
		}
		if result.Timings.ReplyMS > 0 {
			reply = append(reply, result.Timings.ReplyMS)
		}
		if result.Timings.TotalMS > 0 {
			total = append(total, result.Timings.TotalMS)
		}
	}
	if summary.TranscriptSamples > 0 {
		summary.AverageTranscriptSimilarity = similarities / float64(summary.TranscriptSamples)
	}
	summary.STTP50MS, summary.STTP90MS = percentiles(stt)
	summary.ReplyP50MS, summary.ReplyP90MS = percentiles(reply)
	summary.TotalP50MS, summary.TotalP90MS = percentiles(total)
	return summary
}

func hasRouteMismatch(issues []string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, " mismatch:") {
			return true
		}
	}
	return false
}

func percentiles(values []int64) (int64, int64) {
	if len(values) == 0 {
		return 0, 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	at := func(percent int) int64 {
		index := (len(values)*percent + 99) / 100
		if index < 1 {
			index = 1
		}
		return values[index-1]
	}
	return at(50), at(90)
}

func textSimilarity(left, right string) float64 {
	a := normalizedRunes(left)
	b := normalizedRunes(right)
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	maxLength := len(a)
	if len(b) > maxLength {
		maxLength = len(b)
	}
	return 1 - float64(editDistance(a, b))/float64(maxLength)
}

func normalizedRunes(value string) []rune {
	var output []rune
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			output = append(output, r)
		}
	}
	return output
}

func editDistance(left, right []rune) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, a := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, b := range right {
			cost := 0
			if a != b {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func escapeQuotes(value string) string {
	return strings.NewReplacer("\\", "_", `"`, "_", "\r", "_", "\n", "_").Replace(value)
}
