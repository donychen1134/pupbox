package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type DeviceTelemetrySample struct {
	Time             string  `json:"time"`
	ChipTemperatureC float64 `json:"chip_temperature_c,omitempty"`
	TemperatureKnown bool    `json:"temperature_known"`
	Volume           int     `json:"volume,omitempty"`
	VolumeKnown      bool    `json:"volume_known"`
	Battery          int     `json:"battery,omitempty"`
	BatteryKnown     bool    `json:"battery_known"`
	Charging         bool    `json:"charging"`
}

type DeviceTelemetrySummary struct {
	SampleSize       int     `json:"sample_size"`
	TemperatureCount int     `json:"temperature_count"`
	CurrentC         float64 `json:"current_c,omitempty"`
	MinimumC         float64 `json:"minimum_c,omitempty"`
	MaximumC         float64 `json:"maximum_c,omitempty"`
}

type deviceTelemetryResponse struct {
	Samples []DeviceTelemetrySample `json:"samples"`
	Summary DeviceTelemetrySummary  `json:"summary"`
}

type DeviceTelemetryStore struct {
	path        string
	limit       int
	mu          sync.Mutex
	initialized bool
	count       int
}

func NewDeviceTelemetryStore(path string, limit int) *DeviceTelemetryStore {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if limit <= 0 {
		limit = 2880
	}
	return &DeviceTelemetryStore{path: path, limit: limit}
}

func (s *DeviceTelemetryStore) Ensure() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked()
}

func (s *DeviceTelemetryStore) Append(sample DeviceTelemetrySample) error {
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
		return err
	}
	if sample.Time == "" {
		sample.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	encodeErr := json.NewEncoder(file).Encode(sample)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	s.count++
	return s.pruneLocked()
}

func (s *DeviceTelemetryStore) Recent(limit int) ([]DeviceTelemetrySample, error) {
	if s == nil {
		return []DeviceTelemetrySample{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > s.limit {
		limit = min(120, s.limit)
	}
	lines, err := readRecentLines(s.path, limit)
	if err != nil {
		return nil, err
	}
	samples := make([]DeviceTelemetrySample, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var sample DeviceTelemetrySample
		if json.Unmarshal([]byte(lines[i]), &sample) == nil {
			samples = append(samples, sample)
		}
	}
	return samples, nil
}

func summarizeDeviceTelemetry(samples []DeviceTelemetrySample) DeviceTelemetrySummary {
	summary := DeviceTelemetrySummary{SampleSize: len(samples)}
	for _, sample := range samples {
		if !sample.TemperatureKnown {
			continue
		}
		if summary.TemperatureCount == 0 || sample.ChipTemperatureC < summary.MinimumC {
			summary.MinimumC = sample.ChipTemperatureC
		}
		if summary.TemperatureCount == 0 || sample.ChipTemperatureC > summary.MaximumC {
			summary.MaximumC = sample.ChipTemperatureC
		}
		summary.TemperatureCount++
	}
	for _, sample := range samples {
		if sample.TemperatureKnown {
			summary.CurrentC = sample.ChipTemperatureC
			break
		}
	}
	return summary
}

func (s *DeviceTelemetryStore) ensureLocked() error {
	if s.initialized {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	count, err := countEventLines(s.path)
	if err != nil {
		return err
	}
	s.count = count
	s.initialized = true
	return s.pruneLocked()
}

func (s *DeviceTelemetryStore) pruneLocked() error {
	extra := min(128, max(1, s.limit/10))
	if s.count < s.limit+extra {
		return nil
	}
	lines, err := readRecentLines(s.path, s.limit+1)
	if err != nil {
		return err
	}
	if len(lines) <= s.limit {
		s.count = len(lines)
		return nil
	}
	lines = lines[len(lines)-s.limit:]
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.count = len(lines)
	return nil
}

func readRecentLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 16*1024), 256*1024)
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
	return lines, scanner.Err()
}
