package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpeechDiskCacheSurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	firstVoice := &countingVoiceProvider{}
	first := New(Config{Voice: firstVoice, SpeechCacheDir: dir})

	wantAudio, wantMIME, err := first.synthesizeSpeech(context.Background(), "汪，豆豆在这里。")
	if err != nil {
		t.Fatalf("first synthesis: %v", err)
	}
	if calls := firstVoice.speakCalls.Load(); calls != 1 {
		t.Fatalf("first provider calls = %d, want 1", calls)
	}

	secondVoice := &countingVoiceProvider{}
	second := New(Config{Voice: secondVoice, SpeechCacheDir: dir})
	gotAudio, gotMIME, err := second.synthesizeSpeech(context.Background(), "汪，豆豆在这里。")
	if err != nil {
		t.Fatalf("cached synthesis: %v", err)
	}
	if calls := secondVoice.speakCalls.Load(); calls != 0 {
		t.Fatalf("second provider calls = %d, want 0", calls)
	}
	if string(gotAudio) != string(wantAudio) || gotMIME != wantMIME {
		t.Fatalf("cached speech = (%q, %q), want (%q, %q)", gotAudio, gotMIME, wantAudio, wantMIME)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".tts" {
		t.Fatalf("cache entries = %#v, want one hashed .tts file", entries)
	}
	if strings.Contains(entries[0].Name(), "豆豆") {
		t.Fatalf("cache filename contains reply text: %q", entries[0].Name())
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPrewarmSpeechDeduplicatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	voice := &countingVoiceProvider{}
	srv := New(Config{Voice: voice, SpeechCacheDir: dir})
	srv.PrewarmSpeech(context.Background(), []string{"第一句", "第一句", "第二句", " "})

	if calls := voice.speakCalls.Load(); calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	if srv.warmTotal.Load() != 2 || srv.warmDone.Load() != 2 || srv.warmErrors.Load() != 0 {
		t.Fatalf(
			"warmup status = total:%d done:%d errors:%d",
			srv.warmTotal.Load(),
			srv.warmDone.Load(),
			srv.warmErrors.Load(),
		)
	}

	secondVoice := &countingVoiceProvider{}
	second := New(Config{Voice: secondVoice, SpeechCacheDir: dir})
	second.PrewarmSpeech(context.Background(), []string{"第一句", "第二句"})
	if calls := secondVoice.speakCalls.Load(); calls != 0 {
		t.Fatalf("provider calls after restart = %d, want 0", calls)
	}
}

func TestSpeechDiskCachePrunesOldEntries(t *testing.T) {
	cache := NewSpeechDiskCache(t.TempDir(), 2)
	for _, key := range []string{"one", "two", "three"} {
		if err := cache.Put(key, "audio/mpeg", []byte("audio:"+key)); err != nil {
			t.Fatalf("put %q: %v", key, err)
		}
	}
	_, ready, entries, err := cache.Status()
	if err != nil || !ready || entries != 2 {
		t.Fatalf("cache status = ready:%v entries:%d err:%v, want true, 2, nil", ready, entries, err)
	}
}
