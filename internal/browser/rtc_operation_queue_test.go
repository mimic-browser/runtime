package browser

import (
	"context"
	_ "embed"
	"testing"
	"time"
)

//go:embed testdata/rtc_operation_queue.js
var rtcOperationQueue string

func TestRTCOperationsStartAfterCallAndRecoverAfterRejection(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, rtcOperationQueue)
		if err != nil || result != true {
			t.Fatalf("RTC operation queue: %v; result %v", err, result)
		}
	})
}
