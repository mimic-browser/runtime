package browser

import (
	"context"
	"math"
	"strconv"
	"testing"
)

func TestWebAnimationReturnsCoherentPlaybackControls(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), `(()=>{
const element=document.createElement('div');document.body.append(element);
element.style.opacity='.2';const effectConstructed=new KeyframeEffect(element,{opacity:[0,1]},{duration:500,fill:'both'}),idle=new Animation(effectConstructed,document.timeline),animation=element.animate({opacity:[0,1]},{duration:500,fill:'both'});let finishes=0;animation.addEventListener('finish',()=>finishes++);
const effect=animation.effect,initial={animation:animation instanceof Animation,effect:effect instanceof KeyframeEffect,state:animation.playState,pending:animation.pending,current:animation.currentTime,timing:effect.getTiming(),frames:effect.getKeyframes(),elementAnimations:element.getAnimations()[0]===animation,documentAnimations:document.getAnimations()[0]===animation};
animation.startTime=performance.now()-250;const running={pending:animation.pending,start:typeof animation.startTime,current:animation.currentTime,computed:effect.getComputedTiming(),opacity:getComputedStyle(element).opacity,inline:element.style.opacity};animation.finish();const finished={state:animation.playState,current:animation.currentTime,opacity:getComputedStyle(element).opacity};animation.cancel();return{constructors:[Animation.length,KeyframeEffect.length,Element.prototype.animate.length,idle.playState,idle.currentTime,document.timeline instanceof DocumentTimeline],initial,running,finished,finishes,canceled:{state:animation.playState,current:animation.currentTime,count:document.getAnimations().length}};
})()`)
		if err != nil {
			t.Fatal(err)
		}
		got := value.(map[string]any)
		constructors := got["constructors"].([]any)
		if constructors[0] != int64(0) && constructors[0] != float64(0) || constructors[1] != int64(1) && constructors[1] != float64(1) || constructors[2] != int64(1) && constructors[2] != float64(1) || constructors[3] != "idle" || constructors[4] != nil || constructors[5] != true {
			t.Fatalf("animation constructors: %#v", constructors)
		}
		initial := got["initial"].(map[string]any)
		current, currentOK := numberParameter(initial["current"])
		if initial["animation"] != true || initial["effect"] != true || initial["state"] != "running" || initial["pending"] != true || !currentOK || current != 0 || initial["elementAnimations"] != true || initial["documentAnimations"] != true {
			t.Fatalf("initial animation: %#v", initial)
		}
		timing := initial["timing"].(map[string]any)
		duration, durationOK := numberParameter(timing["duration"])
		iterations, iterationsOK := numberParameter(timing["iterations"])
		if !durationOK || duration != 500 || timing["fill"] != "both" || !iterationsOK || iterations != 1 {
			t.Fatalf("animation timing: %#v", timing)
		}
		running := got["running"].(map[string]any)
		runningCurrent, runningCurrentOK := numberParameter(running["current"])
		if running["pending"] != false || running["start"] != "number" || !runningCurrentOK || runningCurrent <= 0 {
			t.Fatalf("running animation: %#v", running)
		}
		computed := running["computed"].(map[string]any)
		progress, progressOK := numberParameter(computed["progress"])
		iteration, iterationOK := numberParameter(computed["currentIteration"])
		if !progressOK || progress < .45 || progress > .65 || !iterationOK || iteration != 0 {
			t.Fatalf("computed animation timing: %#v", computed)
		}
		opacity, opacityErr := strconv.ParseFloat(running["opacity"].(string), 64)
		if opacityErr != nil || opacity < .45 || opacity > .65 || running["inline"] != "0.2" {
			t.Fatalf("sampled animation style: %#v", running)
		}
		finished := got["finished"].(map[string]any)
		finishedCurrent, finishedCurrentOK := numberParameter(finished["current"])
		if finished["state"] != "finished" || !finishedCurrentOK || finishedCurrent != 500 {
			t.Fatalf("finished animation: %#v", finished)
		}
		canceled := got["canceled"].(map[string]any)
		count, countOK := numberParameter(canceled["count"])
		if canceled["state"] != "idle" || canceled["current"] != nil || !countOK || count != 0 {
			t.Fatalf("canceled animation: %#v", canceled)
		}
	})
}

// Chrome 152 AnimationClock::CurrentTime caches its sample for the running task:
// https://github.com/chromium/chromium/blob/152.0.7977.82/third_party/blink/renderer/core/animation/animation_clock.cc
func TestWebAnimationObservationsShareTaskTime(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), `(() => {
  const element = document.createElement("div");
  document.body.append(element);
  const animation = element.animate(
    { opacity: [0, 1] },
    { duration: 1000, fill: "both" },
  );
  animation.startTime = document.timeline.currentTime - 250;
  const before = {
    timeline: document.timeline.currentTime,
    current: animation.currentTime,
    timing: animation.effect.getComputedTiming(),
    opacity: Number(getComputedStyle(element).opacity),
  };
  const started = performance.now();
  while (performance.now() - started < 100) {}
  const after = {
    timeline: document.timeline.currentTime,
    current: animation.currentTime,
    timing: animation.effect.getComputedTiming(),
    opacity: Number(getComputedStyle(element).opacity),
  };
  animation.pause();
  queueMicrotask(() => {
    globalThis.animationTaskSample = document.timeline.currentTime;
  });
  return { before, after };
})();`)
		if err != nil {
			t.Fatal(err)
		}
		got := value.(map[string]any)
		before, after := got["before"].(map[string]any), got["after"].(map[string]any)
		for _, sample := range []map[string]any{before, after} {
			current, _ := numberParameter(sample["current"])
			opacity, _ := numberParameter(sample["opacity"])
			timing := sample["timing"].(map[string]any)
			local, _ := numberParameter(timing["localTime"])
			progress, _ := numberParameter(timing["progress"])
			if current != 250 || local != current || progress != .25 || opacity != .25 {
				t.Fatalf("incoherent animation sample: %#v", sample)
			}
		}
		if before["timeline"] != after["timeline"] || before["current"] != after["current"] {
			t.Fatalf("animation time advanced inside one task: %#v", got)
		}
		value, err = page.Evaluate(context.Background(), `(() => {
  const animation = document.getAnimations()[0];
  animation.play();
  animation.startTime = document.timeline.currentTime - 400;
  const current = animation.currentTime;
  const result = {
    timeline: document.timeline.currentTime,
    microtask: globalThis.animationTaskSample,
    current,
    local: animation.effect.getComputedTiming().localTime,
    opacity: Number(getComputedStyle(document.querySelector("div")).opacity),
  };
  animation.cancel();
  return result;
})();`)
		if err != nil {
			t.Fatal(err)
		}
		next := value.(map[string]any)
		oldTime, _ := numberParameter(before["timeline"])
		nextTime, _ := numberParameter(next["timeline"])
		nextCurrent, _ := numberParameter(next["current"])
		nextOpacity, _ := numberParameter(next["opacity"])
		if nextTime <= oldTime || next["microtask"] != before["timeline"] || next["current"] != next["local"] || nextCurrent != 400 || math.Abs(nextOpacity-nextCurrent/1000) > .0001 {
			t.Fatalf("next task or microtask animation observations: %#v", next)
		}
	})
}
