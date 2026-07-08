package openaiapi

import "testing"

func TestTTSSpeedConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want float64
	}{
		{name: "default", in: "", want: defaultTTSSpeed},
		{name: "custom", in: "0.75", want: 0.75},
		{name: "minimum", in: "0.25", want: 0.25},
		{name: "maximum", in: "4.0", want: 4.0},
		{name: "too slow", in: "0.1", want: defaultTTSSpeed},
		{name: "too fast", in: "5.0", want: defaultTTSSpeed},
		{name: "invalid", in: "slow", want: defaultTTSSpeed},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := New(Config{TTSSpeed: tt.in})
			if got := client.TTSSpeed(); got != tt.want {
				t.Fatalf("TTSSpeed() = %v, want %v", got, tt.want)
			}
		})
	}
}
