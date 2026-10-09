package browser

import (
	"context"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestComputedStyleDoesNotInvokeAuthorGeometryGetters(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const element=document.createElement('div');document.body.appendChild(element);
 element.style.cssText='opacity:0.5;width:20px;height:30px';
 for(const name of ['offsetWidth','offsetHeight'])Object.defineProperty(element,name,{get(){throw new Error('author geometry getter')}});
 const style=getComputedStyle(element),checks=[style.opacity==='0.5',style.width==='20px',style.height==='30px',style.length>0,typeof style.item(0)==='string'];
 element.style.opacity='0.75';element.style.width='40px';checks.push(style.opacity==='0.75',style.width==='40px');
 element.style.removeProperty('width');element.style.removeProperty('height');
 checks.push(typeof style.width==='string',typeof style.height==='string',style.getPropertyPriority('width')==='');
 return checks.every(Boolean)?'ok':JSON.stringify(checks);
 })()`)
	if err != nil || value != "ok" {
		t.Fatalf("computed style: %v %v", value, err)
	}
}

func TestComputedStyleEnumerationTracksMutations(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const element=document.createElement('div');document.body.appendChild(element);
 const style=getComputedStyle(element);
 const keys=Reflect.ownKeys(style);
 if(!keys.includes('epubCaptionSide')||Object.getOwnPropertyDescriptor(style,'epubCaptionSide')!==undefined)return 'legacy keys';
 element.style.color='rgb(1, 2, 3)';
 const first=JSON.parse(JSON.stringify(style));
 element.style.color='rgb(4, 5, 6)';
 const second=JSON.parse(JSON.stringify(style));
 return first.color==='rgb(1, 2, 3)'&&second.color==='rgb(4, 5, 6)'?'ok':JSON.stringify([first.color,second.color]);
 })()`)
	if err != nil || value != "ok" {
		t.Fatalf("computed style enumeration: %v %v", value, err)
	}
}

func TestScalarComputedValuesPreserveInheritanceAndMutation(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const root=document.createElement('div'),middle=document.createElement('div'),leaf=document.createElement('div');
 root.style.cssText='color:rgb(1, 2, 3);cursor:crosshair;visibility:hidden;direction:rtl;pointer-events:none;white-space-collapse:preserve;text-wrap-mode:nowrap;line-height:1.5;caret-color:auto';
 middle.style.cssText='cursor:inherit;visibility:unset;direction:inherit;pointer-events:inherit;white-space-collapse:inherit;text-wrap-mode:inherit;line-height:inherit;caret-color:inherit';
 middle.append(leaf);root.append(middle);document.body.append(root);
 const read=()=>{const s=getComputedStyle(leaf);return [s.cursor,s.visibility,s.direction,s.pointerEvents,s.whiteSpaceCollapse,s.textWrapMode,s.lineHeight,s.caretColor]};
 const before=read();root.style.cssText='color:rgb(4, 5, 6);cursor:wait;visibility:visible;direction:ltr;pointer-events:auto;white-space-collapse:collapse;text-wrap-mode:wrap;line-height:2;caret-color:currentcolor';
 const after=read();root.remove();return JSON.stringify([before,after]);
 })()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[["crosshair","hidden","rtl","none","preserve","nowrap","24px","rgb(1, 2, 3)"],["wait","visible","ltr","auto","collapse","wrap","32px","rgb(4, 5, 6)"]]`
	if value != want {
		t.Fatalf("computed scalar inheritance: %v; want %s", value, want)
	}
}

func TestScalarComputedValuesLeaveFallbackGeometryOrderIndependent(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		reduced := true
		p.SetMediaPreferences("", &reduced)
		value, err := p.Evaluate(context.Background(), `(() => {
  const read = (scalarsFirst) => {
    document.body.innerHTML = '<div style="width:40px;height:20px"></div>';
    const element = document.body.firstChild;
    const style = getComputedStyle(element);
    const scalars = () => [style.backgroundImage, style.position, style.zIndex, style.borderTopStyle, style.overflowX, style.boxSizing];
    const box = () => {
      const rect = element.getBoundingClientRect();
      return [rect.width, rect.height, style.width, style.height];
    };
    const first = scalarsFirst ? [scalars(), box()] : [null, box()];
    if (!scalarsFirst) first[0] = scalars();
    element.style.cssText = 'width:80px;height:30px;position:relative;z-index:7;overflow-x:hidden;box-sizing:border-box';
    return [first, [scalars(), box()]];
  };
  const before = read(true), after = read(false);
  return JSON.stringify({same:JSON.stringify(before) === JSON.stringify(after),before});
})()`)
		want := `{"same":true,"before":[[["none","static","auto","none","visible","content-box"],[40,20,"40px","20px"]],[["none","relative","7","none","hidden","border-box"],[80,30,"80px","30px"]]]}`
		if err != nil || value != want {
			t.Fatalf("scalar/geometry observation ordering: %v %v", value, err)
		}
	})
}

func TestTaffyRootMemoPreservesNegativeCustomAndWidthBoundaries(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const plain=document.createElement('section');plain.style.cssText='width:123px;height:17px';
 const custom=document.createElement('x-layout');custom.style.cssText='display:flex;width:180px;height:19px';
 const customChild=document.createElement('span');customChild.style.cssText='display:block;width:31px;height:7px';custom.append(customChild);
 const outer=document.createElement('div');outer.style.cssText='display:flex;width:400px';
 const boundary=document.createElement('div');boundary.style.cssText='display:flex;width:200px';
 const leaf=document.createElement('div');leaf.style.cssText='width:50px;height:11px';boundary.append(leaf);outer.append(boundary);
 document.body.append(plain,custom,outer);
 const read=e=>{const r=e.getBoundingClientRect();return [r.width,r.height,r.x,r.y]};
 const first=[read(plain),read(plain),read(custom),read(custom),read(customChild),read(leaf),read(boundary)];
 const second=[read(plain),read(plain),read(custom),read(custom),read(customChild),read(leaf),read(boundary)];
 return JSON.stringify({same:JSON.stringify(first)===JSON.stringify(second),first});
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"same":true,"first":[[123,17,8,8],[123,17,8,8],[180,19,0,0],[180,19,0,0],[31,7,0,0],[50,11,0,0],[200,11,0,0]]}`
	if value != want {
		t.Fatalf("taffy root memo boundaries: %v; want %s", value, want)
	}
}
