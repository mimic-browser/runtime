package browser

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// mediaSource is the canonical attachment state. JavaScript wrappers and object
// URLs refer to this object; neither owns another readyState or buffer list.
// No decoder or graphics backend is required for its container lifecycle.
type mediaSource struct {
	mu            sync.Mutex
	id            string
	owner         *Realm
	active        bool
	state         string
	duration      float64
	attachment    *mediaLoad
	buffers       []*mediaSourceBuffer
	allBuffers    map[string]*mediaSourceBuffer
	bufferSeq     uint64
	metadataReady bool
}

type mediaSourceBuffer struct {
	id              string
	source          *mediaSource
	removed         bool
	updating        bool
	active          bool
	mode            string
	timestampOffset float64
	appendStart     float64
	appendEnd       float64
	operationSeq    uint64
	contentType     string
	parser          *mp4InitParser
	metadata        *mp4Initialization
}

func newMediaSource(id string, owner *Realm) *mediaSource {
	return &mediaSource{id: id, owner: owner, active: true, state: "closed", duration: math.NaN(), allBuffers: map[string]*mediaSourceBuffer{}}
}

func (m *mediaSource) MediaSourceID() string { return m.id }

// attach does not dispatch sourceopen inline. Resource selection queues the
// opening task on the media element's Page; the source's owner queues its event.
func (m *mediaSource) attach(load *mediaLoad) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active || m.attachment != nil {
		return false
	}
	m.attachment = load
	m.state = "open"
	return true
}

// detach changes observable state synchronously. Its caller queues events after
// the mutation, so callbacks observe any later transitions before their tasks run.
func (m *mediaSource) detach(load *mediaLoad) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attachment != load || load == nil {
		return false
	}
	m.attachment = nil
	m.state = "closed"
	m.duration = math.NaN()
	m.metadataReady = false
	load.initialization, load.readyState = nil, 0
	for _, buffer := range m.buffers {
		buffer.removed, buffer.updating, buffer.active = true, false, false
		buffer.parser, buffer.metadata = nil, nil
	}
	m.buffers = nil
	return true
}

// A retained object survives owner deactivation, but its browser work does not.
// Teardown must not deliver sourceclose to the deactivated execution context.
func (m *mediaSource) deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = false
	m.state = "closed"
	m.duration = math.NaN()
	m.metadataReady = false
	if m.attachment != nil {
		m.attachment.initialization, m.attachment.readyState = nil, 0
	}
	m.attachment = nil
	for _, buffer := range m.buffers {
		buffer.removed, buffer.updating, buffer.active = true, false, false
		buffer.parser, buffer.metadata = nil, nil
	}
	m.buffers = nil
	m.owner = nil
}

func (r *Realm) deactivateMediaSources() {
	for _, source := range r.mediaSources {
		source.deactivate()
	}
}

func (r *Realm) installMediaSourceHosts(host map[string]any) {
	host["mediaSourceState"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		load := r.mediaLoads[int64(numarg(args, 0))]
		if load == nil || load.source == nil {
			return r.val(nil), nil
		}
		source := load.source
		source.mu.Lock()
		defer source.mu.Unlock()
		width, height, ready := 0, 0, 0
		if source.metadataReady && source.state != "closed" {
			ready = load.readyState
			if load.initialization != nil {
				for _, track := range load.initialization.tracks {
					if track.width != 0 {
						width, height = track.width, track.height
						break
					}
				}
			}
		}
		return r.val(map[string]any{"duration": source.duration, "width": width, "height": height, "readyState": ready}), nil
	})
	host["installMediaSourceNotifier"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.mediaSourceNotifier = args[0]
		return nil, nil
	})
	host["mediaSourceCreate"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) {
		if r.mediaSources == nil {
			r.mediaSources = map[string]*mediaSource{}
		}
		id := uuid.NewString()
		source := newMediaSource(id, r)
		if r.inactive {
			source.deactivate()
		}
		r.mediaSources[id] = source
		return r.val(id), nil
	})
	host["mediaSourceGet"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		switch strarg(args, 1) {
		case "readyState":
			return r.val(source.state), nil
		case "duration":
			return r.val(source.duration), nil
		case "sourceBuffers", "activeSourceBuffers":
			ids := []string{}
			for _, buffer := range source.buffers {
				if strarg(args, 1) != "activeSourceBuffers" || buffer.active {
					ids = append(ids, buffer.id)
				}
			}
			return r.val(ids), nil
		}
		return nil, fmt.Errorf("unsupported MediaSource observation")
	})
	host["mediaSourceObjectURL"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		owner := strarg(args, 1)
		p := r.agent.Page()
		p.mu.RLock()
		creator := p.realmOwners[owner]
		p.mu.RUnlock()
		if creator == nil || creator.inactive || creator.closed {
			return r.val(""), nil
		}
		raw := "blob:" + creator.origin + "/" + uuid.NewString()
		p.ctx.network.PutMediaSourceURL(raw, owner, source)
		return r.val(raw), nil
	})
	host["mediaSourceOperation"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		source.mu.Lock()
		events := [][2]string{}
		var mediaEvent string
		var attachment *mediaLoad
		defer func() {
			source.mu.Unlock()
			for _, event := range events {
				source.queueEvent(event[0], event[1])
			}
			if attachment != nil && mediaEvent != "" {
				attachment.queueEvent(mediaEvent)
			}
		}()
		operation := strarg(args, 1)
		if operation == "removeSourceBuffer" {
			buffer := source.allBuffers[strarg(args, 2)]
			if buffer == nil || buffer.removed {
				return r.val(map[string]any{"name": "NotFoundError", "message": "Failed to execute 'removeSourceBuffer' on 'MediaSource': The SourceBuffer provided is not contained in this MediaSource."}), nil
			}
			buffer.removed, buffer.updating, buffer.active = true, false, false
			buffer.parser, buffer.metadata = nil, nil
			buffer.operationSeq++
			for i, member := range source.buffers {
				if member == buffer {
					source.buffers = append(source.buffers[:i], source.buffers[i+1:]...)
					break
				}
			}
			events = append(events, [2]string{"sourceBuffers", "removesourcebuffer"})
			if source.metadataReady && len(source.buffers) == 0 && source.attachment != nil {
				attachment = source.attachment
				attachment.readyState = 4
				mediaEvent = "canplay"
			}
			return r.val(map[string]any{}), nil
		}
		if source.state != "open" {
			message := "Failed to execute '" + operation + "' on 'MediaSource': The MediaSource's readyState is not 'open'."
			if operation == "duration" {
				message = "Failed to set the 'duration' property on 'MediaSource': The MediaSource's readyState is not 'open'."
			}
			return r.val(map[string]any{"name": "InvalidStateError", "message": message}), nil
		}
		for _, buffer := range source.buffers {
			if buffer.updating && (operation == "duration" || operation == "endOfStream") {
				prefix := "Failed to execute 'endOfStream' on 'MediaSource': "
				if operation == "duration" {
					prefix = "Failed to set the 'duration' property on 'MediaSource': "
				}
				return r.val(map[string]any{"name": "InvalidStateError", "message": prefix + "The 'updating' attribute is true on one or more of this MediaSource's SourceBuffers."}), nil
			}
		}
		switch operation {
		case "addSourceBuffer":
			source.bufferSeq++
			id := fmt.Sprintf("%s/%d", source.id, source.bufferSeq)
			buffer := &mediaSourceBuffer{id: id, source: source, mode: "segments", appendEnd: math.Inf(1), contentType: strarg(args, 2)}
			source.buffers = append(source.buffers, buffer)
			source.allBuffers[id] = buffer
			events = append(events, [2]string{"sourceBuffers", "addsourcebuffer"})
			return r.val(map[string]any{"value": id}), nil
		case "duration":
			source.duration = numarg(args, 2)
		case "endOfStream":
			if strarg(args, 2) != "" {
				return nil, fmt.Errorf("unsupported MediaSource error completion")
			}
			source.state = "ended"
			events = append(events, [2]string{"source", "sourceended"})
		default:
			return nil, fmt.Errorf("unsupported MediaSource operation %s", operation)
		}
		return r.val(map[string]any{}), nil
	})
	host["sourceBufferGet"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		buffer := source.allBuffers[strarg(args, 1)]
		if buffer == nil {
			return nil, fmt.Errorf("unknown SourceBuffer identity")
		}
		switch strarg(args, 2) {
		case "mode":
			return r.val(buffer.mode), nil
		case "updating":
			return r.val(buffer.updating), nil
		case "timestampOffset":
			return r.val(buffer.timestampOffset), nil
		case "appendWindowStart":
			return r.val(buffer.appendStart), nil
		case "appendWindowEnd":
			return r.val(buffer.appendEnd), nil
		case "buffered":
			if buffer.removed {
				return r.val(map[string]any{"name": "InvalidStateError", "message": "Failed to read the 'buffered' property from 'SourceBuffer': This SourceBuffer has been removed from the parent media source."}), nil
			}
			return r.val(0), nil
		}
		return nil, fmt.Errorf("unsupported SourceBuffer observation")
	})
	host["sourceBufferSet"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		buffer := source.allBuffers[strarg(args, 1)]
		property := strarg(args, 2)
		prefix := "Failed to set the '" + property + "' property on 'SourceBuffer': "
		fail := func(name, message string) (engine.Value, error) {
			return r.val(map[string]any{"name": name, "message": prefix + message}), nil
		}
		if buffer == nil || buffer.removed {
			return fail("InvalidStateError", "This SourceBuffer has been removed from the parent media source.")
		}
		if buffer.updating {
			return fail("InvalidStateError", "This SourceBuffer is still processing an 'appendBuffer' or 'remove' operation.")
		}
		value := numarg(args, 3)
		switch property {
		case "mode":
			buffer.mode = strarg(args, 3)
		case "timestampOffset":
			buffer.timestampOffset = value
		case "appendWindowStart":
			if math.IsNaN(value) || value < 0 || value >= buffer.appendEnd {
				return fail("TypeError", fmt.Sprintf("The value provided (%g) is outside the range (0, %g].", value, buffer.appendEnd))
			}
			buffer.appendStart = value
		case "appendWindowEnd":
			if math.IsNaN(value) {
				return fail("TypeError", "The value provided is not a number.")
			}
			if value <= buffer.appendStart {
				return fail("TypeError", fmt.Sprintf("The value provided (%g) is less than the minimum bound (%g).", value, buffer.appendStart))
			}
			buffer.appendEnd = value
		default:
			return nil, fmt.Errorf("unsupported SourceBuffer property %s", property)
		}
		return r.val(map[string]any{}), nil
	})
	host["sourceBufferAppend"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		data, ok := arg(args, 2).([]byte)
		if !ok {
			return nil, fmt.Errorf("unsupported SourceBuffer append input")
		}
		source := r.mediaSources[strarg(args, 0)]
		if source == nil {
			return nil, fmt.Errorf("unknown MediaSource identity")
		}
		source.mu.Lock()
		buffer := source.allBuffers[strarg(args, 1)]
		if buffer == nil || buffer.removed || source.state == "closed" || buffer.updating {
			message := "This SourceBuffer has been removed from the parent media source."
			if buffer != nil && !buffer.removed && buffer.updating {
				message = "This SourceBuffer is still processing an 'appendBuffer' or 'remove' operation."
			}
			source.mu.Unlock()
			return r.val(map[string]any{"name": "InvalidStateError", "message": "Failed to execute 'appendBuffer' on 'SourceBuffer': " + message}), nil
		}
		var initialization *mp4Initialization
		if len(data) != 0 {
			contentType := strings.ToLower(buffer.contentType)
			mime := strings.TrimSpace(strings.Split(contentType, ";")[0])
			if (mime != "video/mp4" && mime != "audio/mp4") || buffer.metadata != nil {
				source.mu.Unlock()
				return nil, fmt.Errorf("unsupported SourceBuffer media container parsing")
			}
			if buffer.parser == nil {
				buffer.parser = &mp4InitParser{}
			}
			var err error
			initialization, err = buffer.parser.append(data)
			if err != nil {
				source.mu.Unlock()
				return nil, err
			}
			if initialization != nil {
				codec := initialization.tracks[0].codec
				if codec == "aac" && !strings.Contains(contentType, "mp4a.") || codec == "avc" && !strings.Contains(contentType, "avc1.") && !strings.Contains(contentType, "avc3.") {
					source.mu.Unlock()
					return nil, fmt.Errorf("unsupported MP4 initialization codec mismatch")
				}
			}
		}
		reopen := source.state == "ended"
		source.state = "open"
		buffer.updating = true
		buffer.operationSeq++
		seq := buffer.operationSeq
		owner := source.owner
		source.mu.Unlock()
		if reopen {
			source.queueEvent("source", "sourceopen")
		}
		source.queueEvent(buffer.id, "updatestart")
		owner.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
			source.mu.Lock()
			if !source.active || buffer.removed || !buffer.updating || buffer.operationSeq != seq {
				source.mu.Unlock()
				return nil
			}
			buffer.updating = false
			activated := initialization != nil && !buffer.active
			if initialization != nil {
				buffer.metadata = initialization
				buffer.active = true
				if math.IsNaN(source.duration) {
					source.duration = initialization.duration
				}
			}
			metadataReady := !source.metadataReady && len(source.buffers) != 0
			for _, member := range source.buffers {
				metadataReady = metadataReady && member.metadata != nil
			}
			if metadataReady {
				source.metadataReady = true
				if source.attachment != nil {
					source.attachment.initialization = initialization
					source.attachment.readyState = 1
				}
			}
			attachment := source.attachment
			source.mu.Unlock()
			if activated {
				source.queueEvent("activeSourceBuffers", "addsourcebuffer")
			}
			if metadataReady && attachment != nil {
				attachment.queueEvent("loadedmetadata")
			}
			source.queueEvent(buffer.id, "update")
			source.queueEvent(buffer.id, "updateend")
			return nil
		})
		return r.val(map[string]any{}), nil
	})
}

func (m *mediaSource) queueEvent(target, name string) {
	m.mu.Lock()
	owner := m.owner
	active := m.active
	m.mu.Unlock()
	if owner == nil || !active {
		return
	}
	owner.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		return owner.dispatchMediaSourceEvent(ctx, m.id, target, name)
	})
}

// Kept as an owner callback rather than a global engine invocation: every
// source event must enter through its Page's scheduler before calling JavaScript.
func (r *Realm) dispatchMediaSourceEvent(ctx context.Context, sourceID, target, name string) error {
	if r.inactive || r.closed || r.mediaSourceNotifier == nil {
		return nil
	}
	_, err := r.runtime.Call(ctx, r.mediaSourceNotifier, nil, r.val(sourceID), r.val(target), r.val(name))
	return err
}
