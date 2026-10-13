package browser

import (
	"context"
	"strconv"
	"testing"
)

func TestCreateImageBitmapSourcesAndOptionsMatchFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	const observation = `async()=>{
  const rgba=new Uint8ClampedArray([
    255,0,0,255, 0,255,0,192, 0,0,255,128, 255,255,255,64,
    255,255,0,255, 0,255,255,192, 255,0,255,128, 0,0,0,64,
    10,30,50,255, 80,100,120,192, 150,170,190,128, 220,240,250,64
  ]),data=new ImageData(rgba,4,3),optionData=new ImageData(rgba.slice(),4,3),read=bitmap=>{
    const c=new OffscreenCanvas(bitmap.width,bitmap.height),x=c.getContext('2d',{willReadFrequently:true});x.drawImage(bitmap,0,0);
    return {size:[bitmap.width,bitmap.height],pixels:Array.from(x.getImageData(0,0,c.width,c.height).data)};
  },make=async(...args)=>{const b=await createImageBitmap(...args),result=read(b);b.close();return result};
  const original=await createImageBitmap(data),copy=await createImageBitmap(original);rgba.fill(0);
  const stable=read(copy);original.close();copy.close();
  const blob=new Blob([Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAYAAAD0In+KAAAADklEQVR4nGP4z8DwHwQBEPgD/U6VwW8AAAAASUVORK5CYII='),c=>c.charCodeAt(0))],{type:'image/png'});
  const result={
    blob:await make(blob),flip:await make(optionData,{imageOrientation:'flipY'}),
    none:await make(optionData,{premultiplyAlpha:'none'}),premultiply:await make(optionData,{premultiplyAlpha:'premultiply'}),
    pixelated:await make(optionData,{resizeWidth:7,resizeHeight:5,resizeQuality:'pixelated'}),
    low:await make(optionData,{resizeWidth:7,resizeHeight:5,resizeQuality:'low'}),
    high:await make(optionData,{resizeWidth:7,resizeHeight:5,resizeQuality:'high'}),
    width:await make(optionData,{resizeWidth:2}),height:await make(optionData,{resizeHeight:6}),stable
  };
  const compact=value=>({size:value.size,sum:value.pixels.reduce((a,b)=>a+b,0),first:value.pixels.slice(0,4),last:value.pixels.slice(-4)}),
    resized=value=>({size:value.size,first:value.pixels.slice(0,4),last:value.pixels.slice(-4)});
  return JSON.stringify({blob:compact(result.blob),flip:compact(result.flip),none:compact(result.none),premultiply:compact(result.premultiply),
    pixelated:resized(result.pixelated),low:resized(result.low),high:resized(result.high),width:{size:result.width.size},height:resized(result.height),
    qualityDistinct:result.low.pixels.some((v,i)=>v!==result.high.pixels[i]),stable:compact(result.stable)});
}`
	value, err := p.Evaluate(context.Background(), `(async()=>await (`+observation+`)())()`)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.Evaluate(context.Background(), `(async()=>new Promise((resolve,reject)=>{
	    const source='onmessage=async()=>{try{postMessage(await ('+`+strconv.Quote(observation)+`+')())}catch(e){postMessage("ERROR:"+e.name+":"+e.message)}}';
    const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'})),w=new Worker(url);
    w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.onerror=reject;w.postMessage(null);
  }))()`)
	if err != nil {
		t.Fatal(err)
	}
	if value != worker {
		t.Fatalf("Window and Worker ImageSource pipelines diverged:\nwindow=%v\nworker=%v", value, worker)
	}
	const expected = `{"blob":{"size":[2,1],"sum":1020,"first":[255,0,0,255],"last":[0,255,0,255]},"flip":{"size":[4,3],"sum":6583,"first":[10,30,50,255],"last":[255,255,255,64]},"none":{"size":[4,3],"sum":6583,"first":[255,0,0,255],"last":[219,239,251,64]},"premultiply":{"size":[4,3],"sum":6583,"first":[255,0,0,255],"last":[219,239,251,64]},"pixelated":{"size":[7,5],"first":[255,0,0,255],"last":[219,239,251,64]},"low":{"size":[7,5],"first":[255,0,0,255],"last":[219,239,251,64]},"high":{"size":[7,5],"first":[255,0,0,255],"last":[223,243,255,63]},"width":{"size":[2,2]},"height":{"size":[8,6],"first":[255,0,0,255],"last":[219,239,251,64]},"qualityDistinct":true,"stable":{"size":[4,3],"sum":6583,"first":[255,0,0,255],"last":[219,239,251,64]}}`
	if value != expected {
		t.Fatalf("frozen Chrome 152 ImageSource observation:\n got %v\nwant %v", value, expected)
	}
}
