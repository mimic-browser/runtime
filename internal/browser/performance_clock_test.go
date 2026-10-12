package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPerformanceClamperStableMonotonicBuckets(t *testing.T) {
	parallelBrowserTest(t)
	c := performanceClamper{seed: 123456789}
	for _, isolated := range []bool{false, true} {
		step := int64(100)
		if isolated {
			step = 5
		}
		last := int64(-1000000)
		for us := int64(-20000); us < 20000; us++ {
			v := c.micros(us, isolated)
			if v%step != 0 || v < last || v != c.micros(us, isolated) || v-us > step || us-v > step {
				t.Fatalf("isolated=%v at %d: %d after %d", isolated, us, v, last)
			}
			last = v
		}
	}
	origin := time.Unix(1789070000, 123456789)
	if c.now(origin, origin, false) != 0 || c.now(origin.Add(-time.Second), origin, false) != 0 {
		t.Fatal("origin and pre-origin timestamps must return zero")
	}
}

func TestPerformanceClamperPreservesTimeTicksDoubleRounding(t *testing.T) {
	// Both frozen Chrome 152 and current Chrome on the Windows oracle host use
	// this boot-time magnitude. Consecutive 100us buckets retain the same
	// observable double representation used by the hCaptcha timing collector.
	c := performanceClamper{seed: 123456789, precisionBaseMillis: 81910026.7959}
	origin := time.Unix(1789070000, 0)
	first := c.now(origin.Add(32600*time.Microsecond), origin, false)
	second := c.now(origin.Add(32700*time.Microsecond), origin, false)
	if first != 32.599999994039536 || second-first != 0.10000000894069672 {
		t.Fatalf("unexpected TimeTicks double projection: %.17g %.17g", first, second-first)
	}
}

// Frozen Chrome 152 local oracle: ordinary timestamps lie on a 100us grid,
// isolated timestamps on a 5us grid; both are nondecreasing. Sampling the
// minimum positive delta alone also measures host-call overhead, not resolution.
func TestPerformanceNowChrome152Grids(t *testing.T) {
	serialBrowserTest(t)
	for _, isolated := range []bool{false, true} {
		t.Run(fmt.Sprint(isolated), func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if isolated {
						w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
						w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
					}
					fmt.Fprint(w, "<!doctype html><body></body>")
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := p.Navigate(ctx, server.URL); err != nil {
					t.Fatal(err)
				}
				factor := 10
				if isolated {
					factor = 200
				}
				check := fmt.Sprintf(`(()=>{let last=-1;for(let i=0;i<128;i++){let v=performance.now();if(v<last||Math.abs(v*%d-Math.round(v*%d))>1e-5)return false;last=v}return true})()`, factor, factor)
				if value, err := p.Evaluate(ctx, check); err != nil || value != true {
					t.Fatalf("window grid: %v %v", value, err)
				}
				value, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{const url=URL.createObjectURL(new Blob(['postMessage(`+check+`)']));const w=new Worker(url);w.onerror=e=>reject(e.message);w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)}})`)
				if err != nil || value != true {
					t.Fatalf("worker grid: %v %v", value, err)
				}
			})
		})
	}
}
