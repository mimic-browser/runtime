package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/speech"
)

func TestSpeechSynthesisChromeOracle(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		source, err := os.ReadFile("testdata/speech_synthesis_oracle.js")
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile("testdata/speech_synthesis_chrome152.json")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := p.Evaluate(ctx, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal(expected, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(actual)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("got %v; want %v", got, want)
		}
	})
}

type controlledSpeech struct {
	mu       sync.Mutex
	notify   func(speech.Event)
	requests []speech.Utterance
	active   uint64
	closed   bool
}

func (b *controlledSpeech) Speak(u speech.Utterance) {
	b.mu.Lock()
	b.requests = append(b.requests, u)
	b.active = u.ID
	b.mu.Unlock()
}
func (b *controlledSpeech) Cancel() { b.mu.Lock(); b.active = 0; b.mu.Unlock() }
func (b *controlledSpeech) Pause() {
	b.mu.Lock()
	id := b.active
	b.mu.Unlock()
	b.notify(speech.Event{Kind: "pause", ID: id})
}
func (b *controlledSpeech) Resume() {
	b.mu.Lock()
	id := b.active
	b.mu.Unlock()
	b.notify(speech.Event{Kind: "resume", ID: id})
}
func (b *controlledSpeech) Close() { b.mu.Lock(); b.closed = true; b.mu.Unlock() }

func TestSpeechPortableProviderQueueAndCancellation(t *testing.T) {
	serialBrowserTest(t)
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var backend *controlledSpeech
			b, err := NewWithOptions(factory, chrome152.New(), Options{SpeechProvider: func(notify func(speech.Event)) speech.Backend {
				backend = &controlledSpeech{notify: notify}
				notify(speech.Event{Kind: "voices", Voices: []speech.Voice{{ID: "test-provider:voice", Name: "Test voice", Lang: "en-US", Local: true, Default: true}}})
				return backend
			}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := b.NewContext().NewPage()
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			eval := func(script string) any {
				t.Helper()
				v, err := p.Evaluate(ctx, script)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			eval(`0`)
			if backend == nil {
				t.Fatal("realm did not begin platform voice discovery during bootstrap")
			}
			drain := func() {
				t.Helper()
				if err := p.AdvanceTime(ctx, time.Millisecond); err != nil {
					t.Fatal(err)
				}
			}
			eval(`speechSynthesis.getVoices()`)
			drain()
			if v := eval(`(()=>{const a=speechSynthesis.getVoices(),b=speechSynthesis.getVoices();return a!==b&&a[0]===b[0]&&a[0].name==='Test voice'&&a[0].voiceURI==='Test voice'})()`); v != true {
				t.Fatalf("provider projection: %v", v)
			}
			p.Top.Realm.activationAt = p.ClockNow()
			eval(`globalThis.events=[];globalThis.a=new SpeechSynthesisUtterance('one');globalThis.b=new SpeechSynthesisUtterance('two');a.voice=speechSynthesis.getVoices()[0];for(const u of [a,b]){u.onstart=e=>events.push('start:'+u.text);u.onerror=e=>events.push('error:'+e.error+':'+u.text);u.onend=e=>events.push('end:'+u.text)}speechSynthesis.speak(a);speechSynthesis.speak(b)`)
			if v := eval(`speechSynthesis.speaking&&speechSynthesis.pending&&a.voice===speechSynthesis.getVoices()[0]`); v != true {
				t.Fatalf("queued state: %v", v)
			}
			backend.mu.Lock()
			first := backend.requests[0]
			backend.mu.Unlock()
			if first.Text != "one" || first.VoiceID != "test-provider:voice" {
				t.Fatalf("provider request: %+v", first)
			}
			backend.notify(speech.Event{Kind: "start", ID: first.ID})
			drain()
			eval(`speechSynthesis.cancel();speechSynthesis.speak(a)`)
			backend.mu.Lock()
			second := backend.requests[len(backend.requests)-1]
			backend.mu.Unlock()
			if first.ID == second.ID {
				t.Fatal("requeued utterance reused native operation identity")
			}
			backend.notify(speech.Event{Kind: "end", ID: first.ID})
			drain()
			if v := eval(`speechSynthesis.speaking`); v != true {
				t.Fatal("late completion canceled a new run")
			}
			backend.notify(speech.Event{Kind: "pause", ID: first.ID})
			drain()
			if v := eval(`speechSynthesis.paused`); v != false {
				t.Fatal("late pause affected a new run")
			}
			backend.notify(speech.Event{Kind: "start", ID: second.ID})
			drain()
			eval(`speechSynthesis.pause()`)
			drain()
			if v := eval(`speechSynthesis.paused`); v != true {
				t.Fatal("provider pause not projected")
			}
			eval(`speechSynthesis.resume()`)
			drain()
			if v := eval(`speechSynthesis.paused`); v != false {
				t.Fatal("provider resume not projected")
			}
			backend.notify(speech.Event{Kind: "end", ID: second.ID, CharIndex: 3})
			drain()
			if v := eval(`JSON.stringify(events)`); v != `["start:one","error:interrupted:one","error:canceled:two","start:one","end:one"]` {
				t.Fatalf("event lifecycle: %v", v)
			}
			old := p.Top.Realm
			old.deactivate()
			backend.mu.Lock()
			deactivatedClosed := backend.closed
			backend.mu.Unlock()
			if !deactivatedClosed || old.speech.backend != nil || old.speech.current != 0 || len(old.speech.runs) != 0 {
				t.Fatal("provider survived document deactivation")
			}
			old.openSpeechProvider()
			if old.speech.backend != nil {
				t.Fatal("inactive document reopened provider")
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			backend.mu.Lock()
			closed := backend.closed
			backend.mu.Unlock()
			if !closed {
				t.Fatal("provider resources outlived Page")
			}
		})
	}
}
