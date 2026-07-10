package server

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	speechCacheMagic        = "PUPBOX-TTS-V1\n"
	maxCachedSpeechBytes    = 12 << 20
	defaultSpeechCacheLimit = 512
)

type SpeechDiskCache struct {
	dir      string
	limit    int
	mu       sync.Mutex
	readyErr error
}

func NewSpeechDiskCache(dir string, limit int) *SpeechDiskCache {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultSpeechCacheLimit
	}
	return &SpeechDiskCache{dir: dir, limit: limit}
}

func (c *SpeechDiskCache) Ensure() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensureLocked()
}

func (c *SpeechDiskCache) Status() (string, bool, int, error) {
	if c == nil {
		return "", false, 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return c.dir, false, 0, err
	}
	entries, err := c.entriesLocked()
	if err != nil {
		c.readyErr = err
		return c.dir, false, 0, err
	}
	return c.dir, true, len(entries), nil
}

func (c *SpeechDiskCache) Get(key string) ([]byte, string, bool, error) {
	if c == nil {
		return nil, "", false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return nil, "", false, err
	}
	data, err := os.ReadFile(c.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	audio, mime, err := decodeCachedSpeech(data)
	if err != nil {
		_ = os.Remove(c.path(key))
		return nil, "", false, err
	}
	return audio, mime, true, nil
}

func (c *SpeechDiskCache) Put(key, mime string, audio []byte) error {
	if c == nil || len(audio) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return err
	}
	if err := c.pruneLocked(key); err != nil {
		return err
	}

	payload, err := encodeCachedSpeech(mime, audio)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".speech-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, c.path(key)); err != nil {
		return err
	}
	c.readyErr = nil
	return nil
}

func (c *SpeechDiskCache) ensureLocked() error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		c.readyErr = err
		return err
	}
	if err := os.Chmod(c.dir, 0o700); err != nil {
		c.readyErr = err
		return err
	}
	c.readyErr = nil
	return nil
}

func (c *SpeechDiskCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, fmt.Sprintf("%x.tts", sum))
}

func (c *SpeechDiskCache) pruneLocked(incomingKey string) error {
	if _, err := os.Stat(c.path(incomingKey)); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := c.entriesLocked()
	if err != nil {
		return err
	}
	removeCount := len(entries) - c.limit + 1
	if removeCount <= 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].info.ModTime().Before(entries[j].info.ModTime())
	})
	for _, entry := range entries[:removeCount] {
		if err := os.Remove(filepath.Join(c.dir, entry.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

type speechCacheEntry struct {
	name string
	info os.FileInfo
}

func (c *SpeechDiskCache) entriesLocked() ([]speechCacheEntry, error) {
	dirEntries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	entries := make([]speechCacheEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tts" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		entries = append(entries, speechCacheEntry{name: entry.Name(), info: info})
	}
	return entries, nil
}

func encodeCachedSpeech(mime string, audio []byte) ([]byte, error) {
	mime = strings.TrimSpace(mime)
	if mime == "" || len(mime) > 255 {
		return nil, errors.New("invalid cached speech MIME type")
	}
	if len(audio) == 0 || len(audio) > maxCachedSpeechBytes {
		return nil, errors.New("invalid cached speech audio size")
	}
	payload := make([]byte, len(speechCacheMagic)+2+len(mime)+len(audio))
	copy(payload, speechCacheMagic)
	offset := len(speechCacheMagic)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(mime)))
	offset += 2
	copy(payload[offset:], mime)
	offset += len(mime)
	copy(payload[offset:], audio)
	return payload, nil
}

func decodeCachedSpeech(payload []byte) ([]byte, string, error) {
	if len(payload) < len(speechCacheMagic)+2 || string(payload[:len(speechCacheMagic)]) != speechCacheMagic {
		return nil, "", errors.New("invalid cached speech header")
	}
	offset := len(speechCacheMagic)
	mimeLength := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if mimeLength == 0 || mimeLength > 255 || len(payload) <= offset+mimeLength {
		return nil, "", errors.New("invalid cached speech metadata")
	}
	mime := string(payload[offset : offset+mimeLength])
	audio := append([]byte(nil), payload[offset+mimeLength:]...)
	if len(audio) > maxCachedSpeechBytes {
		return nil, "", errors.New("cached speech audio is too large")
	}
	return audio, mime, nil
}
