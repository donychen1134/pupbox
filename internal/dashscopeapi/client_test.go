package dashscopeapi

import "testing"

func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	client := New(Config{})

	if got, want := client.Name(), "dashscope"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
	if got, want := client.ChatModel(), "qwen-turbo"; got != want {
		t.Fatalf("ChatModel() = %q, want %q", got, want)
	}
	if got, want := client.STTModel(), "qwen3-asr-flash"; got != want {
		t.Fatalf("STTModel() = %q, want %q", got, want)
	}
	if got, want := client.TTSModel(), "cosyvoice-v3-flash"; got != want {
		t.Fatalf("TTSModel() = %q, want %q", got, want)
	}
	if got, want := client.TTSVoice(), "longhuhu_v3"; got != want {
		t.Fatalf("TTSVoice() = %q, want %q", got, want)
	}
	if got, want := client.TTSFormat(), "mp3"; got != want {
		t.Fatalf("TTSFormat() = %q, want %q", got, want)
	}
	if got, want := client.TTSSpeed(), 0.88; got != want {
		t.Fatalf("TTSSpeed() = %v, want %v", got, want)
	}
	if got, want := client.ttsPrompt, ""; got != want {
		t.Fatalf("ttsPrompt = %q, want empty", got)
	}
}

func TestParseSpeechRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want float64
	}{
		{name: "custom", in: "0.75", want: 0.75},
		{name: "minimum", in: "0.5", want: 0.5},
		{name: "maximum", in: "2.0", want: 2.0},
		{name: "too slow", in: "0.25", want: defaultTTSSpeed},
		{name: "too fast", in: "2.1", want: defaultTTSSpeed},
		{name: "invalid", in: "slow", want: defaultTTSSpeed},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseSpeechRate(tt.in); got != tt.want {
				t.Fatalf("parseSpeechRate(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSampleRate(t *testing.T) {
	t.Parallel()

	if got, want := parseSampleRate("16000"), 16000; got != want {
		t.Fatalf("parseSampleRate(16000) = %v, want %v", got, want)
	}
	if got, want := parseSampleRate("12345"), 24000; got != want {
		t.Fatalf("parseSampleRate(12345) = %v, want %v", got, want)
	}
}

func TestNormalizeAudioMIME(t *testing.T) {
	t.Parallel()

	if got, want := normalizeAudioMIME("recording.wav", ""), "audio/wav"; got != want {
		t.Fatalf("normalizeAudioMIME(wav) = %q, want %q", got, want)
	}
	if got, want := normalizeAudioMIME("recording.bin", "audio/webm;codecs=opus"), "audio/webm"; got != want {
		t.Fatalf("normalizeAudioMIME(content type) = %q, want %q", got, want)
	}
}
