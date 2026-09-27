package browser

import (
	"context"
	"testing"
)

func TestWindowFrameIndicesUpdateWithoutLengthRead(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(() => {
			const events = [];
			const record = () => events.push([
				Object.getOwnPropertyNames(window).filter(key => /^\d+$/.test(key)),
				Object.hasOwn(window, '0'),
				typeof window[0],
			]);
			record();
			const frame = document.createElement('iframe');
			document.body.append(frame);
			record();
			// Reading length must not be needed to publish or remove indices.
			void window.length;
			frame.remove();
			record();
			const container = document.createElement('div');
			container.append(document.createElement('iframe'));
			document.body.append(container);
			record();
			container.remove();
			record();
			return JSON.stringify(events);
		})()`)
		want := `[[[],false,"undefined"],[["0"],true,"object"],[[],false,"undefined"],[["0"],true,"object"],[[],false,"undefined"]]`
		if err != nil || value != want {
			t.Fatalf("window frame index lifecycle: %v, %v", value, err)
		}
	})
}
