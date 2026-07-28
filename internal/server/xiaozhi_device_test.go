package server

import (
	"encoding/json"
	"testing"
)

func TestResolveVolumeCommand(t *testing.T) {
	client := &xiaozhiConnection{volume: 50, volumeKnown: true}
	tests := []struct {
		name       string
		text       string
		wantVolume int
		wantMatch  bool
	}{
		{name: "louder", text: "豆豆，声音大一点", wantVolume: 65, wantMatch: true},
		{name: "quieter", text: "声音小一点", wantVolume: 35, wantMatch: true},
		{name: "too quiet", text: "音量太小了", wantVolume: 65, wantMatch: true},
		{name: "absolute", text: "把音量调到80", wantVolume: 80, wantMatch: true},
		{name: "capability", text: "你能调整声音大小吗", wantVolume: -1, wantMatch: true},
		{name: "standalone quieter", text: "轻一点", wantVolume: 35, wantMatch: true},
		{name: "unrelated size", text: "这个苹果大一点", wantVolume: -1, wantMatch: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			volume, reply, matched := client.resolveVolumeCommand(test.text)
			if volume != test.wantVolume || matched != test.wantMatch {
				t.Fatalf("resolveVolumeCommand(%q) = (%d, %q, %v)", test.text, volume, reply, matched)
			}
			if matched && reply == "" {
				t.Fatal("matched command returned an empty reply")
			}
		})
	}
}

func TestHandleMCPResponseUpdatesDeviceVolume(t *testing.T) {
	client := &xiaozhiConnection{statusMCPID: 7}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"result": map[string]any{
			"content": []map[string]string{{
				"type": "text",
				"text": `{"audio_speaker":{"volume":42}}`,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client.handleMCPResponse(payload)
	if got := client.currentDeviceVolume(); got != 42 {
		t.Fatalf("device volume = %d, want 42", got)
	}
}
