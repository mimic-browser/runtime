package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBeaconExtractedBodiesAndBindings(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		type upload struct{ path, body, contentType, mode, cookie string }
		uploads := make(chan upload, 8)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				body, _ := io.ReadAll(r.Body)
				uploads <- upload{r.URL.Path, string(body), r.Header.Get("Content-Type"), r.Header.Get("Sec-Fetch-Mode"), r.Header.Get("Cookie")}
				w.WriteHeader(204)
				return
			}
			w.Header().Set("Set-Cookie", "beacon=shared; Path=/")
			fmt.Fprint(w, "<!doctype html><body><iframe src='/child'></iframe>")
		}))
		defer server.Close()
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(()=>{
 const view=new Uint8Array([9,0,255,128,9]);
 const form=new FormData();form.append('a','b');form.append('file',new Blob(['file'],{type:'text/plain'}),'x.txt');
 const inputs=[['null',null],['text','héllo'],['urlparams',new URLSearchParams({a:'b c'})],['bytes',new DataView(view.buffer,1,3)],['blob',new Blob(['json'],{type:'application/json'})],['form',form]];
 if(!inputs.every(([name,body])=>navigator.sendBeacon('/echo/'+name,body)))return false;
 const expect=(fn,message)=>{try{fn();return false}catch(e){return e instanceof TypeError&&e.message===message}};
 const prefix="Failed to execute 'sendBeacon' on 'Navigator': ";
 if(!expect(()=>navigator.sendBeacon(),prefix+'1 argument required, but only 0 present.'))return false;
 if(!expect(()=>navigator.sendBeacon('http://['),prefix+'The URL argument is ill-formed or unsupported.'))return false;
 if(!expect(()=>navigator.sendBeacon('data:text/plain,x'),prefix+'Beacons are only supported over HTTP(S).'))return false;
 if(!expect(()=>navigator.sendBeacon('/echo',new ReadableStream()),prefix+'sendBeacon cannot have a ReadableStream body.'))return false;
 if(!expect(()=>navigator.sendBeacon('/echo',Symbol()),prefix+'Cannot convert a Symbol value to a string'))return false;
 if(!expect(()=>Navigator.prototype.sendBeacon.call({},'/echo'),'Illegal invocation'))return false;
 const child=document.querySelector('iframe').contentWindow;
 return navigator.sendBeacon.length===1&&!('prototype' in navigator.sendBeacon)&&Navigator.prototype.sendBeacon.call(child.navigator,'/echo/borrowed','child');
})()`, true)
		seen := map[string]upload{}
		for len(seen) < 7 {
			select {
			case u := <-uploads:
				seen[u.path] = u
			case <-time.After(5 * time.Second):
				t.Fatalf("uploads=%v", seen)
			}
		}
		for path, want := range map[string]string{"null": "", "text": "héllo", "urlparams": "a=b+c", "bytes": string([]byte{0, 255, 128}), "blob": "json", "borrowed": "child"} {
			got := seen["/echo/"+path]
			if got.body != want || !strings.Contains(got.cookie, "beacon=shared") {
				t.Fatalf("%s=%+v", path, got)
			}
		}
		if seen["/echo/text"].contentType != "text/plain;charset=UTF-8" || seen["/echo/urlparams"].contentType != "application/x-www-form-urlencoded;charset=UTF-8" || seen["/echo/bytes"].contentType != "" || seen["/echo/blob"].mode != "cors" {
			t.Fatalf("body policy=%v", seen)
		}
		form := seen["/echo/form"]
		if !strings.HasPrefix(form.contentType, "multipart/form-data; boundary=") || !strings.Contains(form.body, `filename="x.txt"`) || !strings.Contains(form.body, "file\r\n") {
			t.Fatalf("multipart=%+v", form)
		}
	})
}

func TestBeaconQueueQuotaNavigationAndContextTeardown(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		started := make(chan string, 8)
		cancelled := make(chan string, 8)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				_, _ = io.Copy(io.Discard, r.Body)
				started <- r.URL.Path
				<-r.Context().Done()
				cancelled <- r.URL.Path
				return
			}
			fmt.Fprint(w, "<!doctype html><body>queue")
		}))
		defer server.Close()
		defer p.ctx.Cancel()
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		// Bound transport observations separately from realm/bootstrap work.
		// Sharing one deadline across three navigations can expire before any
		// upload assertion runs on slower machines, without a transport failure.
		awaitUpload := func(want string) {
			t.Helper()
			select {
			case got := <-started:
				if got != want {
					t.Fatalf("accepted upload: got %q, want %q", got, want)
				}
			case <-time.After(time.Second):
				t.Fatal("accepted upload was not started")
			}
		}
		navigate := func(page *Page, url string) {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := page.Navigate(ctx, url); err != nil {
				t.Fatal(err)
			}
		}
		historyEval(t, p, `navigator.sendBeacon('/hold/old','x'.repeat(60000))&&!navigator.sendBeacon('/hold/overflow','x'.repeat(6000))&&!navigator.sendBeacon('/hold/utf8','é'.repeat(4000))`, true)
		awaitUpload("/hold/old")
		historyEval(t, p, `fetch('/hold/fetch',{method:'POST',body:'x'.repeat(6000),keepalive:true}).then(()=>false,e=>e instanceof TypeError)`, true)
		other, err := p.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		navigate(other, server.URL)
		historyEval(t, other, `navigator.sendBeacon('/hold/other','x'.repeat(60000))`, true)
		awaitUpload("/hold/other")
		navigate(p, server.URL+"/next")
		historyEval(t, p, `navigator.sendBeacon('/hold/new','x'.repeat(60000))`, true)
		awaitUpload("/hold/new")
		closed := make(chan struct{})
		go func() { p.ctx.ClosePage(p.ID); close(closed) }()
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("Page close waited for Context-owned beacon")
		}
		select {
		case path := <-cancelled:
			t.Fatalf("Page close cancelled accepted upload %s", path)
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = p.ctx.Close(); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			select {
			case <-cancelled:
			case <-ctx.Done():
				t.Fatal("Context close did not cancel pending transport")
			}
		}
	})
}

func TestBeaconCORSUsesSharedPreflightAndPolicy(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		requests := make(chan string, 4)
		remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- r.Method
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "content-type")
			w.WriteHeader(204)
		}))
		defer remote.Close()
		local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><body>cors") }))
		defer local.Close()
		if err := p.Navigate(context.Background(), local.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `navigator.sendBeacon(`+strconv.Quote(remote.URL)+`,new Blob(['{}'],{type:'application/json'}))`, true)
		for _, want := range []string{"OPTIONS", "POST"} {
			select {
			case got := <-requests:
				if got != want {
					t.Fatalf("method=%s want=%s", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Beacon did not traverse shared CORS boundary")
			}
		}
	})
}

func TestBeaconBlockedPolicyReleasesQuota(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		requests := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				requests <- struct{}{}
				w.WriteHeader(204)
				return
			}
			w.Header().Set("Content-Security-Policy", "connect-src 'none'")
			fmt.Fprint(w, "<!doctype html><body>blocked")
		}))
		defer server.Close()
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `navigator.sendBeacon('/blocked','x'.repeat(65536))`, true)
		budget := &p.Top.Realm.keepaliveBudget
		deadline := time.Now().Add(time.Second)
		for {
			budget.mu.Lock()
			pending := budget.bytes
			budget.mu.Unlock()
			if pending == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("failed CSP request retained its queue budget")
			}
			time.Sleep(time.Millisecond)
		}
		select {
		case <-requests:
			t.Fatal("CSP-blocked Beacon reached the transport")
		default:
		}
		historyEval(t, p, `(()=>{const request=new Request('/blocked',{method:'POST',body:'text',keepalive:true});return request.clone().keepalive&&new Request(request.clone()).keepalive})()`, true)
	})
}
