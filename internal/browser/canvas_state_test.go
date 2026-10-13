package browser

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

const canvasStateFixture = `(async()=>{
 const canvas=new OffscreenCanvas(3,2),ctx=canvas.getContext('2d');
 if(ctx!==canvas.getContext('2d')||ctx.canvas!==canvas||canvas.getContext('webgpu')!==null)return 'context identity';
 if(ctx.fillStyle!=='#000000')return 'default';ctx.fillStyle='red';ctx.save();ctx.fillStyle='blue';ctx.scale(2,3);ctx.restore();if(ctx.fillStyle!=='#ff0000'||ctx.getTransform().a!==1)return 'state restore';
 ctx.fillRect(0,0,2,1);if(Array.from(ctx.getImageData(0,0,3,1).data).join(',')!=='255,0,0,255,255,0,0,255,0,0,0,0')return 'fill pixels';
 ctx.clearRect(1,0,1,1);if(ctx.getImageData(1,0,1,1).data[3]!==0)return 'clear pixels';
 const bytes=new Uint8ClampedArray([1,2,3,4,5,6,7,8]),image=new ImageData(bytes,2);if(image.data!==bytes)return 'image identity';ctx.putImageData(image,0,1);if(Array.from(ctx.getImageData(0,1,2,1).data).join(',')!=='0,0,0,4,0,0,0,8')return 'premul';
 const snapshot=await createImageBitmap(canvas);if(snapshot.width!==3||snapshot.height!==2)return 'snapshot';snapshot.close();if(snapshot.width||snapshot.height)return 'close';
 ctx.fillStyle='blue';const bitmap=canvas.transferToImageBitmap();if(bitmap.width!==3||ctx.fillStyle!=='#0000ff'||ctx.getImageData(0,0,1,1).data[3]!==0)return 'transfer bitmap';
 ctx.scale(2,2);canvas.width=3;if(ctx!==canvas.getContext('2d')||ctx.fillStyle!=='#000000'||ctx.getTransform().a!==1)return 'resize';
 const other=new OffscreenCanvas(3,2);if(other.getContext('2d')===ctx)return 'isolation';
 return true;
})()`

func TestOffscreenCanvasSerializationSharesCanonicalPixelsAcrossRealms(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	const observation = `async()=>{
    const canvas=new OffscreenCanvas(3,2),ctx=canvas.getContext('2d');
    ctx.fillStyle='rgba(240,80,20,.75)';ctx.fillRect(0,0,2,2);
    ctx.putImageData(new ImageData(new Uint8ClampedArray([5,10,20,128]),1,1),2,1);
    const before=Array.from(ctx.getImageData(0,0,3,2).data),blob=await canvas.convertToBlob(),bytes=new Uint8Array(await blob.arrayBuffer());
    const bitmap=await createImageBitmap(canvas),copy=new OffscreenCanvas(3,2),copyContext=copy.getContext('2d');
    copyContext.drawImage(bitmap,0,0);
    const copied=Array.from(copyContext.getImageData(0,0,3,2).data);
    ctx.reset();
    return {before,copied,reset:Array.from(ctx.getImageData(0,0,3,2).data),type:blob.type,size:blob.size,signature:Array.from(bytes.slice(0,8))};
  }`
	value, err := p.Evaluate(context.Background(), `(async()=>JSON.stringify(await (`+observation+`)()))()`)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.Evaluate(context.Background(), `(async()=>new Promise((resolve,reject)=>{
    const source='onmessage=async()=>postMessage(JSON.stringify(await ('+`+strconv.Quote(observation)+`+')()))';
    const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'})),w=new Worker(url);
    w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.onerror=reject;w.postMessage(null);
  }))()`)
	if err != nil {
		t.Fatal(err)
	}
	if value != worker {
		t.Fatalf("Window and Worker canvas serialization diverged:\nwindow=%v\nworker=%v", value, worker)
	}
	encoded := value.(string)
	if !strings.Contains(encoded, `"type":"image/png"`) || !strings.Contains(encoded, `"signature":[137,80,78,71,13,10,26,10]`) {
		t.Fatalf("unexpected canonical PNG observation: %s", encoded)
	}
	if !strings.Contains(encoded, `"reset":[0,0,0,0`) {
		t.Fatalf("reset did not clear the authoritative surface: %s", encoded)
	}
}

func TestGenericFontUpdateInvalidatesWindowAndExistingWorkerCanvasMetrics(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	const start = `(()=>{
    globalThis.fontWorker=new Worker(URL.createObjectURL(new Blob([
      "onmessage=()=>{const x=new OffscreenCanvas(100,20).getContext('2d');x.font='16px serif';postMessage(x.measureText('iiiiWWWW').width)}"
    ],{type:'text/javascript'})));
    globalThis.workerMeasure=()=>new Promise((resolve,reject)=>{fontWorker.onmessage=e=>resolve(e.data);fontWorker.onerror=reject;fontWorker.postMessage(null)});
    globalThis.mainMeasure=()=>{const x=new OffscreenCanvas(100,20).getContext('2d');x.font='16px serif';return x.measureText('iiiiWWWW').width};
    return Promise.all([mainMeasure(),workerMeasure()]);
  })()`
	beforeValue, err := p.Evaluate(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	before := beforeValue.([]any)
	if before[0] != before[1] {
		t.Fatalf("initial Window/Worker font profile differs: %v", before)
	}
	if err := p.SetGenericFontFamilies("Courier New", "", ""); err != nil {
		t.Fatal(err)
	}
	afterValue, err := p.Evaluate(context.Background(), `Promise.all([mainMeasure(),workerMeasure()])`)
	if err != nil {
		t.Fatal(err)
	}
	after := afterValue.([]any)
	if after[0] != after[1] {
		t.Fatalf("updated Window/Worker font profile differs: %v", after)
	}
	if after[0] == before[0] {
		t.Fatalf("cached generic-family metrics survived profile update: before=%v after=%v", before, after)
	}
}

func TestCanvasStrokeCapsUseSnapshottedGeometryAcrossRealms(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	const observation = `()=>{
    const draw=cap=>{const c=new OffscreenCanvas(20,20),x=c.getContext('2d');x.lineWidth=6;x.lineCap=cap;x.beginPath();x.moveTo(5,10);x.lineTo(15,10);x.stroke();x.lineCap=cap==='butt'?'square':'butt';const data=Array.from(x.getImageData(0,0,20,20).data),alpha=data.filter((_,i)=>i%4===3),a=(px,py)=>data[(py*20+px)*4+3];return [a(3,9),a(2,7),alpha.filter(Boolean).length,alpha.reduce((sum,v)=>sum+v,0)]};
    return {butt:draw('butt'),round:draw('round'),square:draw('square')};
  }`
	main, err := p.Evaluate(context.Background(), `JSON.stringify((`+observation+`)())`)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.Evaluate(context.Background(), `(async()=>new Promise((resolve,reject)=>{
    const source='onmessage=()=>postMessage(JSON.stringify(('+`+strconv.Quote(observation)+`+')()))';
    const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'})),w=new Worker(url);
    w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.onerror=reject;w.postMessage(null);
  }))()`)
	if err != nil {
		t.Fatal(err)
	}
	if main != worker {
		t.Fatalf("Window and Worker stroke caps differ: main=%v worker=%v", main, worker)
	}
	var got struct {
		Butt, Round, Square []float64
	}
	if err := json.Unmarshal([]byte(main.(string)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Butt[0] != 0 || got.Round[0] == 0 || got.Square[0] == 0 {
		t.Fatalf("endpoint cap geometry differs from Chrome semantics: %+v", got)
	}
	if got.Square[1] <= got.Round[1] {
		t.Fatalf("round/square corner geometry collapsed: %+v", got)
	}
	if !(got.Butt[3] < got.Round[3] && got.Round[3] < got.Square[3]) {
		t.Fatalf("stroke cap coverage ordering is incoherent: %+v", got)
	}
}

func TestCanvasFullTurnArcsInBothDirections(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const tau=2*Math.PI;
  for(const method of ['arc','ellipse'])for(const delta of [-2*tau,-tau,tau,2*tau])for(const ccw of [false,true]){
    const canvas=new OffscreenCanvas(49,44),ctx=canvas.getContext('2d',{willReadFrequently:true});
    ctx.scale(0.4,0.4);ctx.fillStyle='#ff22ff';ctx.beginPath();
    if(method==='arc')ctx.arc(40,40,40,0,delta,ccw);
    else ctx.ellipse(40,40,40,40,0,0,delta,ccw);
    ctx.fill();
    if(!ctx.getImageData(0,0,49,44).data.some(v=>v))return method+':'+delta+':'+ccw;
  }
  return 'ok';
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("full-turn canvas arcs: %v %v", value, err)
	}
}

func TestCanvasFrequentReadArcCoverageMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const ctx=new OffscreenCanvas(2,2).getContext('2d',{willReadFrequently:true});
  ctx.fillStyle='black';ctx.fillRect(0,0,2,2);
  ctx.fillStyle='white';ctx.fillRect(2,2,1,1);
  ctx.beginPath();ctx.arc(0,0,2,0,1,true);ctx.fill();
  return Array.from(ctx.getImageData(0,0,2,2).data).join(',');
})()`)
	const want = "255,255,255,255,192,192,192,255,244,244,244,255,53,53,53,255"
	if err != nil || value != want {
		t.Fatalf("frozen Chrome 152 frequent-read curved coverage: %v %v", value, err)
	}
}

func TestCanvasShadowsMaterializeForPathsAndText(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const canvas=new OffscreenCanvas(40,24),ctx=canvas.getContext('2d');
  const alpha=(x,y)=>ctx.getImageData(x,y,1,1).data[3];
  ctx.fillStyle='red';ctx.shadowColor='blue';ctx.shadowBlur=0;ctx.shadowOffsetX=8;
  ctx.fillRect(2,2,3,3);
  if(alpha(10,3)===0||alpha(20,3)!==0)return 'path offset';
  ctx.clearRect(0,0,40,24);
  ctx.shadowOffsetX=0;ctx.shadowBlur=8;ctx.fillRect(18,8,2,2);
  if(alpha(15,8)===0||alpha(18,8)===0)return 'path blur';
  ctx.clearRect(0,0,40,24);
  ctx.font='12px sans-serif';ctx.shadowBlur=6;ctx.fillText('A',18,12);
  if(alpha(14,8)===0)return 'text blur';
  ctx.clearRect(0,0,40,24);
  if(ctx.getImageData(0,0,40,24).data.some(v=>v))return 'clear';
  canvas.width=40;
  if(ctx.getImageData(0,0,40,24).data.some(v=>v))return 'reset';
  return 'ok';
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("canvas shadows: %v %v", value, err)
	}
}

func TestCanvasShadowBlurUsesFrozenChromeFloat32State(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const ctx=new OffscreenCanvas(20,20).getContext('2d');
  return [9.79908737641225,0.1,1e40,1e100,Infinity,NaN,-1,0].map(input=>{
    ctx.shadowBlur=input;
    return String(ctx.shadowBlur);
  }).join(',');
})()`)
	const want = "9.799087524414062,0.10000000149011612,3.4028234663852886e+38,3.4028234663852886e+38,3.4028234663852886e+38,3.4028234663852886e+38,3.4028234663852886e+38,0"
	if err != nil || value != want {
		t.Fatalf("shadowBlur must retain Chrome 152's float32/clamped values: %v %v", value, err)
	}
}

func TestCanvasFrequentReadShadowMaskQuantizesBeforeColor(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const ctx=new OffscreenCanvas(20,20).getContext('2d',{willReadFrequently:true});
  ctx.shadowColor='#809980';
  ctx.shadowBlur=9.79908737641225;
  ctx.fillRect(10,10,1,1);
  return Array.from(ctx.getImageData(7,3,1,1).data).join(',');
})()`)
	if err != nil || value != "255,255,255,1" {
		t.Fatalf("Chrome 152 frequent-read shadow A8 mask before source color: %v %v", value, err)
	}
}

func TestCanvasFrequentReadHairlineMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const ctx=new OffscreenCanvas(20,20).getContext('2d',{willReadFrequently:true});
  ctx.beginPath();
  ctx.moveTo(3.2,4.7);
  ctx.lineTo(15.8,11.3);
  ctx.stroke();
  const actual=ctx.getImageData(0,0,20,20).data;
  const alpha={83:134,84:31,103:72,104:224,105:153,106:19,125:102,126:236,127:141,128:7,
    147:114,148:248,149:128,169:127,170:250,171:116,190:5,191:139,192:238,193:104,
    212:17,213:151,214:226,215:72,234:29,235:130};
  for(let i=0;i<400;i++) {
    const want=alpha[i]||0;
    if(actual[i*4]||actual[i*4+1]||actual[i*4+2]||actual[i*4+3]!==want)
      return i+':'+Array.from(actual.slice(i*4,i*4+4)).join(',')+'!='+want;
  }
  return 'exact';
})()`)
	if err != nil || value != "exact" {
		t.Fatalf("frozen Chrome 152 frequent-read hairline pixels: %v %v", value, err)
	}
}

func TestCanvasDrawImageBlendAndGrayscale(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const source=new OffscreenCanvas(2,2),s=source.getContext('2d');
  s.fillStyle='#ff0000';s.fillRect(0,0,2,2);
  const canvas=new OffscreenCanvas(2,2),ctx=canvas.getContext('2d');
  ctx.drawImage(source,0,0);
  ctx.globalCompositeOperation='difference';ctx.drawImage(source,0,0);
  const same=ctx.getImageData(0,0,2,2).data;
  if(same[0]||same[1]||same[2]||same[3]!==255)return 'difference';
  ctx.globalCompositeOperation='copy';ctx.filter='grayscale(100%)';ctx.drawImage(source,0,0);
  const gray=ctx.getImageData(0,0,1,1).data;
  if(Math.abs(gray[0]-54)>1||gray[0]!==gray[1]||gray[1]!==gray[2]||gray[3]!==255)return 'grayscale';
  return 'ok';
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("drawImage blend/filter: %v %v", value, err)
	}
}

func TestCanvasZeroImageDataDimensionMessage(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const ctx=document.createElement('canvas').getContext('2d');const messages=[];for(const size of [[0,1],[1,0]]){try{ctx.getImageData(0,0,...size)}catch(e){messages.push([e.name,e.message])}}return JSON.stringify(messages)})()`)
	if err != nil || value != `[["IndexSizeError","Failed to execute 'getImageData' on 'CanvasRenderingContext2D': The source width is 0."],["IndexSizeError","Failed to execute 'getImageData' on 'CanvasRenderingContext2D': The source height is 0."]]` {
		t.Fatalf("zero ImageData dimensions: %v %v", value, err)
	}
}

func TestCanvasConicGradientChromeQuadrants(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const c=document.createElement('canvas');c.width=c.height=9;const x=c.getContext('2d');const g=x.createConicGradient(0,4.5,4.5);g.addColorStop(0,'red');g.addColorStop(.25,'lime');g.addColorStop(.5,'blue');g.addColorStop(.75,'white');g.addColorStop(1,'red');x.fillStyle=g;x.fillRect(0,0,9,9);return JSON.stringify({length:x.createConicGradient.length,colors:[[4,1],[7,4],[4,7],[1,4]].map(([a,b])=>Array.from(x.getImageData(a,b,1,1).data))})})()`)
	if err != nil || value != `{"length":3,"colors":[[255,255,255,255],[255,0,0,255],[0,255,0,255],[0,0,255,255]]}` {
		t.Fatalf("conic gradient: %v %v", value, err)
	}
}

func TestCanvasConicGradientRejectsNonFiniteArguments(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const x=document.createElement('canvas').getContext('2d');try{x.createConicGradient(Infinity,0,0)}catch(e){return JSON.stringify([e.name,e.message])}})()`)
	if err != nil || value != `["TypeError","Failed to execute 'createConicGradient' on 'CanvasRenderingContext2D': The provided double value is non-finite."]` {
		t.Fatalf("nonfinite conic gradient: %v %v", value, err)
	}
}

func TestCanvasLanguageStateChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const c=document.createElement('canvas'),x=c.getContext('2d');const initial=x.lang;c.setAttribute('lang','de');const fromCanvas=x.lang;x.lang='es';x.save();x.lang='fr';const saved=x.lang;x.restore();const restored=x.lang;x.reset();return JSON.stringify([initial,fromCanvas,saved,restored,x.lang,c.getAttribute('lang')])})()`)
	if err != nil || value != `["inherit","inherit","fr","es","inherit","de"]` {
		t.Fatalf("canvas language state: %v %v", value, err)
	}
}

func TestCanvasPatternSnapshotsAndRepeatsChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const source=document.createElement('canvas');source.width=2;source.height=1;const a=source.getContext('2d');a.fillStyle='red';a.fillRect(0,0,1,1);a.fillStyle='blue';a.fillRect(1,0,1,1);const target=document.createElement('canvas');target.width=4;target.height=2;const x=target.getContext('2d');const pattern=x.createPattern(source,'repeat-x');a.fillStyle='lime';a.fillRect(0,0,1,1);x.fillStyle=pattern;x.fillRect(0,0,4,2);return JSON.stringify({length:x.createPattern.length,tag:Object.prototype.toString.call(pattern),colors:[[0,0],[1,0],[2,0],[3,0],[0,1]].map(([px,py])=>Array.from(x.getImageData(px,py,1,1).data))})})()`)
	if err != nil || value != `{"length":2,"tag":"[object CanvasPattern]","colors":[[255,0,0,255],[0,0,255,255],[255,0,0,255],[0,0,255,255],[0,0,0,0]]}` {
		t.Fatalf("canvas pattern: %v %v", value, err)
	}
}

func TestCanvasPatternCombinesTransformsChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const source=document.createElement('canvas');source.width=2;source.height=1;const a=source.getContext('2d');a.fillStyle='red';a.fillRect(0,0,1,1);a.fillStyle='blue';a.fillRect(1,0,1,1);const target=document.createElement('canvas');target.width=5;target.height=1;const x=target.getContext('2d'),pattern=x.createPattern(source,'repeat');pattern.setTransform(new DOMMatrix().translate(1,0));x.translate(1,0);x.fillStyle=pattern;x.fillRect(0,0,4,1);return JSON.stringify([0,1,2,3,4].map(i=>Array.from(x.getImageData(i,0,1,1).data).slice(0,3)))})()`)
	if err != nil || value != `[[0,0,0],[0,0,255],[255,0,0],[0,0,255],[255,0,0]]` {
		t.Fatalf("canvas pattern transform: %v %v", value, err)
	}
}

func TestCanvasFocusRingForFocusedFallbackElement(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const c=document.createElement('canvas');c.width=c.height=20;const button=document.createElement('button');c.append(button);document.body.append(c);const x=c.getContext('2d');x.beginPath();x.rect(5,5,10,10);x.drawFocusIfNeeded(button);const before=x.getImageData(3,8,1,1).data[3];button.focus();x.drawFocusIfNeeded(button);const after=x.getImageData(3,8,1,1).data[3],center=x.getImageData(8,8,1,1).data[3];return JSON.stringify([before,after>0,center,x.drawFocusIfNeeded.length])})()`)
	if err != nil || value != `[0,true,0,1]` {
		t.Fatalf("canvas focus ring: %v %v", value, err)
	}
}

func TestCanvasStateWindowAndWorker(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		for _, worker := range []bool{false, true} {
			script := canvasStateFixture
			if worker {
				code := "onmessage=async()=>{try{postMessage(await " + script + ")}catch(e){postMessage(String(e))}}"
				script = `new Promise((resolve,reject)=>{const url=URL.createObjectURL(new Blob([` + strconv.Quote(code) + `]));const w=new Worker(url);w.onerror=e=>reject(Error(e.message));w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.postMessage(null)})`
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			value, err := p.Evaluate(ctx, script)
			cancel()
			if err != nil || value != true {
				t.Fatalf("worker=%v: %v %v", worker, value, err)
			}
		}
	})
}

func TestHTMLCanvasUsesCanonicalContextState(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(()=>{const c=document.createElement('canvas');if(c.width!==300||c.height!==150)return 'dimensions';const ctx=c.getContext('2d');c.width=2;c.height=2;ctx.fillStyle='red';ctx.fillRect(0,0,1,1);if(ctx.getImageData(0,0,1,1).data[0]!==255)return 'pixels';c.width=2;return ctx===c.getContext('2d')&&ctx.fillStyle==='#000000'&&ctx.getImageData(0,0,1,1).data[3]===0})()`)
		if err != nil || value != true {
			t.Fatalf("HTML canvas: %v %v", value, err)
		}
	})
}

func TestCanvasDeferredDrawsStayBoundedAndOrdered(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(()=>{
 const make=()=>{const c=new OffscreenCanvas(4,4),x=c.getContext('2d');x.fillStyle='rgba(20,40,60,.05)';return x},batched=make(),checkpointed=make();
 for(let i=0;i<600;i++){batched.fillRect(i%4,(i>>2)%4,1,1);checkpointed.fillRect(i%4,(i>>2)%4,1,1);if(i%100===99)checkpointed.getImageData(0,0,1,1)}
 const read=x=>Array.from(x.getImageData(0,0,4,4).data).join(',');if(read(batched)!==read(checkpointed))return 'batch folding';
 batched.clearRect(-1,-1,6,6);if(batched.getImageData(0,0,4,4).data.some(v=>v))return 'full clear';
 batched.fillStyle='red';batched.fillRect(0,0,4,4);batched.save();batched.translate(2,0);batched.clearRect(0,0,4,4);batched.restore();
 const pixels=batched.getImageData(0,0,4,1).data;return pixels[0]===255&&pixels[3]===255&&pixels[8]===0&&pixels[11]===0;
})()`)
		if err != nil || value != true {
			t.Fatalf("deferred canvas draws: %v %v", value, err)
		}
	})
}

func TestCanvasTextObservationsAreLocalAndOrdered(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		value, err := p.Evaluate(context.Background(), `(async()=>{
 const c=new OffscreenCanvas(80,24),x=c.getContext('2d');x.font='10px sans-serif';
 const run=text=>{x.clearRect(0,0,80,24);x.fillText(text,2,12);return Array.from(x.getImageData(0,0,80,24).data)};
 const a=run('AAAA'),b=run('AAAB'),again=run('AAAA');if(a.join(',')!==again.join(','))return 'repeatability';let changes=0;const prefix=x.measureText('AAA').width,last=Math.max(x.measureText('A').width,x.measureText('B').width);
 for(let i=0;i<a.length;i++){if(a[i]===b[i])continue;changes++;const px=Math.floor(i/4)%80;if(px<Math.floor(2+prefix)-1||px>Math.ceil(2+prefix+last)+1)return 'nonlocal change'}if(!changes)return 'character ignored';
 const crop=x.getImageData(5,4,20,10).data;for(let y=0;y<10;y++)for(let xx=0;xx<20;xx++)for(let k=0;k<4;k++)if(crop[(y*20+xx)*4+k]!==again[((y+4)*80+xx+5)*4+k])return 'overlap';
 const snap=await createImageBitmap(c),copy=new OffscreenCanvas(80,24),cx=copy.getContext('2d');cx.drawImage(snap,0,0);if(Array.from(cx.getImageData(0,0,80,24).data).join(',')!==again.join(','))return 'snapshot';
 const cropped=await createImageBitmap(c,5,4,20,10),cc=new OffscreenCanvas(20,10),ccx=cc.getContext('2d');if(cropped.width!==20||cropped.height!==10)return 'crop dimensions';ccx.drawImage(cropped,0,0);if(Array.from(ccx.getImageData(0,0,20,10).data).join(',')!==Array.from(crop).join(','))return 'crop snapshot';
 x.clearRect(0,0,80,24);x.fillText('A',2,12);x.clearRect(0,0,80,24);if(x.getImageData(0,0,80,24).data.some(v=>v))return 'clear ordering';
 x.fillText('A',2,12);x.putImageData(new ImageData(80,24),0,0);if(x.getImageData(0,0,80,24).data.some(v=>v))return 'put ordering';
 const width=x.measureText('AAAA').width;if(width!==x.measureText('AAAB').width||width<=0)return 'metrics';
 for(const text of ['', '   ']){const m=x.measureText(text);if(m.actualBoundingBoxLeft||m.actualBoundingBoxRight||m.actualBoundingBoxAscent||m.actualBoundingBoxDescent)return 'empty ink'}
 x.letterSpacing='2px';if(x.measureText('AAAA').width!==width+8)return 'spacing';x.font='invalid';if(x.font!=='10px sans-serif')return 'font validation';
 c.width=80;if(x.getImageData(0,0,80,24).data.some(v=>v))return 'reset';
 const opaque=new OffscreenCanvas(2,2).getContext('2d',{alpha:false});if(opaque.getImageData(0,0,1,1).data[3]!==255)return 'opaque init';opaque.clearRect(0,0,2,2);if(opaque.getImageData(0,0,1,1).data[3]!==255)return 'opaque clear';
 if(x instanceof CanvasRenderingContext2D||!(x instanceof OffscreenCanvasRenderingContext2D))return 'prototype';
 if(Function.prototype.toString.call(OffscreenCanvas.prototype.getContext)!=='function getContext() { [native code] }')return 'native source';
 return true;
 })()`)
		if err != nil || value != true {
			t.Fatalf("text observations: %v %v", value, err)
		}
	})
}
