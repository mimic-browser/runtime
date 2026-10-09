package browser

import "testing"

func TestSelectorsFilteredNthSiblings(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `(()=>{
   document.body.innerHTML='<ul><li id="a" class="x"></li><li id="b"></li><li id="c" class="x"></li><li id="d" class="y"></li><li id="e" class="x"></li></ul>';
   const query=s=>Array.from(document.querySelectorAll(s),e=>e.id).join(',');
   const values=[query('li:nth-child(2 of .x)'),query('li:nth-last-child(2 of .x)'),query('li:nth-child(2n of .x,.y)'),query('ul:has(> li:nth-child(3 of .x)) > li:nth-child(-n+2 of :is(.x,.y))')];
   document.querySelector('#b').className='x';
   values.push(query('li:nth-child(2 of .x)'));
   document.querySelector('#a').remove();
   values.push(query('li:nth-child(2 of .x)'));
   for(const selector of [':nth-child(2 of ::before)', ':nth-of-type(2 of .x)', ':nth-child(2 of .x,:bogus)']) {
    try {document.querySelector(selector);values.push('accepted')}catch(e){values.push(e.name)}
   }
   return values.join('|');
  })()`, `c|c|c,e|a,c|b|c|SyntaxError|SyntaxError|SyntaxError`)
	})
}
