package browser

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLazyAudioSharesRTCTrackOwnershipWithoutGlobals(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/audio_rtc_track_ownership.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		got, err := page.Evaluate(ctx, "(async()=>JSON.stringify(await eval("+strconv.Quote(string(source))+")))()")
		want := `{"addedGlobals":[],"destination":{"stable":true,"id":true,"kind":"audio","readyState":"live","muted":false,"disabled":true,"enabled":true},"peer":{"stable":true,"kind":"audio","readyState":"live","receiverKind":"audio"}}`
		if err != nil || got != want {
			t.Fatalf("media track ownership: got %v, %v; want %s", got, err, want)
		}
	})
}
