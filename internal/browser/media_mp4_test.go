package browser

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"regexp"
	"testing"
)

func authoredMP4Initialization(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile("testdata/media_source_mp4_init_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`atob\('([^']+)'\)`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("authored metadata fixture has no initialization bytes")
	}
	data, err := base64.StdEncoding.DecodeString(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMP4InitializationChunkingAndRetention(t *testing.T) {
	data := authoredMP4Initialization(t)
	for _, chunk := range []int{1, 7, 37, len(data)} {
		parser := &mp4InitParser{}
		var metadata *mp4Initialization
		for offset := 0; offset < len(data); offset += chunk {
			end := offset + chunk
			if end > len(data) {
				end = len(data)
			}
			value, err := parser.append(data[offset:end])
			if err != nil {
				t.Fatalf("chunk=%d offset=%d: %v", chunk, offset, err)
			}
			if value != nil {
				metadata = value
			}
		}
		if metadata == nil || len(metadata.tracks) != 1 || metadata.tracks[0].width != 16 || metadata.tracks[0].height != 16 || !math.IsInf(metadata.duration, 1) {
			t.Fatalf("chunk=%d metadata=%+v", chunk, metadata)
		}
		if len(parser.pending) != 0 || cap(parser.pending) != 0 || parser.remaining != 0 {
			t.Fatalf("completed initialization retained input bytes: %+v", parser)
		}
		payload := make([]byte, 1024*1024)
		binary.BigEndian.PutUint32(payload[:4], uint32(len(payload)))
		copy(payload[4:8], "mdat")
		if _, err := parser.append(payload); err == nil {
			t.Fatal("unsupported media samples became successful metadata")
		}
		if len(parser.pending) > 16 {
			t.Fatalf("parser retained unsupported sample payload: %d bytes", len(parser.pending))
		}
	}
}

func TestAVCMetadataTruncationDoesNotPanic(t *testing.T) {
	for length := 0; length < 64; length++ {
		if err := validateAVCInitialization(make([]byte, length)); err == nil {
			t.Fatalf("accepted invalid configuration of length %d", length)
		}
	}
}
