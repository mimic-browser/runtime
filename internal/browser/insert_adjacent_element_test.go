package browser

import (
	"context"
	"testing"
)

func TestInsertAdjacentElement(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	got, err := p.Evaluate(context.Background(), `(() => {
  const host = document.createElement('section');
  const target = document.createElement('div');
  host.appendChild(target);
  document.body.appendChild(host);
  const before = document.createElement('b');
  const first = document.createElement('i');
  const last = document.createElement('u');
  const after = document.createElement('em');
  const results = [
    target.insertAdjacentElement('beforebegin', before) === before,
    target.insertAdjacentElement('afterbegin', first) === first,
    target.insertAdjacentElement('beforeend', last) === last,
    target.insertAdjacentElement('afterend', after) === after,
    host.children.length === 3 && host.firstChild === before && host.lastChild === after,
    target.firstChild === first && target.lastChild === last,
    before.parentNode === host && after.parentNode === host,
    target.insertAdjacentElement('beforeend', first) === first && target.lastChild === first,
  ];
  const detached = document.createElement('div');
  results.push(detached.insertAdjacentElement('beforebegin', document.createElement('b')) === null);
  try { target.insertAdjacentElement('invalid', document.createElement('b')); results.push(false); }
  catch (error) { results.push(error.name === 'SyntaxError'); }
  const style = document.createElement('style');
  style.textContent = '.probe { color: red }';
  results.push(document.head.insertAdjacentElement('beforeend', style) === style &&
    style.parentNode === document.head && document.head.removeChild(style) === style &&
    style.parentNode === null);
  return results;
})()`)
	if err != nil {
		t.Fatal(err)
	}
	checks, ok := got.([]any)
	if !ok || len(checks) != 11 {
		t.Fatalf("unexpected result: %#v", got)
	}
	for i, check := range checks {
		if check != true {
			t.Fatalf("check %d failed: %#v", i, got)
		}
	}
}
