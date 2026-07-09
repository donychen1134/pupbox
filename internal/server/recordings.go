package server

import (
	"errors"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const defaultRecordingLimit = 20

var validRecordingID = regexp.MustCompile(`^[a-f0-9-]{4,80}$`)

type RecordingStore struct {
	dir   string
	limit int
}

type RecordingMeta struct {
	TraceID string
	MIME    string
	Size    int64
}

func NewRecordingStore(dir string, limit int) *RecordingStore {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultRecordingLimit
	}
	return &RecordingStore{dir: dir, limit: limit}
}

func (s *RecordingStore) Save(traceID string, audio []byte, filename, contentType string) (*RecordingMeta, error) {
	if s == nil || len(audio) == 0 || !validRecordingID.MatchString(traceID) {
		return nil, nil
	}
	mimeType := normalizeRecordingMIME(filename, contentType)
	path := filepath.Join(s.dir, traceID+recordingExtension(filename, mimeType))

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.dir, "."+traceID+".*.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(audio); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, err
	}
	if err := s.prune(); err != nil {
		return nil, err
	}
	return &RecordingMeta{TraceID: traceID, MIME: mimeType, Size: int64(len(audio))}, nil
}

func (s *RecordingStore) Find(traceID string) (string, string, error) {
	if s == nil || !validRecordingID.MatchString(traceID) {
		return "", "", os.ErrNotExist
	}
	matches, err := filepath.Glob(filepath.Join(s.dir, traceID+".*"))
	if err != nil {
		return "", "", err
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, normalizeRecordingMIME(path, mime.TypeByExtension(filepath.Ext(path))), nil
		}
	}
	return "", "", os.ErrNotExist
}

func (s *RecordingStore) prune() error {
	if s == nil || s.limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && !info.IsDir() {
			files = append(files, info)
		}
	}
	if len(files) <= s.limit {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})
	for _, info := range files[:len(files)-s.limit] {
		if err := os.Remove(filepath.Join(s.dir, info.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func normalizeRecordingMIME(filename, contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "audio/x-wav" {
		return "audio/wav"
	}
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return normalizeRecordingMIME("", strings.Split(mimeType, ";")[0])
		}
	}
	return "audio/wav"
}

func recordingExtension(filename, mimeType string) string {
	switch strings.ToLower(strings.Split(mimeType, ";")[0]) {
	case "audio/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" && len(ext) <= 8 {
		return ext
	}
	return ".wav"
}
