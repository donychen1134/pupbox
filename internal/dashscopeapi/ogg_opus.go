package dashscopeapi

import (
	"encoding/binary"
	"errors"
	"time"
)

const oggOpusClockRate = 48000

// oggOpusWriter turns the raw Opus packets used by Xiaozhi into the continuous
// Ogg/Opus byte stream expected by DashScope's "opus" input format.
type oggOpusWriter struct {
	serial          uint32
	sequence        uint32
	granulePosition uint64
	frameSamples    uint64
	sampleRate      int
	channels        int
}

func newOggOpusWriter(sampleRate, channels, frameDurationMS int) *oggOpusWriter {
	return &oggOpusWriter{
		serial:       uint32(time.Now().UnixNano()),
		frameSamples: uint64(frameDurationMS * oggOpusClockRate / 1000),
		sampleRate:   sampleRate,
		channels:     channels,
	}
}

func (w *oggOpusWriter) Headers() [][]byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8] = 1
	head[9] = byte(w.channels)
	binary.LittleEndian.PutUint32(head[12:16], uint32(w.sampleRate))

	vendor := []byte("pupbox")
	tags := make([]byte, 8+4+len(vendor)+4)
	copy(tags, "OpusTags")
	binary.LittleEndian.PutUint32(tags[8:12], uint32(len(vendor)))
	copy(tags[12:], vendor)

	return [][]byte{
		w.writePage(head, 0x02, 0),
		w.writePage(tags, 0, 0),
	}
}

func (w *oggOpusWriter) WritePacket(packet []byte) ([]byte, error) {
	if len(packet) == 0 {
		return nil, errors.New("cannot wrap an empty Opus packet")
	}
	w.granulePosition += w.frameSamples
	return w.writePage(packet, 0, w.granulePosition), nil
}

func (w *oggOpusWriter) writePage(packet []byte, headerType byte, granule uint64) []byte {
	segmentCount := len(packet)/255 + 1
	page := make([]byte, 27+segmentCount+len(packet))
	copy(page, "OggS")
	page[4] = 0
	page[5] = headerType
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[14:18], w.serial)
	binary.LittleEndian.PutUint32(page[18:22], w.sequence)
	w.sequence++
	page[26] = byte(segmentCount)

	remaining := len(packet)
	for i := 0; i < segmentCount; i++ {
		segmentSize := min(remaining, 255)
		page[27+i] = byte(segmentSize)
		remaining -= segmentSize
	}
	copy(page[27+segmentCount:], packet)
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	return page
}

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, value := range data {
		crc ^= uint32(value) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
