package browser

import (
	"context"
	"testing"
)

func TestBlitzRelationalSelectorsRefreshCanonicalState(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(context.Background(), `(() => {
 document.head.innerHTML='<style>.host{width:10px}.host:has(> .flag){width:20px}.host:has(+ .peer){height:30px}.host:has(input:checked){width:40px}</style>';
 document.body.innerHTML='<section class="host"><i></i><input type="checkbox"></section><aside></aside>';
 const host=document.querySelector('section'),child=host.firstChild,input=host.lastChild,peer=host.nextElementSibling,style=getComputedStyle(host);
 const check=(width,height)=>{if(style.width!==width||style.height!==height)throw Error(style.width+' / '+style.height+' != '+width+' / '+height)};
 const base=style.height;
 check('10px',base);child.className='flag';check('20px',base);
 child.className='';check('10px',base);peer.className='peer';check('10px','30px');
 peer.remove();check('10px',base);document.body.appendChild(peer);check('10px','30px');
 input.checked=true;check('40px','30px');input.checked=false;check('10px','30px');
 child.className='flag';child.remove();check('10px','30px');host.appendChild(child);check('20px','30px');
 document.head.firstChild.sheet.cssRules[1].style.width='50px';check('50px','30px');
 peer.className='';check('50px',base);
 return true;
})()`)
		if err != nil || value != true {
			t.Fatalf("relational styles: %v %v", value, err)
		}
		state := p.Top.Realm.blitz
		if state == nil || state.fallback != "" || state.document.Owner == nil {
			t.Fatal("relational selectors did not use the native producer")
		}
		builds := state.document.Builds
		_, err = p.Evaluate(context.Background(), `getComputedStyle(document.querySelector('section')).width`)
		if err != nil || state.document.Builds != builds {
			t.Fatalf("clean observation rebuilt the relational projection: %v", err)
		}
	})
}

func TestBlitzRelationalSelectorsRetainUnsupportedPredicateFallback(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(context.Background(), `(() => {
 document.head.innerHTML='<style>section{width:10px}section:has(input:required){width:20px}</style>';
 document.body.innerHTML='<section><input required></section>';
 const section=document.querySelector('section'),input=section.firstChild,style=getComputedStyle(section);
 const first=style.width;input.removeAttribute('required');
 return first+','+style.width;
})()`)
		if err != nil || value != "20px,10px" {
			t.Fatalf("unsupported relational predicate: %v %v", value, err)
		}
		if p.Top.Realm.blitz == nil || p.Top.Realm.blitz.fallback == "" {
			t.Fatal("unsupported native predicate did not retain the semantic fallback")
		}
	})
}
