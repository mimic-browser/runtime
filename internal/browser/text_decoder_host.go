package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// Decoder ownership is realm-local. Completed streams release their state;
// no decoder or JavaScript callback crosses a Page or survives runtime teardown.
func installTextDecoderHost(host map[string]any, runtime engine.Runtime) {
	type decoderState struct {
		decoder  *encoding.Decoder
		encoding encoding.Encoding
	}
	states := map[int]*decoderState{}
	sequence := 0
	function := runtime.Function
	if borrowed, ok := runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		function = borrowed.TransientFunction
	}
	host["textEncodingName"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		label := strarg(args, 0)
		// HTML labels trim only ASCII whitespace at the binding boundary;
		// htmlindex.Get also trims Unicode whitespace, which TextDecoder rejects.
		for _, character := range label {
			if character < 0x21 || character > 0x7e {
				return runtime.Value(""), nil
			}
		}
		codec, err := htmlindex.Get(label)
		if err != nil || codec == encoding.Replacement {
			return runtime.Value(""), nil
		}
		name, err := htmlindex.Name(codec)
		if err != nil {
			return runtime.Value(""), nil
		}
		return runtime.Value(name), nil
	})
	host["releaseTextDecoder"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		delete(states, int(numarg(args, 0)))
		return nil, nil
	})
	host["decodeText"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id := int(numarg(args, 0))
		state := states[id]
		if state == nil {
			codec, err := htmlindex.Get(strarg(args, 1))
			if err != nil {
				return nil, err
			}
			state = &decoderState{codec.NewDecoder(), codec}
			sequence++
			id = sequence
		}
		input := byteSlice(arg(args, 2))
		atEOF, _ := arg(args, 3).(bool)
		fatal, _ := arg(args, 4).(bool)
		var output strings.Builder
		consumed := 0
		var failure error
		bufferSize := 4096
		if fatal {
			bufferSize = 4
		}
		buffer := make([]byte, bufferSize)
		for {
			written, read, err := state.decoder.Transform(buffer, input[consumed:], atEOF)
			text := buffer[:written]
			if fatal && bytes.Contains(text, []byte("\ufffd")) {
				encoded, encodeErr := state.encoding.NewEncoder().Bytes(text)
				if encodeErr != nil || !bytes.Equal(encoded, input[consumed:consumed+read]) {
					failure = errors.New("Invalid encoded data")
					break
				}
			}
			consumed += read
			output.Write(text)
			if err == transform.ErrShortDst {
				continue
			}
			if err != nil && err != transform.ErrShortSrc {
				failure = err
			}
			break
		}
		result := map[string]any{"id": id, "text": output.String(), "pending": []int{}}
		if failure != nil {
			delete(states, id)
			result["error"] = failure.Error()
		} else if atEOF {
			delete(states, id)
		} else {
			states[id] = state
			pending := make([]int, len(input)-consumed)
			for i, b := range input[consumed:] {
				pending[i] = int(b)
			}
			result["pending"] = pending
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return runtime.Value(string(encoded)), nil
	})
}
