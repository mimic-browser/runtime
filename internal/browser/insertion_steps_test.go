package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestFrameInsertionAfterInnerHTMLAndFragmentMove(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<!doctype html><body></body>")) }))
	defer server.Close()
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
			b, err := New(factory, chrome152.New())
			if err != nil {
				t.Fatal(err)
			}
			c := b.NewContext()
			defer c.Close()
			p, err := c.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			// Windows CI runs this behind large browser-test batches; allow the
			// iframe navigation enough scheduling headroom without changing the
			// behavioral assertion or accepting an incomplete lifecycle.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := p.Navigate(ctx, server.URL); err != nil {
				t.Fatal(err)
			}
			v, err := p.Evaluate(ctx, `new Promise(resolve=>{
 const parent=document.createElement('div');parent.innerHTML='<section><iframe src="/child"></iframe></section>';
 const iframe=parent.querySelector('iframe'),fragment=document.createDocumentFragment();
 fragment.appendChild(parent);
 if(iframe.isConnected)throw Error('detached fragment connected');
 iframe.addEventListener('load',()=>resolve(iframe.isConnected&&iframe.contentWindow!==null&&parent.parentNode===document.body&&fragment.childNodes.length===0));
 document.body.appendChild(fragment);
})`)
			if err != nil || v != true {
				t.Fatalf("frame insertion: %v %v", v, err)
			}
		})
	}
}

// Native insertion/connectivity do not invoke author DOM accessors. The reduced
// probe also passes in frozen Chrome 152 headless; these DOM semantics do not
// depend on presentation mode.
func TestFrameInsertionIgnoresPublicTreeOverrides(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), "about:blank"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(() => {
  const shadowHost = document.createElement('div');
  document.body.appendChild(shadowHost);
  const shadow = shadowHost.attachShadow({ mode: 'closed' });
  const observer = new MutationObserver(() => {});
  observer.observe(document.body, { childList: true, subtree: true });
  observer.observe(shadow, { childList: true, subtree: true });
  const connected = Object.getOwnPropertyDescriptor(Node.prototype, 'isConnected').get;
  const windowOf = Object.getOwnPropertyDescriptor(
    HTMLIFrameElement.prototype,
    'contentWindow',
  ).get;
  const documentOf = Object.getOwnPropertyDescriptor(
    HTMLIFrameElement.prototype,
    'contentDocument',
  ).get;
  let reads = 0;
  const hideTree = (node) => {
    for (const key of ['children', 'childNodes', 'parentNode', 'isConnected']) {
      Object.defineProperty(node, key, {
        get() {
          reads++;
          return undefined;
        },
      });
    }
  };
  hideTree(shadowHost);
  for (const parent of [document.body, shadow]) {
    for (const method of ['appendChild', 'insertBefore']) {
      const wrapper = document.createElement('section');
      const frame = document.createElement('iframe');
      frame.style.cssText = 'width: 320px; height: 120px; border: 0';
      wrapper.appendChild(frame);
      hideTree(wrapper);
      hideTree(frame);
      if (connected.call(frame)) return 'connected detached descendant';
      parent[method](wrapper, null);
      if (!connected.call(frame)) return 'disconnected descendant';
      if (!windowOf.call(frame) || !documentOf.call(frame)) return 'missing frame';
      if (windowOf.call(frame).innerWidth !== 320 || windowOf.call(frame).innerHeight !== 120)
        return 'incorrect child viewport';
      parent.removeChild(wrapper);
      if (connected.call(frame)) return 'connected removed descendant';
    }
  }
  const records = observer.takeRecords();
  observer.disconnect();
  if (records.length !== 8) return 'incorrect mutation records: ' + records.length;
  return reads === 0 ? true : 'public getter reads: ' + reads;
})();`, true)
	})
}

func TestShadowFrameInsertionDispatchesInitialLoad(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		historyEval(t, p, `(() => {
  const host = document.createElement('div');
  host.id = 'frame-host';
  document.body.appendChild(host);
  const root = host.attachShadow({ mode: 'closed' });
  const frame = document.createElement('iframe');
  frame.style.display = 'none';
  let loaded = 0;
  frame.onload = () => {
    if (loaded++) return;
    frame.contentDocument.open('text/html', 'replace');
    frame.contentDocument.write('<!doctype html><body>child</body>');
    frame.contentDocument.close();
    if (document.getElementById('frame-host') !== host) throw Error('owner document changed');
    frame.style.display = 'block';
  };
  root.appendChild(frame);
  return loaded > 0 && frame.style.display === 'block' ? true : 'initial load: ' + loaded;
})();`, true)
	})
}
