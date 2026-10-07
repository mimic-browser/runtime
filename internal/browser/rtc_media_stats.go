package browser

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// Count at the actual RTP boundary, without a timer or a stats worker per
// stream. No script state is accessed by transport callbacks.
type rtcPacketStats struct {
	ssrc           uint32
	typeName       string
	kind           string
	packets, bytes atomic.Uint64
}
type rtcStatsInterceptor struct {
	interceptor.NoOp
	mu      sync.Mutex
	streams []*rtcPacketStats
}

func (s *rtcStatsInterceptor) NewInterceptor(string) (interceptor.Interceptor, error) { return s, nil }
func (s *rtcStatsInterceptor) record(info *interceptor.StreamInfo, typeName string) *rtcPacketStats {
	kind := "video"
	if strings.HasPrefix(strings.ToLower(info.MimeType), "audio/") {
		kind = "audio"
	}
	record := &rtcPacketStats{ssrc: info.SSRC, typeName: typeName, kind: kind}
	s.mu.Lock()
	s.streams = append(s.streams, record)
	s.mu.Unlock()
	return record
}
func (s *rtcStatsInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	record := s.record(info, "outbound-rtp")
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attrs interceptor.Attributes) (int, error) {
		n, err := writer.Write(header, payload, attrs)
		if err == nil {
			record.packets.Add(1)
			record.bytes.Add(uint64(len(payload)))
		}
		return n, err
	})
}
func (s *rtcStatsInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	record := s.record(info, "inbound-rtp")
	return interceptor.RTPReaderFunc(func(buf []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attrs, err := reader.Read(buf, attrs)
		if err == nil {
			var header rtp.Header
			size, parseErr := header.Unmarshal(buf[:n])
			if parseErr == nil {
				record.packets.Add(1)
				record.bytes.Add(uint64(n - size))
			}
		}
		return n, attrs, err
	})
}
func (s *rtcStatsInterceptor) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := map[string]any{}
	for _, record := range s.streams {
		id := fmt.Sprintf("%s-%d", record.typeName, record.ssrc)
		row := map[string]any{"id": id, "type": record.typeName, "timestamp": float64(time.Now().UnixNano()) / 1e6, "ssrc": record.ssrc, "kind": record.kind}
		if record.typeName == "outbound-rtp" {
			row["packetsSent"] = record.packets.Load()
			row["bytesSent"] = record.bytes.Load()
		} else {
			row["packetsReceived"] = record.packets.Load()
			row["bytesReceived"] = record.bytes.Load()
		}
		rows[id] = row
	}
	return rows
}
