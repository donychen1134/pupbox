package replay

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCollectDownloadsChronologicalPrivateCorpus(t *testing.T) {
	const token = "test-access-token"
	recordings := map[string][]byte{
		"abcd-1": []byte("first-audio"),
		"abcd-2": []byte("second-audio"),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/events":
			if r.URL.Query().Get("limit") != "50" {
				t.Errorf("limit = %q", r.URL.Query().Get("limit"))
			}
			_ = json.NewEncoder(w).Encode(eventsResponse{Events: []conversationEvent{
				{
					Time: "2026-07-24T10:05:00Z", TraceID: "abcd-2", Endpoint: "voice",
					Transcript: "玩猜动物", Reply: "旧回复二", Source: "activity:intent",
					ActivityID: "animal_guess", HasRecording: true, RecordingMIME: "audio/wav",
					ParentFeedback: "missed",
				},
				{
					Time: "2026-07-24T10:01:00Z", TraceID: "abcd-1", Endpoint: "voice",
					Transcript: "你好豆豆", Reply: "旧回复一", Source: "dashscope",
					HasRecording: true, RecordingMIME: "audio/wav", ParentFeedback: "good",
				},
				{
					Time: "2026-07-24T09:00:00Z", TraceID: "text-1", Endpoint: "chat",
					HasRecording: false,
				},
			}})
		case strings.HasPrefix(r.URL.Path, "/api/recordings/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/recordings/")
			data, ok := recordings[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	output := filepath.Join(t.TempDir(), "private-corpus")
	result, err := Collect(context.Background(), CollectOptions{
		ServerURL: server.URL,
		Token:     token,
		OutputDir: output,
		Limit:     50,
		Feedback:  "all",
		GroupGap:  5 * time.Minute,
		Client:    server.Client(),
		Now:       func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if result.Collected != 2 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	entries := readTestManifest(t, filepath.Join(output, "manifest.jsonl"))
	if len(entries) != 2 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].ID != "abcd-1" || entries[0].Session != "session-001" || entries[0].Order != 1 {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].ID != "abcd-2" || entries[1].Session != "session-001" || entries[1].Order != 2 {
		t.Fatalf("second entry = %+v", entries[1])
	}
	if entries[0].Expected.Source != "dashscope" {
		t.Fatalf("good expected route = %+v", entries[0].Expected)
	}
	if entries[1].Expected != (ExpectedRoute{}) {
		t.Fatalf("missed expected route = %+v, want empty", entries[1].Expected)
	}
	assertPrivateMode(t, output, 0o700)
	assertPrivateMode(t, filepath.Join(output, "manifest.jsonl"), 0o600)
	assertPrivateMode(t, filepath.Join(output, entries[0].File), 0o600)
}

func TestRunReplaysSessionAndWritesReport(t *testing.T) {
	const token = "test-access-token"
	corpusDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(corpusDir, "audio"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries := []CorpusEntry{
		testCorpusEntry(t, corpusDir, "case-1", "session-001", 1, "你好豆豆", "dashscope", ""),
		testCorpusEntry(t, corpusDir, "case-2", "session-001", 2, "猜动物", "activity:intent", "animal_guess"),
	}
	if err := writeManifest(filepath.Join(corpusDir, "manifest.jsonl"), entries); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var sessions []string
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/voice" || r.URL.Query().Get("tts") != "off" {
			http.Error(w, "bad target", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = io.ReadAll(file)
		file.Close()

		mu.Lock()
		requests++
		index := requests
		sessions = append(sessions, r.Header.Get("X-Pupbox-Session-ID"))
		mu.Unlock()

		response := voiceResponse{
			Transcript: "你好豆豆",
			Reply:      "你好呀。",
			Source:     "dashscope",
			Timings:    TimingStats{STTMS: 100, ReplyMS: 200, TotalMS: 300},
		}
		if index == 2 {
			response.Transcript = "猜动物"
			response.Reply = "我来出题。"
			response.Source = "activity:intent"
			response.Activity = &activity{ID: "animal_guess"}
			response.Timings = TimingStats{STTMS: 200, ReplyMS: 300, TotalMS: 500}
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	reportPath := filepath.Join(t.TempDir(), "report.json")
	report, path, err := Run(context.Background(), RunOptions{
		ServerURL: server.URL,
		Token:     token,
		CorpusDir: corpusDir,
		Report:    reportPath,
		Client:    server.Client(),
		Now:       func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if path != reportPath {
		t.Fatalf("report path = %q", path)
	}
	if report.Summary.Total != 2 || report.Summary.Passed != 2 || report.Summary.RouteMatches != 2 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Summary.STTP50MS != 100 || report.Summary.STTP90MS != 200 {
		t.Fatalf("STT percentiles = %d/%d", report.Summary.STTP50MS, report.Summary.STTP90MS)
	}
	if len(sessions) != 2 || sessions[0] == "" || sessions[0] != sessions[1] {
		t.Fatalf("sessions = %#v", sessions)
	}
	assertPrivateMode(t, reportPath, 0o600)
}

func TestRunMarksParentMissedCaseForReview(t *testing.T) {
	entry := CorpusEntry{ParentFeedback: "missed"}
	result := RunResult{Transcript: "听到了", Reply: "回答"}
	result.NeedsReview = entry.ParentFeedback == "missed"
	result.Issues = evaluateResult(entry, result)
	result.Pass = len(result.Issues) == 0
	summary := summarizeRun([]RunResult{result})
	if !result.Pass || summary.NeedsReview != 1 {
		t.Fatalf("result=%+v summary=%+v", result, summary)
	}
}

func TestCorpusFileRejectsTraversal(t *testing.T) {
	if _, err := corpusFile(t.TempDir(), "../private.wav"); err == nil {
		t.Fatal("corpusFile accepted path traversal")
	}
}

func TestRunRejectsRecordingSymlink(t *testing.T) {
	corpusDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(corpusDir, "audio"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private.wav")
	if err := os.WriteFile(target, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(corpusDir, "audio", "linked.wav")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	entry := CorpusEntry{ID: "case-1", File: "audio/linked.wav", SHA256: strings.Repeat("0", 64)}
	result := runEntry(context.Background(), http.DefaultClient, nil, "", corpusDir, "run", entry)
	if result.Error != "recording must be a regular file" {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestTextSimilarity(t *testing.T) {
	if got := textSimilarity("豆豆，你好！", "豆豆你好"); got != 1 {
		t.Fatalf("similarity = %v, want 1", got)
	}
	if got := textSimilarity("猜动物", "叔叔"); got >= 0.5 {
		t.Fatalf("unrelated similarity = %v", got)
	}
}

func testCorpusEntry(t *testing.T, corpusDir, id, session string, order int, transcript, source, activityID string) CorpusEntry {
	t.Helper()
	data := []byte("audio-" + id)
	relative := filepath.ToSlash(filepath.Join("audio", id+".wav"))
	if err := os.WriteFile(filepath.Join(corpusDir, relative), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return CorpusEntry{
		ID:                 id,
		File:               relative,
		SHA256:             hex.EncodeToString(sum[:]),
		MIME:               "audio/wav",
		Session:            session,
		Order:              order,
		OriginalTranscript: transcript,
		Expected:           ExpectedRoute{Source: source, ActivityID: activityID},
	}
}

func readTestManifest(t *testing.T, path string) []CorpusEntry {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var entries []CorpusEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry CorpusEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return entries
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func Example() {
	fmt.Println("recordings remain outside the repository")
	// Output: recordings remain outside the repository
}
