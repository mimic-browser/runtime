package browser

import (
	"context"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestV8ObjectOrderAcrossRealmAndMessageBoundaries(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := p.Evaluate(ctx, `(async () => {
  const leaf = { zeta: 1, alpha: 2, 10: 3, 2: 4, "01": 5, 4294967295: 6 };
  delete leaf.zeta;
  leaf.zeta = 7;
  const original = { leaf, again: leaf };
  original.self = original;
  const keys = (value) => Object.keys(value.leaf).join("|");
  const expected = keys(original);
  const check = (value, boundary) => {
    if (
      keys(value) !== expected ||
      value.leaf !== value.again ||
      value.self !== value
    ) {
      throw new Error(boundary + " changed property order or graph identity");
    }
  };
  check(structuredClone(original), "structuredClone");

  const frame = document.createElement("iframe");
  document.body.appendChild(frame);
  try {
    const child = frame.contentWindow;
    const remote = child.eval(
      [
        'globalThis.ordered = { zeta: 1, alpha: 2, "10": 3, "2": 4, "01": 5 };',
        'ordered[Symbol.for("order")] = 6;',
        "delete ordered.zeta;",
        "ordered.zeta = 7;",
        "ordered;",
      ].join("\n"),
    );
    const observed = Reflect.ownKeys(remote).map(String).join("|");
    const native = child.eval('Reflect.ownKeys(ordered).map(String).join("|")');
    if (observed !== native)
      throw new Error("iframe reflection changed property order");
  } finally {
    frame.remove();
  }

  const channel = new MessageChannel();
  try {
    const received = await new Promise((resolve) => {
      channel.port2.onmessage = (event) => resolve(event.data);
      channel.port1.postMessage(original);
    });
    check(received, "MessagePort");
  } finally {
    channel.port1.close();
    channel.port2.close();
  }

  const url = URL.createObjectURL(
    new Blob(["onmessage = event => postMessage(event.data);"], {
      type: "text/javascript",
    }),
  );
  const worker = new Worker(url);
  try {
    const received = await new Promise((resolve, reject) => {
      worker.onmessage = (event) => resolve(event.data);
      worker.onerror = (event) => reject(new Error(event.message));
      worker.postMessage(original);
    });
    check(received, "Worker");
  } finally {
    worker.terminate();
    URL.revokeObjectURL(url);
  }
  return expected;
})();`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "2|10|alpha|01|4294967295|zeta" {
		t.Fatalf("observable property order: %v", result)
	}
}
