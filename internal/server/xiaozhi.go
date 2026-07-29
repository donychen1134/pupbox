package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/donychen1134/pupbox/internal/dog"
	"github.com/gorilla/websocket"
)

type xiaozhiVoiceProvider interface {
	StreamTranscribeOpus(
		ctx context.Context,
		audio <-chan []byte,
		sampleRate int,
		frameDurationMS int,
		onTranscript func(text, emotion string) error,
	) error
	StreamSpeakOpus(ctx context.Context, text string, onPacket func([]byte) error) error
	OpusOutputSampleRate() int
	OpusOutputFrameDuration() int
}

type xiaozhiConfig struct {
	enabled     bool
	deviceID    string
	wsURL       string
	idleTimeout time.Duration
	farewell    string
	volumeMin   int
	volumeMax   int
	streaming   bool
}

type xiaozhiClientHello struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	Transport   string `json:"transport"`
	AudioParams struct {
		Format        string `json:"format"`
		SampleRate    int    `json:"sample_rate"`
		Channels      int    `json:"channels"`
		FrameDuration int    `json:"frame_duration"`
	} `json:"audio_params"`
}

type xiaozhiDeviceMessage struct {
	Type      string          `json:"type"`
	State     string          `json:"state,omitempty"`
	Mode      string          `json:"mode,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type xiaozhiConnection struct {
	server      *Server
	conn        *websocket.Conn
	voice       xiaozhiVoiceProvider
	ctx         context.Context
	cancel      context.CancelFunc
	sessionID   string
	sampleRate  int
	frameMS     int
	inputFrame  int
	inputRate   int
	audio       chan []byte
	writeMu     sync.Mutex
	deviceMu    sync.Mutex
	turnMu      sync.Mutex
	activityMu  sync.Mutex
	mcpID       int64
	statusMCPID int64
	volume      int
	volumeKnown bool
	turnStarted time.Time
	lastAudioAt time.Time
	audioBytes  int64
	audioFrames int64
	turnCancel  context.CancelFunc
	lastActive  time.Time
	turnActive  bool
	idleClosing bool
}

type xiaozhiDeviceStatus struct {
	Connected    bool   `json:"connected"`
	ConnectedAt  string `json:"connected_at,omitempty"`
	LastSeenAt   string `json:"last_seen_at,omitempty"`
	Volume       int    `json:"volume,omitempty"`
	VolumeKnown  bool   `json:"volume_known"`
	Battery      int    `json:"battery,omitempty"`
	BatteryKnown bool   `json:"battery_known"`
	Charging     bool   `json:"charging"`
}

var xiaozhiUpgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

func (s *Server) xiaozhiReady() bool {
	_, ok := s.voice.(xiaozhiVoiceProvider)
	return s.xiaozhi.enabled && s.useVoice && ok
}

func (s *Server) handleXiaozhiOTA(w http.ResponseWriter, r *http.Request) {
	if s.xiaozhi.deviceID != "" && normalizeDeviceID(r.Header.Get("Device-Id")) != s.xiaozhi.deviceID {
		writeError(w, http.StatusForbidden, "device is not allowed")
		return
	}
	if !s.xiaozhiReady() {
		writeError(w, http.StatusServiceUnavailable, "xiaozhi voice service is not ready")
		return
	}

	wsURL := s.xiaozhi.wsURL
	if wsURL == "" {
		scheme := "ws"
		if r.TLS != nil {
			scheme = "wss"
		}
		wsURL = scheme + "://" + r.Host + "/xiaozhi/v1/"
	}
	s.log.Info("xiaozhi ota configuration served", "websocket_url", wsURL)
	writeJSON(w, http.StatusOK, map[string]any{
		"websocket": map[string]any{
			"url":     wsURL,
			"token":   s.accessToken,
			"version": 1,
		},
		"server_time": map[string]any{
			"timestamp":       time.Now().UnixMilli(),
			"timezone_offset": 0,
		},
	})
}

func (s *Server) handleXiaozhiWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.xiaozhiReady() {
		writeError(w, http.StatusServiceUnavailable, "xiaozhi voice service is not ready")
		return
	}
	if s.xiaozhi.deviceID != "" && normalizeDeviceID(r.Header.Get("Device-Id")) != s.xiaozhi.deviceID {
		writeError(w, http.StatusForbidden, "device is not allowed")
		return
	}
	if s.accessToken != "" && !s.validAccessToken(r) {
		writeError(w, http.StatusUnauthorized, "access token required")
		return
	}

	conn, err := xiaozhiUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("xiaozhi websocket upgrade failed", "error", err)
		return
	}
	voice := s.voice.(xiaozhiVoiceProvider)
	ctx, cancel := context.WithCancel(r.Context())
	client := &xiaozhiConnection{
		server:     s,
		conn:       conn,
		voice:      voice,
		ctx:        ctx,
		cancel:     cancel,
		sessionID:  xiaozhiSessionID(r.Header.Get("Client-Id"), r.Header.Get("Device-Id")),
		sampleRate: voice.OpusOutputSampleRate(),
		frameMS:    voice.OpusOutputFrameDuration(),
		inputFrame: 60,
		inputRate:  16000,
		audio:      make(chan []byte, 128),
	}
	client.run()
}

func (c *xiaozhiConnection) run() {
	defer func() {
		c.cancelCurrentTurn()
		c.cancel()
		close(c.audio)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(1 << 20)

	messageType, payload, err := c.conn.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return
	}
	var hello xiaozhiClientHello
	if err := json.Unmarshal(payload, &hello); err != nil ||
		hello.Type != "hello" ||
		hello.Transport != "websocket" ||
		hello.Version != 1 ||
		hello.AudioParams.Format != "opus" {
		_ = c.writeJSON(map[string]any{
			"type":    "alert",
			"status":  "连接失败",
			"message": "豆豆的语音协议不匹配",
			"emotion": "sad",
		})
		return
	}
	if hello.AudioParams.FrameDuration > 0 {
		c.inputFrame = hello.AudioParams.FrameDuration
	}
	if hello.AudioParams.SampleRate > 0 {
		c.inputRate = hello.AudioParams.SampleRate
	}
	if err := c.writeJSON(map[string]any{
		"type":       "hello",
		"transport":  "websocket",
		"session_id": c.sessionID,
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    c.sampleRate,
			"channels":       1,
			"frame_duration": c.frameMS,
		},
	}); err != nil {
		return
	}
	c.requestDeviceStatus()
	c.touchActivity()
	go c.watchProductIdle()
	c.server.markXiaozhiConnected()
	c.server.log.Info("xiaozhi websocket connected", "session_id", c.sessionID)
	defer func() {
		c.server.markXiaozhiDisconnected()
		c.server.log.Info("xiaozhi websocket disconnected", "session_id", c.sessionID)
	}()

	asrDone := make(chan error, 1)
	go func() {
		asrDone <- c.voice.StreamTranscribeOpus(
			c.ctx,
			c.audio,
			c.inputRate,
			c.inputFrame,
			c.handleTranscript,
		)
	}()

	for {
		messageType, payload, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			c.touchActivity()
			c.noteAudio(len(payload))
			packet := append([]byte(nil), payload...)
			select {
			case c.audio <- packet:
			case err := <-asrDone:
				c.handleASRError(err)
				return
			case <-c.ctx.Done():
				return
			}
		case websocket.TextMessage:
			var message xiaozhiDeviceMessage
			if json.Unmarshal(payload, &message) != nil {
				continue
			}
			switch message.Type {
			case "listen":
				if message.State == "start" || message.State == "detect" {
					c.touchActivity()
					c.beginTurn()
				}
			case "abort":
				c.cancelCurrentTurn()
			case "mcp":
				c.handleMCPResponse(message.Payload)
			}
		}

		select {
		case err := <-asrDone:
			c.handleASRError(err)
			return
		default:
		}
	}
}

func (s *Server) markXiaozhiConnected() {
	now := time.Now().UTC().Format(time.RFC3339)
	s.xiaozhiMu.Lock()
	s.xiaozhiInfo.Connected = true
	s.xiaozhiInfo.ConnectedAt = now
	s.xiaozhiInfo.LastSeenAt = now
	s.xiaozhiMu.Unlock()
}

func (s *Server) markXiaozhiDisconnected() {
	s.xiaozhiMu.Lock()
	s.xiaozhiInfo.Connected = false
	s.xiaozhiInfo.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	s.xiaozhiMu.Unlock()
}

func (s *Server) updateXiaozhiDeviceStatus(volume int, volumeKnown bool, battery int, batteryKnown, charging bool) {
	s.xiaozhiMu.Lock()
	s.xiaozhiInfo.Volume = volume
	s.xiaozhiInfo.VolumeKnown = volumeKnown
	s.xiaozhiInfo.Battery = battery
	s.xiaozhiInfo.BatteryKnown = batteryKnown
	s.xiaozhiInfo.Charging = charging
	s.xiaozhiInfo.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	s.xiaozhiMu.Unlock()
}

func (s *Server) xiaozhiDeviceSnapshot() (xiaozhiDeviceStatus, bool) {
	s.xiaozhiMu.RLock()
	defer s.xiaozhiMu.RUnlock()
	return s.xiaozhiInfo, s.xiaozhiInfo.LastSeenAt != ""
}

func (c *xiaozhiConnection) handleTranscript(text, emotion string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	c.beginResponse()
	defer c.endResponse()

	turnCtx, cancel := context.WithCancel(c.ctx)
	c.setCurrentTurn(cancel)
	defer func() {
		cancel()
		c.clearCurrentTurn()
	}()

	started, lastAudioAt, audioBytes, audioFrames := c.takeTurnStats()
	if started.IsZero() {
		started = time.Now()
	}
	traceID := c.server.nextTraceID()
	timings := TimingStats{
		AudioBytes:      audioBytes,
		AudioDurationMS: audioFrames * int64(c.inputFrame),
	}
	if !lastAudioAt.IsZero() {
		timings.STTMS = elapsedMS(lastAudioAt)
	}

	if err := c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "stt",
		"text":       text,
	}); err != nil {
		return err
	}
	if err := c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "tts",
		"state":      "start",
	}); err != nil {
		return err
	}

	history := c.server.sessions.History(c.sessionID)
	replyStarted := time.Now()
	_ = c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "llm",
		"emotion":    xiaozhiEmotion(emotion),
		"text":       "",
	})

	type replyResult struct {
		streamedReplyResult
		replyMS          int64
		firstTokenMS     int64
		deviceCommandErr error
	}
	sentences := make(chan string, 8)
	resultCh := make(chan replyResult, 1)
	go func() {
		firstTokenMS := int64(0)
		var result streamedReplyResult
		var deviceCommandErr error
		if volume, volumeReply, matched := c.resolveVolumeCommand(text); matched {
			if volume >= 0 {
				deviceCommandErr = c.setDeviceVolume(volume)
			}
			result = streamedReplyResult{reply: volumeReply, source: "device:volume"}
			result.err = emitSpeechSentence(volumeReply, func(sentence string) error {
				select {
				case sentences <- sentence:
					return nil
				case <-turnCtx.Done():
					return turnCtx.Err()
				}
			})
		} else {
			result = c.server.streamReply(
				turnCtx,
				text,
				history,
				func(sentence string) error {
					select {
					case sentences <- sentence:
						return nil
					case <-turnCtx.Done():
						return turnCtx.Err()
					}
				},
				func() {
					if firstTokenMS == 0 {
						firstTokenMS = elapsedMS(replyStarted)
					}
				},
			)
		}
		resultCh <- replyResult{
			streamedReplyResult: result,
			replyMS:             elapsedMS(replyStarted),
			firstTokenMS:        firstTokenMS,
			deviceCommandErr:    deviceCommandErr,
		}
		close(sentences)
	}()

	firstAudioMS := int64(-1)
	var ttsErr error
	for sentence := range sentences {
		if err := c.writeJSON(map[string]any{
			"session_id": c.sessionID,
			"type":       "tts",
			"state":      "sentence_start",
			"text":       sentence,
		}); err != nil {
			ttsErr = err
			cancel()
			break
		}
		ttsStarted := time.Now()
		err := c.streamSpeechOpus(turnCtx, sentence, func() {
			if firstAudioMS < 0 {
				firstAudioMS = elapsedMS(ttsStarted)
				timings.VoiceResponseMS = elapsedMS(replyStarted)
			}
		})
		timings.TTSMS += elapsedMS(ttsStarted)
		if err != nil {
			ttsErr = err
			cancel()
			break
		}
	}
	result := <-resultCh
	reply := dog.SpeechOnlyReply(dog.ClampReply(result.reply, 100))
	safety := result.safety
	activity := result.activity
	source := result.source
	chatErr := errors.Join(result.err, result.deviceCommandErr)
	timings.ReplyMS = result.replyMS
	timings.LLMFirstTokenMS = result.firstTokenMS
	timings.TTSFirstAudioMS = max(firstAudioMS, 0)
	timings.TotalMS = elapsedMS(started)
	timings.TurnTotalMS = timings.TotalMS

	_ = c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "tts",
		"state":      "stop",
	})

	response := chatResponse{
		TraceID:    traceID,
		Transcript: text,
		Reply:      reply,
		Safety:     safety,
		Activity:   activity,
		Mode:       c.server.mode(),
		Source:     source,
		AIError:    errorString(chatErr),
		TTSError:   errorString(ttsErr),
		Timings:    timings,
	}
	c.server.sessions.Append(c.sessionID, text, reply, activityID(activity))
	c.server.recordConversation("xiaozhi", response, nil, eventErrors{
		Chat: errorString(chatErr),
		TTS:  errorString(ttsErr),
	})
	if ttsErr != nil && !errors.Is(ttsErr, context.Canceled) {
		c.server.log.Warn("xiaozhi TTS failed", "trace_id", traceID, "error", ttsErr)
	}
	if activityID(activity) == "farewell" {
		c.closeProductSession("explicit farewell")
	}
	return nil
}

func (c *xiaozhiConnection) touchActivity() {
	c.activityMu.Lock()
	c.lastActive = time.Now()
	c.activityMu.Unlock()
}

func (c *xiaozhiConnection) beginResponse() {
	c.activityMu.Lock()
	c.lastActive = time.Now()
	c.turnActive = true
	c.activityMu.Unlock()
}

func (c *xiaozhiConnection) endResponse() {
	c.activityMu.Lock()
	c.lastActive = time.Now()
	c.turnActive = false
	c.activityMu.Unlock()
}

func (c *xiaozhiConnection) watchProductIdle() {
	timeout := c.server.xiaozhi.idleTimeout
	if timeout <= 0 {
		return
	}
	interval := min(timeout/4, time.Second)
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.claimIdleClose(timeout) {
				continue
			}
			c.sayFarewellAndClose()
			return
		}
	}
}

func (c *xiaozhiConnection) claimIdleClose(timeout time.Duration) bool {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()
	if c.idleClosing || c.turnActive || c.lastActive.IsZero() || time.Since(c.lastActive) < timeout {
		return false
	}
	c.idleClosing = true
	c.turnActive = true
	return true
}

func (c *xiaozhiConnection) sayFarewellAndClose() {
	farewell := c.server.xiaozhi.farewell
	if farewell == "" {
		farewell = "豆豆要休息一会儿啦，下次再来找我玩，拜拜。"
	}
	c.server.log.Info("xiaozhi product session idle", "session_id", c.sessionID)

	_ = c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "tts",
		"state":      "start",
	})
	_ = c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "tts",
		"state":      "sentence_start",
		"text":       farewell,
	})
	if err := c.streamSpeechOpus(c.ctx, farewell, nil); err != nil && !errors.Is(err, context.Canceled) {
		c.server.log.Warn("xiaozhi idle farewell failed", "session_id", c.sessionID, "error", err)
	}
	_ = c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "tts",
		"state":      "stop",
	})

	c.closeProductSession("product session idle")
}

func (c *xiaozhiConnection) closeProductSession(reason string) {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return
	case <-timer.C:
	}
	c.writeMu.Lock()
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason),
		time.Now().Add(time.Second),
	)
	_ = c.conn.Close()
	c.writeMu.Unlock()
}

func (c *xiaozhiConnection) beginTurn() {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	c.turnStarted = time.Now()
	c.lastAudioAt = time.Time{}
	c.audioBytes = 0
	c.audioFrames = 0
}

func (c *xiaozhiConnection) noteAudio(size int) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnStarted.IsZero() {
		c.turnStarted = time.Now()
	}
	c.lastAudioAt = time.Now()
	c.audioBytes += int64(size)
	c.audioFrames++
}

func (c *xiaozhiConnection) takeTurnStats() (time.Time, time.Time, int64, int64) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	return c.turnStarted, c.lastAudioAt, c.audioBytes, c.audioFrames
}

func (c *xiaozhiConnection) setCurrentTurn(cancel context.CancelFunc) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnCancel != nil {
		c.turnCancel()
	}
	c.turnCancel = cancel
}

func (c *xiaozhiConnection) clearCurrentTurn() {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	c.turnCancel = nil
}

func (c *xiaozhiConnection) cancelCurrentTurn() {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnCancel != nil {
		c.turnCancel()
		c.turnCancel = nil
	}
}

func (c *xiaozhiConnection) writeJSON(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(value)
}

func (c *xiaozhiConnection) writeBinary(packet []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(websocket.BinaryMessage, packet)
}

func (c *xiaozhiConnection) handleASRError(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	c.server.log.Warn("xiaozhi realtime ASR ended", "error", err)
	_ = c.writeJSON(map[string]any{
		"type":    "alert",
		"status":  "网络慢了一点",
		"message": "豆豆刚才没听清，请再说一次",
		"emotion": "sad",
	})
}

func normalizeDeviceID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func xiaozhiSessionID(clientID, deviceID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(clientID) + "|" + normalizeDeviceID(deviceID)))
	return "xiaozhi-" + hex.EncodeToString(sum[:8])
}

func xiaozhiEmotion(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "happy", "sad", "angry", "surprised", "fearful", "disgusted":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "neutral"
	}
}
