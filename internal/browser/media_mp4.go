package browser

import (
	"encoding/binary"
	"fmt"
	"math"
)

// MP4 initialization stores timing/track metadata, never coded sample payload.
// Headers and a partial moov are retained only until the initialization box is
// complete. Media fragments remain an explicit unsupported boundary.
type mp4InitParser struct {
	pending   []byte
	remaining uint64
	kind      string
	skip      bool
}

type mp4Initialization struct {
	duration float64
	tracks   []mp4TrackMetadata
}

type mp4TrackMetadata struct {
	id        uint32
	timescale uint32
	width     int
	height    int
	codec     string
}

func (p *mp4InitParser) append(data []byte) (*mp4Initialization, error) {
	var initialized *mp4Initialization
	for len(data) != 0 {
		if p.remaining != 0 {
			n := uint64(len(data))
			if n > p.remaining {
				n = p.remaining
			}
			if !p.skip {
				p.pending = append(p.pending, data[:int(n)]...)
			}
			p.remaining -= n
			data = data[int(n):]
			if p.remaining != 0 {
				continue
			}
			if p.kind == "moov" {
				metadata, err := parseMP4Initialization(p.pending)
				if err != nil {
					return nil, err
				}
				initialized = metadata
			}
			p.pending = nil
			p.kind = ""
			continue
		}
		needed := 8
		if len(p.pending) >= 8 && binary.BigEndian.Uint32(p.pending[:4]) == 1 {
			needed = 16
		}
		if len(p.pending) < needed {
			n := needed - len(p.pending)
			if n > len(data) {
				n = len(data)
			}
			p.pending = append(p.pending, data[:n]...)
			data = data[n:]
			if len(p.pending) < needed {
				continue
			}
			if needed == 8 && binary.BigEndian.Uint32(p.pending[:4]) == 1 {
				continue
			}
		}
		size := uint64(binary.BigEndian.Uint32(p.pending[:4]))
		if needed == 16 {
			size = binary.BigEndian.Uint64(p.pending[8:16])
		}
		if size < uint64(needed) || size > uint64(math.MaxInt) {
			return nil, fmt.Errorf("unsupported MP4 initialization box size")
		}
		p.kind = string(p.pending[4:8])
		p.skip = false
		switch p.kind {
		case "moov":
		case "ftyp", "free", "skip", "styp":
			p.skip = true
		default:
			return nil, fmt.Errorf("unsupported MP4 box %q: media fragment parsing is unavailable", p.kind)
		}
		p.remaining = size - uint64(needed)
		// parseMP4Initialization consumes the payload, not the outer header.
		p.pending = nil
		if p.remaining == 0 && p.kind == "moov" {
			return nil, fmt.Errorf("unsupported empty MP4 initialization")
		}
	}
	return initialized, nil
}

type mp4Box struct {
	kind string
	data []byte
}

func mp4Boxes(data []byte) ([]mp4Box, error) {
	var boxes []mp4Box
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, fmt.Errorf("truncated MP4 metadata header")
		}
		size := uint64(binary.BigEndian.Uint32(data[:4]))
		header := uint64(8)
		if size == 1 {
			if len(data) < 16 {
				return nil, fmt.Errorf("truncated MP4 extended metadata header")
			}
			size = binary.BigEndian.Uint64(data[8:16])
			header = 16
		}
		if size < header || size > uint64(len(data)) {
			return nil, fmt.Errorf("invalid MP4 metadata box bounds")
		}
		boxes = append(boxes, mp4Box{kind: string(data[4:8]), data: data[int(header):int(size)]})
		data = data[int(size):]
	}
	return boxes, nil
}

func mp4Child(data []byte, name string) ([]byte, error) {
	boxes, err := mp4Boxes(data)
	if err != nil {
		return nil, err
	}
	for _, box := range boxes {
		if box.kind == name {
			return box.data, nil
		}
	}
	return nil, fmt.Errorf("unsupported MP4 initialization: missing %s", name)
}

func mp4Times(data []byte) (uint32, uint64, error) {
	if len(data) < 20 {
		return 0, 0, fmt.Errorf("truncated MP4 timing metadata")
	}
	if data[0] == 0 {
		return binary.BigEndian.Uint32(data[12:16]), uint64(binary.BigEndian.Uint32(data[16:20])), nil
	}
	if data[0] != 1 || len(data) < 32 {
		return 0, 0, fmt.Errorf("unsupported MP4 timing metadata version")
	}
	return binary.BigEndian.Uint32(data[20:24]), binary.BigEndian.Uint64(data[24:32]), nil
}

func parseMP4Initialization(data []byte) (*mp4Initialization, error) {
	boxes, err := mp4Boxes(data)
	if err != nil {
		return nil, err
	}
	result := &mp4Initialization{duration: math.Inf(1)}
	fragmented := false
	for _, box := range boxes {
		switch box.kind {
		case "mvex":
			fragmented = true
		case "mvhd":
			scale, duration, err := mp4Times(box.data)
			if err != nil || scale == 0 {
				return nil, fmt.Errorf("unsupported MP4 movie timing")
			}
			if duration != 0 && duration != math.MaxUint32 && duration != math.MaxUint64 {
				result.duration = float64(duration) / float64(scale)
			}
		case "trak":
			track, err := parseMP4Track(box.data)
			if err != nil {
				return nil, err
			}
			result.tracks = append(result.tracks, track)
		}
	}
	if !fragmented || len(result.tracks) != 1 {
		return nil, fmt.Errorf("unsupported nonfragmented MP4 initialization")
	}
	return result, nil
}

func parseMP4Track(data []byte) (mp4TrackMetadata, error) {
	var result mp4TrackMetadata
	tkhd, err := mp4Child(data, "tkhd")
	if err != nil || len(tkhd) < 16 {
		return result, fmt.Errorf("unsupported MP4 track identity")
	}
	idOffset := 12
	if tkhd[0] == 1 {
		idOffset = 20
	} else if tkhd[0] != 0 {
		return result, fmt.Errorf("unsupported MP4 track version")
	}
	if len(tkhd) < idOffset+4 {
		return result, fmt.Errorf("truncated MP4 track identity")
	}
	result.id = binary.BigEndian.Uint32(tkhd[idOffset : idOffset+4])
	mdia, err := mp4Child(data, "mdia")
	if err != nil {
		return result, err
	}
	mdhd, err := mp4Child(mdia, "mdhd")
	if err != nil {
		return result, err
	}
	result.timescale, _, err = mp4Times(mdhd)
	if err != nil || result.timescale == 0 {
		return result, fmt.Errorf("unsupported MP4 track timing")
	}
	minf, err := mp4Child(mdia, "minf")
	if err != nil {
		return result, err
	}
	stbl, err := mp4Child(minf, "stbl")
	if err != nil {
		return result, err
	}
	for _, name := range []string{"stts", "stsc", "stco"} {
		table, err := mp4Child(stbl, name)
		if err != nil || len(table) < 8 || binary.BigEndian.Uint32(table[4:8]) != 0 {
			return result, fmt.Errorf("unsupported populated MP4 %s table", name)
		}
	}
	stsd, err := mp4Child(stbl, "stsd")
	if err != nil || len(stsd) < 8 || binary.BigEndian.Uint32(stsd[4:8]) != 1 {
		return result, fmt.Errorf("unsupported MP4 sample descriptions")
	}
	entries, err := mp4Boxes(stsd[8:])
	if err != nil || len(entries) != 1 {
		return result, fmt.Errorf("unsupported MP4 codec initialization")
	}
	entry := entries[0]
	if entry.kind == "mp4a" {
		if len(entry.data) < 28 || binary.BigEndian.Uint16(entry.data[8:10]) != 0 {
			return result, fmt.Errorf("unsupported MP4 audio sample entry")
		}
		configuration, err := mp4Child(entry.data[28:], "esds")
		if err != nil {
			return result, err
		}
		if err := validateAACInitialization(configuration); err != nil {
			return result, err
		}
		result.codec = "aac"
		return result, nil
	}
	if (entry.kind != "avc1" && entry.kind != "avc3") || len(entry.data) < 78 {
		return result, fmt.Errorf("unsupported MP4 codec initialization")
	}
	result.codec = "avc"
	configuration, err := mp4Child(entries[0].data[78:], "avcC")
	if err != nil {
		return result, err
	}
	if err := validateAVCInitialization(configuration); err != nil {
		return result, err
	}
	result.width = int(binary.BigEndian.Uint16(entry.data[24:26]))
	result.height = int(binary.BigEndian.Uint16(entry.data[26:28]))
	if result.width == 0 || result.height == 0 {
		return result, fmt.Errorf("unsupported MP4 video dimensions")
	}
	displayWidth := binary.BigEndian.Uint32(tkhd[len(tkhd)-8 : len(tkhd)-4])
	displayHeight := binary.BigEndian.Uint32(tkhd[len(tkhd)-4:])
	if displayWidth != 0 && displayHeight != 0 {
		if uint64(result.width)*uint64(displayHeight) > uint64(result.height)*uint64(displayWidth) {
			result.height = int(math.Round(float64(result.width) * float64(displayHeight) / float64(displayWidth)))
		} else {
			result.width = int(math.Round(float64(result.height) * float64(displayWidth) / float64(displayHeight)))
		}
	}
	return result, nil
}
