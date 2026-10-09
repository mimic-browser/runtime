package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultFormStateReachesStyleBeforeJavaScriptReads(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><style>#answer{display:none}#toggle:checked~#answer{display:block}</style><body><form><input id="toggle" type="checkbox" checked><div id="answer">VISIBLE</div></form></body>`)
	}))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `[document.body.innerText,getComputedStyle(document.getElementById('answer')).display].join('|')`, `VISIBLE|block`)
		historyEval(t, p, `(()=>{const box=document.getElementById('toggle');box.checked=false;const before=document.body.innerText;box.form.reset();return [before,document.body.innerText,box.checked].join('|');})()`, `|VISIBLE|true`)
	})
}
