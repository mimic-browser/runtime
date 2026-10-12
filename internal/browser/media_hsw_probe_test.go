package browser

import (
	"context"
	"fmt"
	"testing"
)

// Frozen headful Chrome 152 control: .build/hsw-semantic-oracle-20261013/chrome152.json.
// HSW asks these particular MIME strings through all three media surfaces.
func TestHSWMediaTypeObservations(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const audio=document.createElement('audio'), video=document.createElement('video');
  return ['audio/mpegurl','audio/x-m4a','video/x-matroska',
    'video/mp4; codecs="avc1.42E01E"','video/webm; codecs="vp8"',
    'video/webm; codecs="vp9"'].map(type=>[
      audio.canPlayType(type),video.canPlayType(type),
      MediaSource.isTypeSupported(type),MediaRecorder.isTypeSupported(type)]);
})()`)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(value)
	want := "[[maybe maybe false false] [maybe maybe false false] [maybe maybe false true] [probably probably true true] [probably probably true true] [probably probably true true]]"
	if got != want {
		t.Fatalf("media probe = %s, want %s", got, want)
	}
}
