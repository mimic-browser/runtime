package browser

import (
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestBlobModuleWorkerRetainsScriptAfterImmediateRevocation(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	t.Cleanup(func() { _ = c.Close() })
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, p)
	historyEval(t, p, `new Promise(resolve => {
  const url = URL.createObjectURL(new Blob(["postMessage('started')"], {type:'text/javascript'}));
  const worker = new Worker(url, {type:'module'});
  worker.onmessage = event => { worker.terminate(); resolve(event.data); };
  worker.onerror = event => { worker.terminate(); resolve('error:' + event.message); };
  URL.revokeObjectURL(url);
})`, "started")
}

func TestBlobWorkerRetainsScriptAfterImmediateRevocation(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `new Promise(resolve => {
  const url = URL.createObjectURL(new Blob(["postMessage('started')"], {type:'text/javascript'}));
  const worker = new Worker(url);
  worker.onmessage = event => { worker.terminate(); resolve(event.data); };
  worker.onerror = () => { worker.terminate(); resolve('error'); };
  URL.revokeObjectURL(url);
})`, "started")
	})
}

func TestBlobSharedWorkerConnectsThroughMessagePort(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `new Promise(resolve => {
  const deadline = setTimeout(() => resolve('timeout'), 1500);
	  // Retained verbatim from the compatibility workload. It deliberately
	  // installs onconnect in a Promise reaction after initial evaluation.
	  const source = 'const h=["platform","platformVersion","model","bitness","architecture","uaFullVersion"];navigator.userAgentData.getHighEntropyValues(h).then((a=>{const n=a?h.map((n=>a[n]||null)):null,e=navigator.userAgentData.brands.map((a=>a.brand+" "+a.version));onconnect=a=>a.ports[0].postMessage([navigator.userAgent,navigator.deviceMemory,navigator.hardwareConcurrency,e,n])}));';
  const url = URL.createObjectURL(new Blob([source], {type:'text/javascript'}));
  const worker = new SharedWorker(url);
  worker.port.addEventListener('message', event => {
	    const [userAgent, deviceMemory, hardwareConcurrency, brands, values] = event.data;
    worker.port.close();
    clearTimeout(deadline);
	    resolve(typeof userAgent === 'string' && userAgent.includes('Chrome/152.') && deviceMemory > 0 && hardwareConcurrency > 0 && brands.every(value => typeof value === 'string') && values.length === 6 && values[4] === 'x86' ? 'ok' : 'invalid');
  });
  worker.onerror = event => resolve('error:' + event.message);
  worker.port.start();
  URL.revokeObjectURL(url);
})`, "ok")
	})
}

func TestBlobFetchRetainsResourceAfterImmediateRevocation(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `async function probe() {
  const url = URL.createObjectURL(new Blob(['body'], {type:'text/plain'}));
  const pending = fetch(url).then(response => response.text(), () => 'error');
  URL.revokeObjectURL(url);
  const initial = await pending;
  const later = await fetch(url).then(() => 'unexpected', () => 'error');
  return initial + ':' + later;
}
probe()`, "body:error")
	})
}

func TestBlobWorkerRejectsPreviouslyRevokedURL(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `new Promise(resolve => {
  const url = URL.createObjectURL(new Blob(["postMessage('unexpected')"], {type:'text/javascript'}));
  URL.revokeObjectURL(url);
  const worker = new Worker(url);
  worker.onmessage = event => { worker.terminate(); resolve(event.data); };
  worker.onerror = () => { worker.terminate(); resolve('error'); };
})`, "error")
	})
}

func TestWorkerBlobFetchRetainsResourceAfterImmediateRevocation(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `new Promise(resolve => {
  const source = [
    "const url = URL.createObjectURL(new Blob(['body'], {type:'text/plain'}));",
    "const pending = fetch(url).then(r => r.text(), () => 'error');",
    "URL.revokeObjectURL(url);",
    "pending.then(async initial => {",
    "  const later = await fetch(url).then(() => 'unexpected', () => 'error');",
    "  postMessage(initial + ':' + later);",
    "});"
  ].join('\n');
  const url = URL.createObjectURL(new Blob([source], {type:'text/javascript'}));
  const worker = new Worker(url);
  worker.onmessage = event => { worker.terminate(); URL.revokeObjectURL(url); resolve(event.data); };
  worker.onerror = event => { worker.terminate(); URL.revokeObjectURL(url); resolve('error:' + event.message); };
})`, "body:error")
	})
}
