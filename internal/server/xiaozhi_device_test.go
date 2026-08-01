package server

import (
	"encoding/json"
	"testing"
)

func TestResolveVolumeCommand(t *testing.T) {
	client := testXiaozhiDeviceConnection()
	client.volume = 50
	client.volumeKnown = true
	tests := []struct {
		name       string
		text       string
		wantVolume int
		wantMatch  bool
	}{
		{name: "louder", text: "豆豆，声音大一点", wantVolume: 65, wantMatch: true},
		{name: "quieter", text: "声音小一点", wantVolume: 35, wantMatch: true},
		{name: "too quiet", text: "音量太小了", wantVolume: 65, wantMatch: true},
		{name: "absolute", text: "把音量调到80", wantVolume: 75, wantMatch: true},
		{name: "capability", text: "你能调整声音大小吗", wantVolume: -1, wantMatch: true},
		{name: "standalone quieter", text: "轻一点", wantVolume: 35, wantMatch: true},
		{name: "toddler louder wording", text: "是小兔子吗？你大点声。", wantVolume: 65, wantMatch: true},
		{name: "short loud volume", text: "大音量。", wantVolume: 65, wantMatch: true},
		{name: "short quiet volume", text: "小音量。", wantVolume: 35, wantMatch: true},
		{name: "raise without subject", text: "调高一点。", wantVolume: 65, wantMatch: true},
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

func TestStripVolumeCommandKeepsConversation(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "是小兔子吗？你大点声。", want: "是小兔子吗"},
		{text: "豆豆，声音小一点吧。", want: ""},
		{text: "把音量调到60", want: ""},
		{text: "大音量。", want: ""},
		{text: "是小鸭子吗？大音量。", want: "是小鸭子吗"},
	}
	for _, test := range tests {
		if got := stripVolumeCommand(test.text); got != test.want {
			t.Errorf("stripVolumeCommand(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestHandleMCPResponseUpdatesDeviceVolume(t *testing.T) {
	client := testXiaozhiDeviceConnection()
	client.statusMCPID = 7
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"result": map[string]any{
			"content": []map[string]string{{
				"type": "text",
				"text": `{"audio_speaker":{"volume":42},"battery":{"level":64,"charging":true}}`,
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
	status, ok := client.server.xiaozhiDeviceSnapshot()
	if !ok {
		t.Fatal("device snapshot was not saved")
	}
	if status.Battery != 64 || !status.BatteryKnown || !status.Charging {
		t.Fatalf("battery snapshot = %+v", status)
	}
}

func testXiaozhiDeviceConnection() *xiaozhiConnection {
	return &xiaozhiConnection{
		server: &Server{
			xiaozhi: xiaozhiConfig{
				volumeMin: 20,
				volumeMax: 75,
			},
		},
	}
}
