//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"
)

func TestBinaryStringProjectionOwnsViewedBytes(t *testing.T) {
	runtime := (Factory{}).New()
	defer runtime.Close()
	if err := runtime.Set("binaryText", runtime.(interface{ BinaryStringFunction() any }).BinaryStringFunction()); err != nil {
		t.Fatal(err)
	}

	result, err := runtime.Eval(context.Background(), `(() => {
  const storage = new Uint8Array([19, 0, 128, 255, 65, 23]);
  const text = binaryText(new Uint8Array(storage.buffer, 1, 4));
  if (
    typeof text !== 'string' ||
    text.length !== 4 ||
    text.charCodeAt(0) !== 0 ||
    text.charCodeAt(1) !== 128 ||
    text.charCodeAt(2) !== 255 ||
    text.charCodeAt(3) !== 65
  )
    return false;
  const retained = [];
  for (const size of [0, 1, 1024, 65536, 65537, 131072, 32]) {
    const view = new Uint8Array(size);
    view.fill(size & 255);
    const current = binaryText(view);
    retained.push([current, size, size & 255]);
    view.fill(7);
  }
  storage.fill(42);
  for (const [current, size, code] of retained) {
    if (current.length !== size) return false;
    for (let i = 0; i < size; i++) if (current.charCodeAt(i) !== code) return false;
  }
  if (text.charCodeAt(1) !== 128) return false;
  let touched = false;
  try {
    binaryText(
      new Proxy(
        {},
        {
          get() {
            touched = true;
            throw Error('author getter');
          },
        },
      ),
    );
    return false;
  } catch (error) {}
  return !touched;
})();`, "binary-text-view.js")
	if err != nil || result.Export() != true {
		t.Fatalf("binary string ownership: %v, %v", result, err)
	}
	adapter := runtime.(*adapter)
	if cap(adapter.binaryStringScratch) > 64<<10 {
		t.Fatalf("retained %d transfer bytes", cap(adapter.binaryStringScratch))
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if adapter.binaryStringScratch != nil {
		t.Fatal("transfer storage retained after close")
	}
}
