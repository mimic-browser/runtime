// Package imageresource decodes resource bytes and intrinsic metadata. It never
// renders a DOM, SVG scene, or GPU surface.
package imageresource

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"github.com/gen2brain/gav1d/avif"
	_ "golang.org/x/image/webp"
	"hash/adler32"
	"hash/crc32"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Metadata struct {
	Width, Height     int
	Vector            bool
	PixelsUnavailable bool
}

type DecodedImage struct {
	Metadata
	Pixels []byte
}

// Resource is the single immutable backing for every WebAPI view of an image.
// Metadata, validation and pixels are monotonic capabilities; consumers cannot
// observe or retain a partially materialized bitmap.
type Resource struct {
	decodeBudget      func(int64) error
	metadataOnly      bool
	compressedBytes   []byte
	contentType       string
	metadataOnce      sync.Once
	metadata          Metadata
	metadataFormat    string
	metadataErr       error
	validationOnce    sync.Once
	validationErr     error
	pixelsUnavailable bool
	decodeOnce        sync.Once
	decoded           *DecodedImage
	decodeErr         error
}

func New(data []byte, contentType string) *Resource {
	owned := make([]byte, len(data))
	copy(owned, data)
	return &Resource{compressedBytes: owned, contentType: contentType}
}

// NewWithDecodeBudget charges logical RGBA pixel work immediately before each
// raster decode. The ordinary constructor keeps the original path unchanged.
func NewWithDecodeBudget(data []byte, contentType string, budget func(int64) error) *Resource {
	r := New(data, contentType)
	r.decodeBudget = budget
	return r
}

// MetadataOnly parses intrinsic dimensions without retaining the encoded
// representation. It is used only by an explicit resource policy which denies
// later image decoding; ordinary image resources retain their existing path.
func MetadataOnly(data []byte, contentType string) (*Resource, error) {
	metadata, format, err := decodeMetadata(data, contentType)
	if err != nil {
		return nil, err
	}
	r := &Resource{contentType: contentType, metadata: metadata, metadataFormat: format, metadataOnly: true}
	r.metadataOnce.Do(func() {})
	return r, nil
}

func (r *Resource) RequireMetadata() (Metadata, error) {
	r.metadataOnce.Do(func() { r.metadata, r.metadataFormat, r.metadataErr = decodeMetadata(r.compressedBytes, r.contentType) })
	return r.metadata, r.metadataErr
}

func (r *Resource) RequireValidatedImage() (Metadata, error) {
	if r.metadataOnly {
		return Metadata{}, fmt.Errorf("image body not retained by resource policy")
	}
	metadata, err := r.RequireMetadata()
	if err != nil {
		return Metadata{}, err
	}
	r.validationOnce.Do(func() {
		if metadata.Vector {
			return
		}
		if r.decodeBudget != nil {
			if err := r.decodeBudget(int64(metadata.Width) * int64(metadata.Height) * 4); err != nil {
				r.validationErr = err
				return
			}
		}
		_, unavailable, err := decodeRaster(r.compressedBytes, r.metadataFormat)
		r.pixelsUnavailable, r.validationErr = unavailable, err
	})
	metadata.PixelsUnavailable = r.pixelsUnavailable
	return metadata, r.validationErr
}

func (r *Resource) RequireDecodedImage() (*DecodedImage, error) {
	metadata, err := r.RequireValidatedImage()
	if err != nil {
		return nil, err
	}
	r.decodeOnce.Do(func() {
		result := &DecodedImage{Metadata: metadata}
		if metadata.Vector || metadata.PixelsUnavailable {
			r.decoded = result
			return
		}
		if r.decodeBudget != nil {
			if err := r.decodeBudget(int64(metadata.Width) * int64(metadata.Height) * 4); err != nil {
				r.decodeErr = err
				return
			}
		}
		decoded, _, decodeErr := decodeRaster(r.compressedBytes, r.metadataFormat)
		if decodeErr != nil {
			r.decodeErr = decodeErr
			return
		}
		width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
		pixels := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
		result.Width, result.Height, result.Pixels = width, height, pixels.Pix
		r.decoded = result
	})
	return r.decoded, r.decodeErr
}

func (r *Resource) RetainedBytes() int {
	n := cap(r.compressedBytes)
	if r.decoded != nil {
		n += cap(r.decoded.Pixels)
	}
	return n
}

// Image is retained as the eager compatibility result for package callers.
type Image struct {
	Width, Height int
	Pixels        []byte
	Vector        bool
	// Valid intrinsic metadata can outlive an unavailable pixel stream. Chrome
	// accepts that resource for Image loading, but ImageBitmap cannot use it.
	PixelsUnavailable bool
}

func Decode(data []byte, contentType string) (*Image, error) {
	decoded, err := New(data, contentType).RequireDecodedImage()
	if err != nil {
		return nil, err
	}
	return &Image{Width: decoded.Width, Height: decoded.Height, Pixels: decoded.Pixels, Vector: decoded.Vector, PixelsUnavailable: decoded.PixelsUnavailable}, nil
}

func decodeMetadata(data []byte, contentType string) (Metadata, string, error) {
	if len(data) > 32<<20 {
		return Metadata{}, "", fmt.Errorf("image byte limit")
	}
	// An SVG tag in a raster file's metadata is ordinary payload text. Sniff
	// only document-shaped bytes; PNG ancillary chunks can contain arbitrary XML.
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	looksLikeSVG := len(trimmed) > 0 && trimmed[0] == '<' && bytes.Contains(trimmed[:min(len(trimmed), 512)], []byte("<svg"))
	if strings.Contains(strings.ToLower(contentType), "svg") || looksLikeSVG {
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, err := decoder.Token()
			if err != nil {
				return Metadata{}, "", err
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local != "svg" || start.Name.Space != "http://www.w3.org/2000/svg" {
				return Metadata{}, "", fmt.Errorf("invalid SVG image root")
			}
			attrs := map[string]string{}
			for _, a := range start.Attr {
				attrs[a.Name.Local] = a.Value
			}
			width, hasWidth := intrinsicLength(attrs["width"])
			height, hasHeight := intrinsicLength(attrs["height"])
			ratio := 0.0
			parts := strings.Fields(strings.ReplaceAll(attrs["viewBox"], ",", " "))
			if len(parts) == 4 {
				vw, e1 := strconv.ParseFloat(parts[2], 64)
				vh, e2 := strconv.ParseFloat(parts[3], 64)
				if e1 == nil && e2 == nil && vw > 0 && vh > 0 && !math.IsInf(vw/vh, 0) {
					ratio = vw / vh
				}
			}
			if !hasWidth {
				width = 300
			}
			if !hasHeight {
				height = 150
			}
			if ratio > 0 {
				if hasWidth && !hasHeight {
					height = width / ratio
				} else if hasHeight && !hasWidth {
					width = height * ratio
				} else if !hasWidth && !hasHeight {
					if ratio > 2 {
						height = width / ratio
					} else {
						width = height * ratio
					}
				}
			}
			if width > 16384 || height > 16384 {
				return Metadata{}, "", fmt.Errorf("image dimension limit")
			}
			w, h := int(math.Round(width)), int(math.Round(height))
			if w > 16384 || h > 16384 {
				return Metadata{}, "", fmt.Errorf("image dimension limit")
			}
			for {
				_, err = decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					return Metadata{}, "", err
				}
			}
			return Metadata{Width: w, Height: h, Vector: true}, "svg", nil
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Metadata{}, "", err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16777216 {
		return Metadata{}, "", fmt.Errorf("image dimension limit")
	}
	return Metadata{Width: config.Width, Height: config.Height}, format, nil
}

func decodeRaster(data []byte, format string) (image.Image, bool, error) {
	var decoded image.Image
	var err error
	if format == "avif" {
		// Decode into the same intrinsic pixel state used by images and Canvas.
		// No renderer, GPU or native graphics library is involved. Bound codec
		// allocations as well as the final image, including internal AV1 frames.
		decoded, err = avif.Decode(bytes.NewReader(data), avif.Options{AutoRotate: true, FrameSizeLimit: 16777216})
	} else {
		decoded, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil && format == "png" {
		if repaired := repairPNGAdler(data); repaired != nil {
			decoded, _, err = image.Decode(bytes.NewReader(repaired))
		}
		if err != nil {
			return nil, true, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width <= 0 || height <= 0 || int64(width)*int64(height) > 16777216 {
		return nil, false, fmt.Errorf("image dimension limit")
	}
	return decoded, false, nil
}

// Chrome accepts a PNG's complete decompressed stream despite an invalid Adler
// trailer. Repair only that verified condition; invalid or incomplete deflate
// data remain unavailable, with their separately parsed intrinsic metadata.
func repairPNGAdler(data []byte) []byte {
	var stream []byte
	for at := 8; at+12 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[at:]))
		if size > len(data)-at-12 {
			return nil
		}
		if string(data[at+4:at+8]) == "IDAT" {
			stream = append(stream, data[at+8:at+8+size]...)
		}
		at += size + 12
	}
	if len(stream) < 6 {
		return nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 128<<20))
	reader.Close()
	if readErr != zlib.ErrChecksum {
		return nil
	}
	binary.BigEndian.PutUint32(stream[len(stream)-4:], adler32.Checksum(raw))
	result := append([]byte(nil), data[:8]...)
	written := false
	for at := 8; at+12 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[at:]))
		if size > len(data)-at-12 {
			return nil
		}
		if string(data[at+4:at+8]) == "IDAT" {
			if !written {
				result = binary.BigEndian.AppendUint32(result, uint32(len(stream)))
				start := len(result)
				result = append(result, []byte("IDAT")...)
				result = append(result, stream...)
				result = binary.BigEndian.AppendUint32(result, crc32.ChecksumIEEE(result[start:]))
				written = true
			}
		} else {
			result = append(result, data[at:at+size+12]...)
		}
		at += size + 12
	}
	return result
}

var intrinsicNumber = regexp.MustCompile(`^([+-]?(?:[0-9]*\.[0-9]+|[0-9]+)(?:[eE][+-]?[0-9]+)?)(px|in|cm|mm|q|pt|pc)?$`)

func intrinsicLength(raw string) (float64, bool) {
	match := intrinsicNumber.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	scale := map[string]float64{"": 1, "px": 1, "in": 96, "cm": 96 / 2.54, "mm": 96 / 25.4, "q": 96 / 101.6, "pt": 96.0 / 72, "pc": 16}[match[2]]
	return math.Max(0, value*scale), true
}
