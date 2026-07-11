package server

import (
	"encoding/binary"
	"math"
)

const (
	silenceWindowMS       = 20
	silencePreRollMS      = 200
	silencePostRollMS     = 300
	silenceMinSavingsMS   = 300
	silenceMinRMS         = 0.0045
	silenceMaxRMS         = 0.018
	silenceRelativeToPeak = 0.12
)

type pcmWAV struct {
	Channels      uint16
	SampleRate    uint32
	BitsPerSample uint16
	PCM           []byte
}

type SilenceTrimStats struct {
	OriginalDurationMS int64
	InputDurationMS    int64
	TrimmedMS          int64
	Applied            bool
}

func trimWAVSilence(data []byte) ([]byte, SilenceTrimStats, bool) {
	wav, ok := parsePCMWAV(data)
	if !ok || wav.BitsPerSample != 16 {
		return data, SilenceTrimStats{}, false
	}
	bytesPerFrame := int(wav.Channels) * 2
	totalFrames := len(wav.PCM) / bytesPerFrame
	originalMS := int64(totalFrames) * 1000 / int64(wav.SampleRate)
	stats := SilenceTrimStats{OriginalDurationMS: originalMS, InputDurationMS: originalMS}
	windowFrames := max(1, int(wav.SampleRate)*silenceWindowMS/1000)
	windowCount := (totalFrames + windowFrames - 1) / windowFrames
	levels := make([]float64, windowCount)
	peakRMS := 0.0
	for window := 0; window < windowCount; window++ {
		startFrame := window * windowFrames
		endFrame := min(startFrame+windowFrames, totalFrames)
		var sumSquares float64
		sampleCount := 0
		for offset := startFrame * bytesPerFrame; offset < endFrame*bytesPerFrame; offset += 2 {
			sample := float64(int16(binary.LittleEndian.Uint16(wav.PCM[offset:offset+2]))) / 32768
			sumSquares += sample * sample
			sampleCount++
		}
		if sampleCount > 0 {
			levels[window] = math.Sqrt(sumSquares / float64(sampleCount))
			peakRMS = max(peakRMS, levels[window])
		}
	}

	threshold := max(silenceMinRMS, min(silenceMaxRMS, peakRMS*silenceRelativeToPeak))
	active := make([]bool, len(levels))
	for index, level := range levels {
		active[index] = level >= threshold
	}
	stable := func(index int) bool {
		count := 0
		for neighbor := max(0, index-1); neighbor <= min(len(active)-1, index+1); neighbor++ {
			if active[neighbor] {
				count++
			}
		}
		return count >= 2
	}
	firstActive, lastActive := -1, -1
	for index := range active {
		if stable(index) {
			if firstActive < 0 {
				firstActive = index
			}
			lastActive = index
		}
	}
	if firstActive < 0 {
		return data, stats, true
	}

	preRollFrames := int(wav.SampleRate) * silencePreRollMS / 1000
	postRollFrames := int(wav.SampleRate) * silencePostRollMS / 1000
	startFrame := max(0, firstActive*windowFrames-preRollFrames)
	endFrame := min(totalFrames, (lastActive+1)*windowFrames+postRollFrames)
	inputMS := int64(endFrame-startFrame) * 1000 / int64(wav.SampleRate)
	trimmedMS := originalMS - inputMS
	if trimmedMS < silenceMinSavingsMS {
		return data, stats, true
	}

	startByte := startFrame * bytesPerFrame
	endByte := endFrame * bytesPerFrame
	trimmed := encodePCM16WAV(wav.Channels, wav.SampleRate, wav.PCM[startByte:endByte])
	stats.InputDurationMS = inputMS
	stats.TrimmedMS = trimmedMS
	stats.Applied = true
	return trimmed, stats, true
}

func parsePCMWAV(data []byte) (pcmWAV, bool) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return pcmWAV{}, false
	}
	var wav pcmWAV
	var audioFormat uint16
	for offset := 12; offset+8 <= len(data); {
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		body := offset + 8
		if chunkSize < 0 || body+chunkSize > len(data) {
			return pcmWAV{}, false
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if chunkSize >= 16 {
				audioFormat = binary.LittleEndian.Uint16(data[body : body+2])
				wav.Channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
				wav.SampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
				wav.BitsPerSample = binary.LittleEndian.Uint16(data[body+14 : body+16])
			}
		case "data":
			wav.PCM = data[body : body+chunkSize]
		}
		offset = body + chunkSize
		if offset%2 == 1 {
			offset++
		}
	}
	if audioFormat != 1 || wav.Channels == 0 || wav.SampleRate == 0 || wav.BitsPerSample == 0 || len(wav.PCM) == 0 {
		return pcmWAV{}, false
	}
	bytesPerFrame := int(wav.Channels) * int(wav.BitsPerSample) / 8
	if bytesPerFrame <= 0 || len(wav.PCM)%bytesPerFrame != 0 {
		return pcmWAV{}, false
	}
	return wav, true
}

func encodePCM16WAV(channels uint16, sampleRate uint32, pcm []byte) []byte {
	data := make([]byte, 44+len(pcm))
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+len(pcm)))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], channels)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	blockAlign := channels * 2
	binary.LittleEndian.PutUint32(data[28:32], sampleRate*uint32(blockAlign))
	binary.LittleEndian.PutUint16(data[32:34], blockAlign)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(pcm)))
	copy(data[44:], pcm)
	return data
}
