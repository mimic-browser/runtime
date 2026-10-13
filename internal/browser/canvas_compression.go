package browser

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"math"
)

// Canvas serialization is synchronous. Keep compression behind a private host
// boundary so every canvas surface uses one deterministic zlib implementation.
func compressCanvasScanline(scan []byte) ([]byte, error) {
	var output bytes.Buffer
	writer, err := zlib.NewWriterLevel(&output, zlib.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(scan); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func blurCanvasMask(sourceBytes []byte, width, height int, weightBytes []byte, outLeft, outTop, outWidth, outHeight int) ([]byte, error) {
	if width < 0 || height < 0 || len(sourceBytes) != width*height*4 || len(weightBytes)%8 != 0 ||
		outLeft < 0 || outTop < 0 || outWidth < 0 || outHeight < 0 || outLeft+outWidth > width || outTop+outHeight > height {
		return nil, fmt.Errorf("invalid canvas blur mask")
	}
	source := make([]float32, width*height)
	for i := range source {
		source[i] = math.Float32frombits(binary.LittleEndian.Uint32(sourceBytes[i*4:]))
	}
	weights := make([]float64, len(weightBytes)/8)
	for i := range weights {
		weights[i] = math.Float64frombits(binary.LittleEndian.Uint64(weightBytes[i*8:]))
	}
	if len(weights) == 0 {
		return nil, fmt.Errorf("invalid canvas blur kernel")
	}
	radius := len(weights) - 1
	rowTop := max(0, outTop-radius)
	rowBottom := min(height, outTop+outHeight+radius)
	horizontal := make([]float32, (rowBottom-rowTop)*outWidth)
	for y := rowTop; y < rowBottom; y++ {
		for x := outLeft; x < outLeft+outWidth; x++ {
			value := float64(source[y*width+x]) * weights[0]
			for k := 1; k <= radius; k++ {
				if x-k >= 0 {
					value += float64(source[y*width+x-k]) * weights[k]
				}
				if x+k < width {
					value += float64(source[y*width+x+k]) * weights[k]
				}
			}
			horizontal[(y-rowTop)*outWidth+x-outLeft] = float32(value)
		}
	}
	output := make([]byte, outWidth*outHeight*4)
	for y := outTop; y < outTop+outHeight; y++ {
		for x := outLeft; x < outLeft+outWidth; x++ {
			column := x - outLeft
			value := float64(horizontal[(y-rowTop)*outWidth+column]) * weights[0]
			for k := 1; k <= radius; k++ {
				if y-k >= 0 {
					value += float64(horizontal[(y-k-rowTop)*outWidth+column]) * weights[k]
				}
				if y+k < height {
					value += float64(horizontal[(y+k-rowTop)*outWidth+column]) * weights[k]
				}
			}
			binary.LittleEndian.PutUint32(output[((y-outTop)*outWidth+column)*4:], math.Float32bits(float32(value)))
		}
	}
	return output, nil
}
