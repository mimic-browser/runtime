package browser

import (
	"context"
	"strconv"
	"testing"
)

func TestCanvasDrawImageSamplingMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	const observation = `async()=>{
  const source=new OffscreenCanvas(3,2),s=source.getContext('2d');
  s.putImageData(new ImageData(new Uint8ClampedArray([
    255,0,0,255, 0,255,0,255, 0,0,255,255,
    255,255,0,255, 0,255,255,128, 255,0,255,64
  ]),3,2),0,0);
  const draw=(args,enabled=true,quality='low')=>{
    const c=new OffscreenCanvas(10,8),x=c.getContext('2d',{willReadFrequently:true});
    x.imageSmoothingEnabled=enabled;x.imageSmoothingQuality=quality;x.drawImage(source,...args);
    return {canvas:c,pixels:Array.from(x.getImageData(0,0,10,8).data)};
  },pixel=(pixels,x,y)=>pixels.slice((y*10+x)*4,(y*10+x+1)*4);
  const nearest=draw([1.25,.75,7,5],false),low=draw([1.25,.75,7,5]),high=draw([1.25,.75,7,5],true,'high');
  const down=(()=>{const c=new OffscreenCanvas(1,1),x=c.getContext('2d',{willReadFrequently:true});x.drawImage(source,0,0,1,1);return Array.from(x.getImageData(0,0,1,1).data)})();
  const negativeSource=draw([3,2,-2,-2,0,0,6,6]),negativeDest=draw([7,6,-6,-4]),outside=draw([-1,-1,5,4,0,0,10,8]);
  const copyMode=(()=>{const c=new OffscreenCanvas(10,8),x=c.getContext('2d',{willReadFrequently:true});x.drawImage(source,0,0,7,5);x.globalCompositeOperation='copy';x.drawImage(source,1,0,2,2,2,1,4,4);return Array.from(x.getImageData(0,0,10,8).data)})();
  const bitmap=await createImageBitmap(low.canvas),copy=new OffscreenCanvas(10,8),cx=copy.getContext('2d',{willReadFrequently:true});cx.drawImage(bitmap,0,0);bitmap.close();
  const copied=Array.from(cx.getImageData(0,0,10,8).data),blobA=new Uint8Array(await low.canvas.convertToBlob().then(b=>b.arrayBuffer())),blobB=new Uint8Array(await copy.convertToBlob().then(b=>b.arrayBuffer()));
  return JSON.stringify({
    nearest:[pixel(nearest.pixels,1,1),pixel(nearest.pixels,4,3),pixel(nearest.pixels,7,5)],
    low:[pixel(low.pixels,1,1),pixel(low.pixels,2,1),pixel(low.pixels,4,3),pixel(low.pixels,7,4)],down,
    qualityDiff:high.pixels.some((v,i)=>v!==low.pixels[i]),
    negativeSource:[pixel(negativeSource.pixels,0,0),pixel(negativeSource.pixels,2,2),pixel(negativeSource.pixels,5,5)],
    negativeDest:[pixel(negativeDest.pixels,1,2),pixel(negativeDest.pixels,6,5)],
    outside:[pixel(outside.pixels,2,2),pixel(outside.pixels,7,5)],
    copyMode:[pixel(copyMode,0,0),pixel(copyMode,2,1)],
    copy:copied.every((v,i)=>v===low.pixels[i]),png:blobA.length===blobB.length&&blobA.every((v,i)=>v===blobB[i])
  });
}`
	value, err := p.Evaluate(context.Background(), `(async()=>await (`+observation+`)())()`)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.Evaluate(context.Background(), `(async()=>new Promise((resolve,reject)=>{
    const source='onmessage=async()=>postMessage(await ('+`+strconv.Quote(observation)+`+')())';
    const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'})),w=new Worker(url);
    w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.onerror=reject;w.postMessage(null);
  }))()`)
	if err != nil {
		t.Fatal(err)
	}
	if value != worker {
		t.Fatalf("Window and Worker drawImage surfaces diverged:\nwindow=%v\nworker=%v", value, worker)
	}
	const expected = `{"nearest":[[255,0,0,255],[0,255,255,128],[255,0,255,64]],"low":[[255,0,0,255],[246,9,0,255],[37,240,94,187],[255,0,255,64]],"down":[0,255,85,192],"qualityDiff":true,"negativeSource":[[85,170,0,255],[9,176,114,206],[255,0,255,64]],"negativeDest":[[255,0,0,255],[255,0,255,64]],"outside":[[255,0,0,255],[255,0,255,64]],"copyMode":[[0,0,0,0],[64,191,0,255]],"copy":true,"png":true}`
	if value != expected {
		t.Fatalf("frozen Chrome 152 drawImage observation:\n got %v\nwant %v", value, expected)
	}
}
