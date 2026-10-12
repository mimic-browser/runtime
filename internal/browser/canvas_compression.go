package browser

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
)

// Canvas serialization is synchronous. Keep compression behind a private host
// boundary so every canvas surface uses one deterministic zlib implementation.
func compressCanvasScanline(encoded string) (string, error) {
	scan, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid canvas scanline: %w", err)
	}
	var output bytes.Buffer
	writer, err := zlib.NewWriterLevel(&output, zlib.BestSpeed)
	if err != nil {
		return "", err
	}
	if _, err := writer.Write(scan); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(output.Bytes()), nil
}
