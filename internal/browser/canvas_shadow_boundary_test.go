package browser

import (
	"context"
	"math"
	"testing"
)

// Frozen headful Chrome 152 paints the shadow of an off-canvas arc before
// clipping to the canvas. A radial shader shades the blurred result at its
// destination, rather than making the off-canvas source mask transparent.
func TestCanvasOffCanvasArcShadowAndGradient(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const result=[];
  for(const [offsetX,offsetY] of [[0,0],[5,3]])
  for(const fill of ['solid','radial']) for(const shadow of [false,true]) {
    const canvas=document.createElement('canvas');
    canvas.width=canvas.height=20;
    const ctx=canvas.getContext('2d');
    if(fill==='solid') ctx.fillStyle='#e6331a';
    else {
      const gradient=ctx.createRadialGradient(2,10,9,10,17,5);
      gradient.addColorStop(0,'#e6331a');
      gradient.addColorStop(1,'#e64d66');
      ctx.fillStyle=gradient;
    }
    if(shadow) {
      ctx.shadowColor='#ff99e6';
      ctx.shadowBlur=19.021020889282227;
      ctx.shadowOffsetX=offsetX;
      ctx.shadowOffsetY=offsetY;
    }
    ctx.beginPath();
    ctx.arc(4,17,19,0.6365142281379026,3.5549875599466514);
    ctx.fill();
    const alpha=ctx.getImageData(0,0,20,20).data.filter((_,i)=>i%4===3);
    result.push([alpha.reduce((sum,v)=>sum+v,0),alpha.filter(v=>v>0).length]);
  }
  return result;
})()`)
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := value.([]any)
	if !ok || len(rows) != 8 {
		t.Fatalf("canvas shadow observations = %#v", value)
	}
	// The limits cover the measured NVIDIA/ANGLE reference and the small
	// per-pixel coverage variation of the profile-calibrated CPU model.
	want := [][2]float64{
		{1820, 10}, {14478, 400}, {1310, 8}, {11823, 238},
		{1820, 10}, {14019, 400}, {1310, 8}, {12895, 310},
	}
	number := func(value any) (float64, bool) {
		switch v := value.(type) {
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case float64:
			return v, true
		default:
			return 0, false
		}
	}
	for i, row := range rows {
		pair, ok := row.([]any)
		if !ok || len(pair) != 2 {
			t.Fatalf("canvas shadow row %d = %#v", i, row)
		}
		alpha, alphaOK := number(pair[0])
		count, countOK := number(pair[1])
		if !alphaOK || !countOK || math.Abs(alpha-want[i][0]) > 80 || math.Abs(count-want[i][1]) > 1 {
			t.Fatalf("canvas shadow row %d = %#v (%T, %T), Chrome 152 = %v", i, row, pair[0], pair[1], want[i])
		}
	}
}
