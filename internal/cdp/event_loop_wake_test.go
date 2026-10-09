package cdp

import (
	"context"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// Disable periodic ticks so success proves that a post/resume wakes the pump,
// rather than imposing a fragile sub-10ms timing assertion on a loaded machine.
func TestEventLoopWakeWithoutTicker(t *testing.T) {
	b, err := browser.New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	other, err := s.Context.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	startPump := func(p *browser.Page) (context.CancelFunc, <-chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.pumpEventLoopWithTicks(ctx, p, nil)
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cancelled pump did not exit")
			}
		})
		return cancel, done
	}
	eval := func(p *browser.Page, source string) any {
		t.Helper()
		p.LockCommands()
		defer p.UnlockCommands()
		value, err := p.EvaluateCommand(context.Background(), "", source)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	waitTrue := func(p *browser.Page, source string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if eval(p, source) == true {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("work was stranded without a ticker: %s", source)
	}

	resume := s.pausePump(s.Page, false)
	cancel, done := startPump(s.Page)
	startPump(other)
	eval(s.Page, `globalThis.ran=false;globalThis.future=false;setTimeout(()=>{ran=true},0);setTimeout(()=>{future=true},3600000);void 0`)
	eval(other, `globalThis.ran=false;setTimeout(()=>{ran=true},0);void 0`)
	waitTrue(other, "globalThis.ran===true")
	if eval(s.Page, "globalThis.ran") != false {
		t.Fatal("another Page's wake bypassed the pause")
	}
	// Explicitly consume any remaining post hint: resume must supply its own.
	select {
	case <-s.Page.EventLoopWake():
	default:
	}
	resume()
	waitTrue(s.Page, "globalThis.ran===true")
	if eval(s.Page, "globalThis.future") != false {
		t.Fatal("wake advanced time to a future task")
	}
	// A protocol command advances the canonical clock while the pump is paused.
	// Resuming must account for that same wall interval only once.
	resumeBusy := s.pausePump(s.Page, false)
	clockBefore, wallBefore := s.Page.ClockNow(), time.Now()
	eval(s.Page, `
globalThis.busyDone = false;
const busyStart = performance.now();
while (performance.now() - busyStart < 150) {}
setTimeout(() => {
  busyDone = true;
}, 0);
void 0;
`)
	resumeBusy()
	waitTrue(s.Page, "globalThis.busyDone===true")
	if elapsed, wall := s.Page.ClockNow().Sub(clockBefore), time.Since(wallBefore); elapsed > wall+75*time.Millisecond {
		t.Fatalf("pump counted external execution twice: clock=%s wall=%s", elapsed, wall)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled pump stayed asleep")
	}
	eval(s.Page, `globalThis.afterCancel=false;setTimeout(()=>{afterCancel=true},0);void 0`)
	s.Page.WakeEventLoop()
	if eval(s.Page, "globalThis.afterCancel") != false {
		t.Fatal("wake restarted a cancelled pump")
	}
}
