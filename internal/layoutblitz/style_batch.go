package layoutblitz

/*
#include <stdint.h>
#include <stddef.h>
typedef struct MimicBlitzHandle MimicBlitzHandle;
int32_t mimic_blitz_style_batch(MimicBlitzHandle*, uint64_t, const char*, size_t, uint8_t*, size_t, size_t*);
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// StyleBatch resolves all requested properties from one native computed style.
// Names and output are copied; no caller pointer survives the native call.
func (o *Owner) StyleBatch(id uint64, names []string) ([]string, error) {
	if len(names) > 4096 {
		return nil, fmt.Errorf("blitz: too many style batch properties")
	}
	values := make([]string, 0, len(names))
	err := o.styleBatch(id, names, func(_ int, value []byte) error {
		values = append(values, string(value))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// styleBatch visits borrowed UTF-8 values within this call. Visitors must copy
// retained data and must not reenter the owner. Large one-off values do not grow
// the retained scratch buffer beyond this bound.
const styleBatchScratchLimit = 64 << 10

func (o *Owner) styleBatch(id uint64, names []string, visit func(int, []byte) error) error {
	if len(names) == 0 {
		return nil
	}
	if len(names) > 4096 {
		return fmt.Errorf("blitz: too many style batch properties")
	}
	var input strings.Builder
	for _, name := range names {
		if name == "" || strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
			return fmt.Errorf("blitz: invalid style batch property")
		}
		if input.Len()+len(name)+1 > 1<<20 {
			return fmt.Errorf("blitz: style batch input exceeds limit")
		}
		input.WriteString(name)
		input.WriteByte(0)
	}
	encoded := input.String()
	if len(o.styleBatchScratch) == 0 {
		o.styleBatchScratch = make([]byte, 4096)
	}
	buffer := o.styleBatchScratch
	for {
		var written C.size_t
		status := C.mimic_blitz_style_batch(o.handle, C.uint64_t(id), chars(encoded), C.size_t(len(encoded)), (*C.uint8_t)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)), &written)
		runtime.KeepAlive(encoded)
		if uint64(written) > 64<<20 {
			return fmt.Errorf("blitz: style batch readback exceeds limit")
		}
		if status == -3 {
			if int(written) <= len(buffer) {
				return fmt.Errorf("blitz: invalid style batch capacity response")
			}
			buffer = make([]byte, int(written))
			if len(buffer) <= styleBatchScratchLimit {
				o.styleBatchScratch = buffer
			}
			continue
		}
		if err := check(status); err != nil {
			return err
		}
		if uint64(written) > uint64(len(buffer)) {
			return fmt.Errorf("blitz: style batch output exceeds capacity")
		}
		data := buffer[:int(written)]
		for index := range names {
			if len(data) < 4 {
				return fmt.Errorf("blitz: truncated style batch length")
			}
			size := uint64(binary.LittleEndian.Uint32(data))
			data = data[4:]
			if size > uint64(len(data)) {
				return fmt.Errorf("blitz: truncated style batch value")
			}
			value := data[:int(size)]
			if !utf8.Valid(value) {
				return fmt.Errorf("blitz: invalid style batch UTF-8")
			}
			if err := visit(index, value); err != nil {
				return err
			}
			data = data[int(size):]
		}
		if len(data) != 0 {
			return fmt.Errorf("blitz: excess style batch output")
		}
		return nil
	}
}
