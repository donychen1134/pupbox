package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
	"github.com/gorilla/websocket"
)

func TestEndingActivity(t *testing.T) {
	for _, test := range []struct {
		activity *dog.Activity
		want     bool
	}{
		{activity: &dog.Activity{ID: "farewell"}, want: true},
		{activity: &dog.Activity{ID: "quiet"}, want: true},
		{activity: &dog.Activity{ID: "story"}, want: false},
		{activity: nil, want: false},
	} {
		if got := endingActivity(test.activity); got != test.want {
			t.Errorf("endingActivity(%#v) = %v, want %v", test.activity, got, test.want)
		}
	}
}

func TestXiaozhiVoiceStandbyOnlyResumesOnListeningStart(t *testing.T) {
	client := &xiaozhiConnection{lastActive: time.Now().Add(-time.Minute)}
	if !client.claimVoiceStandby(30 * time.Second) {
		t.Fatal("expected inactive conversation to enter voice standby")
	}
	if client.claimVoiceStandby(30 * time.Second) {
		t.Fatal("voice standby was claimed twice")
	}
	client.resumeFromStandby()
	if client.standbySent || time.Since(client.lastActive) > time.Second {
		t.Fatalf("listening start did not resume activity: %+v", client)
	}
}

func TestXiaozhiProductIdleFarewellAndReconnect(t *testing.T) {
	const deviceID = "02:00:00:00:00:01"
	const farewell = "豆豆先休息啦，拜拜。"
	srv := New(Config{
		Voice:             &fakeXiaozhiVoice{},
		AccessToken:       "test-token",
		EnableXiaozhi:     true,
		XiaozhiDeviceID:   deviceID,
		XiaozhiIdleTime:   60 * time.Millisecond,
		XiaozhiFarewell:   farewell,
		XiaozhiSleepGrace: 25 * time.Millisecond,
	})
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	connect := func() *websocket.Conn {
		t.Helper()
		headers := http.Header{}
		headers.Set("Authorization", "Bearer test-token")
		headers.Set("Device-Id", deviceID)
		conn, _, err := websocket.DefaultDialer.Dial(
			"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/xiaozhi/v1/",
			headers,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(map[string]any{
			"type": "hello", "version": 1, "transport": "websocket",
			"audio_params": map[string]any{
				"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60,
			},
		}); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		var hello map[string]any
		if err := conn.ReadJSON(&hello); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if hello["type"] != "hello" {
			conn.Close()
			t.Fatalf("unexpected hello: %#v", hello)
		}
		return conn
	}

	conn := connect()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var gotStart, gotSentence, gotAudio, gotStop, gotSleep bool
	for !gotSleep {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if messageType == websocket.BinaryMessage {
			gotAudio = true
			continue
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		if message["type"] == "pupbox" && message["action"] == "sleep" {
			gotSleep = int64(message["delay_ms"].(float64)) == 25
			continue
		}
		if message["type"] != "tts" {
			continue
		}
		switch message["state"] {
		case "start":
			gotStart = true
		case "sentence_start":
			gotSentence = message["text"] == farewell
		case "stop":
			gotStop = true
		}
	}
	if !gotStart || !gotSentence || !gotAudio || !gotStop || !gotSleep {
		t.Fatalf("idle farewell states: start=%v sentence=%v audio=%v stop=%v sleep=%v",
			gotStart, gotSentence, gotAudio, gotStop, gotSleep)
	}

	var closed bool
	for !closed {
		if _, _, err := conn.ReadMessage(); err != nil {
			closed = true
		}
	}
	conn.Close()

	reconnected := connect()
	reconnected.Close()
}

func TestXiaozhiExplicitFarewellClosesAfterSpeechAndReconnects(t *testing.T) {
	const deviceID = "02:00:00:00:00:01"
	srv := New(Config{
		Voice:             &fakeXiaozhiVoice{transcript: "晚安，我要睡觉了"},
		AccessToken:       "test-token",
		EnableXiaozhi:     true,
		XiaozhiDeviceID:   deviceID,
		XiaozhiSleepGrace: 25 * time.Millisecond,
	})
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	connect := func() *websocket.Conn {
		t.Helper()
		headers := http.Header{}
		headers.Set("Authorization", "Bearer test-token")
		headers.Set("Device-Id", deviceID)
		conn, _, err := websocket.DefaultDialer.Dial(
			"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/xiaozhi/v1/",
			headers,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(map[string]any{
			"type": "hello", "version": 1, "transport": "websocket",
			"audio_params": map[string]any{
				"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60,
			},
		}); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		var hello map[string]any
		if err := conn.ReadJSON(&hello); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		return conn
	}

	conn := connect()
	if err := conn.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var gotFarewell, gotAudio, gotStop bool
	for !gotStop {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if messageType == websocket.BinaryMessage {
			gotAudio = true
			continue
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil || message["type"] != "tts" {
			continue
		}
		if message["state"] == "sentence_start" {
			text, _ := message["text"].(string)
			gotFarewell = strings.Contains(text, "休息") || strings.Contains(text, "再见") ||
				strings.Contains(text, "拜拜") || strings.Contains(text, "晚安")
		}
		gotStop = message["state"] == "stop"
	}
	if !gotFarewell || !gotAudio {
		t.Fatalf("explicit farewell: sentence=%v audio=%v", gotFarewell, gotAudio)
	}
	var sleep map[string]any
	if err := conn.ReadJSON(&sleep); err != nil {
		t.Fatal(err)
	}
	if sleep["type"] != "pupbox" || sleep["action"] != "sleep" ||
		int64(sleep["delay_ms"].(float64)) != 25 {
		t.Fatalf("unexpected product sleep request: %#v", sleep)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected websocket to close after explicit farewell")
	}
	conn.Close()

	reconnected := connect()
	reconnected.Close()
}

func TestXiaozhiOTAAndVoiceRoundTrip(t *testing.T) {
	const deviceID = "02:00:00:00:00:01"
	voice := &fakeXiaozhiVoice{}
	srv := New(Config{
		Voice:           voice,
		AccessToken:     "test-token",
		EnableXiaozhi:   true,
		XiaozhiDeviceID: deviceID,
	})
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	otaReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/xiaozhi/ota/", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	otaReq.Header.Set("Device-Id", deviceID)
	otaResp, err := http.DefaultClient.Do(otaReq)
	if err != nil {
		t.Fatal(err)
	}
	defer otaResp.Body.Close()
	if otaResp.StatusCode != http.StatusOK {
		t.Fatalf("OTA status = %d", otaResp.StatusCode)
	}
	var ota struct {
		WebSocket struct {
			URL     string `json:"url"`
			Token   string `json:"token"`
			Version int    `json:"version"`
		} `json:"websocket"`
	}
	if err := json.NewDecoder(otaResp.Body).Decode(&ota); err != nil {
		t.Fatal(err)
	}
	if ota.WebSocket.Token != "test-token" || ota.WebSocket.Version != 1 ||
		!strings.HasSuffix(ota.WebSocket.URL, "/xiaozhi/v1/") {
		t.Fatalf("unexpected OTA response: %+v", ota.WebSocket)
	}

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/xiaozhi/v1/"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")
	headers.Set("Device-Id", deviceID)
	headers.Set("Client-Id", "test-client")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":      "hello",
		"version":   1,
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	audioParams := hello["audio_params"].(map[string]any)
	if hello["type"] != "hello" || int(audioParams["frame_duration"].(float64)) != 60 {
		t.Fatalf("unexpected hello: %#v", hello)
	}

	if err := conn.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}

	var gotSTT, gotStart, gotSentence, gotAudio, gotStop bool
	for i := 0; i < 10 && !gotStop; i++ {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if messageType == websocket.BinaryMessage {
			gotAudio = string(payload) == string([]byte{1, 2, 3})
			continue
		}
		var message map[string]any
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatal(err)
		}
		switch message["type"] {
		case "stt":
			gotSTT = message["text"] == "云朵像棉花糖"
		case "tts":
			switch message["state"] {
			case "start":
				gotStart = true
			case "sentence_start":
				gotSentence = message["text"] != ""
			case "stop":
				gotStop = true
			}
		}
	}
	if !gotSTT || !gotStart || !gotSentence || !gotAudio || !gotStop {
		t.Fatalf("round trip states: stt=%v start=%v sentence=%v audio=%v stop=%v",
			gotSTT, gotStart, gotSentence, gotAudio, gotStop)
	}
}

func TestXiaozhiVolumeCommandUsesMCP(t *testing.T) {
	const deviceID = "02:00:00:00:00:01"
	srv := New(Config{
		Voice:           &fakeXiaozhiVoice{transcript: "豆豆，声音大一点"},
		AccessToken:     "test-token",
		EnableXiaozhi:   true,
		XiaozhiDeviceID: deviceID,
	})
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")
	headers.Set("Device-Id", deviceID)
	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/xiaozhi/v1/",
		headers,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type": "hello", "version": 1, "transport": "websocket",
		"audio_params": map[string]any{
			"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}

	var statusRequest struct {
		Type    string `json:"type"`
		Payload struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&statusRequest); err != nil {
		t.Fatal(err)
	}
	if statusRequest.Type != "mcp" || statusRequest.Payload.Method != "tools/call" ||
		statusRequest.Payload.Params.Name != "self.get_device_status" {
		t.Fatalf("unexpected status request: %+v", statusRequest)
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "mcp",
		"payload": map[string]any{
			"jsonrpc": "2.0",
			"id":      statusRequest.Payload.ID,
			"result": map[string]any{
				"content": []map[string]string{{
					"type": "text", "text": `{"audio_speaker":{"volume":40}}`,
				}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "listen", "state": "start", "mode": "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}

	gotVolume := -1
	gotReply := false
	for i := 0; i < 12 && (!gotReply || gotVolume < 0); i++ {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message struct {
			Type    string `json:"type"`
			State   string `json:"state"`
			Text    string `json:"text"`
			Payload struct {
				Params struct {
					Name      string `json:"name"`
					Arguments struct {
						Volume int `json:"volume"`
					} `json:"arguments"`
				} `json:"params"`
			} `json:"payload"`
		}
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		if message.Type == "mcp" && message.Payload.Params.Name == "self.audio_speaker.set_volume" {
			gotVolume = message.Payload.Params.Arguments.Volume
		}
		if message.Type == "tts" && message.State == "sentence_start" {
			gotReply = strings.Contains(message.Text, "大声一点")
		}
	}
	if gotVolume != 55 || !gotReply {
		t.Fatalf("volume command: volume=%d reply=%v", gotVolume, gotReply)
	}
}

func TestXiaozhiOTARejectsUnknownDevice(t *testing.T) {
	srv := New(Config{
		Voice:           &fakeXiaozhiVoice{},
		AccessToken:     "test-token",
		EnableXiaozhi:   true,
		XiaozhiDeviceID: "02:00:00:00:00:01",
	})
	req := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", strings.NewReader("{}"))
	req.Header.Set("Device-Id", "02:00:00:00:00:02")
	recorder := httptest.NewRecorder()

	srv.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if strings.Contains(recorder.Body.String(), "test-token") {
		t.Fatal("forbidden response disclosed the access token")
	}
}

type fakeXiaozhiVoice struct {
	transcript string
}

func (*fakeXiaozhiVoice) Available() bool { return true }
func (*fakeXiaozhiVoice) Name() string    { return "fake-xiaozhi" }
func (*fakeXiaozhiVoice) STTModel() string {
	return "fake-realtime-stt"
}
func (*fakeXiaozhiVoice) TTSModel() string  { return "fake-opus-tts" }
func (*fakeXiaozhiVoice) TTSVoice() string  { return "fake-voice" }
func (*fakeXiaozhiVoice) TTSFormat() string { return "opus" }
func (*fakeXiaozhiVoice) TTSSpeed() float64 { return 1 }
func (*fakeXiaozhiVoice) Transcribe(context.Context, []byte, string, string) (string, error) {
	return "", nil
}
func (*fakeXiaozhiVoice) Speak(context.Context, string) ([]byte, string, error) {
	return nil, "audio/ogg", nil
}
func (v *fakeXiaozhiVoice) StreamTranscribeOpus(
	ctx context.Context,
	audio <-chan []byte,
	_ int,
	_ int,
	onTranscript func(string, string) error,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-audio:
		transcript := v.transcript
		if transcript == "" {
			transcript = "云朵像棉花糖"
		}
		return onTranscript(transcript, "happy")
	}
}
func (*fakeXiaozhiVoice) StreamSpeakOpus(
	_ context.Context,
	_ string,
	onPacket func([]byte) error,
) error {
	return onPacket([]byte{1, 2, 3})
}
func (*fakeXiaozhiVoice) OpusOutputSampleRate() int    { return 24000 }
func (*fakeXiaozhiVoice) OpusOutputFrameDuration() int { return 60 }
