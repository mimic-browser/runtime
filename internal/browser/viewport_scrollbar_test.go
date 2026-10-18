//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"testing"
)

// Frozen headful Chrome 152: a classic auto scrollbar consumes root layout
// width, but 100vw retains the physical viewport width.
func TestViewportScrollbarRootGeometry(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(() => {
  document.body.style.margin = '0';
  document.body.innerHTML = '<div id="content"></div><div id="vw" style="position:fixed;width:100vw;height:1px"></div>';
  const content = document.getElementById('content');
  const vw = document.getElementById('vw');
  const measure = () => [
    innerWidth,
    document.documentElement.clientWidth,
    document.body.clientWidth,
    document.documentElement.getBoundingClientRect().width,
    vw.getBoundingClientRect().width,
  ];
  content.style.height = '10px';
  const short = measure();
  content.style.height = (innerHeight + 100) + 'px';
  const tall = measure();
  document.documentElement.style.scrollbarWidth = 'thin';
  const thin = measure();
  document.documentElement.style.scrollbarWidth = 'none';
  const none = measure();
  document.documentElement.style.scrollbarWidth = 'auto';
  document.documentElement.style.overflowY = 'hidden';
  const hidden = measure();
  content.style.height = '10px';
  document.documentElement.style.overflowY = 'scroll';
  const scroll = measure();
  return JSON.stringify({ short, tall, thin, none, hidden, scroll });
})()`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"short":[1272,1272,1272,1272,1272],"tall":[1272,1257,1257,1257,1272],"thin":[1272,1262,1262,1262,1272],"none":[1272,1272,1272,1272,1272],"hidden":[1272,1272,1272,1272,1272],"scroll":[1272,1257,1257,1257,1257]}`
	if value != want {
		t.Fatalf("viewport scrollbar geometry: %v, want %s", value, want)
	}
}

func TestVisualViewportExcludesClassicScrollbar(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(() => {
	  document.body.style.margin = '0';
	  document.body.innerHTML = '<div id="content"></div>';
	  const content = document.getElementById('content');
	  content.style.height = '10px';
	  const short = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  content.style.height = (innerHeight + 100) + 'px';
	  const tall = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  return JSON.stringify({short,tall});
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"short":[1272,1272,1272],"tall":[1272,1257,1257]}`
	if value != want {
		t.Fatalf("visual viewport scrollbar geometry: %v, want %s", value, want)
	}
}

func TestAuthorScrollbarPseudoWidthAffectsViewport(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(() => {
	  document.head.innerHTML = '<style id="scrollbar-style">::-webkit-scrollbar { width: 14px }</style>';
	  document.body.style.margin = '0';
	  document.body.innerHTML = '<div style="height:3000px"></div>';
	  const authored = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  document.getElementById('scrollbar-style').textContent = '::-webkit-scrollbar { width: 10px }';
	  const updated = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  document.getElementById('scrollbar-style').textContent =
	    'html::-webkit-scrollbar { width: 12px !important } @media print { html::-webkit-scrollbar { width: 20px !important } }';
	  const cascaded = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  document.getElementById('scrollbar-style').textContent =
	    '::-webkit-scrollbar { width: 12px } html.wide::-webkit-scrollbar { width: 20px }';
	  document.documentElement.classList.add('wide');
	  const classMatched = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  document.documentElement.classList.remove('wide');
	  const classRemoved = [innerWidth, visualViewport.width, document.documentElement.clientWidth];
	  return JSON.stringify({authored,updated,cascaded,classMatched,classRemoved});
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"authored":[1272,1258,1258],"updated":[1272,1262,1262],"cascaded":[1272,1260,1260],"classMatched":[1272,1252,1252],"classRemoved":[1272,1260,1260]}`
	if value != want {
		t.Fatalf("author scrollbar pseudo width: %v, want %s", value, want)
	}
}
