package htmlstream

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrPaused = errors.New("HTML stream paused for a script")
var ErrAborted = errors.New("HTML stream aborted")

type streamEvent struct {
	need   bool
	script int64
	done   bool
	err    error
}
type streamInput struct {
	data        string
	appendTail  string
	resume, eof bool
}

// Stream retains the HTML5 tree-construction state and the tokenizer's call
// stack while input is unavailable. All DOM operations are canonical backend
// operations, and script callbacks execute on the caller's goroutine.
// A caller serializes operations; callbacks may reenter Write/Close.
type Stream struct {
	events                                  chan streamEvent
	input                                   chan streamInput
	abort                                   chan struct{}
	stopped                                 chan struct{}
	abortOnce                               sync.Once
	waiting, closed, paused, closeRequested bool
	writes, scripts                         int
	outerResumes                            int
	pausedScript                            int64
	queued                                  string
	line, column                            int
	previousCR                              bool
	scriptPositions                         map[int64]ScriptPosition
}

// ScriptPosition is the location of an inline script's first source unit.
type ScriptPosition struct{ Line, Column int }

func (s *Stream) ScriptPosition(id int64) (ScriptPosition, bool) {
	position, ok := s.scriptPositions[id]
	return position, ok
}

type streamReader struct {
	stream   *Stream
	pending  []byte
	suffixes [][]byte
	partial  func()
}

func New(backend Backend, root int64) *Stream {
	s := &Stream{events: make(chan streamEvent), input: make(chan streamInput), abort: make(chan struct{}), stopped: make(chan struct{}), line: 1, column: 1, scriptPositions: make(map[int64]ScriptPosition)}
	tree := &tree{backend: backend, handles: map[int64]*Node{}}
	p := &parser{tree: tree, doc: tree.node(root), scripting: true, framesetOK: true, im: initialIM}
	reader := &streamReader{stream: s}
	p.tokenizer = html.NewTokenizer(reader)
	go s.parse(p, reader)
	return s
}

func (s *Stream) emit(event streamEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.abort:
		return false
	}
}
func (r *streamReader) Read(out []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.partial != nil {
			r.partial()
		}
		if !r.stream.emit(streamEvent{need: true}) {
			return 0, ErrAborted
		}
		select {
		case <-r.stream.abort:
			return 0, ErrAborted
		case command := <-r.stream.input:
			if command.appendTail != "" && len(r.suffixes) > 0 {
				r.suffixes[0] = append(r.suffixes[0], command.appendTail...)
			}
			if command.eof {
				return 0, io.EOF
			}
			if command.resume {
				if len(r.suffixes) == 0 {
					return 0, fmt.Errorf("HTML stream insertion point missing")
				}
				last := len(r.suffixes) - 1
				r.pending = r.suffixes[last]
				r.suffixes = r.suffixes[:last]
			} else {
				r.pending = []byte(command.data)
			}
		}
	}
	n := copy(out, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (s *Stream) parse(p *parser, reader *streamReader) {
	defer close(s.stopped)
	defer func() {
		if value := recover(); value != nil {
			s.emit(streamEvent{done: true, err: fmt.Errorf("HTML tree construction: %v", value)})
		}
	}()
	for {
		current := p.oe.top()
		p.tokenizer.AllowCDATA(current != nil && current.Namespace() != "")
		// x/net's tokenizer coalesces character tokens. At input exhaustion,
		// expose only its unambiguous character prefix, retaining incomplete
		// references and markup until more input arrives.
		emitted := ""
		rawText, rcdata := false, false
		rawTag := ""
		if current != nil && current.Namespace() == "" {
			switch current.DataAtom() {
			case atom.Script, atom.Style, atom.Xmp, atom.Iframe, atom.Noembed, atom.Noframes, atom.Plaintext:
				rawText = true
				rawTag = current.Data()
			case atom.Title, atom.Textarea:
				rcdata = true
				rawTag = current.Data()
			}
		}
		reader.partial = func() {
			// The HTML in-table-text mode defers a run of characters until it
			// can decide whether the whole run is whitespace. Upstream relies
			// on its coalesced token here; do not split that decision at a write.
			if current != nil && current.Namespace() == "" {
				switch current.DataAtom() {
				case atom.Table, atom.Tbody, atom.Tfoot, atom.Thead, atom.Tr:
					return
				}
			}
			raw := string(p.tokenizer.Raw())
			safe := characterPrefix(raw, rawTag)
			safe = strings.ReplaceAll(strings.ReplaceAll(safe, "\r\n", "\n"), "\r", "\n")
			if !rawText {
				if index := strings.LastIndexByte(safe, '&'); index >= 0 {
					tail := safe[index+1:]
					incomplete := true
					for _, c := range tail {
						if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '#') {
							incomplete = false
							break
						}
					}
					if incomplete {
						safe = safe[:index]
					}
				}
				safe = html.UnescapeString(safe)
			}
			if rawText || rcdata {
				safe = strings.ReplaceAll(safe, "\x00", "\ufffd")
			}
			if len(safe) <= len(emitted) || !strings.HasPrefix(safe, emitted) {
				return
			}
			added := safe[len(emitted):]
			emitted = safe
			previous := p.tok
			p.tok = Token{Type: TextToken, Data: added}
			p.parseCurrentToken()
			p.tok = previous
		}
		kind := p.tokenizer.Next()
		reader.partial = nil
		p.tok = p.tokenizer.Token()
		for _, character := range string(p.tokenizer.Raw()) {
			if character == '\n' && s.previousCR {
				s.previousCR = false
				continue
			}
			if character == '\n' || character == '\r' {
				s.line++
				s.column = 1
				s.previousCR = character == '\r'
			} else if character > 0xffff {
				s.column += 2
				s.previousCR = false
			} else {
				s.column++
				s.previousCR = false
			}
		}
		if kind == ErrorToken {
			if err := p.tokenizer.Err(); err != nil && err != io.EOF {
				s.emit(streamEvent{done: true, err: err})
				return
			}
		}
		if kind == TextToken && emitted != "" {
			if !strings.HasPrefix(p.tok.Data, emitted) {
				s.emit(streamEvent{done: true, err: fmt.Errorf("HTML tokenizer character checkpoint mismatch")})
				return
			}
			p.tok.Data = p.tok.Data[len(emitted):]
			if p.tok.Data == "" {
				continue
			}
		}
		script := int64(0)
		if kind == EndTagToken && p.tok.DataAtom == atom.Script && len(p.templateStack) == 0 {
			if node := p.oe.top(); node != nil && node.DataAtom() == atom.Script && node.Namespace() == "" {
				script = node.id
			}
		}
		p.parseCurrentToken()
		if kind == StartTagToken && p.tok.DataAtom == atom.Script {
			if node := p.oe.top(); node != nil && node.DataAtom() == atom.Script && node.Namespace() == "" {
				s.scriptPositions[node.id] = ScriptPosition{Line: s.line, Column: s.column}
			}
		}
		if kind == ErrorToken {
			s.emit(streamEvent{done: true})
			return
		}
		if script != 0 {
			suffix := append([]byte(nil), p.tokenizer.Buffered()...)
			suffix = append(suffix, reader.pending...)
			reader.pending = nil
			reader.suffixes = append(reader.suffixes, suffix)
			p.tokenizer = html.NewTokenizer(reader)
			if !s.emit(streamEvent{script: script}) {
				return
			}
		}
	}
}

// The tokenizer emits data before starting a recognized markup token. During
// starvation, an unfinished '<...' may still become markup; other '<' bytes
// are ordinary character data. Raw/RCDATA elements only reserve their own
// possible end tag, not arbitrary less-than characters in scripts or text.
func characterPrefix(raw, rawTag string) string {
	if rawTag == "plaintext" {
		return raw
	}
	index := strings.LastIndexByte(raw, '<')
	if index < 0 {
		return raw
	}
	tail := raw[index:]
	if rawTag != "" {
		closing := "</" + strings.ToLower(rawTag)
		lower := strings.ToLower(tail)
		if strings.HasPrefix(closing, lower) {
			return raw[:index]
		}
		if strings.HasPrefix(lower, closing) && len(lower) > len(closing) && strings.ContainsRune(" \t\r\n\f/>", rune(lower[len(closing)])) {
			return raw[:index]
		}
		return raw
	}
	// A pending tag/comment/declaration token starts with '<'. Never expose
	// its contents as text merely because its terminating '>' is not here yet.
	if raw[0] == '<' && (len(raw) == 1 || markupLead(raw[1])) {
		return ""
	}
	if len(tail) == 1 || markupLead(tail[1]) {
		return raw[:index]
	}
	return raw
}

func markupLead(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '/' || c == '!' || c == '?'
}

func (s *Stream) command(input streamInput) error {
	if !s.waiting {
		select {
		case <-s.abort:
			return ErrAborted
		case event := <-s.events:
			if event.done {
				s.closed = true
				return event.err
			}
			if !event.need {
				return fmt.Errorf("HTML stream expected input boundary")
			}
			s.waiting = true
		}
	}
	select {
	case <-s.abort:
		return ErrAborted
	case s.input <- input:
		s.waiting = false
		return nil
	}
}
func (s *Stream) drive(onScript func(int64) error) error {
	for {
		select {
		case <-s.abort:
			return ErrAborted
		case event := <-s.events:
			if event.done {
				s.closed = true
				return event.err
			}
			if event.need {
				s.waiting = true
				if s.outerResumes > 0 && s.scripts == 0 {
					s.outerResumes--
					if err := s.command(streamInput{resume: true}); err != nil {
						return err
					}
					continue
				}
				if s.closeRequested && s.scripts == 0 && s.writes <= 1 {
					if err := s.command(streamInput{eof: true}); err != nil {
						return err
					}
					continue
				}
				return nil
			}
			if event.script != 0 {
				s.scripts++
				var err error
				if onScript != nil {
					err = onScript(event.script)
				}
				s.scripts--
				// A nested document.write can suspend at a different external
				// script. Its pause owns the tokenizer continuation; the outer
				// script must not send a second resume and consume that boundary.
				if s.paused && err == nil {
					s.outerResumes++
					return ErrPaused
				}
				if err != nil {
					if errors.Is(err, ErrPaused) {
						s.paused = true
						s.pausedScript = event.script
						return ErrPaused
					}
					s.Abort()
					return err
				}
				queued := s.queued
				s.queued = ""
				if err := s.command(streamInput{resume: true, appendTail: queued}); err != nil {
					return err
				}
			}
		}
	}
}
func (s *Stream) Write(source string, onScript func(int64) error) error {
	if s.closed {
		return fmt.Errorf("HTML stream is closed")
	}
	if s.paused {
		s.queued += source
		return nil
	}
	s.writes++
	defer func() { s.writes-- }()
	if source == "" {
		return nil
	}
	if err := s.command(streamInput{data: source}); err != nil {
		return err
	}
	return s.drive(onScript)
}
func (s *Stream) Close(onScript func(int64) error) error {
	if s.closed {
		return nil
	}
	s.closeRequested = true
	if s.scripts > 0 || s.writes > 0 || s.paused {
		return nil
	}
	if err := s.command(streamInput{eof: true}); err != nil {
		return err
	}
	return s.drive(onScript)
}
func (s *Stream) Resume(onScript func(int64) error) error {
	if !s.paused {
		return nil
	}
	s.paused = false
	s.scripts++
	var scriptErr error
	if onScript != nil {
		scriptErr = onScript(s.pausedScript)
	}
	s.scripts--
	if s.paused && scriptErr == nil {
		s.outerResumes++
		return ErrPaused
	}
	if scriptErr != nil {
		if errors.Is(scriptErr, ErrPaused) {
			s.paused = true
			return ErrPaused
		}
		s.Abort()
		return scriptErr
	}
	queued := s.queued
	s.queued = ""
	s.pausedScript = 0
	if err := s.command(streamInput{resume: true, appendTail: queued}); err != nil {
		return err
	}
	return s.drive(onScript)
}
func (s *Stream) Closed() bool { return s.closed }
func (s *Stream) Paused() bool { return s.paused }
func (s *Stream) Abort()       { s.abortOnce.Do(func() { close(s.abort) }); <-s.stopped; s.closed = true }
