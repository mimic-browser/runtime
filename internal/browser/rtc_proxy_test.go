package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	quickjsengine "github.com/moreveal/mimic/internal/engine/quickjs"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/profile"
	"github.com/moreveal/mimic/internal/rtctest"
)

//go:embed testdata/rtc_proxy_roundtrip.js
var rtcProxyRoundTrip string

//go:embed testdata/rtc_unsupported_gathering.js
var rtcUnsupportedGathering string

//go:embed testdata/rtc_permission_free_roundtrip.js
var rtcPermissionFreeRoundTrip string

//go:embed testdata/rtc_real_stun.js
var rtcRealSTUN string

//go:embed testdata/rtc_permission_gather.js
var rtcPermissionGather string

func rtcProxyPage(t *testing.T, factory engine.Factory, proxyURL string) *Page {
	t.Helper()
	b, err := New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := profile.Proxy{}
	if proxyURL != "" {
		proxy.Username = u.User.Username()
		proxy.Password, _ = u.User.Password()
		u.User = nil
		proxy.Server = u.String()
	}
	doc := profile.FromEnvironment(b.Environment(), b.Environment().ProfileID, proxy)
	c := b.newContext(&doc)
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRTCProxyBrowserRoundTripAndTeardown(t *testing.T) {
	serialBrowserTest(t)
	cert, _ := rtctest.Certificate(t)
	for _, scheme := range []string{"http", "socks5"} {
		for engineName, factory := range map[string]engine.Factory{"v8": v8engine.Factory{}, "goja": gojaengine.Factory{}, "quickjs": quickjsengine.Factory{}} {
			for _, teardown := range []string{"close", "navigation"} {
				t.Run(scheme+"/"+engineName+"/"+teardown, func(t *testing.T) {
					server := rtctest.NewTURN(t, false, false, cert)
					proxy := rtctest.NewProxy(t, scheme, "", cert)
					stun4, stun6 := rtctest.NewSink(t, false), rtctest.NewSink(t, true)
					p := rtcProxyPage(t, factory, proxy.URL)
					configuration := map[string]any{"iceTransportPolicy": "all", "iceServers": []any{map[string]any{"urls": []string{"stun:" + stun4.Address, "stun:" + stun6.Address}}, map[string]any{"urls": []string{"turn:" + server.Address + "?transport=tcp", "turn:" + stun4.Address + "?transport=udp"}, "username": "rtc", "credential": "secret"}}}
					raw, _ := json.Marshal(configuration)
					ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
					defer cancel()
					result, err := p.Evaluate(ctx, fmt.Sprintf("globalThis.rtcTestConfig=%s; globalThis.rtcTestPolicyUpdate='all'; globalThis.rtcLeaveOpen=%t;\n%s.catch(e => 'FAIL:' + e.name + ':' + e.message)", raw, teardown == "navigation", strings.TrimSuffix(strings.TrimSpace(rtcProxyRoundTrip), ";")))
					if err != nil || strings.HasPrefix(fmt.Sprint(result), "FAIL:") {
						t.Fatalf("public WebRTC round trip: %v; result %v", err, result)
					}
					if strings.Contains(fmt.Sprint(result), "127.0.0.2") {
						t.Fatal("proxy socket disclosed", result)
					}
					if teardown == "navigation" {
						if err := p.Navigate(ctx, "about:blank"); err != nil {
							t.Fatal(err)
						}
					}
					rtctest.Wait(t, "document releases TURN allocations", func() bool { return server.Active.Load() == 0 })
					if server.Direct.Load() != 0 || server.WriteBytes.Load() == 0 || server.ReadBytes.Load() == 0 || stun4.Packets.Load() != 0 || stun6.Packets.Load() != 0 {
						t.Fatalf("egress direct=%d read=%d write=%d forbiddenUDP=%d/%d", server.Direct.Load(), server.ReadBytes.Load(), server.WriteBytes.Load(), stun4.Packets.Load(), stun6.Packets.Load())
					}
				})
			}
		}
	}
}

func TestRTCProxyBrowserUnsupportedGathering(t *testing.T) {
	serialBrowserTest(t)
	cert, _ := rtctest.Certificate(t)
	for _, scheme := range []string{"http", "https", "socks5"} {
		for _, policy := range []string{"all", "relay"} {
			for _, remote := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/remote=%t", scheme, policy, remote), func(t *testing.T) {
					proxy := rtctest.NewProxy(t, scheme, "", cert)
					stun4, stun6 := rtctest.NewSink(t, false), rtctest.NewSink(t, true)
					p := rtcProxyPage(t, v8engine.Factory{}, proxy.URL)
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					globals, _ := json.Marshal(map[string]any{"rtcTestPolicy": policy, "rtcTestSTUN": []string{"stun:" + stun4.Address, "stun:" + stun6.Address}, "rtcTestTURN": []string{"turn:" + stun4.Address, "turn:" + stun6.Address}, "rtcTestRemote": remote})
					script := "Object.assign(globalThis," + string(globals) + ");\n" + rtcUnsupportedGathering
					result, err := p.Evaluate(ctx, script)
					if err != nil || result != true {
						t.Fatalf("unsupported transport claimed candidates/success: %v %v", result, err)
					}
					if proxy.Requests.Load() != 0 || stun4.Packets.Load() != 0 || stun6.Packets.Load() != 0 {
						t.Fatal("unsupported transport attempted egress")
					}
				})
			}
		}
	}
}

func TestRTCWithoutProxyPermissionFreeDataChannel(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, rtcPermissionFreeRoundTrip)
		if err != nil || result != true {
			t.Fatalf("unproxied real DataChannel/privacy: %v %v", result, err)
		}
	})
}

func TestRTCWithoutProxyRealSTUNAndRelayPolicy(t *testing.T) {
	serialBrowserTest(t)
	for _, policy := range []string{"all", "relay"} {
		t.Run(policy, func(t *testing.T) {
			stun := rtctest.NewSTUN(t, false)
			p := rtcProxyPage(t, v8engine.Factory{}, "")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			globals, _ := json.Marshal(map[string]any{"rtcTestPolicy": policy, "rtcTestSTUN": "stun:" + stun.Address})
			result, err := p.Evaluate(ctx, "Object.assign(globalThis,"+string(globals)+");\n"+rtcRealSTUN)
			if err != nil || result != true {
				t.Fatalf("real STUN/policy projection: %v %v", result, err)
			}
			if policy == "all" && stun.Packets.Load() == 0 {
				t.Fatal("candidate without STUN transaction")
			}
		})
	}
}

func TestRTCWithoutProxyTURNUDPDataChannel(t *testing.T) {
	serialBrowserTest(t)
	server := rtctest.NewUDPTURN(t)
	p := rtcProxyPage(t, v8engine.Factory{}, "")
	configuration := map[string]any{"iceTransportPolicy": "relay", "iceServers": []any{map[string]any{"urls": "turn:" + server.Address + "?transport=udp", "username": "rtc", "credential": "secret"}}}
	raw, _ := json.Marshal(configuration)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := p.Evaluate(ctx, fmt.Sprintf("globalThis.rtcTestConfig=%s; globalThis.rtcTestPolicyUpdate='relay'; globalThis.rtcLeaveOpen=false;\n%s.catch(e => 'FAIL:'+e.name+':'+e.message)", raw, strings.TrimSuffix(strings.TrimSpace(rtcProxyRoundTrip), ";")))
	if err != nil || strings.HasPrefix(fmt.Sprint(result), "FAIL:") {
		t.Fatalf("real TURN UDP: %v %v", result, err)
	}
	if server.ReadBytes.Load() == 0 || server.WriteBytes.Load() == 0 {
		t.Fatal("TURN did not relay payload")
	}
	rtctest.Wait(t, "TURN UDP allocation teardown", func() bool { return server.Active.Load() == 0 })
}

func TestRTCWithoutProxyPermissionDependentHosts(t *testing.T) {
	serialBrowserTest(t)
	p := rtcProxyPage(t, v8engine.Factory{}, "")
	for _, permission := range []string{"granted", "denied"} {
		if err := p.ctx.SetPermission("null", "camera", permission); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		result, err := p.Evaluate(ctx, fmt.Sprintf("globalThis.rtcNumericHostsPermitted=%t;\n%s", permission == "granted", rtcPermissionGather))
		cancel()
		if err != nil || result != true {
			t.Fatalf("%s local-address privacy: %v %v", permission, result, err)
		}
	}
}
