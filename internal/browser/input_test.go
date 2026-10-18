package browser

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestProtocolKeyboardAndTextMatchChrome152(t *testing.T) {
	parallelBrowserTest(t)
	raw, err := os.ReadFile("testdata/cdp_input_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Setup     string
		Scenarios []struct {
			Name     string
			Setup    string
			Commands []struct {
				Method string
				Params map[string]any
			}
			Result any
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		for _, scenario := range fixture.Scenarios {
			t.Run(scenario.Name, func(t *testing.T) {
				ctx := context.Background()
				if _, err := page.Evaluate(ctx, fixture.Setup+scenario.Setup); err != nil {
					t.Fatal(err)
				}
				for _, command := range scenario.Commands {
					var err error
					if command.Method == "Runtime.evaluate" {
						_, err = page.Evaluate(ctx, command.Params["expression"].(string))
					} else {
						err = page.DispatchProtocolInput(ctx, command.Method, command.Params)
					}
					if err != nil {
						t.Fatalf("%s: %v", command.Method, err)
					}
				}
				actual, err := page.Evaluate(ctx, `JSON.stringify({events:inputEvents,value:entry.value,start:entry.selectionStart,end:entry.selectionEnd,active:document.activeElement.id})`)
				if err != nil {
					t.Fatal(err)
				}
				var decoded any
				if err := json.Unmarshal([]byte(actual.(string)), &decoded); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decoded, scenario.Result) {
					expected, _ := json.Marshal(scenario.Result)
					t.Fatalf("input differs from Chrome\nactual: %s\nexpected: %s", actual, expected)
				}
			})
		}
	})
}

func TestDefaultControlGeometryMatchesChrome152(t *testing.T) {
	parallelBrowserTest(t)
	raw, err := os.ReadFile("testdata/cdp_input_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		GeometrySetup string
		Geometry      []struct {
			ID            string
			Width, Height float64
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		actual, err := page.Evaluate(context.Background(), fixture.GeometrySetup)
		if err != nil {
			t.Fatal(err)
		}
		var rows []struct {
			ID            string
			Width, Height float64
		}
		if err := json.Unmarshal([]byte(actual.(string)), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(fixture.Geometry) {
			t.Fatalf("missing controls: %d", len(rows))
		}
		for i, row := range rows {
			expected := fixture.Geometry[i]
			if row.ID != expected.ID || math.Abs(row.Width-expected.Width) > 1.0/64 || row.Height != expected.Height {
				t.Errorf("%s box %gx%g; Chrome %gx%g", row.ID, row.Width, row.Height, expected.Width, expected.Height)
			}
		}
		value, err := page.Evaluate(context.Background(), `(()=>{const e=document.querySelector('#go'),list=e.getClientRects();return list instanceof DOMRectList&&list.length===1&&Array.from(list)[0]===list.item(0)&&list.item(1)===null&&list[0].width===e.getBoundingClientRect().width})()`)
		if err != nil || value != true {
			t.Fatalf("DOMRectList geometry projection: %v %v", value, err)
		}
	})
}

func TestInputInternalsDoNotReenterAuthorSelectors(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		_, err := page.Evaluate(ctx, `document.body.innerHTML='<select id="choice"><option>alpha</option><option>beta</option></select><input id="next">';
		globalThis.selectorCalls=[];
		for(const prototype of [Document.prototype,Element.prototype,DocumentFragment.prototype])for(const name of ['querySelector','querySelectorAll']){
		  const original=prototype[name];prototype[name]=function(...args){selectorCalls.push(name);return Reflect.apply(original,this,args)};
		}
		globalThis.choice=document.getElementById('choice');choice.getBoundingClientRect();choice.getClientRects();choice.focus();`)
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range []struct {
			method string
			params map[string]any
		}{
			{"Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Tab"}},
			{"Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 10, "y": 10}},
		} {
			if err := page.DispatchProtocolInput(ctx, command.method, command.params); err != nil {
				t.Fatal(err)
			}
		}
		actual, err := page.Evaluate(ctx, `JSON.stringify({calls:selectorCalls,active:document.activeElement.id,options:choice.options.length})`)
		if err != nil || actual != `{"calls":[],"active":"next","options":2}` {
			t.Fatalf("internal selector traversal: %v %v", actual, err)
		}
	})
}

func TestProtocolMouseMoveCoalescedSampleMatchesChrome(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `globalThis.inputMoves=[]; document.addEventListener('pointermove', event => { const samples=event.getCoalescedEvents(); inputMoves.push({trusted:event.isTrusted, same:samples[0]===event, samples:samples.map(sample=>[sample.x,sample.y,sample.isTrusted,sample.timeStamp===event.timeStamp])}); });`); err != nil {
			t.Fatal(err)
		}
		if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 37.5, "y": 48.25}); err != nil {
			t.Fatal(err)
		}
		value, err := page.Evaluate(ctx, `JSON.stringify(inputMoves)`)
		if err != nil || value != `[{"trusted":true,"same":false,"samples":[[37.5,48.25,true,true]]}]` {
			t.Fatalf("coalesced pointer sample: %v %v", value, err)
		}
		value, err = page.Evaluate(ctx, `JSON.stringify([new PointerEvent('pointermove').getCoalescedEvents().length, typeof PointerEvent.prototype.getPredictedEvents, isSecureContext])`)
		if err != nil || value != `[0,"function",true]` {
			t.Fatalf("PointerEvent methods in secure context: %v %v", value, err)
		}
	})
}

func TestProtocolPointerBoundaryEventsFollowElementAncestry(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		setup := `document.body.innerHTML='<div id="parent" style="position:absolute;left:10px;top:10px;width:200px;height:100px"><div id="left" style="position:absolute;left:0;top:0;width:80px;height:80px"></div><div id="right" style="position:absolute;left:100px;top:0;width:80px;height:80px"></div></div>';
		globalThis.boundaryEvents=[];
		for(const id of ['parent','left','right']){
		  const element=document.getElementById(id);
		  for(const type of ['pointerenter','mouseenter','pointerleave','mouseleave'])
		    element.addEventListener(type,event=>boundaryEvents.push([type,id,event.relatedTarget?.id||null,event.bubbles,event.isTrusted]));
		}
		JSON.stringify([document.elementFromPoint(30,30)?.id,document.elementFromPoint(130,30)?.id])`
		value, err := page.Evaluate(ctx, setup)
		if err != nil || value != `["left","right"]` {
			t.Fatalf("pointer fixture hit targets: %v %v", value, err)
		}
		for _, point := range [][2]int{{30, 30}, {130, 30}} {
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{
				"type": "mouseMoved", "x": point[0], "y": point[1],
			}); err != nil {
				t.Fatal(err)
			}
		}
		value, err = page.Evaluate(ctx, `JSON.stringify(boundaryEvents)`)
		want := `[["pointerenter","parent",null,false,true],["mouseenter","parent",null,false,true],["pointerenter","left",null,false,true],["mouseenter","left",null,false,true],["pointerleave","left","right",false,true],["mouseleave","left","right",false,true],["pointerenter","right","left",false,true],["mouseenter","right","left",false,true]]`
		if err != nil || value != want {
			t.Fatalf("pointer boundary transitions: %v %v", value, err)
		}
	})
}

func TestMouseEventCoordinatesMatchChromeIntegerBoundary(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		value, err := page.Evaluate(ctx, `JSON.stringify([new MouseEvent('x',{screenX:37.9,screenY:-1.9,clientX:37.9,clientY:-1.9,button:1.9,buttons:3.9,movementX:2.9,movementY:-2.9}),new PointerEvent('x',{screenX:37.9,screenY:-1.9,clientX:37.9,clientY:-1.9,button:1.9,buttons:3.9,movementX:2.9,movementY:-2.9})].map(e=>[e.screenX,e.screenY,e.clientX,e.clientY,e.button,e.buttons,e.movementX,e.movementY]))`)
		if err != nil || value != `[[37,-2,37,-2,1,3,2,-2],[37.9,-1.9,37.9,-1.9,1,3,2,-2]]` {
			t.Fatalf("constructed mouse and pointer coordinates: %v %v", value, err)
		}
		if _, err := page.Evaluate(ctx, `globalThis.nativeCoordinates=[]; for(const type of ['pointermove','mousemove']) document.addEventListener(type,event=>nativeCoordinates.push([type,event.clientX,event.clientY,event.isTrusted]));`); err != nil {
			t.Fatal(err)
		}
		if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 37.9, "y": 48.1}); err != nil {
			t.Fatal(err)
		}
		value, err = page.Evaluate(ctx, `JSON.stringify(nativeCoordinates)`)
		if err != nil || value != `[["pointermove",37.9,48.1,true],["mousemove",37,48,true]]` {
			t.Fatalf("trusted mouse and pointer coordinates: %v %v", value, err)
		}
	})
}

func TestProtocolInputSharesFocusAndFormStateAcrossWorlds(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `document.body.innerHTML='<input id="text"><input id="other">';globalThis.mainEvents=[];document.addEventListener('input',e=>mainEvents.push([e.target===document.querySelector('#text'),e.isTrusted,e.target.value]));`); err != nil {
			t.Fatal(err)
		}
		world, err := page.isolatedWorld(ctx, page.Top.Realm, "input-test")
		if err != nil {
			t.Fatal(err)
		}
		d := NewDebugger(page)
		defer d.Close()
		result, err := d.Evaluate(ctx, "", world.ID, `globalThis.isolatedEvents=[];let field=document.querySelector('#text');field.value='abc';field.focus();field.setSelectionRange(1,2);document.addEventListener('input',e=>isolatedEvents.push([e.target===field,e.isTrusted,e.target.value]));document.activeElement===field`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != true {
			t.Fatalf("isolated form/focus setup: %#v %v", result, err)
		}
		if err := page.DispatchProtocolInput(ctx, "Input.insertText", map[string]any{"text": "X"}); err != nil {
			t.Fatal(err)
		}
		value, err := page.Evaluate(ctx, `JSON.stringify({value:document.querySelector('#text').value,focused:document.activeElement.id,events:mainEvents})`)
		if err != nil || value != `{"value":"aXc","focused":"text","events":[[true,true,"aXc"]]}` {
			t.Fatalf("main world did not observe canonical input: %v %v", value, err)
		}
		result, err = d.Evaluate(ctx, "", world.ID, `JSON.stringify({value:field.value,events:isolatedEvents})`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != `{"value":"aXc","events":[[true,true,"aXc"]]}` {
			t.Fatalf("isolated world did not observe canonical input: %#v %v", result, err)
		}
	})
}

func TestProtocolSelectAndContentClickAcrossWorlds(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `document.body.innerHTML='<select id="choice"><option value="a">A</option><option value="b">B</option></select><button id="button" type="button" onclick="window.clicks=(window.clicks||0)+1">Go</button>';globalThis.changes=[];document.addEventListener('change',e=>changes.push([e.target.id,e.target.value,e.isTrusted]));`); err != nil {
			t.Fatal(err)
		}
		world, err := page.isolatedWorld(ctx, page.Top.Realm, "form-test")
		if err != nil {
			t.Fatal(err)
		}
		d := NewDebugger(page)
		defer d.Close()
		result, err := d.Evaluate(ctx, "", world.ID, `(()=>{const select=document.querySelector('#choice');select.value=undefined;for(const option of select.options){option.selected=option.value==='b';if(option.selected&&!select.multiple)break}select.dispatchEvent(new Event('change',{bubbles:true}));document.querySelector('#button').click();return JSON.stringify({value:select.value,selected:Array.from(select.selectedOptions,option=>option.value)})})()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != `{"value":"b","selected":["b"]}` {
			t.Fatalf("isolated selection: %#v %v", result, err)
		}
		value, err := page.Evaluate(ctx, `JSON.stringify({value:document.querySelector('#choice').value,clicks:window.clicks,handler:typeof document.querySelector('#button').onclick,changes})`)
		if err != nil || value != `{"value":"b","clicks":1,"handler":"function","changes":[["choice","b",false]]}` {
			t.Fatalf("main canonical selection/content handler: %v %v", value, err)
		}
		point, err := page.Evaluate(ctx, `(()=>{const r=document.querySelector('#button').getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})()`)
		if err != nil {
			t.Fatal(err)
		}
		coordinates := point.([]any)
		for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": coordinates[0], "y": coordinates[1], "button": "left", "clickCount": float64(1)}); err != nil {
				t.Fatal(err)
			}
		}
		value, err = page.Evaluate(ctx, `window.clicks`)
		if err != nil || numberValue(value) != 2 {
			t.Fatalf("trusted pointer did not use same content handler: %v %v", value, err)
		}
	})
}

func TestProtocolMouseReleaseAfterInterveningLayoutPatch(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `document.body.innerHTML='<div id="spacer" style="height:40px"></div><button id="one">One</button><button id="two">Two</button>';window.clicks=[];for(const button of document.querySelectorAll('button'))button.addEventListener('click',()=>clicks.push(button.id));`); err != nil {
			t.Fatal(err)
		}
		point, err := page.Evaluate(ctx, `(()=>{const r=document.querySelector('#one').getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})()`)
		if err != nil {
			t.Fatal(err)
		}
		coordinates := point.([]any)
		for _, kind := range []string{"mouseMoved", "mousePressed"} {
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": coordinates[0], "y": coordinates[1], "button": "left", "clickCount": 1}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := page.Evaluate(ctx, `document.querySelector('#spacer').remove()`); err != nil {
			t.Fatal(err)
		}
		if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": coordinates[0], "y": coordinates[1], "button": "left", "clickCount": 1}); err != nil {
			t.Fatal(err)
		}
		value, err := page.Evaluate(ctx, `JSON.stringify(clicks)`)
		if err != nil || value != `["one"]` {
			t.Fatalf("unmoved pointer lost pressed control after a later layout patch: %v %v", value, err)
		}
	})
}

func TestProtocolMouseClickNavigatesThroughAnchorDescendant(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			_, _ = w.Write([]byte(`<!doctype html><title>next</title><body>arrived</body>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><a href="/next" style="display:block;width:120px;height:40px"><span style="display:block;width:120px;height:40px">continue</span></a>`))
	}))
	defer server.Close()

	page := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := page.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 60, "y": 20, "button": "left", "clickCount": 1}); err != nil {
			t.Fatal(err)
		}
	}
	for page.URL() != server.URL+"/next" || !page.LoadEventEnded() {
		realm := page.Top.Realm
		if err := realm.scheduler.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		if err := realm.RunReady(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if page.URL() != server.URL+"/next" {
		t.Fatalf("anchor descendant click did not navigate: %s", page.URL())
	}
}

func TestProtocolPointerFocusDoesNotScrollMovedTarget(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `
document.body.style.margin='0';
document.body.innerHTML='<button id="target" style="position:absolute;top:10px">target</button><div style="height:3000px"></div>';
document.getElementById('target').addEventListener('mousedown',event=>event.currentTarget.style.top='1500px');
`); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"mouseMoved", "mousePressed"} {
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 5, "y": 15, "button": "left", "buttons": 1, "clickCount": 1}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := page.Evaluate(ctx, `JSON.stringify({scrollY,active:document.activeElement.id,top:document.getElementById('target').getBoundingClientRect().top})`)
		if err != nil || got != `{"scrollY":0,"active":"target","top":1500}` {
			t.Fatalf("trusted pointer focus scrolled moved target: %v err=%v", got, err)
		}
	})
}

func TestProtocolMouseClickCannotBypassOverlayWithHitHint(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		before := page.URL()
		if _, err := page.Evaluate(ctx, `document.body.innerHTML='<a id="target" href="#arrived" style="display:block;width:100px;height:40px">go</a><div style="position:absolute;left:0;top:0;width:200px;height:100px;z-index:2"></div>'`); err != nil {
			t.Fatal(err)
		}
		document, ok := page.Document()
		target, ok := document.Find("#target")
		if !ok {
			t.Fatal("missing target node")
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			params := map[string]any{"type": kind, "x": 20, "y": 20, "button": "left", "clickCount": 1, "_mimicNodeId": target.ID, "_mimicLocalX": 20, "_mimicLocalY": 20}
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", params); err != nil {
				t.Fatal(err)
			}
		}
		if page.URL() != before {
			t.Fatalf("non-protocol hint bypassed overlay: %s", page.URL())
		}
	})
}

func TestProtocolMouseInputRoutesThroughIframeCoordinates(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		value, err := page.Evaluate(ctx, `(()=>{
			document.body.style.margin='0';
			const frame=document.createElement('iframe');
			frame.style.cssText='display:block;margin-left:40px;margin-top:30px';
			document.body.append(frame);
			frame.contentDocument.body.innerHTML='<button style="margin-left:10px;margin-top:5px;width:80px;height:30px" onclick="window.clicks=(window.clicks||0)+1">Go</button>';
			const outer=frame.getBoundingClientRect(),inner=frame.contentDocument.querySelector('button').getBoundingClientRect();
			const x=outer.x+2+inner.x+inner.width/2,y=outer.y+2+inner.y+inner.height/2;
			if(outer.width!==304||outer.height!==154||document.elementFromPoint(x,y)!==frame)throw Error('iframe actionability geometry');
			return [x,y];
		})()`)
		if err != nil {
			t.Fatal(err)
		}
		coordinates := value.([]any)
		d := NewDebugger(page)
		defer d.Close()
		parentWorld, err := page.IsolatedWorld(ctx, page.Top.ID, "iframe-actionability-parent")
		if err != nil {
			t.Fatal(err)
		}
		parentHit, err := d.Evaluate(ctx, page.Top.ID, parentWorld, `(()=>{const frame=document.querySelector('iframe'),r=frame.getBoundingClientRect();return document.elementFromPoint(r.x+10,r.y+10)===frame&&document.elementsFromPoint(r.x+10,r.y+10)[0]===frame})()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || parentHit["exceptionDetails"] != nil || parentHit["result"].(map[string]any)["value"] != true {
			t.Fatalf("isolated parent iframe hit test: %#v %v", parentHit, err)
		}
		for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
			if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": coordinates[0], "y": coordinates[1], "button": "left", "clickCount": float64(1)}); err != nil {
				t.Fatal(err)
			}
		}
		clicked, err := page.Evaluate(ctx, `document.querySelector('iframe').contentWindow.clicks`)
		if err != nil || numberValue(clicked) != 1 {
			t.Fatalf("iframe button was not clicked: %v %v", clicked, err)
		}
	})
}

func TestProtocolMouseInputHonorsFramePaintOrder(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		ctx := context.Background()
		if _, err := page.Evaluate(ctx, `
document.body.style.margin = '0';
globalThis.framesForInput = [];
for (let i = 0; i < 2; i++) {
  const frame = document.createElement('iframe');
  frame.style.cssText = 'position:absolute;left:40px;top:30px;width:100px;height:100px;border:0';
  document.body.append(frame);
  frame.contentDocument.body.style.margin = '0';
  frame.contentDocument.body.innerHTML = '<button style="width:100px;height:100px" onclick="window.clicks=(window.clicks||0)+1">go</button>';
  framesForInput.push(frame);
}
globalThis.cover = document.createElement('label');
cover.style.cssText = 'position:absolute;left:40px;top:30px;width:100px;height:100px;z-index:2;display:none';
cover.onclick = () => globalThis.coverClicks = (globalThis.coverClicks || 0) + 1;
document.body.append(cover);
true;
`); err != nil {
			t.Fatal(err)
		}
		for step, mutation := range []string{
			`true`,
			`framesForInput[1].style.pointerEvents = 'none'`,
			`cover.style.display = 'block'`,
			`cover.style.pointerEvents = 'none'; framesForInput[1].style.pointerEvents = 'auto'; framesForInput[1].style.visibility = 'hidden'`,
		} {
			if _, err := page.Evaluate(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"mousePressed", "mouseReleased"} {
				if err := page.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 70, "y": 60, "button": "left", "clickCount": 1}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := page.Evaluate(ctx, `JSON.stringify([framesForInput[0].contentWindow.clicks||0,framesForInput[1].contentWindow.clicks||0,globalThis.coverClicks||0])`)
			want := []string{`[0,1,0]`, `[1,1,0]`, `[1,1,1]`, `[2,1,1]`}[step]
			if err != nil || got != want {
				t.Fatalf("step %d: frame paint order: %v %v, want %s", step, got, err, want)
			}
		}
	})
}

func TestProtocolInputSuppressionSurvivesNavigation(t *testing.T) {
	parallelBrowserTest(t)
	page := testPage(t)
	ctx := context.Background()
	navigateCapabilityFixture(t, page)
	if err := page.DispatchProtocolInput(ctx, "Input.setIgnoreInputEvents", map[string]any{"ignore": true}); err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, page)
	if _, err := page.Evaluate(ctx, `document.body.innerHTML='<input>';document.querySelector('input').focus()`); err != nil {
		t.Fatal(err)
	}
	if err := page.DispatchProtocolInput(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "text": "a"}); err != nil {
		t.Fatal(err)
	}
	value, err := page.Evaluate(ctx, `document.querySelector('input').value===''&&!navigator.userActivation.isActive`)
	if err != nil || value != true {
		t.Fatalf("suppression was lost or granted activation: %v %v", value, err)
	}
	if err := page.DispatchProtocolInput(ctx, "Input.setIgnoreInputEvents", map[string]any{"ignore": false}); err != nil {
		t.Fatal(err)
	}
	if err := page.DispatchProtocolInput(ctx, "Input.insertText", map[string]any{"text": "b"}); err != nil {
		t.Fatal(err)
	}
	value, err = page.Evaluate(ctx, `document.querySelector('input').value`)
	if err != nil || value != "b" {
		t.Fatalf("reenabled input: %v %v", value, err)
	}
}
