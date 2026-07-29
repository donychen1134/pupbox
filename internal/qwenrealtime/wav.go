package qwenrealtime

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func DecodeWAV16KMono(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("recording is not a RIFF/WAVE file")
	}
	var formatFound bool
	var pcm []byte
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if size < 0 || offset+size > len(data) {
			return nil, errors.New("WAV chunk exceeds file size")
		}
		chunk := data[offset : offset+size]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, errors.New("WAV format chunk is truncated")
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			channels := binary.LittleEndian.Uint16(chunk[2:4])
			rate := binary.LittleEndian.Uint32(chunk[4:8])
			bits := binary.LittleEndian.Uint16(chunk[14:16])
			if format != 1 || channels != 1 || rate != 16000 || bits != 16 {
				return nil, fmt.Errorf("WAV must be PCM 16 kHz 16-bit mono, got format=%d channels=%d rate=%d bits=%d", format, channels, rate, bits)
			}
			formatFound = true
		case "data":
			pcm = append([]byte(nil), chunk...)
		}
		offset += size
		if size%2 != 0 {
			offset++
		}
	}
	if !formatFound || len(pcm) == 0 {
		return nil, errors.New("WAV is missing PCM format or audio data")
	}
	if len(pcm)%2 != 0 {
		return nil, errors.New("WAV PCM data has an incomplete sample")
	}
	return pcm, nil
}
