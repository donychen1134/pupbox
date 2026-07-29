package qwenrealtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRealtimeSessionRunsManualAudioTurn(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("model") != DefaultModel {
			t.Errorf("model = %q", r.URL.Query().Get("model"))
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Error(err)
			return
		}
		var update map[string]any
		if json.Unmarshal(payload, &update) != nil || update["type"] != "session.update" {
			t.Errorf("session update = %s", payload)
			return
		}
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})

		var gotAudio bool
		for {
			_, payload, err = conn.ReadMessage()
			if err != nil {
				t.Error(err)
				return
			}
			var event struct {
				Type  string `json:"type"`
				Audio string `json:"audio"`
			}
			if json.Unmarshal(payload, &event) != nil {
				continue
			}
			if event.Type == "input_audio_buffer.append" {
				audio, _ := base64.StdEncoding.DecodeString(event.Audio)
				gotAudio = gotAudio || len(audio) > 0
			}
			if event.Type == "response.create" {
				break
			}
		}
		if !gotAudio {
			t.Error("no input audio received")
		}
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "云朵像什么"})
		_ = conn.WriteJSON(map[string]any{"type": "response.audio_transcript.delta", "delta": "像棉花糖。"})
		_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})})
		_ = conn.WriteJSON(map[string]any{"type": "response.audio_transcript.done", "transcript": "云朵像棉花糖。"})
		_ = conn.WriteJSON(map[string]any{"type": "response.done"})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := New(Config{APIKey: "test-key", URL: wsURL})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.RunTurn(ctx, []byte{0, 0, 1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transcript != "云朵像什么" || result.Reply != "云朵像棉花糖。" || result.AudioBytes != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDecodeWAV16KMono(t *testing.T) {
	t.Parallel()
	pcm := []byte{1, 0, 2, 0}
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 16000)
	binary.LittleEndian.PutUint32(wav[28:32], 32000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(pcm)))
	copy(wav[44:], pcm)

	got, err := DecodeWAV16KMono(wav)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(pcm) {
		t.Fatalf("PCM = %v, want %v", got, pcm)
	}
}
