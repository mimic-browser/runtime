package browser

import (
	"context"
	"testing"
)

// Chrome 152 returns exact UTF-16 units from textContent, including aggregation.
func TestCanonicalTextContentCodeUnits(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		got, err := page.Evaluate(context.Background(), `(()=>{
   const value = "\ud800\\\"\n\ud801\ud83d\ude42";
   const box = document.createElement('div'); box.textContent = value;
   const text = document.createTextNode(value), comment = document.createComment(value);
   const nested = document.createElement('span'); nested.append(text, comment);
   const fragment = document.createDocumentFragment(); fragment.append(nested);
   if (box.textContent !== value || box.firstChild.data !== value || fragment.textContent !== value) return false;
   JSON.stringify = () => { throw Error('author stringify') };
   JSON.parse = () => { throw Error('author parse') };
   String.fromCharCode = () => { throw Error('author fromCharCode') };
   String.prototype.charCodeAt = () => { throw Error('author charCodeAt') };
   String.prototype.slice = () => { throw Error('author slice') };
   text.data = value; box.textContent = value;
   return text.data === value && box.textContent === value && comment.data === value;
  })()`)
		if err != nil || got != true {
			t.Fatalf("canonical text: %v %v", got, err)
		}
	})
}
