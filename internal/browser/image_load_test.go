package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestImageReadbackOriginPropagatesAndResets(t *testing.T) {
	serialBrowserTest(t)
	pixels, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAYAAAD0In+KAAAADklEQVR4nGP4z8DwHwQBEPgD/U6VwW8AAAAASUVORK5CYII=")
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if r.URL.Path == "/cors" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Write(pixels)
	}))
	defer images.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		expression := `(async()=>{const url=` + strconv.Quote(images.URL) + `;const image=new Image();image.src=url+'/opaque';await image.decode();const a=new OffscreenCanvas(2,1),x=a.getContext('2d');x.drawImage(image,0,0);const read=c=>{try{c.getImageData(0,0,1,1);return 'ok'}catch(e){return e.name}};if(read(x)!=='SecurityError')return 'image taint';const bitmap=await createImageBitmap(a),b=new OffscreenCanvas(2,1),y=b.getContext('2d');y.drawImage(bitmap,0,0);if(read(y)!=='SecurityError')return 'bitmap taint';b.width=2;if(read(y)!=='ok')return 'reset';const allowed=new Image();allowed.crossOrigin='anonymous';allowed.src=url+'/cors';await allowed.decode();y.drawImage(allowed,0,0);if(String(y.getImageData(0,0,2,1).data)!=='255,0,0,255,0,255,0,255')return 'cors pixels';const denied=new Image();denied.crossOrigin='anonymous';denied.src=url+'/denied';return await denied.decode().then(()=> 'cors accepted',e=>e.name==='EncodingError'?'ok':e.name)})()`
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, expression)
		if err != nil || value != "ok" {
			t.Fatalf("readback: %v %v", value, err)
		}
	})
}

func TestDetachedImageLoadCoalescesAndBlocksDocumentLoad(t *testing.T) {
	serialBrowserTest(t)
	var obsolete, images atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<script>globalThis.events=[];const img=new Image;img.onload=()=>{events.push('image');document.body.className='loaded';document.body.append(img)};img.onerror=()=>events.push('error');img.src='/obsolete.png';img.src='/pixel.svg';addEventListener('load',()=>events.push('window'));</script>`)
		case "/obsolete.png":
			obsolete.Add(1)
			w.WriteHeader(404)
		case "/pixel.svg":
			images.Add(1)
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
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
	if err = p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `events.join(',')+':'+document.body.className`)
	if err != nil || value != "image,window:loaded" {
		t.Fatalf("load ordering: %v %v", value, err)
	}
	if obsolete.Load() != 0 || images.Load() != 1 {
		t.Fatalf("requests: obsolete=%d image=%d", obsolete.Load(), images.Load())
	}
}

func TestLazyImageDoesNotBlockWindowLoad(t *testing.T) {
	parallelBrowserTest(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.svg" {
			close(started)
			<-release
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
			return
		}
		fmt.Fprint(w, `<img loading="lazy" src="/slow.svg"><script>addEventListener('load',()=>globalThis.loaded=true)</script>`)
	}))
	defer server.Close()
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Navigate(ctx, server.URL) }()
	deadlineFailure := func(message string) {
		t.Helper()
		stacks := make([]byte, 256<<10)
		t.Logf("navigation deadline goroutines:\n%s", stacks[:runtime.Stack(stacks, true)])
		close(release)
		t.Fatal(message)
	}
	finished := false
	var navigationErr error
	// Bootstrap runs before parser resource scheduling. Start the load-delay
	// gate only when the deliberately blocked image reaches transport. A load
	// event before that point is also valid; independent scheduling is checked
	// below with its own unchanged two-second deadline.
	select {
	case navigationErr = <-done:
		finished = true
	case <-started:
	case <-ctx.Done():
		deadlineFailure("navigation did not reach load or image transport")
	}
	if !finished {
		select {
		case navigationErr = <-done:
		case <-time.After(2 * time.Second):
			deadlineFailure("lazy image delayed the Window load event")
		}
	}
	if navigationErr != nil {
		close(release)
		t.Fatal(navigationErr)
	}
	close(release)
	value, err := p.Evaluate(context.Background(), `document.readyState+':'+globalThis.loaded`)
	if err != nil || value != "complete:true" {
		t.Fatalf("load state: %v %v", value, err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("lazy image request was not scheduled independently")
	}
}

func TestImageReplacementCancelsObsoleteRequest(t *testing.T) {
	serialBrowserTest(t)
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, "<body></body>")
		case "/old":
			close(started)
			<-r.Context().Done()
			close(canceled)
		case "/new":
			w.Header().Set("Content-Type", "image/svg+xml")
			fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
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
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Evaluate(ctx, `globalThis.img=new Image;globalThis.events=[];globalThis.loaded=new Promise(resolve=>{img.onload=()=>{events.push('load');resolve()};img.onerror=()=>events.push('error')});img.src='/old';true`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("old image did not start")
	}
	value, err := p.Evaluate(ctx, `(async()=>{img.src='/new';await loaded;return events.join(',')})()`)
	if err != nil || value != "load" {
		t.Fatalf("replacement events: %v %v", value, err)
	}
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("old image was not canceled")
	}
}
