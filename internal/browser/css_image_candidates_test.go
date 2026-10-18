package browser

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Keep discovery on the compatibility path, with many non-image declarations.
// The callback timing excludes navigation, loading and the next checkpoint.
func TestCSSBackgroundSparseCandidates(t *testing.T) {
	historyTestPages(t, func(t *testing.T, p *Page) {
		reduced := true
		p.SetMediaPreferences("", &reduced)
		navigateCapabilityFixture(t, p)
		var css strings.Builder
		for i := 0; i < 180; i++ {
			fmt.Fprintf(&css, ".row { --v%d: %d; color: green }\n", i, i)
		}
		css.WriteString(`.image{background-image:var(--image)} .parent{background-image:url(data:,parent)} .inherit{all:inherit} .off{background-image:none!important} @media(prefers-reduced-motion:reduce){.media{background-image:url(data:,media)}} .unused{background-image:url(data:,unused)}`)
		r := p.Top.Realm
		setup, err := r.runtime.Eval(context.Background(), fmt.Sprintf(`document.head.innerHTML = '<style>' + %q + '</style>';
document.body.innerHTML =
  '<section style="--image:url(data:,variable)"><i class="image"></i></section><section class="parent"><i class="inherit"></i></section><i class="media"></i><i class="image off" style="--image:url(data:,forbidden)"></i>' +
  '<div class="row"></div>'.repeat(1800);
true;`, css.String()), "resource-fixture")
		if err != nil {
			t.Fatal(err)
		}
		releaseDebuggerValue(r, setup)
		if err := r.runtime.Set("resourceReadForTest", r.computedStyleFlatRead); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			started := time.Now()
			value, err := r.runtime.Eval(context.Background(), `JSON.stringify(JSON.parse(resourceReadForTest(0, 'imageResources', '')).sort());`, "resource-candidates")
			t.Logf("resource discovery %d: %s", i, time.Since(started))
			if err != nil {
				t.Fatal(err)
			}
			got := value.Export()
			releaseDebuggerValue(r, value)
			if got != `["data:,media","data:,parent","data:,variable"]` {
				t.Fatalf("resources: %v", got)
			}
		}
	})
}

func TestCSSBackgroundCandidateEquivalence(t *testing.T) {
	for _, mode := range []string{"native", "reduced-motion", "device-scale", "frame", "shadow", "animation"} {
		t.Run(mode, func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				navigateCapabilityFixture(t, p)
				if mode == "reduced-motion" {
					reduced := true
					p.SetMediaPreferences("", &reduced)
				}
				if mode == "device-scale" {
					p.env.Display.DeviceScaleFactor = 2
				}
				r := p.Top.Realm
				if mode == "frame" {
					historyEval(t, p, `new Promise((resolve) => {
  const f = document.createElement('iframe');
  f.onload = () => resolve(true);
  f.srcdoc = '<!doctype html><body>';
  document.body.appendChild(f);
});`, true)
					for _, child := range p.Top.children {
						r = child.Realm
					}
					if r == p.Top.Realm {
						t.Fatal("missing child realm")
					}
				}
				if err := r.runtime.Set("resourceReadForTest", r.computedStyleFlatRead); err != nil {
					t.Fatal(err)
				}
				source := `document.head.innerHTML =
  '<style>.image{background:url(data:,rule)} .parent{background-image:url(data:,parent)} .inherit{all:inherit} .hidden{display:none} .off{background-image:none!important} .unused{background-image:url(data:,unused)}</style>';
document.body.innerHTML =
  '<div class="parent"><i class="inherit"></i></div><i id="target" class="image"></i><i class="image off"></i><div class="hidden"><i class="image"></i></div><i style="background-image:var(--picture);--picture:url(data:,inline)"></i><div id="extra"></div>';
const compareResources = () => {
  const actual = JSON.parse(resourceReadForTest(0, 'imageResources', '')).sort();
  const expected = new Set();
  for (const element of document.querySelectorAll('*')) {
    let visible = true;
    for (let parent = element; parent; parent = parent.parentElement)
      if (getComputedStyle(parent).display === 'none') visible = false;
    if (visible)
      for (const match of getComputedStyle(element).backgroundImage.matchAll(
        /url\(["']?([^"')]+)["']?\)/g,
      ))
        expected.add(match[1]);
  }
  const oracle = Array.from(expected).sort();
  if (JSON.stringify(actual) !== JSON.stringify(oracle))
    throw Error(JSON.stringify({ actual, oracle }));
  return actual;
};`
				if mode == "shadow" {
					source += `document.getElementById('extra').attachShadow({ mode: 'open' }).innerHTML = '<slot></slot>';`
				}
				if mode == "animation" {
					source += `document
  .getElementById('extra')
  .animate(
    [{ backgroundImage: 'url(data:,animated)' }, { backgroundImage: 'url(data:,animated)' }],
    { duration: 100000, fill: 'both' },
  );`
				}
				source += `compareResources();
document.getElementById('target').className = 'off';
compareResources();
document.styleSheets[0].insertRule('#target{background-image:url(data:,cssom)!important}', 0);
compareResources();
document
  .getElementById('target')
  .style.setProperty('background-image', 'url(data:,edited)', 'important');
compareResources();
document.querySelector('.hidden').style.display = 'block';
compareResources();
document.getElementById('target').remove();
compareResources();
true;`
				value, err := r.runtime.Eval(context.Background(), source, "resource-equivalence")
				defer releaseDebuggerValue(r, value)
				if err != nil || value.Export() != true {
					t.Fatalf("resource equivalence: %v %v", value, err)
				}
			})
		})
	}
}

func TestCSSBackgroundResourcePreflight(t *testing.T) {
	for _, mode := range []string{"escaped-inline", "escaped-sheet"} {
		t.Run(mode, func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				navigateCapabilityFixture(t, p)
				r := p.Top.Realm
				if err := r.runtime.Set("resourceReadForTest", r.computedStyleFlatRead); err != nil {
					t.Fatal(err)
				}
				source := `document.body.innerHTML='<div id="image"></div>';`
				switch mode {
				case "escaped-inline":
					source += `document.getElementById('image').setAttribute('style', String.raw` + "`" + `background-image:u\72l(data:,preflight)` + "`" + `);`
				case "escaped-sheet":
					source += `document.head.innerHTML=String.raw` + "`" + `<style>#image{background-image:u\72l(data:,preflight)}</style>` + "`" + `;`
				}
				source += `JSON.stringify({computed:getComputedStyle(document.getElementById('image')).backgroundImage,resources:JSON.parse(resourceReadForTest(0,'imageResources',''))});`
				value, err := r.runtime.Eval(context.Background(), source, "resource-preflight")
				defer releaseDebuggerValue(r, value)
				if err != nil {
					t.Fatal(err)
				}
				if got := fmt.Sprint(value.Export()); !strings.Contains(got, `"resources":["data:,preflight"]`) {
					t.Fatalf("resource preflight lost computed image: %s", got)
				}
			})
		})
	}
}

func TestCSSBackgroundPreferencesInvalidateAdmission(t *testing.T) {
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `document.head.innerHTML='<style>#target{background-image:url(data:,ordinary);width:10px}@media(prefers-reduced-motion:reduce){#target{background-image:url(data:,reduced);width:20px}}</style>';document.body.innerHTML='<div id="target"></div>';true`, true)
		r := p.Top.Realm
		if err := r.runtime.Set("resourceReadForTest", r.computedStyleFlatRead); err != nil {
			t.Fatal(err)
		}
		for _, reduced := range []bool{false, true, false} {
			if err := p.SetMediaPreferences("light", &reduced); err != nil {
				t.Fatal(err)
			}
			wantURL, wantWidth := "data:,ordinary", "10px"
			if reduced {
				wantURL, wantWidth = "data:,reduced", "20px"
			}
			value, err := r.runtime.Eval(context.Background(), `JSON.stringify({resources:JSON.parse(resourceReadForTest(0,'imageResources','')),width:getComputedStyle(document.getElementById('target')).width,reduce:matchMedia('(prefers-reduced-motion:reduce)').matches})`, "resource-preferences")
			if err != nil {
				t.Fatal(err)
			}
			got := fmt.Sprint(value.Export())
			releaseDebuggerValue(r, value)
			want := fmt.Sprintf(`{"resources":[%q],"width":%q,"reduce":%t}`, wantURL, wantWidth, reduced)
			if got != want {
				t.Fatalf("preferences: %s want %s", got, want)
			}
			if r.blitz == nil || (r.blitz.fallback != "") != reduced {
				t.Fatalf("reduced=%t admission=%+v", reduced, r.blitz)
			}
		}
	})
}
