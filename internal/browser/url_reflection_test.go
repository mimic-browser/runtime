package browser

import (
	"context"
	"testing"
)

// Frozen headful Chrome 152 returns the attribute text for unresolvable URL
// attributes on about:blank. URL construction remains a throwing operation.
func TestURLAttributesPreserveUnresolvableText(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(() => {
  for (const tag of ['link', 'a', 'img', 'script']) {
    const property = ['img', 'script'].includes(tag) ? 'src' : 'href';
    const element = document.createElement(tag);
    if (element[property] !== '') return tag + ': absent';
    for (const text of ['/asset.js', 'http://[invalid', 'https://example.com/x', '']) {
      element.setAttribute(property, text);
      if (element[property] !== text) return tag + ': ' + text;
    }
  }
  for (const text of ['/asset.js', 'http://[invalid']) {
    try { new URL(text, 'about:blank'); return 'constructor accepted ' + text; }
    catch (error) { if (error.name !== 'TypeError') return error.name; }
  }
  return true;
})()`)
		if err != nil || value != true {
			t.Fatalf("URL reflection: %v %v", value, err)
		}
	})
}
