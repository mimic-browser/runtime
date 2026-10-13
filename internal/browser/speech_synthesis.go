package browser

import (
	"context"
	"math"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/speech"
)

type speechUtterance struct {
	VoiceReference engine.Value `json:"-"`
	ID             uint64       `json:"id"`
	Text           string       `json:"text"`
	Lang           string       `json:"lang"`
	Voice          string       `json:"voice"`
	Volume         float64      `json:"volume"`
	Rate           float64      `json:"rate"`
	Pitch          float64      `json:"pitch"`
}

// All Web Speech state is event-loop owned. Providers own only system resources
// and report facts; they cannot mutate queues, wrappers, or browser lifecycle.
type speechRun struct {
	owner     *Realm
	utterance *speechUtterance
}

type speechSynthesisState struct {
	backend      speech.Backend
	voices       []speech.Voice
	voiceRecords map[string]speech.Voice
	utterances   map[uint64]*speechUtterance
	sequence     uint64
	runSequence  uint64
	runs         map[uint64]*speechRun
	queue        []uint64
	current      uint64
	started      bool
	paused       bool
	startedAt    time.Time
}

func (r *Realm) speechState() *speechSynthesisState {
	if r.speech == nil {
		r.speech = &speechSynthesisState{voices: []speech.Voice{}, voiceRecords: map[string]speech.Voice{}, utterances: map[uint64]*speechUtterance{}, runs: map[uint64]*speechRun{}}
	}
	return r.speech
}

func (r *Realm) openSpeechProvider() {
	if r.inactive || r.closed {
		return
	}
	s := r.speechState()
	if s.backend != nil {
		return
	}
	s.backend = r.agent.Page().ctx.browser.speechProvider(func(event speech.Event) {
		if r.resourceContext.Err() != nil {
			return
		}
		event.Voices = append([]speech.Voice(nil), event.Voices...)
		r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error { return r.receiveSpeechEvent(ctx, event) })
	})
}

// Navigation deactivates runnable work before retained JS wrappers are
// collected. Stop synthesis there, while preserving immutable voice and
// utterance records for references to the old document.
func (r *Realm) closeSpeechProvider() {
	if s := r.speech; s != nil {
		if s.backend != nil {
			s.backend.Close()
			s.backend = nil
		}
		s.queue = nil
		s.runs = map[uint64]*speechRun{}
		s.current = 0
		s.started = false
		s.paused = false
		s.startedAt = time.Time{}
	}
}

func (r *Realm) emitSpeech(ctx context.Context, kind string, id uint64, details map[string]any) error {
	if r.speechNotifier == nil {
		return nil
	}
	if details == nil {
		details = map[string]any{}
	}
	stamp := r.agent.Page().performanceClamper.now(r.scheduler.Now(), r.performanceOrigin, r.securityState().crossOriginIsolated)
	details["timeStamp"] = stamp
	if details["error"] == "not-allowed" {
		details["elapsedTime"] = float64(float32(stamp))
	}
	details["kind"] = kind
	details["id"] = id
	_, err := r.runtime.Call(ctx, r.speechNotifier, nil, r.val(details))
	return err
}

func (r *Realm) emitSpeechRun(ctx context.Context, kind string, run *speechRun, details map[string]any) error {
	if run == nil {
		return nil
	}
	if run.owner == r {
		return r.emitSpeech(ctx, kind, run.utterance.ID, details)
	}
	_, err := r.crossFrameData(run.owner, func(ctx context.Context) (any, error) {
		restore := r.enterFrameDocumentEntry(run.owner)
		defer restore()
		return nil, run.owner.emitSpeech(ctx, kind, run.utterance.ID, details)
	})
	return err
}

func (r *Realm) speechElapsed() float64 {
	s := r.speechState()
	if s.startedAt.IsZero() {
		return float64(float32(r.agent.Page().performanceClamper.now(r.scheduler.Now(), r.performanceOrigin, r.securityState().crossOriginIsolated)))
	}
	return float64(float32(float64(r.scheduler.Now().Sub(s.startedAt)) / float64(time.Second)))
}

func (r *Realm) startSpeech() {
	if r.inactive || r.closed {
		return
	}
	s := r.speechState()
	if s.current != 0 || s.paused || len(s.queue) == 0 {
		return
	}
	id := s.queue[0]
	s.queue = s.queue[1:]
	run := s.runs[id]
	u := run.utterance
	if u == nil {
		return
	}
	s.current = id
	s.started = false
	s.startedAt = time.Time{}
	r.openSpeechProvider()
	s.backend.Speak(speech.Utterance{ID: id, Text: u.Text, Lang: u.Lang, VoiceID: u.Voice, Volume: u.Volume, Rate: u.Rate, Pitch: u.Pitch})
}

func (r *Realm) receiveSpeechEvent(ctx context.Context, event speech.Event) error {
	s := r.speechState()
	if event.Kind == "voices" {
		s.voices = append([]speech.Voice{}, event.Voices...)
		for _, voice := range s.voices {
			s.voiceRecords[voice.ID] = voice
		}
		return r.emitSpeech(ctx, "voiceschanged", 0, nil)
	}
	if event.Kind == "pause" || event.Kind == "resume" {
		if event.ID != 0 && event.ID != s.current {
			return nil
		}
		s.paused = event.Kind == "pause"
		if event.ID != 0 && event.ID == s.current {
			return r.emitSpeechRun(ctx, event.Kind, s.runs[event.ID], map[string]any{"elapsedTime": r.speechElapsed()})
		}
		if !s.paused {
			r.startSpeech()
		}
		return nil
	}
	if event.ID == 0 || event.ID != s.current {
		return nil
	}
	details := map[string]any{"elapsedTime": float64(float32(event.Elapsed)), "charIndex": event.CharIndex, "charLength": event.CharLength, "name": "", "error": event.Error}
	kind := event.Kind
	switch kind {
	case "start":
		s.started = true
		s.startedAt = r.scheduler.Now()
	case "word", "sentence":
		details["name"] = kind
		kind = "boundary"
	case "end", "error":
		s.current = 0
		s.started = false
		s.startedAt = time.Time{}
	}
	run := s.runs[event.ID]
	if kind == "end" || kind == "error" {
		delete(s.runs, event.ID)
	}
	err := r.emitSpeechRun(ctx, kind, run, details)
	if kind == "end" || kind == "error" {
		r.startSpeech()
	}
	return err
}

func addSpeechHosts(r *Realm, h map[string]any) {
	h["installSpeechNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.speechNotifier = a[0]
		// Installing the callback does not request platform voices. A cold
		// document can observe an empty list until getVoices() starts discovery.
		return nil, nil
	})
	h["speechState"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		s := r.speechState()
		switch strarg(a, 0) {
		case "owner":
			return r.val(r.ID), nil
		case "status":
			return r.val(map[string]any{"pending": len(s.queue) > 0, "speaking": s.current != 0, "paused": s.paused}), nil
		case "voices":
			r.openSpeechProvider()
			rows := []map[string]any{}
			for _, voice := range s.voices {
				rows = append(rows, map[string]any{"id": voice.ID, "name": voice.Name, "lang": voice.Lang, "default": voice.Default, "localService": voice.Local})
			}
			return r.val(rows), nil
		case "voiceRead":
			voice := s.voiceRecords[strarg(a, 1)]
			return r.val(map[string]any{"id": voice.ID, "name": voice.Name, "lang": voice.Lang, "default": voice.Default, "localService": voice.Local}), nil
		case "voiceReference":
			u := s.utterances[uint64(numarg(a, 1))]
			if u == nil || u.VoiceReference == nil {
				return nil, nil
			}
			return u.VoiceReference, nil
		case "create":
			s.sequence++
			u := &speechUtterance{ID: s.sequence, Text: strarg(a, 1), Volume: 1, Rate: 1, Pitch: 1}
			s.utterances[u.ID] = u
			return r.val(u.ID), nil
		case "read":
			u := s.utterances[uint64(numarg(a, 1))]
			if u == nil {
				return nil, nil
			}
			return r.val(map[string]any{"text": u.Text, "lang": u.Lang, "voice": u.Voice, "volume": u.Volume, "rate": u.Rate, "pitch": u.Pitch}), nil
		case "set":
			u := s.utterances[uint64(numarg(a, 1))]
			if u == nil {
				return nil, nil
			}
			switch strarg(a, 2) {
			case "text":
				u.Text = strarg(a, 3)
			case "lang":
				u.Lang = strarg(a, 3)
			case "voice":
				u.Voice = strarg(a, 3)
				u.VoiceReference = a[4]
			case "volume":
				u.Volume = float64(float32(math.Max(0, math.Min(1, numarg(a, 3)))))
			case "rate":
				u.Rate = float64(float32(math.Max(.1, math.Min(10, numarg(a, 3)))))
			case "pitch":
				u.Pitch = float64(float32(math.Max(0, math.Min(2, numarg(a, 3)))))
			}
			return nil, nil
		case "speak":
			if r.inactive || r.closed {
				return nil, nil
			}
			id := uint64(numarg(a, 1))
			owner := r
			if ownerID := strarg(a, 2); ownerID != "" && ownerID != r.ID {
				p := r.agent.Page()
				p.mu.RLock()
				owner = p.realmOwners[ownerID]
				p.mu.RUnlock()
			}
			if owner == nil || owner.origin != r.origin || owner.speech == nil || owner.speech.utterances[id] == nil {
				return nil, nil
			}
			run := &speechRun{owner: owner, utterance: owner.speech.utterances[id]}
			if r.activationAt.IsZero() {
				return nil, r.emitSpeechRun(context.Background(), "error", run, map[string]any{"error": "not-allowed", "elapsedTime": r.speechElapsed()})
			}
			s.runSequence++
			s.runs[s.runSequence] = run
			s.queue = append(s.queue, s.runSequence)
			r.startSpeech()
			return nil, nil
		case "cancel":
			id, started, elapsed := s.current, s.started, r.speechElapsed()
			queued := append([]uint64{}, s.queue...)
			s.queue = nil
			s.current = 0
			s.started = false
			s.startedAt = time.Time{}
			if s.backend != nil {
				s.backend.Cancel()
			}
			if id != 0 {
				kind := "canceled"
				if started {
					kind = "interrupted"
				}
				if err := r.emitSpeechRun(context.Background(), "error", s.runs[id], map[string]any{"error": kind, "elapsedTime": elapsed}); err != nil {
					return nil, err
				}
			}
			delete(s.runs, id)
			for _, id := range queued {
				if err := r.emitSpeechRun(context.Background(), "error", s.runs[id], map[string]any{"error": "canceled", "elapsedTime": 0}); err != nil {
					return nil, err
				}
				delete(s.runs, id)
			}
			return nil, nil
		case "pause":
			if s.backend != nil && !s.paused {
				s.backend.Pause()
			}
			return nil, nil
		case "resume":
			if s.backend != nil && s.paused {
				s.backend.Resume()
			}
			return nil, nil
		}
		return nil, nil
	})
}
