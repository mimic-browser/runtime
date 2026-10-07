package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func navigateCapabilityFixture(t *testing.T, p *Page) {
	navigateCapabilityFixtureMode(t, p, false)
}

func navigateCapabilityFixtureMode(t *testing.T, p *Page, isolated bool) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isolated {
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		}
		fmt.Fprint(w, "<!doctype html><body>capability fixture</body>")
	}))
	t.Cleanup(server.Close)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
}

func TestNavigatorCapturedShape(t *testing.T) {
	parallelBrowserTest(t)
	fixture, err := os.ReadFile("../../compatibility/captures/navigator-chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture map[string]json.RawMessage
	if err = json.Unmarshal(fixture, &capture); err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		BrowserMode          string `json:"browserMode"`
		EnvironmentProfileID string `json:"environmentProfileId"`
	}
	if err = json.Unmarshal(capture["captureMetadata"], &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.BrowserMode != "headful" || metadata.EnvironmentProfileID != "chrome-152-windows-x64-headful-controlled-v1" {
		t.Fatalf("generic Navigator fixture must be the authoritative headful capture: %+v", metadata)
	}
	probe, err := os.ReadFile("../../compatibility/navigator_capabilities.py")
	if err != nil {
		t.Fatal(err)
	}
	_, source, ok := strings.Cut(string(probe), `SHAPE = r"""`)
	if !ok {
		t.Fatal("missing shared shape probe")
	}
	source, _, ok = strings.Cut(source, `"""`)
	if !ok {
		t.Fatal("unterminated shape probe")
	}
	for _, contextName := range []string{"opaque", "secure", "isolated"} {
		t.Run(contextName, func(t *testing.T) {
			var expected struct {
				Members map[string]any `json:"members"`
			}
			if err := json.Unmarshal(capture[contextName], &expected); err != nil {
				t.Fatal(err)
			}
			p := testPage(t)
			if contextName != "opaque" {
				navigateCapabilityFixtureMode(t, p, contextName == "isolated")
			}
			names := []string{}
			for name := range expected.Members {
				names = append(names, name)
			}
			encoded, _ := json.Marshal(names)
			value, err := p.Evaluate(context.Background(), "("+source+")("+string(encoded)+")")
			if err != nil {
				t.Fatal(err)
			}
			serialized, _ := json.Marshal(value)
			var actual struct {
				Members map[string]any `json:"members"`
			}
			if err = json.Unmarshal(serialized, &actual); err != nil {
				t.Fatal(err)
			}
			for name, want := range expected.Members {
				if !reflect.DeepEqual(want, actual.Members[name]) {
					t.Errorf("%s shape mismatch: want %v; got %v", name, want, actual.Members[name])
				}
			}
		})
	}
}

func TestNavigatorCapturedOperationsV8(t *testing.T) {
	parallelBrowserTest(t)
	fixture, err := os.ReadFile("../../compatibility/captures/navigator-chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	type observations struct {
		Operations map[string]any `json:"operations"`
	}
	var capture struct{ Secure, Opaque observations }
	if err = json.Unmarshal(fixture, &capture); err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile("../../compatibility/probes-navigator-capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	var probes []struct{ Name, Expression string }
	if err = json.Unmarshal(input, &probes); err != nil {
		t.Fatal(err)
	}
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	// Match the retained capture's input availability without depending on the
	// CI host's hardware. Speaker output remains an explicit unsupported boundary.
	b.cameraProvider = &fixtureCameraProvider{}
	b.microphoneProvider = &fixtureMicrophoneProvider{}
	for _, target := range []struct {
		name         string
		observations observations
	}{{"secure", capture.Secure}, {"opaque", capture.Opaque}} {
		t.Run(target.name, func(t *testing.T) {
			c := b.NewContext()
			defer c.Close()
			p, err := c.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			if target.name == "secure" {
				navigateCapabilityFixture(t, p)
			}
			for _, probe := range probes {
				// These differences are measured and reported, never normalized away
				// in the retained differential: network is a different machine profile,
				// Bluetooth startup is time-variable, and no power backend is installed.
				if probe.Name == "connection" || (target.name == "secure" && (probe.Name == "bluetooth.available" || probe.Name == "serial.ports" || probe.Name == "wakeLock.request")) {
					continue
				}
				source, _ := json.Marshal(probe.Expression)
				value, err := p.Evaluate(context.Background(), `(async()=>{try{const value=await Promise.race([eval(`+string(source)+`),new Promise(resolve=>setTimeout(()=>resolve({pending:true}),1500))]);return {value:value===undefined?{undefined:true}:value}}catch(e){return {error:{name:e.name,message:e.message}}}})()`)
				if err != nil {
					t.Errorf("%s: %v", probe.Name, err)
					continue
				}
				encoded, _ := json.Marshal(value)
				var actual any
				_ = json.Unmarshal(encoded, &actual)
				want := target.observations.Operations[probe.Name]
				if target.name == "secure" {
					want = navigatorAutomationObservation(t, probe.Name, want)
				}
				if !reflect.DeepEqual(actual, want) {
					t.Errorf("%s: want %v, got %v (retained Chrome: %v)", probe.Name, want, actual, target.observations.Operations[probe.Name])
				}
			}
		})
	}
}

// Keep the frozen observations unchanged and verify them before asserting the
// explicitly requested automation policy. A prompt must reject rather than
// wait for UI; enumerated devices must belong to implemented native backends.
func navigatorAutomationObservation(t *testing.T, name string, frozen any) any {
	t.Helper()
	switch name {
	case "clipboard.read", "keyboard.lock", "media.getUserMedia":
		chrome := map[string]any{"value": map[string]any{"pending": true}}
		if !reflect.DeepEqual(frozen, chrome) {
			t.Fatalf("%s: retained interactive Chrome observation changed: %v", name, frozen)
		}
		message := "Permission requires an explicit automation decision"
		if name == "media.getUserMedia" {
			message = "Permission denied; grant access through Browser.setPermission or Browser.grantPermissions"
		}
		return map[string]any{"error": map[string]any{"name": "NotAllowedError", "message": message}}
	case "media.enumerate":
		anonymous := func(kind string) any {
			return map[string]any{"deviceId": "", "groupId": "", "kind": kind, "label": ""}
		}
		chrome := map[string]any{"value": []any{anonymous("audioinput"), anonymous("videoinput"), anonymous("audiooutput")}}
		if !reflect.DeepEqual(frozen, chrome) {
			t.Fatalf("retained Chrome device observation changed: %v", frozen)
		}
		return map[string]any{"value": []any{anonymous("audioinput"), anonymous("videoinput")}}
	default:
		return frozen
	}
}

func TestNavigatorStorageLifecycle(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(async()=>{
 const bucket=await navigator.storageBuckets.open('regression');
 if(bucket.name!=='regression'||await bucket.persisted()||await bucket.expires()!==null)return false;
 if(!(await navigator.storageBuckets.keys()).includes('regression'))return false;
 await navigator.storageBuckets.delete('regression');
 let invalid=false;try{await bucket.estimate()}catch(e){invalid=e.name==='InvalidStateError'}
 const order=[];
 const first=navigator.locks.request('shared-origin',async lock=>{order.push(lock.mode);await new Promise(resolve=>setTimeout(resolve,5));order.push('released')});
 const second=navigator.locks.request('shared-origin',()=>order.push('next'));
 await Promise.all([first,second]);
 const query=await navigator.locks.query();return invalid&&order.join(',')==='exclusive,released,next'&&query.held.length===0&&query.pending.length===0;
})()`)
	if err != nil || value != true {
		t.Fatalf("storage lifecycle: %v %v", value, err)
	}
}

func TestNavigatorCapabilityDomains(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(async()=>{
 const result=[];
 result.push(navigator.vendorSub===''&&navigator.productSub==='20030107'&&navigator.appCodeName==='Mozilla'&&navigator.doNotTrack===null);
 result.push(navigator.hid instanceof HID&&Object.prototype.toString.call(navigator.hid)==='[object HID]'&&navigator.hid===navigator.hid);
 result.push(!('getDevices' in navigator.bluetooth));
 try{Object.getOwnPropertyDescriptor(Navigator.prototype,'hid').get.call({});result.push(false)}catch(e){result.push(e.name==='TypeError')}
 result.push((await navigator.hid.getDevices()).length===0&&(await navigator.usb.getDevices()).length===0&&(await navigator.serial.getPorts()).length===0);
 result.push((await navigator.permissions.query({name:'geolocation'})).state==='prompt');
 result.push((await navigator.storage.estimate()).quota===10737418240);
 result.push(navigator.serviceWorker.controller===null&&(await navigator.serviceWorker.getRegistrations()).length===0);
 result.push(!(await navigator.xr.isSessionSupported('immersive-vr')));
 result.push(navigator.mediaSession.metadata===null&&navigator.mediaSession.playbackState==='none');
 result.push(navigator.deprecatedRunAdAuctionEnforcesKAnonymity===false&&navigator.protectedAudience.queryFeatureSupport('unknown')===undefined);
 const auction=navigator.runAdAuction({});result.push(auction instanceof Promise);try{await auction;result.push(false)}catch(e){result.push(e.name==='NotSupportedError')}
 result.push(navigator.javaEnabled()===false);
 return result.every(Boolean)
})()`)
	if err != nil || value != true {
		t.Fatalf("capabilities: %v %v", value, err)
	}
}

func TestNavigatorPermissionAndNetworkState(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	if err := p.ctx.SetPermission(originOf(p.URL()), "geolocation", "granted"); err != nil {
		t.Fatal(err)
	}
	// The loader can still read the Page environment from a resource goroutine.
	p.mu.Lock()
	p.env.Network.DownlinkMbps = 3.25
	p.env.Network.RTTMillis = 125
	p.env.Preferences.DoNotTrack = true
	p.mu.Unlock()
	value, err := p.Evaluate(context.Background(), `navigator.permissions.query({name:'geolocation'}).then(p=>p.state==='granted'&&navigator.connection.downlink===3.25&&navigator.connection.rtt===125&&navigator.doNotTrack==='1')`)
	if err != nil || value != true {
		t.Fatalf("canonical state: %v %v", value, err)
	}
}

func TestNavigatorSecureExposure(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `!isSecureContext&&!('clipboard' in navigator)&&!('hid' in navigator)&&!('storage' in navigator)&&!('xr' in navigator)&&('geolocation' in navigator)&&('connection' in navigator)`)
	if err != nil || value != true {
		t.Fatalf("insecure exposure: %v %v", value, err)
	}
}

func TestNavigatorActivationRequiresTrustedInput(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `document.body.dispatchEvent(new Event('mousedown'));navigator.userActivation.isActive||navigator.userActivation.hasBeenActive`)
	if err != nil || value != false {
		t.Fatalf("synthetic activation: %v %v", value, err)
	}
	if err := p.DispatchInput(context.Background(), 0, "mousedown"); err != nil {
		t.Fatal(err)
	}
	value, err = p.Evaluate(context.Background(), `navigator.userActivation.isActive&&navigator.userActivation.hasBeenActive&&!navigator.scheduling.isInputPending()`)
	if err != nil || value != true {
		t.Fatalf("trusted activation: %v %v", value, err)
	}
	value, err = p.Evaluate(context.Background(), `new Promise(resolve=>setTimeout(()=>resolve(!navigator.userActivation.isActive&&navigator.userActivation.hasBeenActive),5100))`)
	if err != nil || value != true {
		t.Fatalf("activation expiry: %v %v", value, err)
	}
}

func TestNavigatorFeatureGating(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	p.env.Features = map[string]bool{"WebBluetooth": false, "WebHID": false, "WebUSB": false, "WebXR": false}
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `!('bluetooth' in navigator)&&!('hid' in navigator)&&!('usb' in navigator)&&!('xr' in navigator)&&typeof Bluetooth==='undefined'&&typeof HID==='undefined'&&typeof USB==='undefined'&&typeof XRSystem==='undefined'&&('mediaDevices' in navigator)`)
	if err != nil || value != true {
		t.Fatalf("feature gating: %v %v", value, err)
	}
}

func TestNavigatorSharedPermissionAndClipboard(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	p2, err := p.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	if err = p2.Navigate(context.Background(), p.URL()); err != nil {
		t.Fatal(err)
	}
	if _, err = p2.Evaluate(context.Background(), `navigator.permissions.query({name:'clipboard-read'}).then(p=>{globalThis.permissionStatus=p;globalThis.permissionChanges=0;p.onchange=()=>permissionChanges++})`); err != nil {
		t.Fatal(err)
	}
	if err = p.ctx.SetPermission(originOf(p.URL()), "clipboard-read", "granted"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Evaluate(context.Background(), `navigator.clipboard.writeText('shared fixture')`); err != nil {
		t.Fatal(err)
	}
	value, err := p2.Evaluate(context.Background(), `navigator.clipboard.readText().then(text=>new Promise(resolve=>setTimeout(()=>resolve(text==='shared fixture'&&permissionStatus.state==='granted'&&permissionChanges===1),0)))`)
	if err != nil || value != true {
		t.Fatalf("shared capability state: %v %v", value, err)
	}
}
