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
