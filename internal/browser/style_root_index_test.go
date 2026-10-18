package browser

import (
	"context"
	"testing"
)

func TestRootStyleRulesFollowRelationalMutations(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(()=>{
 document.head.innerHTML='<style>:root{--tone:off}:root:has([data-tone=on]){--tone:on}:root .probe{width:17px}</style>';
 document.body.innerHTML='<div class="probe"></div><div id="switch"></div>';
 const target=document.getElementById('switch');
 const read=()=>[getComputedStyle(document.documentElement).getPropertyValue('--tone').trim(),getComputedStyle(document.body).getPropertyValue('--tone').trim(),getComputedStyle(document.querySelector('.probe')).width];
 const before=read();target.setAttribute('data-tone','on');const enabled=read();target.removeAttribute('data-tone');const restored=read();
 return JSON.stringify({before,enabled,restored});
})()`)
		want := `{"before":["off","off","17px"],"enabled":["on","on","17px"],"restored":["off","off","17px"]}`
		if err != nil || got != want {
			t.Fatalf("root style invalidation: %v %v", got, err)
		}
	})
}

func TestRootStructuralHasCacheInvalidatesWithDescendantChanges(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(()=>{
 document.head.innerHTML='<style>:root{--state:off}:root:has(.marker > [data-state=on]){--state:on}:root:has(input:checked){--checked:yes}</style>';
 document.body.innerHTML='<div class="marker"><span id="target"></span></div><input type="checkbox">';
 const root=document.documentElement,target=document.getElementById('target'),box=document.querySelector('input');
 const read=()=>[getComputedStyle(root).getPropertyValue('--state').trim(),getComputedStyle(root).getPropertyValue('--checked').trim()];
 const result=[read()];
 target.setAttribute('data-other','x');result.push(read());
 target.setAttribute('data-state','on');result.push(read());
 target.removeAttribute('data-state');result.push(read());
 const added=document.createElement('i');added.setAttribute('data-state','on');target.parentNode.append(added);result.push(read());
 added.remove();result.push(read());
 box.checked=true;result.push(read());box.checked=false;result.push(read());
 return JSON.stringify(result);
})()`)
		want := `[["off",""],["off",""],["on",""],["off",""],["on",""],["off",""],["off","yes"],["off",""]]`
		if err != nil || got != want {
			t.Fatalf("root relational cache invalidation: %v %v", got, err)
		}
	})
}
