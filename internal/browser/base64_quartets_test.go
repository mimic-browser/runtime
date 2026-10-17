package browser

import (
	"context"
	"os"
	"strconv"
	"testing"
)

func TestBase64QuartetBoundariesWindowAndWorker(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/base64_quartets.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		for _, worker := range []bool{false, true} {
			expression := string(source)
			if worker {
				code := "onmessage = () => {\n  try {\n    const value = " + string(source) + "\n    postMessage({value});\n  } catch (error) {\n    postMessage({error: String(error)});\n  }\n};"
				expression = `new Promise((resolve) => {
  const url = URL.createObjectURL(new Blob([` + strconv.Quote(code) + `]));
  const worker = new Worker(url);
  worker.onmessage = (event) => {
    worker.terminate();
    URL.revokeObjectURL(url);
    resolve(event.data);
  };
  worker.onerror = (event) => {
    worker.terminate();
    URL.revokeObjectURL(url);
    resolve({error: event.message});
  };
  worker.postMessage(null);
})`
			}
			got, err := p.Evaluate(context.Background(), expression)
			if err != nil {
				t.Fatalf("worker=%v: %v", worker, err)
			}
			if worker {
				outcome, ok := got.(map[string]any)
				if !ok || outcome["value"] != true {
					t.Fatalf("worker result: %#v", got)
				}
			} else if got != true {
				t.Fatalf("window result: %#v", got)
			}
		}
	})
}

func TestBase64QuartetBoundariesRestoredRealm(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/base64_quartets.js")
	if err != nil {
		t.Fatal(err)
	}
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	expression := `(()=>{const frame=document.createElement('iframe');document.body.append(frame);try{return frame.contentWindow.eval(` + strconv.Quote(string(source)) + `)}finally{frame.remove()}})()`
	if got := bootstrapSnapshotEvaluate(t, p, expression); got != true {
		t.Fatalf("restored quartet semantics: %v", got)
	}
	// The fixture removes the child; verify a separately retained restored realm.
	bootstrapSnapshotEvaluate(t, p, `const retainedBase64Frame=document.createElement('iframe');document.body.append(retainedBase64Frame);retainedBase64Frame.contentWindow.eval('atob("YWJjZA==")')`)
	bootstrapSnapshotAssertRestored(t, p, 1)
}
