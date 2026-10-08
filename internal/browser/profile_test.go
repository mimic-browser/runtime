package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestConfigureProfileWaitsForRetainedUploads(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := b.NewContext()
	document := c.Profile()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	accepted, err := p.Evaluate(ctx, fmt.Sprintf(`navigator.sendBeacon(%q, "retained")`, server.URL))
	if err != nil || accepted != true {
		t.Fatalf("beacon acceptance: %v, %v", accepted, err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	c.ClosePage(p.ID)
	before := c.Environment()
	if _, err = c.ConfigureProfile(document, nil, nil, nil); err == nil {
		t.Fatal("reconfigured a transport with an active retained upload")
	}
	if !reflect.DeepEqual(before, c.Environment()) {
		t.Fatal("rejected configuration changed the environment")
	}
	close(release)
	released = true
	for c.activeKeepalives.Load() != 0 {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if _, err = c.ConfigureProfile(document, nil, nil, nil); err != nil {
		t.Fatalf("idle empty Context cannot be configured: %v", err)
	}
	if !c.profileLocked {
		t.Fatal("managed identity was not locked")
	}
}

func TestProfileInheritanceWorkersAndSnapshots(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf(`{"schemaVersion":1,"baseProfile":%q,"hardware":{"logicalProcessors":6,"deviceMemoryGB":4},"locale":{"languages":["fr-FR","en"]},"graphics":{"vendor":"Profile Vendor","renderer":"Profile Renderer"}}`, b.env.ProfileID))
	c, err := b.NewContextWithProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	expr := `(()=>{const gl=document.createElement('canvas').getContext('webgl');gl.getExtension('WEBGL_debug_renderer_info');return navigator.hardwareConcurrency===6&&navigator.language==='fr-FR'&&gl.getParameter(37445)==='Profile Vendor'})()`
	historyEval(t, p, expr, true)
	historyEval(t, p, `(()=>{const f=document.createElement('iframe');document.body.appendChild(f);return f.contentWindow.navigator.hardwareConcurrency===6&&f.contentWindow.navigator.language==='fr-FR'})()`, true)
	bootstrapSnapshotWarm(t, p)
	next, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	historyEval(t, next, expr, true)
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !next.Top.Realm.bootstrapRestored {
		t.Fatal("profile bootstrap not restored")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/worker.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `postMessage({cpu:navigator.hardwareConcurrency,language:navigator.language})`)
		} else {
			fmt.Fprint(w, "<!doctype html><body>profile fixture</body>")
		}
	}))
	defer server.Close()
	if err := next.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	historyEval(t, next, expr, true)
	historyEval(t, next, "navigator.deviceMemory===4", true)
	historyEval(t, next, `new Promise((resolve,reject)=>{const w=new Worker('/worker.js');w.onmessage=e=>{w.terminate();resolve(e.data.cpu===6&&e.data.language==='fr-FR')};w.onerror=reject})`, true)
	view := p.Environment()
	view.Locale.Languages[0] = "mutated"
	view.Graphics.WebGPU.Features[0] = "mutated"
	if p.Environment().Locale.Languages[0] != "fr-FR" || c.Environment().Graphics.WebGPU.Features[0] == "mutated" {
		t.Fatal("getter alias")
	}
}

func TestInternalProfileAndConcurrentContextOwnership(t *testing.T) {
	serialBrowserTest(t)
	raw := []byte(`{"schemaVersion":1,"baseProfile":"chrome-152-windows-x64-headful-controlled-v1","hardware":{"logicalProcessors":3}}`)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			c, err := b.NewContextWithProfile(raw)
			if err != nil {
				done <- err
				return
			}
			defer c.Close()
			p, err := c.NewPage()
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				value, e := p.Evaluate(ctx, "navigator.hardwareConcurrency===3")
				err = e
				if err == nil && value != true {
					err = fmt.Errorf("unexpected profile: %v", value)
				}
			}
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfileMediaChangeUsesPageTasks(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `globalThis.profileMedia=matchMedia('(prefers-reduced-motion: reduce)');globalThis.profileChanges=[];profileMedia.addEventListener('change',e=>profileChanges.push(e instanceof MediaQueryListEvent&&e.isTrusted&&e.matches&&e.media===profileMedia.media));true`, true)
	if _, err := p.UpdateProfile([]byte(`{"preferences":{"reducedMotion":true}}`)); err != nil {
		t.Fatal(err)
	}
	historyEval(t, p, `new Promise(r=>setTimeout(()=>r(profileMedia.matches&&profileChanges.length===1&&profileChanges[0]),0))`, true)
	if _, err := p.UpdateProfile([]byte(`{"preferences":{"reducedMotion":true}}`)); err != nil {
		t.Fatal(err)
	}
	historyEval(t, p, `new Promise(r=>setTimeout(()=>r(profileChanges.length===1),0))`, true)
}

func TestStyleObservationEpochTracksMediaPreferences(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `document.head.innerHTML='<style>div{width:10px;height:10px}@media(prefers-color-scheme:dark){div{width:20px}}@media(prefers-reduced-motion:reduce){div{height:30px}}</style>';document.body.innerHTML='<div></div>';globalThis.mediaStyle=getComputedStyle(document.querySelector('div'));true`, true)
	if _, err := p.UpdateProfile([]byte(`{"preferences":{"colorScheme":"light","reducedMotion":false}}`)); err != nil {
		t.Fatal(err)
	}
	historyEval(t, p, `mediaStyle.width==='10px'&&mediaStyle.height==='10px'`, true)
	if _, err := p.UpdateProfile([]byte(`{"preferences":{"colorScheme":"dark","reducedMotion":true}}`)); err != nil {
		t.Fatal(err)
	}
	historyEval(t, p, `mediaStyle.width==='20px'&&mediaStyle.height==='30px'`, true)
}

func TestProfileCreationObservations(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.NewContextWithProfile([]byte(`{"schemaVersion":1,"baseProfile":"chrome-152-windows-x64-headful-controlled-v1",
		"display":{"width":1920,"height":1080,"availableWidth":1900,"availableHeight":1040,"colorDepth":30},
		"window":{"x":10,"y":20,"outerWidth":1300,"outerHeight":850},
		"graphics":{"vendor":"Profile Vendor","renderer":"Profile Renderer","maxTextureSize":8192},
		"preferences":{"colorScheme":"light","reducedMotion":true,"doNotTrack":true},
		"network":{"saveData":true,"online":false,"effectiveType":"3g","downlinkMbps":3,"rttMillis":75,"cookiesEnabled":false},
		"permissions":{"geolocation":"granted"},
		"capabilities":{"storageQuotaBytes":1048576,"keyboardLayout":{"KeyA":"q"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, p)
	historyEval(t, p, `(async()=>{
		const gl=document.createElement('canvas').getContext('webgl');gl.getExtension('WEBGL_debug_renderer_info');
		const checks=[screen.width===1920,screen.height===1080,screen.availWidth===1900,screen.availHeight===1040,screen.colorDepth===30,
			screenX===10,screenY===20,outerWidth===1300,outerHeight===850,
			gl.getParameter(37445)==='Profile Vendor',gl.getParameter(37446)==='Profile Renderer',gl.getParameter(3379)===8192,
			matchMedia('(prefers-color-scheme: light)').matches,matchMedia('(prefers-reduced-motion: reduce)').matches,navigator.doNotTrack==='1',
			navigator.connection.saveData,!navigator.onLine,navigator.connection.effectiveType==='3g',navigator.connection.downlink===3,navigator.connection.rtt===75,!navigator.cookieEnabled,
			(await navigator.permissions.query({name:'geolocation'})).state==='granted',(await navigator.storage.estimate()).quota===1048576,(await navigator.keyboard.getLayoutMap()).get('KeyA')==='q'];
		return checks.every(Boolean)?true:JSON.stringify(checks);
	})()`, true)
}
