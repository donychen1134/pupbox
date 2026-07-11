package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTrimWAVSilenceKeepsSpeechPadding(t *testing.T) {
	original := testSignalWAV(4_000, 1_100, 2_860, 4_000)
	trimmed, stats, ok := trimWAVSilence(original)
	if !ok || !stats.Applied {
		t.Fatalf("trim result = (%+v, %v), want applied", stats, ok)
	}
	if stats.OriginalDurationMS != 4_000 || stats.InputDurationMS != 2_260 || stats.TrimmedMS != 1_740 {
		t.Fatalf("trim stats = %+v", stats)
	}
	if duration, ok := wavDurationMS(trimmed); !ok || duration != stats.InputDurationMS {
		t.Fatalf("trimmed duration = (%d, %v), want %d", duration, ok, stats.InputDurationMS)
	}
	if duration, _ := wavDurationMS(original); duration != 4_000 {
		t.Fatalf("original WAV changed, duration = %d", duration)
	}
}

func TestTrimWAVSilencePreservesSoftShortUtterance(t *testing.T) {
	original := testSignalWAV(2_000, 800, 1_000, 200)
	trimmed, stats, ok := trimWAVSilence(original)
	if !ok || !stats.Applied {
		t.Fatalf("soft trim result = (%+v, %v), want applied", stats, ok)
	}
	if stats.InputDurationMS != 700 {
		t.Fatalf("soft input duration = %d, want 700", stats.InputDurationMS)
	}
	wav, ok := parsePCMWAV(trimmed)
	if !ok {
		t.Fatal("trimmed soft utterance is not valid PCM WAV")
	}
	peak := int16(0)
	for offset := 0; offset < len(wav.PCM); offset += 2 {
		sample := int16(binary.LittleEndian.Uint16(wav.PCM[offset : offset+2]))
		if sample > peak {
			peak = sample
		}
	}
	if peak != 200 {
		t.Fatalf("soft speech peak = %d, want 200", peak)
	}
}

func TestTrimWAVSilenceFallsBackForUncertainAudio(t *testing.T) {
	tests := map[string][]byte{
		"silence":        testSignalWAV(2_000, 0, 0, 0),
		"isolated click": testSignalWAV(2_000, 1_000, 1_020, 8_000),
		"continuous":     testSignalWAV(2_000, 0, 2_000, 1_000),
	}
	for name, original := range tests {
		t.Run(name, func(t *testing.T) {
			trimmed, stats, ok := trimWAVSilence(original)
			if !ok || stats.Applied {
				t.Fatalf("trim result = (%+v, %v), want parsed fallback", stats, ok)
			}
			if !bytes.Equal(trimmed, original) {
				t.Fatal("fallback audio changed")
			}
		})
	}
}

func TestVoiceTrimsOnlySTTInput(t *testing.T) {
	eventLogPath := filepath.Join(t.TempDir(), "events.jsonl")
	recordingDir := t.TempDir()
	voice := &trimCapturingVoiceProvider{}
	srv := New(Config{
		Voice:          voice,
		TrimSTTSilence: true,
		EventLogPath:   eventLogPath,
		RecordingDir:   recordingDir,
	})
	original := testSignalWAV(4_000, 1_100, 2_860, 4_000)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("audio", "recording.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/voice?tts=off", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("voice status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var response chatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	sttAudio := voice.Audio()
	if duration, ok := wavDurationMS(sttAudio); !ok || duration != 2_260 {
		t.Fatalf("STT audio duration = (%d, %v), want 2260", duration, ok)
	}
	recordingPath, _, err := srv.recordings.Find(response.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(recordingPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, original) {
		t.Fatal("saved diagnostic recording was trimmed")
	}
	events, err := srv.events.Recent(1)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v err=%v", events, err)
	}
	if events[0].Timings.AudioDurationMS != 4_000 || events[0].Timings.STTAudioDurationMS != 2_260 ||
		events[0].Timings.STTTrimmedMS != 1_740 {
		t.Fatalf("event timings = %+v", events[0].Timings)
	}
}

func testSignalWAV(totalMS, activeStartMS, activeEndMS int, amplitude int16) []byte {
	const sampleRate = 16_000
	frames := sampleRate * totalMS / 1000
	pcm := make([]byte, frames*2)
	startFrame := sampleRate * activeStartMS / 1000
	endFrame := sampleRate * activeEndMS / 1000
	for frame := startFrame; frame < endFrame; frame++ {
		value := amplitude
		if frame%2 == 1 {
			value = -value
		}
		binary.LittleEndian.PutUint16(pcm[frame*2:frame*2+2], uint16(value))
	}
	return encodePCM16WAV(1, sampleRate, pcm)
}

type trimCapturingVoiceProvider struct {
	mu    sync.Mutex
	audio []byte
}

func (p *trimCapturingVoiceProvider) Available() bool   { return true }
func (p *trimCapturingVoiceProvider) Name() string      { return "trim-test" }
func (p *trimCapturingVoiceProvider) STTModel() string  { return "trim-stt" }
func (p *trimCapturingVoiceProvider) TTSModel() string  { return "trim-tts" }
func (p *trimCapturingVoiceProvider) TTSVoice() string  { return "trim-voice" }
func (p *trimCapturingVoiceProvider) TTSFormat() string { return "wav" }
func (p *trimCapturingVoiceProvider) TTSSpeed() float64 { return 1 }
func (p *trimCapturingVoiceProvider) Transcribe(_ context.Context, audio []byte, _, _ string) (string, error) {
	p.mu.Lock()
	p.audio = append([]byte(nil), audio...)
	p.mu.Unlock()
	return "豆豆听见了", nil
}
func (p *trimCapturingVoiceProvider) Speak(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (p *trimCapturingVoiceProvider) Audio() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.audio...)
}

var _ VoiceProvider = (*trimCapturingVoiceProvider)(nil)
