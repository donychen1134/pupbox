package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type EventStore struct {
	path string
	mu   sync.Mutex
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
	Timings         TimingStats  `json:"timings"`
	Errors          *EventErrors `json:"errors,omitempty"`
}

type EventErrors struct {
	STT       string `json:"stt,omitempty"`
	Chat      string `json:"chat,omitempty"`
	TTS       string `json:"tts,omitempty"`
	Recording string `json:"recording,omitempty"`
}

type eventErrors struct {
	STT       string
	Chat      string
	TTS       string
	Recording string
}

type eventsResponse struct {
	Events []ConversationEvent `json:"events"`
}

func NewEventStore(path string) *EventStore {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &EventStore{path: path}
}

func (s *EventStore) Append(event ConversationEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(event); err != nil {
		return err
	}
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
