package browser

import (
	"context"
	"testing"
)

func TestRegisteredPropertyObservationInvalidation(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(() => {
document.head.innerHTML = '<style>@property --amount { syntax: "<number>"; inherits: false; initial-value: 1; }</style>';
document.body.innerHTML = '<div id="owner"><span id="child"></span></div>';
const child = document.getElementById('child');
const style = getComputedStyle(child);
const values = [];
const read = () => values.push(style.getPropertyValue('--amount'));
read(); read();
document.head.firstChild.textContent = '@property --amount { syntax: "<number>"; inherits: false; initial-value: 2; }';
read();
const sheet = new CSSStyleSheet();
sheet.replaceSync('@property --amount { syntax: "<number>"; inherits: false; initial-value: 3; }');
document.adoptedStyleSheets = [sheet]; read();
sheet.replaceSync('@property --amount { syntax: "<number>"; inherits: false; initial-value: 4; }');
read(); document.adoptedStyleSheets = []; read();
values.push(style.getPropertyValue('--owned'));
CSS.registerProperty({name: '--owned', syntax: '<number>', inherits: false, initialValue: '7'});
values.push(style.getPropertyValue('--owned'));
child.style.setProperty('--owned', '9'); values.push(style.getPropertyValue('--owned'));
child.style.setProperty('--owned', 'bad'); values.push(style.getPropertyValue('--owned'));
return JSON.stringify(values);
})()`)
		want := `["1","1","2","3","4","2","","7","9","7"]`
		if err != nil || got != want {
			t.Fatalf("registered property observation: %v %v", got, err)
		}
	})
}
