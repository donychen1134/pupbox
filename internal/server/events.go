package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const defaultEventLimit = 500

type EventStore struct {
	path        string
	limit       int
	mu          sync.Mutex
	initialized bool
	count       int
	readyErr    error
}

type ConversationEvent struct {
	Time            string       `json:"time"`
	TraceID         string       `json:"trace_id"`
	Endpoint        string       `json:"endpoint"`
	Transcript      string       `json:"transcript"`
	Reply           string       `json:"reply"`
	Mode            string       `json:"mode"`
	Source          string       `json:"source"`
	SafetyTriggered bool         `json:"safety_triggered"`
	SafetyCategory  string       `json:"safety_category,omitempty"`
	ActivityID      string       `json:"activity_id,omitempty"`
	ActivityLabel   string       `json:"activity_label,omitempty"`
	HasRecording    bool         `json:"has_recording,omitempty"`
	RecordingMIME   string       `json:"recording_mime,omitempty"`
	TTSCache        string       `json:"tts_cache,omitempty"`
	ParentFeedback  string       `json:"parent_feedback,omitempty"`
	FeedbackAt      string       `json:"feedback_at,omitempty"`
	Timings         TimingStats  `json:"timings"`
	Errors          *EventErrors `json:"errors,omitempty"`
}

type EventErrors struct {
	STT       string `json:"stt,omitempty"`
	Chat      string `json:"chat,omitempty"`
	TTS       string `json:"tts,omitempty"`
	Recording string `json:"recording,omitempty"`
	Playback  string `json:"playback,omitempty"`
}

type eventErrors struct {
	STT       string
	Chat      string
	TTS       string
	Recording string
}

type eventsResponse struct {
	Events  []ConversationEvent `json:"events"`
	Summary EventSummary        `json:"summary"`
}

type EventSummary struct {
	SampleSize      int             `json:"sample_size"`
	RatedCount      int             `json:"rated_count"`
	TTSCacheSamples int             `json:"tts_cache_samples"`
	TTSCacheHits    int             `json:"tts_cache_hits"`
	TTSCacheHitRate float64         `json:"tts_cache_hit_rate"`
	WaitFirstAudio  PercentileStats `json:"wait_first_audio"`
	TurnTotal       PercentileStats `json:"turn_total"`
	STT             PercentileStats `json:"stt"`
	Reply           PercentileStats `json:"reply"`
	TTSFirstAudio   PercentileStats `json:"tts_first_audio"`
}

type PercentileStats struct {
	Samples int   `json:"samples"`
	P50MS   int64 `json:"p50_ms"`
	P90MS   int64 `json:"p90_ms"`
}

func NewEventStore(path string, limits ...int) *EventStore {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	limit := defaultEventLimit
	if len(limits) > 0 && limits[0] > 0 {
		limit = limits[0]
	}
	return &EventStore{path: path, limit: limit}
}

func (s *EventStore) Ensure() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked()
}

func (s *EventStore) Status() (string, bool, int, error) {
	if s == nil {
		return "", false, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		_ = s.ensureLocked()
	}
	return s.path, s.readyErr == nil, s.count, s.readyErr
}

func (s *EventStore) Append(event ConversationEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLocked(); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.readyErr = err
		return err
	}

	if err := json.NewEncoder(file).Encode(event); err != nil {
		file.Close()
		s.readyErr = err
		return err
	}
	if err := file.Close(); err != nil {
		s.readyErr = err
		return err
	}
	s.count++
	if s.count > s.limit {
		if err := s.pruneLocked(); err != nil {
			s.readyErr = err
			return err
		}
	}
	s.readyErr = nil
	return nil
}

func (s *EventStore) Recent(limit int) ([]ConversationEvent, error) {
	if s == nil {
		return []ConversationEvent{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []ConversationEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, limit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(lines) == limit {
			copy(lines, lines[1:])
			lines[len(lines)-1] = line
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	events := make([]ConversationEvent, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var event ConversationEvent
		if err := json.Unmarshal([]byte(lines[i]), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func summarizeEvents(events []ConversationEvent) EventSummary {
	summary := EventSummary{SampleSize: len(events)}
	waitFirstAudio := make([]int64, 0, len(events))
	turnTotal := make([]int64, 0, len(events))
	stt := make([]int64, 0, len(events))
	reply := make([]int64, 0, len(events))
	ttsFirstAudio := make([]int64, 0, len(events))
	for _, event := range events {
		if event.ParentFeedback != "" {
			summary.RatedCount++
		}
		if hit, known := ttsCacheResult(event.TTSCache); known {
			summary.TTSCacheSamples++
			if hit {
				summary.TTSCacheHits++
			}
		}
		appendPositive := func(values *[]int64, value int64) {
			if value > 0 {
				*values = append(*values, value)
			}
		}
		appendPositive(&turnTotal, event.Timings.TurnTotalMS)
		appendPositive(&stt, event.Timings.STTMS)
		if event.Source == "dashscope" || event.Source == "openai" {
			appendPositive(&reply, event.Timings.ReplyMS)
		}
		appendPositive(&ttsFirstAudio, event.Timings.TTSFirstAudioMS)
		if event.Timings.PlaybackMS > 0 {
			wait := event.Timings.TurnTotalMS - event.Timings.PlaybackMS - event.Timings.AudioDurationMS
			appendPositive(&waitFirstAudio, wait)
		}
	}
	if summary.TTSCacheSamples > 0 {
		summary.TTSCacheHitRate = float64(summary.TTSCacheHits) / float64(summary.TTSCacheSamples)
	}
	summary.WaitFirstAudio = percentileStats(waitFirstAudio)
	summary.TurnTotal = percentileStats(turnTotal)
	summary.STT = percentileStats(stt)
	summary.Reply = percentileStats(reply)
	summary.TTSFirstAudio = percentileStats(ttsFirstAudio)
	return summary
}

func ttsCacheResult(value string) (hit, known bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stream", "complete", "memory", "disk", "coalesced":
		return true, true
	case "miss", "fallback", "browser-fallback":
		return false, true
	default:
		return false, false
	}
}

func percentileStats(values []int64) PercentileStats {
	if len(values) == 0 {
		return PercentileStats{}
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	nearestRank := func(percent int) int64 {
		index := (percent*len(sorted) + 99) / 100
		if index < 1 {
			index = 1
		}
		return sorted[index-1]
	}
	return PercentileStats{Samples: len(sorted), P50MS: nearestRank(50), P90MS: nearestRank(90)}
}

func (s *EventStore) Update(traceID string, update func(*ConversationEvent)) (bool, error) {
	if s == nil || strings.TrimSpace(traceID) == "" || update == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return false, err
	}

	file, err := os.Open(s.path)
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, s.count)
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event ConversationEvent
		if err := json.Unmarshal([]byte(line), &event); err == nil && event.TraceID == traceID {
			update(&event)
			encoded, encodeErr := json.Marshal(event)
			if encodeErr != nil {
				file.Close()
				return false, encodeErr
			}
			line = string(encoded)
			found = true
		}
		lines = append(lines, line)
	}
	closeErr := file.Close()
	if err := scanner.Err(); err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	if !found {
		return false, nil
	}
	if err := s.writeLinesLocked(lines); err != nil {
		s.readyErr = err
		return false, err
	}
	s.count = len(lines)
	s.readyErr = nil
	return true, nil
}

func (s *EventStore) ensureLocked() error {
	if s.initialized {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.readyErr = err
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.readyErr = err
		return err
	}
	if err := file.Close(); err != nil {
		s.readyErr = err
		return err
	}
	count, err := countEventLines(s.path)
	if err != nil {
		s.readyErr = err
		return err
	}
	s.count = count
	if s.count > s.limit {
		if err := s.pruneLocked(); err != nil {
			s.readyErr = err
			return err
		}
	}
	s.initialized = true
	s.readyErr = nil
	return nil
}

func (s *EventStore) pruneLocked() error {
	file, err := os.Open(s.path)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, s.limit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(lines) == s.limit {
			copy(lines, lines[1:])
			lines[len(lines)-1] = line
		} else {
			lines = append(lines, line)
		}
	}
	closeErr := file.Close()
	if err := scanner.Err(); err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	if err := s.writeLinesLocked(lines); err != nil {
		return err
	}
	s.count = len(lines)
	s.readyErr = nil
	return nil
}

func (s *EventStore) writeLinesLocked(lines []string) error {
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".events-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	writer := bufio.NewWriter(tmp)
	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	return nil
}

func countEventLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, scanner.Err()
}
