package browser

import (
	"context"
	"testing"
)

func TestRegisteredPropertiesResolveOrdinaryComputedValues(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `(()=>{
 document.head.innerHTML='<style>@property --theme{syntax:"<color>";inherits:false;initial-value:rgb(5,5,5)} @property --length{syntax:"<length>";inherits:false;initial-value:12px} #probe{color:var(--theme);width:var(--length);opacity:var(--alpha,.5)}</style>';
 document.body.innerHTML='<div id="probe"></div>';const e=document.getElementById('probe');const s=getComputedStyle(e);const read=()=>[s.color,s.width,s.opacity].join(',');const rows=[read()];e.style.setProperty('--theme','rgb(1,2,3)');e.style.setProperty('--length','20px');rows.push(read());e.style.setProperty('--theme','banana');e.style.setProperty('--length','banana');rows.push(read());return rows.join('|');
 })()`, `rgb(5, 5, 5),12px,0.5|rgb(1, 2, 3),20px,0.5|rgb(5, 5, 5),12px,0.5`)
	})
}

func TestRegisteredPropertiesShareDocumentAcrossWorlds(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		ctx := context.Background()
		world, err := p.IsolatedWorld(ctx, p.Top.ID, "property-owner")
		if err != nil {
			t.Fatal(err)
		}
		d := NewDebugger(p)
		defer d.Close()
		result, err := d.Evaluate(ctx, p.Top.ID, world, `CSS.registerProperty({name:'--shared',syntax:'<length>',inherits:false,initialValue:'17px'});getComputedStyle(document.body).getPropertyValue('--shared')`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != "17px" {
			t.Fatalf("isolated registration: %#v %v", result, err)
		}
		historyEval(t, p, `(()=>{let error;try{CSS.registerProperty({name:'--shared',syntax:'<length>',inherits:false,initialValue:'1px'})}catch(e){error=e.name}return getComputedStyle(document.body).getPropertyValue('--shared')+'|'+error})()`, `17px|InvalidModificationError`)
	})
}
