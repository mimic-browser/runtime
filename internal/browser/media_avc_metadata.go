package browser

import (
	"encoding/binary"
	"fmt"
)

// Validate parameter-set boundaries without decoding frames or deriving dimensions
// from SPS data. The observable initialization dimensions come from MP4 metadata.
func validateAVCInitialization(data []byte) error {
	if len(data) < 7 || data[0] != 1 || data[5]&31 == 0 {
		return fmt.Errorf("unsupported AVC initialization configuration")
	}
	offset := 6
	for _, group := range []struct{ count, kind byte }{{data[5] & 31, 7}, {0, 8}} {
		count := group.count
		if group.kind == 8 {
			if offset >= len(data) || data[offset] == 0 {
				return fmt.Errorf("unsupported AVC picture parameter set")
			}
			count = data[offset]
			offset++
		}
		for index := byte(0); index < count; index++ {
			if offset+2 > len(data) {
				return fmt.Errorf("truncated AVC parameter set")
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if length == 0 || offset+length > len(data) || data[offset]&31 != group.kind {
				return fmt.Errorf("unsupported AVC parameter set bounds")
			}
			offset += length
		}
	}
	return nil
}
