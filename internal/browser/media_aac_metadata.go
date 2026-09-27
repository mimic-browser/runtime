package browser

import "fmt"

func mpeg4Descriptor(data []byte, tag byte) ([]byte, error) {
	if len(data) < 2 || data[0] != tag {
		return nil, fmt.Errorf("unsupported MPEG-4 initialization descriptor")
	}
	size := uint32(0)
	for i := 1; i <= 4; i++ {
		if len(data) <= i {
			break
		}
		size = size<<7 | uint32(data[i]&127)
		if data[i]&128 == 0 {
			if uint64(i+1)+uint64(size) > uint64(len(data)) {
				break
			}
			return data[i+1 : i+1+int(size)], nil
		}
	}
	return nil, fmt.Errorf("truncated MPEG-4 initialization descriptor")
}

func validateAACInitialization(data []byte) error {
	if len(data) < 4 || data[0] != 0 {
		return fmt.Errorf("unsupported MPEG-4 ES descriptor version")
	}
	es, err := mpeg4Descriptor(data[4:], 3)
	if err != nil || len(es) < 3 {
		return fmt.Errorf("unsupported MPEG-4 elementary stream descriptor")
	}
	flags := es[2]
	offset := 3
	if flags&128 != 0 {
		offset += 2
	}
	if flags&64 != 0 {
		if offset >= len(es) {
			return fmt.Errorf("truncated MPEG-4 URL descriptor")
		}
		offset += 1 + int(es[offset])
	}
	if flags&32 != 0 {
		offset += 2
	}
	if offset > len(es) {
		return fmt.Errorf("truncated MPEG-4 stream descriptor flags")
	}
	decoder, err := mpeg4Descriptor(es[offset:], 4)
	if err != nil || len(decoder) < 13 || decoder[0] != 64 || decoder[1]>>2 != 5 {
		return fmt.Errorf("unsupported AAC decoder configuration")
	}
	configuration, err := mpeg4Descriptor(decoder[13:], 5)
	if err != nil {
		return err
	}
	bits := mediaMetadataBits{bytes: configuration}
	objectType := bits.read(5)
	frequency := bits.read(4)
	if frequency == 15 {
		if bits.read(24) == 0 {
			bits.invalid = true
		}
	} else if frequency > 12 {
		bits.invalid = true
	}
	channels := bits.read(4)
	if bits.invalid || objectType != 2 || channels == 0 || channels > 7 {
		return fmt.Errorf("unsupported AAC audio-specific initialization")
	}
	return nil
}

type mediaMetadataBits struct {
	bytes    []byte
	position int
	invalid  bool
}

func (b *mediaMetadataBits) read(count int) uint32 {
	if b.invalid || count < 0 || count > 32 || b.position+count > len(b.bytes)*8 {
		b.invalid = true
		return 0
	}
	var value uint32
	for i := 0; i < count; i++ {
		value = value<<1 | uint32((b.bytes[b.position/8]>>uint(7-b.position%8))&1)
		b.position++
	}
	return value
}
