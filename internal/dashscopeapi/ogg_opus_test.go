package dashscopeapi

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestOggOpusWriterProducesValidStreamingPages(t *testing.T) {
	writer := newOggOpusWriter(16000, 1, 60)
	packets := [][]byte{
		{0xf8, 0xff, 0xfe},
		bytes.Repeat([]byte{0x7f}, 255),
	}
	pages := append([][]byte(nil), writer.Headers()...)
	for _, packet := range packets {
		page, err := writer.WritePacket(packet)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
	}

	serial := binary.LittleEndian.Uint32(pages[0][14:18])
	wantGranules := []uint64{0, 0, 2880, 5760}
	for i, page := range pages {
		if string(page[:4]) != "OggS" {
			t.Fatalf("page %d capture pattern = %q", i, page[:4])
		}
		if got := binary.LittleEndian.Uint32(page[14:18]); got != serial {
			t.Fatalf("page %d serial = %d, want %d", i, got, serial)
		}
		if got := binary.LittleEndian.Uint32(page[18:22]); got != uint32(i) {
			t.Fatalf("page %d sequence = %d", i, got)
		}
		if got := binary.LittleEndian.Uint64(page[6:14]); got != wantGranules[i] {
			t.Fatalf("page %d granule = %d, want %d", i, got, wantGranules[i])
		}

		wantCRC := binary.LittleEndian.Uint32(page[22:26])
		forCRC := append([]byte(nil), page...)
		clear(forCRC[22:26])
		if got := oggCRC(forCRC); got != wantCRC {
			t.Fatalf("page %d checksum = %#x, want %#x", i, got, wantCRC)
		}
	}

	if pages[0][5]&0x02 == 0 {
		t.Fatal("OpusHead page does not have beginning-of-stream flag")
	}
	if got := binary.LittleEndian.Uint32(pages[0][27+1+12 : 27+1+16]); got != 16000 {
		t.Fatalf("OpusHead input sample rate = %d", got)
	}

	stream := bytes.Join(pages, nil)
	parser := newOggPacketParser()
	var decoded [][]byte
	for offset := 0; offset < len(stream); {
		end := min(offset+17, len(stream))
		if err := parser.Feed(stream[offset:end], func(packet []byte) error {
			decoded = append(decoded, append([]byte(nil), packet...))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		offset = end
	}
	if len(decoded) != len(packets) {
		t.Fatalf("decoded packet count = %d, want %d", len(decoded), len(packets))
	}
	for i := range packets {
		if !bytes.Equal(decoded[i], packets[i]) {
			t.Fatalf("decoded packet %d does not match input", i)
		}
	}
}
