package server

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	xiaozhiSpeechPrebufferPackets = 4
	defaultDeviceVolume           = 60
	deviceVolumeStep              = 15
)

var arabicVolumePattern = regexp.MustCompile(`(?:音量|声音|调到|调成)[^0-9]{0,4}([0-9]{1,3})`)

func (c *xiaozhiConnection) requestDeviceStatus() {
	id := c.nextMCPID()
	c.deviceMu.Lock()
	c.statusMCPID = id
	c.deviceMu.Unlock()
	_ = c.writeMCPRequest(id, "self.get_device_status", map[string]any{})
}

func (c *xiaozhiConnection) setDeviceVolume(volume int) error {
	volume = c.clampDeviceVolume(volume)
	id := c.nextMCPID()
	if err := c.writeMCPRequest(id, "self.audio_speaker.set_volume", map[string]any{
		"volume": volume,
	}); err != nil {
		return err
	}
	c.deviceMu.Lock()
	c.volume = volume
	c.volumeKnown = true
	c.deviceMu.Unlock()
	return nil
}

func (c *xiaozhiConnection) writeMCPRequest(id int64, name string, arguments map[string]any) error {
	return c.writeJSON(map[string]any{
		"session_id": c.sessionID,
		"type":       "mcp",
		"payload": map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": arguments,
			},
		},
	})
}

func (c *xiaozhiConnection) nextMCPID() int64 {
	c.deviceMu.Lock()
	defer c.deviceMu.Unlock()
	c.mcpID++
	return c.mcpID
}

func (c *xiaozhiConnection) handleMCPResponse(payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	var response struct {
		ID     int64 `json:"id"`
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(payload, &response) != nil {
		return
	}
	c.deviceMu.Lock()
	isStatus := response.ID == c.statusMCPID
	c.deviceMu.Unlock()
	if !isStatus {
		return
	}
	for _, content := range response.Result.Content {
		var status struct {
			AudioSpeaker *struct {
				Volume int `json:"volume"`
			} `json:"audio_speaker"`
			Battery *struct {
				Level    int  `json:"level"`
				Charging bool `json:"charging"`
			} `json:"battery"`
		}
		if content.Type != "text" || json.Unmarshal([]byte(content.Text), &status) != nil {
			continue
		}
		rawVolume := 0
		volumeKnown := status.AudioSpeaker != nil
		if volumeKnown {
			rawVolume = status.AudioSpeaker.Volume
		}
		volume := c.clampDeviceVolume(rawVolume)
		c.deviceMu.Lock()
		if volumeKnown {
			c.volume = volume
			c.volumeKnown = true
		}
		c.deviceMu.Unlock()
		battery, charging := 0, false
		batteryKnown := status.Battery != nil
		if batteryKnown {
			battery = min(100, max(0, status.Battery.Level))
			charging = status.Battery.Charging
		}
		c.server.updateXiaozhiDeviceStatus(volume, volumeKnown, battery, batteryKnown, charging)
		if volumeKnown && rawVolume != volume {
			_ = c.setDeviceVolume(rawVolume)
		}
		return
	}
}

func (c *xiaozhiConnection) resolveVolumeCommand(text string) (int, string, bool) {
	command := strings.NewReplacer(" ", "", "，", "", ",", "", "。", "", "？", "", "?", "").Replace(text)
	hasSubject := strings.Contains(command, "声音") || strings.Contains(command, "音量") ||
		strings.Contains(command, "大声") || strings.Contains(command, "小声")
	standaloneAdjustment := containsExact(command,
		"大一点", "再大一点", "小一点", "再小一点", "轻一点", "再轻一点", "响一点", "再响一点")
	if !hasSubject && !standaloneAdjustment {
		return -1, "", false
	}

	if match := arabicVolumePattern.FindStringSubmatch(command); len(match) == 2 {
		volume, _ := strconv.Atoi(match[1])
		volume = c.clampDeviceVolume(volume)
		return volume, "好呀，豆豆把声音调好啦。", true
	}

	current := c.currentDeviceVolume()
	switch {
	case containsAny(command, "太小", "大一点", "大点", "调大", "调高", "高一点", "响一点", "大声一点"):
		return c.clampDeviceVolume(current + deviceVolumeStep), "好呀，豆豆大声一点。", true
	case containsAny(command, "太大", "小一点", "小点", "调小", "调低", "低一点", "轻一点", "小声一点"):
		return c.clampDeviceVolume(current - deviceVolumeStep), "好呀，豆豆小声一点。", true
	case containsAny(command, "最大", "最响"):
		return c.server.xiaozhi.volumeMax, "好呀，豆豆把声音调到安全的最大音量啦。", true
	case containsAny(command, "最小", "最轻"):
		return c.server.xiaozhi.volumeMin, "好呀，豆豆小声说话。", true
	case containsAny(command, "能调", "可以调", "调整", "调节", "怎么调"):
		return -1, "可以呀。你可以说，声音大一点，或者声音小一点。", true
	default:
		return -1, "", false
	}
}

func (c *xiaozhiConnection) currentDeviceVolume() int {
	c.deviceMu.Lock()
	defer c.deviceMu.Unlock()
	if !c.volumeKnown {
		return defaultDeviceVolume
	}
	return c.volume
}

func (c *xiaozhiConnection) clampDeviceVolume(volume int) int {
	minimum := c.server.xiaozhi.volumeMin
	maximum := c.server.xiaozhi.volumeMax
	if minimum <= 0 {
		minimum = 20
	}
	if maximum < minimum || maximum > 100 {
		maximum = 75
	}
	return min(maximum, max(minimum, volume))
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func containsExact(text string, values ...string) bool {
	for _, value := range values {
		if text == value {
			return true
		}
	}
	return false
}

func (c *xiaozhiConnection) streamSpeechOpus(ctx context.Context, text string, onFirstPacket func()) error {
	packets := make(chan []byte, 64)
	result := make(chan error, 1)
	go func() {
		err := c.voice.StreamSpeakOpus(ctx, text, func(packet []byte) error {
			packet = append([]byte(nil), packet...)
			select {
			case packets <- packet:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(packets)
		result <- err
	}()

	buffered := make([][]byte, 0, xiaozhiSpeechPrebufferPackets)
	for len(buffered) < xiaozhiSpeechPrebufferPackets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet, ok := <-packets:
			if !ok {
				return c.playBufferedOpus(ctx, buffered, nil, result, onFirstPacket)
			}
			buffered = append(buffered, packet)
		}
	}
	return c.playBufferedOpus(ctx, buffered, packets, result, onFirstPacket)
}

func (c *xiaozhiConnection) playBufferedOpus(
	ctx context.Context,
	buffered [][]byte,
	packets <-chan []byte,
	result <-chan error,
	onFirstPacket func(),
) error {
	nextPacketAt := time.Now()
	first := true
	writePacket := func(packet []byte) error {
		if wait := time.Until(nextPacketAt); wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		if first {
			first = false
			if onFirstPacket != nil {
				onFirstPacket()
			}
		}
		if err := c.writeBinary(packet); err != nil {
			return err
		}
		nextPacketAt = nextPacketAt.Add(time.Duration(c.frameMS) * time.Millisecond)
		return nil
	}

	for _, packet := range buffered {
		if err := writePacket(packet); err != nil {
			return err
		}
	}
	if packets != nil {
		for packet := range packets {
			if err := writePacket(packet); err != nil {
				return err
			}
		}
	}
	err := <-result
	if len(buffered) == 0 && err == nil {
		return errors.New("streaming speech returned no Opus packets")
	}
	return err
}
