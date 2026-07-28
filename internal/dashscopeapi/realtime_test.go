package dashscopeapi

import (
	"encoding/binary"
	"testing"
)

func TestOggPacketParserHandlesSplitPagesAndSkipsHeaders(t *testing.T) {
	stream := append(oggTestPage([][]byte{[]byte("OpusHead-placeholder")}), oggTestPage([][]byte{[]byte("OpusTags-placeholder")})...)
	stream = append(stream, oggTestPage([][]byte{{1, 2, 3}, {4, 5}})...)

	parser := newOggPacketParser()
	var packets [][]byte
	for offset := 0; offset < len(stream); {
		end := min(offset+7, len(stream))
		if err := parser.Feed(stream[offset:end], func(packet []byte) error {
			packets = append(packets, append([]byte(nil), packet...))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		offset = end
	}

	if len(packets) != 2 {
		t.Fatalf("packets = %d, want 2", len(packets))
	}
	if string(packets[0]) != string([]byte{1, 2, 3}) || string(packets[1]) != string([]byte{4, 5}) {
		t.Fatalf("packets = %v", packets)
	}
}

func oggTestPage(packets [][]byte) []byte {
	segments := make([]byte, 0, len(packets))
	body := make([]byte, 0)
	for _, packet := range packets {
		for len(packet) >= 255 {
			segments = append(segments, 255)
			body = append(body, packet[:255]...)
			packet = packet[255:]
		}
		segments = append(segments, byte(len(packet)))
		body = append(body, packet...)
	}
	page := make([]byte, 27+len(segments)+len(body))
	copy(page[:4], "OggS")
	page[4] = 0
	binary.LittleEndian.PutUint32(page[14:18], 1)
	page[26] = byte(len(segments))
	copy(page[27:], segments)
	copy(page[27+len(segments):], body)
	return page
}
