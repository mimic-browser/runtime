package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCSSBoxGraphMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	testCSSObservation(t, "css_box_geometry")
}

func TestCSSOpposingInsetsStretchAutoPositionedBoxes(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		reducedMotion := true
		page.SetMediaPreferences("", &reducedMotion)
		// The reduced-motion profile uses the supported geometry fallback.
		result, err := page.Evaluate(context.Background(), `(() => {
  document.body.style.margin = '0';
  document.body.innerHTML =
    '<div id="root" style="position:absolute;left:0;right:0">' +
      '<div id="row" style="display:flex"><div id="item" style="flex:1 1 0%">content</div></div>' +
    '</div>' +
    '<div id="container" style="position:relative;width:300px;padding:10px;border:5px solid">' +
      '<div id="nested" style="position:absolute;left:20px;right:30px"></div>' +
    '</div>' +
    '<div id="fixed" style="position:fixed;left:15px;right:25px"></div>';
  const width = (id) => document.getElementById(id).getBoundingClientRect().width;
  return JSON.stringify({
    root: width('root'),
    row: width('row'),
    item: width('item'),
    nested: width('nested'),
    containerClient: document.getElementById('container').clientWidth,
    fixed: width('fixed'),
    viewport: innerWidth,
  });
})()`)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Root            float64 `json:"root"`
			Row             float64 `json:"row"`
			Item            float64 `json:"item"`
			Nested          float64 `json:"nested"`
			ContainerClient float64 `json:"containerClient"`
			Fixed           float64 `json:"fixed"`
			Viewport        float64 `json:"viewport"`
		}
		if err := json.Unmarshal([]byte(result.(string)), &got); err != nil {
			t.Fatal(err)
		}
		if got.Root != got.Viewport || got.Row != got.Viewport || got.Item != got.Viewport || got.Nested != got.ContainerClient-50 || got.Fixed != got.Viewport-40 {
			t.Fatalf("opposing inset geometry: %+v", got)
		}
	})
}

func TestCSSFlexBasisZeroSharesSpaceIndependentOfContent(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		reducedMotion := true
		page.SetMediaPreferences("", &reducedMotion)
		result, err := page.Evaluate(context.Background(), `(() => {
  document.body.innerHTML =
    '<div style="display:flex;width:300px">' +
      '<div id="short" style="flex:1 1 0%;width:40px">a</div>' +
      '<div id="long" style="flex:1 1 0%;width:140px">long content</div>' +
      '<div id="zero" style="flex:0 0 0%;min-width:0;width:40px"></div>' +
    '</div>' +
    '<div style="display:flex;width:300px">' +
      '<div id="host" style="flex:1 1 0%;min-width:0"><div style="width:600px"></div></div>' +
      '<div style="width:72px;flex:none"></div>' +
    '</div>';
  return JSON.stringify([
    document.getElementById('short').getBoundingClientRect().width,
    document.getElementById('long').getBoundingClientRect().width,
    document.getElementById('zero').getBoundingClientRect().width,
    document.getElementById('host').getBoundingClientRect().width,
  ]);
})()`)
		if err != nil || result != `[150,150,0,228]` {
			t.Fatalf("zero flex basis: %v %v", result, err)
		}
	})
}

func TestCSSFallbackFlexWidthSurvivesFocusReflow(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		reducedMotion := true
		page.SetMediaPreferences("", &reducedMotion)
		result, err := page.Evaluate(context.Background(), `(() => {
  document.body.style.margin = '0';
  document.head.innerHTML = '<style>.box:focus-within { padding-left: 32px } #host:focus-within #popup { display: block }</style>';
  document.body.innerHTML =
    '<div style="display:flex;width:500px;align-items:center">' +
      '<div id="host" style="display:flex;flex:1 1 0%;min-width:0;position:relative">' +
        '<div style="width:100%"><div class="box" style="display:flex;align-items:center">' +
          '<form style="display:flex;flex:1 1 0%"><input id="field" style="min-width:0;flex:1 1 0%"></form>' +
        '</div></div>' +
        '<div id="popup" style="display:none;position:absolute;left:0;right:0;top:40px;height:200px"></div>' +
      '</div>' +
      '<div style="width:72px;flex:none"></div>' +
    '</div>';
  const host = document.getElementById('host');
  const field = document.getElementById('field');
  const read = () => [host.getBoundingClientRect().width, field.getBoundingClientRect().width];
  const before = read();
  field.focus();
  return JSON.stringify({ before, focused: read(), again: read() });
})()`)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Before  []float64 `json:"before"`
			Focused []float64 `json:"focused"`
			Again   []float64 `json:"again"`
		}
		if err := json.Unmarshal([]byte(result.(string)), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Before) != 2 || len(got.Focused) != 2 || len(got.Again) != 2 ||
			got.Before[0] != 428 || got.Focused[0] != 428 || got.Again[0] != 428 ||
			got.Before[1] <= 0 || got.Focused[1] <= 0 || got.Again[1] != got.Focused[1] {
			t.Fatalf("focus reflow width: %+v", got)
		}
	})
}

func TestCSSFlowRootFlexItemMeasuresNestedInlineContent(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';
document.body.innerHTML='<header style="display:flex;width:300px"><h1 id="heading" style="display:flow-root;margin:0;font-size:20px;line-height:40px"><span><span>Title</span></span></h1></header>';
const heading=document.getElementById('heading'),child=heading.firstElementChild,rect=heading.getBoundingClientRect(),childRect=child.getBoundingClientRect();
return JSON.stringify({height:rect.height,offsetHeight:heading.offsetHeight,childHeight:childRect.height,visible:rect.width>0&&rect.height>0});
})()`)
		const want = `{"height":40,"offsetHeight":40,"childHeight":40,"visible":true}`
		if err != nil || result != want {
			t.Fatalf("flow-root nested inline measurement: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSGridFlowPreservesMixedInlineContentHeight(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';
document.body.innerHTML='<main style="display:grid;width:180px"><section><p id="mixed" style="margin:0;font-size:16px;line-height:20px">alpha beta gamma <a href="#">linked words</a> delta epsilon zeta eta theta</p></section><section><h3 id="later" style="margin:0;line-height:30px">Later heading</h3></section></main>';
const mixed=document.getElementById('mixed').getBoundingClientRect(),later=document.getElementById('later').getBoundingClientRect();
return JSON.stringify({mixedHeight:mixed.height,laterTop:later.top,noOverlap:later.top>=mixed.bottom});
})()`)
		var got struct {
			MixedHeight float64 `json:"mixedHeight"`
			LaterTop    float64 `json:"laterTop"`
			NoOverlap   bool    `json:"noOverlap"`
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(result.(string)), &got); err != nil {
			t.Fatal(err)
		}
		if got.MixedHeight < 40 || !got.NoOverlap {
			t.Fatalf("mixed inline flow collapsed: %+v", got)
		}
	})
}

func TestCSSTaffyLeafDescendantUsesProjectedAncestorOffset(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';
document.body.innerHTML='<main style="display:grid;width:300px"><div style="height:100px"></div><table id="table"><tbody><tr><td id="cell" style="height:30px">target</td></tr></tbody></table></main>';
const table=document.getElementById('table').getBoundingClientRect(),cell=document.getElementById('cell').getBoundingClientRect();
return JSON.stringify({tableTop:table.top,cellTop:cell.top,inside:cell.top>=table.top&&cell.bottom<=table.bottom,hit:document.elementFromPoint(cell.x+2,cell.y+2)?.id});
})()`)
		var got struct {
			TableTop float64 `json:"tableTop"`
			CellTop  float64 `json:"cellTop"`
			Inside   bool    `json:"inside"`
			Hit      string  `json:"hit"`
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(result.(string)), &got); err != nil {
			t.Fatal(err)
		}
		if got.TableTop < 100 || got.CellTop < 100 || !got.Inside || got.Hit != "cell" {
			t.Fatalf("projected leaf descendant offset: %+v", got)
		}
	})
}

func TestCSSBoxStateIsCompleteDuringRecursiveComputedStyle(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
			document.head.innerHTML='<style>.submenuitem { font-size: 14px }</style>';
			document.body.innerHTML='<div style="display:none"><a class="submenuitem">Home</a></div>';
			const rect=document.querySelector('a').getBoundingClientRect();
			return rect.width===0&&rect.height===0;
		})()`)
		if err != nil || result != true {
			t.Fatalf("hidden descendant geometry: %v %v", result, err)
		}
	})
}

func TestCSSDefaultIframeReplacedGeometryAndHitTesting(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.innerHTML='<iframe id="frame"></iframe>';
const frame=document.getElementById('frame'),rect=frame.getBoundingClientRect(),style=getComputedStyle(frame);
return JSON.stringify({rect:[rect.x,rect.y,rect.width,rect.height],computed:[style.width,style.height,style.borderLeftWidth,style.borderLeftStyle],offset:[frame.offsetWidth,frame.offsetHeight],client:[frame.clientWidth,frame.clientHeight],hit:document.elementFromPoint(rect.x+10,rect.y+10)===frame});
})()`)
		const want = `{"rect":[8,8,304,154],"computed":["300px","150px","2px","inset"],"offset":[304,154],"client":[300,150],"hit":true}`
		if err != nil || result != want {
			t.Fatalf("default iframe replaced geometry: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSAncestorTransformMovesDescendantClientRect(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div id="parent" style="position:fixed;left:0;top:0;width:100px;height:100px;transform:translateX(-260px)"><div id="child" style="width:50px;height:40px"></div></div>';
const parent=document.getElementById('parent').getBoundingClientRect(),child=document.getElementById('child').getBoundingClientRect();
return JSON.stringify([parent.x,parent.y,child.x,child.y,child.width,child.height,document.elementFromPoint(10,10)?.id||''])
})()`)
		const want = `[-260,0,-260,0,50,40,""]`
		if err != nil || result != want {
			t.Fatalf("ancestor transform geometry: %#v want %#v, err=%v", result, want, err)
		}
	})
}

func TestCSSFixedInsetsAcceptTailwindCalcProducts(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.documentElement.style.setProperty('--spacing','.25rem');
document.body.style.margin='0';
document.body.innerHTML='<button id="target" style="position:fixed;right:calc(var(--spacing) * 6);bottom:calc(var(--spacing)*24);width:calc(50px*2);height:calc(40px/2)"></button>';
const target=document.getElementById('target'),rect=target.getBoundingClientRect(),style=getComputedStyle(target);
return JSON.stringify({rect:[rect.x,rect.y,rect.width,rect.height],insets:[style.right,style.bottom],hit:document.elementFromPoint(rect.x+50,rect.y+10)===target});
})()`)
		const want = `{"rect":[1148,537,100,20],"insets":["24px","96px"],"hit":true}`
		if err != nil || result != want {
			t.Fatalf("fixed Tailwind calc-product insets: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSFixedInsetsUseViewportWhenAncestorTransformComputesToNone(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.cssText='margin:0;height:2000px;transform:none';
document.body.innerHTML='<div id="target" style="position:fixed;right:24px;bottom:96px;width:100px;height:20px"></div>';
const rect=document.getElementById('target').getBoundingClientRect();return JSON.stringify([rect.x,rect.y,rect.width,rect.height]);
})()`)
		const want = `[1148,537,100,20]`
		if err != nil || result != want {
			t.Fatalf("fixed viewport containing block: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSTaffyCoordinatesSpanBlockAncestorsAndFixedOverlays(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.cssText='margin:0;display:flex;flex-direction:column';
document.body.innerHTML='<header style="height:64px"></header><main style="display:block"><article style="height:100px"></article><div style="display:grid"><button id="share" style="width:86px;height:19px"></button></div></main><aside style="position:fixed;right:24px;bottom:96px;width:280px;height:56px"><button id="chat" style="width:278px;height:22px"></button></aside>';
const share=document.getElementById('share'),chat=document.getElementById('chat'),sr=share.getBoundingClientRect(),cr=chat.getBoundingClientRect();
return JSON.stringify({share:[sr.x,sr.y],chat:[cr.x,cr.y],hit:document.elementFromPoint(sr.x+10,sr.y+10)===share});
})()`)
		// Frozen Chrome receipt: blitz-chrome152-handwritten-audit-2026-09-20.json.
		const want = `{"share":[0,164],"chat":[968,501],"hit":true}`
		if err != nil || result != want {
			t.Fatalf("Taffy ancestor coordinate projection: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSRowFlexItemsUseIntrinsicBasisAndShrink(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div id="row" style="display:flex;width:300px"><div class="item" style="display:inline-flex"><span style="display:block;width:200px;height:10px"></span></div><div class="item" style="display:inline-flex"><span style="display:block;width:200px;height:10px"></span></div><div class="item" style="display:inline-flex"><span style="display:block;width:200px;height:10px"></span></div></div>';
return JSON.stringify(Array.from(document.querySelectorAll('.item'),item=>{const r=item.getBoundingClientRect();return [r.x,r.width]}))
})()`)
		// Chrome's automatic minimum preserves each item's 200px content width.
		const want = `[[0,200],[200,200],[400,200]]`
		if err != nil || result != want {
			t.Fatalf("row flex sizing: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSTaffyNestedFlexBasisKeepsSearchControlVisible(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div style="display:flex;width:416px"><div style="width:40px"></div><div style="display:flex;flex:1 1 0%;min-width:0"><textarea id="q" name="q" style="display:flex;flex:1 1 100%;min-width:0"></textarea></div><div style="width:40px"></div></div>';
const q=document.getElementById('q'),r=q.getBoundingClientRect();return JSON.stringify([r.x,r.width,r.height,q.offsetWidth,q.clientWidth]);
})()`)
		// clientWidth excludes the two border pixels (verified in Chrome 152).
		const want = `[40,336,36,336,334]`
		if err != nil || result != want {
			t.Fatalf("nested flex-basis search control: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSTaffyGridTracksGapAndPlacement(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div id="grid" style="display:grid;width:300px;height:40px;grid-template-columns:repeat(2,50px) 1fr;column-gap:10px"><div id="a" style="width:50px;grid-column-start:1"></div><div id="b" style="width:240px;grid-column-start:2;grid-column-end:span 2"></div></div>';
return JSON.stringify(['grid','a','b'].map(id=>{const e=document.getElementById(id),r=e.getBoundingClientRect();return [r.x,r.width,e.offsetWidth,e.clientWidth]}));
})()`)
		const want = `[[0,300,300,300],[0,50,50,50],[60,240,240,240]]`
		if err != nil || result != want {
			t.Fatalf("grid tracks and placement: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSTaffyGeometryAPIsShareBorderAndOffsetBoxes(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='10px';document.body.innerHTML='<div id="root" style="display:flex;position:relative;box-sizing:border-box;width:200px;height:40px;border:4px solid"><div id="child" style="box-sizing:border-box;width:100px;height:20px;border:3px solid"></div></div>';
const root=document.getElementById('root'),child=document.getElementById('child'),r=child.getBoundingClientRect();return JSON.stringify({rect:[r.x,r.y,r.width,r.height],root:[root.offsetWidth,root.clientWidth],child:[child.offsetWidth,child.clientWidth,child.offsetLeft,child.offsetTop]});
})()`)
		const want = `{"rect":[14,14,100,20],"root":[200,192],"child":[100,94,0,0]}`
		if err != nil || result != want {
			t.Fatalf("Taffy geometry APIs: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSRowFlexAutoMarginConsumesRemainingSpace(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div style="display:flex;width:300px"><div id="start" style="display:inline-flex"><span style="display:block;width:100px;height:10px"></span></div><div id="end" style="display:inline-flex;margin-left:auto"><span style="display:block;width:40px;height:10px"></span></div></div>';
const start=document.getElementById('start').getBoundingClientRect(),end=document.getElementById('end').getBoundingClientRect();
return JSON.stringify([[start.x,start.width],[end.x,end.width]])
})()`)
		const want = `[[0,100],[260,40]]`
		if err != nil || result != want {
			t.Fatalf("row flex auto margin: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSNestedFlexIntrinsicBorderBox(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div style="display:flex;width:300px"><div id="group" style="display:inline-flex;margin-left:auto"><div id="a" style="display:flex;box-sizing:border-box;padding:0 calc(10px)"><span style="width:40px;height:10px;display:block"></span></div><div id="b" style="display:flex;box-sizing:border-box;padding:0 10px"><span style="width:60px;height:10px;display:block"></span></div></div></div>';
return JSON.stringify(['group','a','b'].map(id=>{const r=document.getElementById(id).getBoundingClientRect();return [r.x,r.width]}));
})()`)
		const want = `[[160,140],[160,60],[220,80]]`
		if err != nil || result != want {
			t.Fatalf("nested flex border box: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestRangeTextGeometryUsesElementLayout(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(()=>{
document.body.style.margin='0';document.body.innerHTML='<div id="label" style="display:block;width:120px;height:24px">Sign in with Steam</div>';
const text=document.getElementById('label').firstChild,range=document.createRange();range.selectNode(text);
const rect=range.getBoundingClientRect(),rects=range.getClientRects(),clone=range.cloneRange();
range.setStart(text,0);range.setEnd(text,text.textContent.length);
return JSON.stringify({rect:[rect.x,rect.y,rect.width,rect.height],rectType:rect instanceof DOMRect,rectsType:rects instanceof DOMRectList,rectsLength:rects.length,start:range.startContainer===text,end:range.endContainer===text,offsets:[range.startOffset,range.endOffset],collapsed:range.collapsed,ancestor:range.commonAncestorContainer===text,text:String(clone)});
})()`)
		const want = `{"rect":[0,0,120,24],"rectType":true,"rectsType":true,"rectsLength":1,"start":true,"end":true,"offsets":[0,18],"collapsed":false,"ancestor":true,"text":"Sign in with Steam"}`
		if err != nil || result != want {
			t.Fatalf("range text geometry: %v want %s, err=%v", result, want, err)
		}
	})
}

func TestCSSComputedCatalogMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	for _, name := range []string{"css_geometry_integration", "css_geometry_audit", "css_details_query", "css_foreign_owner", "css_shadow_inheritance", "css_line_rounding", "css_wrapper_flow", "css_specified_values", "css_zero_font", "css_computed_initial", "css_computed_catalog", "css_computed_dynamic"} {
		t.Run(name, func(t *testing.T) {
			parallelOracle(t)
			testCSSObservation(t, name)
		})
	}
}

func testCSSObservation(t *testing.T, name string) {
	source, err := os.ReadFile("testdata/" + name + "_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/" + name + "_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct{ Observation any }
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), "document.body.replaceChildren();JSON.stringify("+string(source)+")")
		if err != nil {
			t.Fatal(err)
		}
		var actualValue any
		if err := json.Unmarshal([]byte(result.(string)), &actualValue); err != nil {
			t.Fatal(err)
		}
		expectedFields, object := oracle.Observation.(map[string]any)
		if !object {
			if !reflect.DeepEqual(actualValue, oracle.Observation) {
				t.Errorf("got %v; want %v", actualValue, oracle.Observation)
			}
			return
		}
		actual, _ := actualValue.(map[string]any)
		for name, expected := range expectedFields {
			if !reflect.DeepEqual(actual[name], expected) {
				if want, ok := expected.(map[string]any); ok {
					got, _ := actual[name].(map[string]any)
					for key, value := range want {
						if !reflect.DeepEqual(got[key], value) {
							t.Errorf("%s.%s: got %v; want %v", name, key, got[key], value)
						}
					}
				} else {
					t.Errorf("%s: got %v; want %v", name, actual[name], expected)
				}
			}
		}
	})
}

func TestCSSForeignOwnerRestoredRealm(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	source, err := os.ReadFile("testdata/css_foreign_owner_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/css_foreign_owner_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct{ Observation any }
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	actual := bootstrapSnapshotEvaluate(t, p, string(source))
	if !reflect.DeepEqual(actual, oracle.Observation) {
		t.Fatalf("got %v; want %v", actual, oracle.Observation)
	}
	bootstrapSnapshotAssertRestored(t, p, 1)
	if p.crossRealmDepth != 0 {
		t.Fatalf("entry depth leaked: %d", p.crossRealmDepth)
	}
}

func TestCSSGeometryAuditRestoredRealms(t *testing.T) {
	parallelBrowserTest(t)
	for _, name := range []string{"css_geometry_integration", "css_geometry_audit", "css_details_query"} {
		t.Run(name, func(t *testing.T) {
			p := bootstrapSnapshotPage(t)
			bootstrapSnapshotWarm(t, p)
			source, err := os.ReadFile("testdata/" + name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile("testdata/" + name + "_chrome152.json")
			if err != nil {
				t.Fatal(err)
			}
			var oracle struct{ Observation any }
			if err = json.Unmarshal(raw, &oracle); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(string(source))
			// These receipts were captured on the standards-mode /plain fixture.
			// An initial about:blank iframe has no doctype and uses quirks mode,
			// whose percentage-height behavior must not be compared to that receipt.
			wrapper := `(() => {
  const frame = document.createElement('iframe');
  document.body.append(frame);
  try {
    const doc = frame.contentDocument;
    doc.open();
    doc.write('<!doctype html><html><head></head><body></body></html>');
    doc.close();
    if (doc.compatMode !== 'CSS1Compat') throw new Error('standards fixture required');
    return frame.contentWindow.eval(ORACLE_SOURCE);
  } finally {
    frame.remove();
  }
})()`
			actual := bootstrapSnapshotEvaluate(t, p, strings.Replace(wrapper, "ORACLE_SOURCE", string(encoded), 1))
			if !reflect.DeepEqual(actual, oracle.Observation) {
				t.Fatalf("got %v; want %v", actual, oracle.Observation)
			}
			bootstrapSnapshotAssertRestored(t, p, 1)
		})
	}
}

func TestCSSGeometryIntegrationQuirksMode(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/css_geometry_integration_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(string(source))
	historyTestPages(t, func(t *testing.T, p *Page) {
		// The unchanged integration probe also runs in an initial 300x150
		// about:blank iframe. Its percentage-height wrapper uses the viewport
		// in quirks mode, while its padding-ratio descendant remains 300px tall.
		wrapper := `(() => {
  const frame = document.createElement('iframe');
  frame.style.cssText = 'width:300px;height:150px;border:0';
  document.body.append(frame);
  try {
    if (frame.contentDocument.compatMode !== 'BackCompat')
      throw new Error('quirks fixture required');
    return JSON.stringify(frame.contentWindow.eval(ORACLE_SOURCE).staticPosition);
  } finally {
    frame.remove();
  }
})()`
		historyEval(t, p, strings.Replace(wrapper, "ORACLE_SOURCE", string(encoded), 1), `[[0,0,200,150],[0,0,200,150],[0,0,200,150],[0,0,200,300],[0,300,200,300]]`)
	})
}
