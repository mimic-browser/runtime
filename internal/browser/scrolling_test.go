package browser

import (
	"context"
	"encoding/json"
	"fmt"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const scrollingFixture = `(() => {
 const eq=(a,b,label)=>{if(JSON.stringify(a)!==JSON.stringify(b))throw new Error(label+': '+JSON.stringify(a)+' != '+JSON.stringify(b))};
 document.head.innerHTML='<style>*{scrollbar-width:none}html,body{margin:0;padding:0}#space{height:2000px}#fixed{position:fixed;top:10px;left:10px;width:20px;height:20px}#scroller{width:200px;height:100px;overflow:auto}#wide{display:flow-root;width:500px;height:400px}#button{display:block;width:80px;height:30px;margin-top:250px}</style>';
 document.body.innerHTML='<div id="scroller"><div id="wide"><button id="button">Click</button></div></div><div id="space"></div><div id="fixed"></div>';
 const s=document.getElementById('scroller'),b=document.getElementById('button'),f=document.getElementById('fixed');
 eq(document.scrollingElement===document[document.compatMode==='BackCompat'?'body':'documentElement'],true,'root');
 eq([s.scrollWidth,s.scrollHeight],[500,400],'overflow extent');
 const before=b.getBoundingClientRect().top,offset=b.offsetTop;
 s.scrollTop=200;eq([s.scrollTop,b.getBoundingClientRect().top-before,b.offsetTop],[200,-200,offset],'nested scroll');
 s.scrollTo({left:50});eq([s.scrollLeft,s.scrollTop],[50,200],'omitted top');
 s.scrollBy(10,20);eq([s.scrollLeft,s.scrollTop],[60,220],'relative');
 s.scrollTop=9999;eq(s.scrollTop,s.scrollHeight-s.clientHeight,'clamp');
 s.scrollTop=-99;eq(s.scrollTop,0,'negative');
 s.scrollLeft=0;b.scrollIntoView({block:'center',inline:'nearest',behavior:'instant'});
 eq(b.getBoundingClientRect().top-s.getBoundingClientRect().top,(s.clientHeight-b.getBoundingClientRect().height)/2,'center nested');
 window.scrollTo(0,400);eq([scrollY,pageYOffset,document.scrollingElement.scrollTop,visualViewport.pageTop],[400,400,400,400],'root aliases');
 eq(f.getBoundingClientRect().top,10,'fixed');eq(s.getBoundingClientRect().top,-400,'viewport projection');
 window.scrollBy({top:25});eq(scrollY,425,'window relative');
 document.scrollingElement.scrollTop=100;eq(scrollY,100,'root setter');
 scrollTo(0,0);s.scrollTop=0;s.scrollLeft=0;
 eq(document.elementFromPoint(10,260)===b,false,'clipped hit');
 b.scrollIntoView({block:'center',behavior:'instant'});
 const r=b.getBoundingClientRect();eq(document.elementFromPoint(r.x+10,r.y+10)===b,true,'scrolled hit');
 s.style.overflow='clip';s.scrollTop=99;eq(s.scrollTop,0,'clip not scrollable');
 document.body.style.height='10px';document.body.innerHTML='';scrollTo(0,999);eq(scrollY,0,'shrink');
 return 'ok';
})()`

const scrollingAsyncFixture = `(async()=>{
 const assert=(ok,label)=>{if(!ok)throw Error(label)};
 document.body.innerHTML='<div style="height:1600px"></div><button id="end">end</button>';
 const end=document.getElementById('end');scrollTo(0,0);end.focus({preventScroll:true});assert(scrollY===0,'preventScroll');end.blur();end.focus();assert(scrollY>0,'focus scroll');
 scrollTo({top:0,behavior:'instant'});
 const events=[];document.addEventListener('scroll',()=>events.push('scroll'));document.addEventListener('scrollend',()=>events.push('scrollend'));
 scrollTo({top:300,behavior:'smooth'});assert(scrollY===0,'smooth is asynchronous');
 await new Promise(resolve=>setTimeout(resolve,800));assert(scrollY===300,'smooth endpoint');assert(events.includes('scroll')&&events.at(-1)==='scrollend','smooth events');
 scrollTo({top:600,behavior:'smooth'});await new Promise(resolve=>setTimeout(resolve,60));scrollTo({top:20,behavior:'instant'});await new Promise(resolve=>setTimeout(resolve,600));assert(scrollY<100,'instant cancels smooth: '+scrollY);scrollTo({top:20,behavior:'instant'});
 const f=document.createElement('iframe');f.srcdoc='<!doctype html><style>body{margin:0}div{height:1200px}</style><div></div><button>frame</button>';await new Promise(resolve=>{f.onload=resolve;document.body.prepend(f)});scrollTo({top:20,behavior:'instant'});
 const w=f.contentWindow,button=w.document.querySelector('button');w.scrollTo(0,100);assert(w.scrollY===100&&window.scrollY===20,'frame isolation: '+w.scrollY+','+window.scrollY);
 const get=Object.getOwnPropertyDescriptor(Element.prototype,'scrollTop').get;assert(get.call(w.document.documentElement)===100,'borrowed scroll getter');
 Element.prototype.scrollTo.call(w.document.documentElement,0,150);assert(w.scrollY===150,'borrowed scroll setter');
 f.remove();return 'ok';
})()`

const scrollingContainersFixture = `(()=>{
 const eq=(a,b,label)=>{if(Math.abs(a-b)>.01)throw Error(label+': '+a+' != '+b)};
 document.head.innerHTML='<style>*{scrollbar-width:none}html,body{margin:0;padding:0}body{height:2500px}#s{width:200px;height:100px;overflow:auto}#content{width:500px;height:400px}</style>';
 document.body.innerHTML='<div id="s" dir="rtl"><div id="content"></div></div>';
 const s=document.getElementById('s'),c=document.getElementById('content');
 eq(s.scrollWidth,500,'RTL extent');const left=c.getBoundingClientRect().left;s.scrollLeft=-50;eq(s.scrollLeft,-50,'negative RTL offset');eq(c.getBoundingClientRect().left-left,50,'RTL projection');s.scrollLeft=50;eq(s.scrollLeft,0,'RTL clamp');
 s.removeAttribute('dir');s.style.direction='ltr';s.innerHTML='<div style="height:400px"><div id="sticky" style="position:sticky;top:0;height:20px">sticky</div></div>';
 s.scrollTop=50;eq(document.getElementById('sticky').getBoundingClientRect().top,s.getBoundingClientRect().top,'sticky top');
 s.style.transform='translateX(0px)';s.innerHTML='<div style="height:400px"></div><div id="fixed" style="position:fixed;top:5px;width:10px;height:10px"></div>';s.scrollTop=50;eq(document.getElementById('fixed').getBoundingClientRect().top,s.getBoundingClientRect().top-45,'transformed fixed container');
 s.style.transform='none';s.innerHTML='<div><div style="height:250px"></div><button id="nested" style="height:30px">nested</button><div style="height:200px"></div></div>';s.scrollTop=0;window.scrollTo(0,300);
 document.getElementById('nested').scrollIntoView({block:'center',container:'nearest',behavior:'instant'});eq(scrollY,300,'nearest preserves outer scroll');eq(s.scrollTop,215,'nearest finds scrolling ancestor');
 return 'ok';
})()`

func TestScrollingContainers(t *testing.T) {
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
	result, err := p.Evaluate(context.Background(), scrollingContainersFixture)
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatal(result)
	}
}

func TestFocusAfterScrollRangeShrinks(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		result, err := page.Evaluate(context.Background(), `(() => {
  document.body.style.margin = '0';
  document.body.innerHTML = '<div id="scroller" style="height:100px;width:200px;overflow:auto"><div id="content" style="height:1000px"><input id="field"></div></div>';
  const scroller = document.getElementById('scroller');
  const content = document.getElementById('content');
  const field = document.getElementById('field');
  scroller.scrollTop = 800;
  content.style.height = '50px';
  field.focus();
  const rect = field.getBoundingClientRect();
  return document.activeElement === field && scroller.scrollTop === 0 && Number.isFinite(rect.top);
})()`)
		if err != nil || result != true {
			t.Fatalf("focus after range shrink: %v %v", result, err)
		}
	})
}

func TestScrollingAsync(t *testing.T) {
	serialBrowserTest(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := p.Evaluate(ctx, scrollingAsyncFixture)
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatal(result)
	}
}

func TestScrollingWheelInput(t *testing.T) {
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
	ctx := context.Background()
	_, err = p.Evaluate(ctx, `document.body.innerHTML='<div style="height:3000px"></div>';globalThis.wheelLog=[];addEventListener('wheel',e=>wheelLog.push([e.deltaY,e.isTrusted]))`)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"type": "mouseWheel", "x": 50, "y": 50, "deltaX": 0, "deltaY": 120}
	if err = p.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", params); err != nil {
		t.Fatal(err)
	}
	result, err := p.Evaluate(ctx, `JSON.stringify([scrollY,wheelLog])`)
	if err != nil {
		t.Fatal(err)
	}
	if result != `[120,[[120,true]]]` {
		t.Fatal(result)
	}
	_, err = p.Evaluate(ctx, `addEventListener('wheel',e=>e.preventDefault(),{passive:false})`)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.DispatchProtocolInput(ctx, "Input.dispatchMouseEvent", params); err != nil {
		t.Fatal(err)
	}
	result, err = p.Evaluate(ctx, `scrollY`)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "120" {
		t.Fatal(result)
	}
}

func TestPropagatedBodyOverflowScrollsViewport(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><body></body>`)
	}))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx := context.Background()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		value, err := p.Evaluate(ctx, `(() => {
  document.documentElement.style.overflow = 'visible';
  document.body.style.cssText = 'margin:0;overflow-x:hidden';
  document.body.innerHTML = '<div style="height:1800px"></div><a id="target" style="display:block;width:80px;height:30px">target</a>';
  return JSON.stringify([document.compatMode, document.scrollingElement.tagName, getComputedStyle(document.body).overflowX]);
})()`)
		if err != nil || value != `["CSS1Compat","HTML","hidden"]` {
			t.Fatalf("fixture: %v %v", value, err)
		}
		id := p.Top.Realm.document.ElementByID(p.Top.Realm.document.Root().ID, "target")
		if id == 0 {
			t.Fatal("target missing")
		}
		if err := p.ScrollNodeIntoView(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		value, err = p.Evaluate(ctx, `(() => {
  const target=document.getElementById('target'),r=target.getBoundingClientRect();
  return JSON.stringify({window:scrollY,body:document.body.scrollTop,html:document.documentElement.scrollTop,
    top:r.top,bottom:r.bottom,height:innerHeight,hit:document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)===target});
})()`)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Window, Body, HTML, Top, Bottom, Height float64
			Hit                                     bool
		}
		if err := json.Unmarshal([]byte(value.(string)), &result); err != nil {
			t.Fatal(err)
		}
		if result.Window <= 0 || result.Body != 0 || result.HTML != result.Window || result.Top < 0 || result.Bottom > result.Height || !result.Hit {
			t.Fatalf("propagated BODY overflow: %+v", result)
		}
	})
}

func TestScrollingGeometry(t *testing.T) {
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
	result, err := p.Evaluate(context.Background(), `try {`+scrollingFixture+`} catch(e) {throw new Error(e.stack)}`)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "ok" {
		t.Fatal(result)
	}
}

func TestVisibleFocusAndNoOpScrollSkipDocumentExtentWalk(t *testing.T) {
	serialBrowserTest(t)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := p.Evaluate(ctx, `(()=>{
 const input=document.createElement('input');input.id='target';document.body.append(input);
 const fragment=document.createDocumentFragment();
 for(let i=0;i<12000;i++){const node=document.createElement('span');node.textContent='content '+i;fragment.append(node)}
 document.body.append(fragment);input.focus();scroll(0,0);
 return document.activeElement===input&&scrollY===0;
})()`)
	if err != nil || value != true {
		t.Fatalf("visible focus: %v %v", value, err)
	}
	id := p.Top.Realm.document.ElementByID(p.Top.Realm.document.Root().ID, "target")
	if id == 0 {
		t.Fatal("target missing")
	}
	if err := p.ScrollNodeIntoView(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	value, err = p.Evaluate(ctx, `scrollY`)
	if err != nil || fmt.Sprint(value) != "0" {
		t.Fatalf("visible protocol scroll changed viewport: %v %v", value, err)
	}
}

func TestScrollingEvents(t *testing.T) {
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
	ctx := context.Background()
	_, err = p.Evaluate(ctx, `document.body.style.height='3000px';globalThis.scrollLog=[];document.addEventListener('scroll',e=>scrollLog.push([e.type,e.isTrusted,e.target===document,scrollY]));scrollTo(0,100);scrollTo(0,200);if(scrollLog.length)throw Error('synchronous scroll event')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.AdvanceTime(ctx, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `JSON.stringify(scrollLog)`)
	if err != nil {
		t.Fatal(err)
	}
	if value != `[["scroll",true,true,200]]` {
		t.Fatal(value)
	}
}
