package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

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
	if hello["type"] != "hello" || int(audioParams["frame_duration"].(float64)) != 100 {
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

type fakeXiaozhiVoice struct{}

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
func (*fakeXiaozhiVoice) StreamTranscribeOpus(
	ctx context.Context,
	audio <-chan []byte,
	onTranscript func(string, string) error,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-audio:
		return onTranscript("云朵像棉花糖", "happy")
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
func (*fakeXiaozhiVoice) OpusOutputFrameDuration() int { return 100 }
