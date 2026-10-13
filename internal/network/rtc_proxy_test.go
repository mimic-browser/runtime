package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/rtctest"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func proxyPeer(t *testing.T, dialer *RTCProxyDialer, config webrtc.Configuration) *webrtc.PeerConnection {
	t.Helper()
	settings := webrtc.SettingEngine{}
	settings.SetNet(RTCIsolatedNet{})
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settings.SetICEProxyDialer(dialer)
	settings.SetICETimeouts(500*time.Millisecond, 3*time.Second, 100*time.Millisecond)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dialer.Close(); pc.Close() })
	return pc
}

func gather(t *testing.T, pc *webrtc.PeerConnection, description webrtc.SessionDescription) webrtc.SessionDescription {
	t.Helper()
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(description); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("ICE gathering timeout")
	}
	return *pc.LocalDescription()
}

func relayExchange(t *testing.T, pc *webrtc.PeerConnection) string {
	t.Helper()
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	// Explicit loopback sockets keep this peer fixture off physical interfaces.
	// Pion's interface enumerator excludes ::1 as IPv4-compatible IPv6.
	udp4, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	udp6, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		udp4.Close()
		t.Fatal(err)
	}
	mux := ice.NewMultiUDPMuxDefault(ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: udp4}), ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: udp6}))
	defer mux.Close()
	settings.SetICEUDPMux(mux)
	remote, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { remote.Close() })
	remote.OnDataChannel(func(channel *webrtc.DataChannel) {
		channel.OnMessage(func(message webrtc.DataChannelMessage) { channel.SendText("echo:" + string(message.Data)) })
	})
	channel, err := pc.CreateDataChannel("transport-proof", nil)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan string, 1)
	channel.OnOpen(func() { channel.SendText("actual-payload") })
	channel.OnMessage(func(message webrtc.DataChannelMessage) { finished <- string(message.Data) })
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	local := gather(t, pc, offer)
	if !strings.Contains(local.SDP, "typ relay") {
		t.Fatalf("no real allocation: %s", local.SDP)
	}
	if err := remote.SetRemoteDescription(local); err != nil {
		t.Fatal(err)
	}
	answer, err := remote.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetRemoteDescription(gather(t, remote, answer)); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-finished:
		if message != "echo:actual-payload" {
			t.Fatal(message)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("DataChannel failed: %s / %s\nlocal: %s\nremote: %s", pc.ConnectionState(), remote.ConnectionState(), pc.LocalDescription().SDP, remote.LocalDescription().SDP)
	}
	return local.SDP
}

func TestRTCProxyRelayEgressMatrix(t *testing.T) {
	cert, roots := rtctest.Certificate(t)
	for _, scheme := range []string{"http", "https", "socks5"} {
		for _, secure := range []bool{false, true} {
			for _, ipv6 := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/turnTLS=%t/ipv6=%t", scheme, secure, ipv6), func(t *testing.T) {
					server := rtctest.NewTURN(t, ipv6, secure, cert)
					proxy := rtctest.NewProxy(t, scheme, "", cert)
					_, port, _ := net.SplitHostPort(server.Address)
					endpoint := "turn.fixture.invalid:" + port
					proxy.Targets[endpoint] = server.Address
					dialer, err := NewRTCProxyDialer(context.Background(), proxy.URL)
					if err != nil {
						t.Fatal(err)
					}
					dialer.proxy.tlsConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
					dialer.turnTLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
					dialer.SetServers(map[string]bool{endpoint: secure})
					prefix := "turn:"
					if secure {
						prefix = "turns:"
					}
					pc := proxyPeer(t, dialer, webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyRelay, ICEServers: []webrtc.ICEServer{{URLs: []string{prefix + endpoint + "?transport=tcp"}, Username: "rtc", Credential: "secret"}}})
					sdp := relayExchange(t, pc)
					if strings.Contains(sdp, "127.0.0.2") || strings.Contains(sdp, " typ host") || strings.Contains(sdp, " typ srflx") {
						t.Fatal("tunnel endpoint leaked into ICE", sdp)
					}
					if server.Direct.Load() != 0 || server.Allocations.Load() == 0 || server.ReadBytes.Load() == 0 || server.WriteBytes.Load() == 0 || proxy.Auth.Load() == 0 {
						t.Fatalf("egress proof: direct=%d allocations=%d read=%d write=%d auth=%d", server.Direct.Load(), server.Allocations.Load(), server.ReadBytes.Load(), server.WriteBytes.Load(), proxy.Auth.Load())
					}
					dialer.Close()
					pc.Close()
					rtctest.Wait(t, "TURN allocation teardown", func() bool { return server.Active.Load() == 0 })
					t.Logf("verified actual relay traffic: allocations=%d read=%d write=%d CONNECT=%d", server.Allocations.Load(), server.ReadBytes.Load(), server.WriteBytes.Load(), proxy.Requests.Load())
				})
			}
		}
	}
}

func TestRTCProxyFailuresNeverFallback(t *testing.T) {
	cert, _ := rtctest.Certificate(t)
	for _, scenario := range []struct {
		scheme, failure string
		turnTLS         bool
	}{{"http", "auth", false}, {"http", "deny", false}, {"http", "stall", false}, {"https", "", false}, {"socks5", "auth", false}, {"socks5", "deny", false}, {"socks5", "stall", false}, {"http", "", true}} {
		t.Run(fmt.Sprintf("%s/%s/turnTLS=%t", scenario.scheme, scenario.failure, scenario.turnTLS), func(t *testing.T) {
			server := rtctest.NewTURN(t, false, scenario.turnTLS, cert)
			proxy := rtctest.NewProxy(t, scenario.scheme, scenario.failure, cert)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			dialer, err := NewRTCProxyDialer(ctx, proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			dialer.SetServers(map[string]bool{server.Address: scenario.turnTLS})
			if _, err := dialer.Dial("udp", server.Address); err != ErrRTCDirectTransport {
				t.Fatal("UDP escaped stream-only dialer", err)
			}
			if _, err := dialer.Dial("tcp", "unconfigured.fixture.invalid:3478"); err != ErrRTCDirectTransport {
				t.Fatal("unconfigured endpoint reached proxy", err)
			}
			prefix := "turn:"
			if scenario.turnTLS {
				prefix = "turns:"
			}
			pc := proxyPeer(t, dialer, webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyAll, ICEServers: []webrtc.ICEServer{{URLs: []string{prefix + server.Address + "?transport=tcp"}, Username: "rtc", Credential: "secret"}}})
			pc.CreateDataChannel("failure", nil)
			offer, err := pc.CreateOffer(nil)
			if err != nil {
				t.Fatal(err)
			}
			local := gather(t, pc, offer)
			if strings.Contains(local.SDP, "a=candidate:") || server.Direct.Load() != 0 || server.Allocations.Load() != 0 {
				t.Fatal("proxy failure escaped isolation", local.SDP)
			}
		})
	}
}

func TestRTCProxyCloseCancelsPendingHandshake(t *testing.T) {
	cert, _ := rtctest.Certificate(t)
	for _, scheme := range []string{"http", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			proxy := rtctest.NewProxy(t, scheme, "stall", cert)
			dialer, err := NewRTCProxyDialer(context.Background(), proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			dialer.SetServers(map[string]bool{"blocked.fixture.invalid:3478": false})
			done := make(chan error, 1)
			go func() {
				conn, err := dialer.Dial("tcp", "blocked.fixture.invalid:3478")
				if conn != nil {
					conn.Close()
				}
				done <- err
			}()
			rtctest.Wait(t, "proxy handshake started", func() bool { return proxy.Requests.Load() > 0 })
			dialer.Close()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled handshake succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("Close retained a pending handshake")
			}
		})
	}
}

func TestRTCProxyTURNAuthenticationFailure(t *testing.T) {
	cert, _ := rtctest.Certificate(t)
	server := rtctest.NewTURN(t, false, false, cert)
	proxy := rtctest.NewProxy(t, "http", "", cert)
	dialer, err := NewRTCProxyDialer(context.Background(), proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer.SetServers(map[string]bool{server.Address: false})
	pc := proxyPeer(t, dialer, webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyAll, ICEServers: []webrtc.ICEServer{{URLs: []string{"turn:" + server.Address + "?transport=tcp"}, Username: "rtc", Credential: "incorrect"}}})
	pc.CreateDataChannel("invalid-turn-auth", nil)
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	local := gather(t, pc, offer)
	if strings.Contains(local.SDP, "a=candidate:") || server.Direct.Load() != 0 || server.Allocations.Load() != 0 || proxy.Auth.Load() == 0 {
		t.Fatal("TURN authentication failure bypassed proxy")
	}
}

func TestRTCIsolatedNetRejectsEveryDirectBoundary(t *testing.T) {
	var isolated RTCIsolatedNet
	sink4, sink6 := rtctest.NewSink(t, false), rtctest.NewSink(t, true)
	for _, address := range []string{sink4.Address, sink6.Address, "unresolved.fixture.invalid:3478"} {
		if _, err := isolated.Dial("udp", address); err != ErrRTCDirectTransport {
			t.Fatal(err)
		}
		if _, err := isolated.Dial("tcp", address); err != ErrRTCDirectTransport {
			t.Fatal(err)
		}
		if _, err := isolated.ResolveUDPAddr("udp", address); err != ErrRTCDirectTransport {
			t.Fatal(err)
		}
		if _, err := isolated.ResolveTCPAddr("tcp", address); err != ErrRTCDirectTransport {
			t.Fatal(err)
		}
		if _, err := isolated.CreateDialer(&net.Dialer{}).Dial("tcp", address); err != ErrRTCDirectTransport {
			t.Fatal(err)
		}
	}
	if _, err := isolated.ResolveIPAddr("ip", "unresolved.fixture.invalid"); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.DialUDP("udp", nil, &net.UDPAddr{}); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.DialTCP("tcp", nil, &net.TCPAddr{}); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.ListenUDP("udp", nil); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.ListenTCP("tcp", nil); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.ListenPacket("udp", ":0"); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.CreateListenConfig(&net.ListenConfig{}).Listen(context.Background(), "tcp", ":0"); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	if _, err := isolated.CreateListenConfig(&net.ListenConfig{}).ListenPacket(context.Background(), "udp", ":0"); err != ErrRTCDirectTransport {
		t.Fatal(err)
	}
	// Deliberately give Pion all policy and forbidden UDP servers, bypassing the
	// browser's URL filter. The network implementation itself must stop egress.
	settings := webrtc.SettingEngine{}
	settings.SetNet(isolated)
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:" + sink4.Address, "stun:" + sink6.Address}}, {URLs: []string{"turn:" + sink4.Address, "turn:" + sink6.Address}, Username: "rtc", Credential: "secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.CreateDataChannel("forbidden", nil)
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if local := gather(t, pc, offer); strings.Contains(local.SDP, "a=candidate:") {
		t.Fatal(local.SDP)
	}
	if sink4.Packets.Load() != 0 || sink6.Packets.Load() != 0 {
		t.Fatal("direct UDP egress")
	}
}

func TestRTCProxyRotationAndBrokenAllocation(t *testing.T) {
	cert, _ := rtctest.Certificate(t)
	server := rtctest.NewTURN(t, false, false, cert)
	proxy := rtctest.NewProxy(t, "http", "", cert)
	newPeer := func() (*webrtc.PeerConnection, *RTCProxyDialer) {
		dialer, err := NewRTCProxyDialer(context.Background(), proxy.URL)
		if err != nil {
			t.Fatal(err)
		}
		dialer.SetServers(map[string]bool{server.Address: false})
		return proxyPeer(t, dialer, webrtc.Configuration{ICETransportPolicy: webrtc.ICETransportPolicyRelay, ICEServers: []webrtc.ICEServer{{URLs: []string{"turn:" + server.Address + "?transport=tcp"}, Username: "rtc", Credential: "secret"}}}), dialer
	}
	first, firstDialer := newPeer()
	relayExchange(t, first)
	one := first.LocalDescription().SDP
	proxy.Exit.Store("127.0.0.3")
	server.RelayIP.Store("127.0.0.3")
	second, secondDialer := newPeer()
	relayExchange(t, second)
	two := second.LocalDescription().SDP
	if !strings.Contains(two, "127.0.0.3") || rtcCandidateLines(first.LocalDescription().SDP) != rtcCandidateLines(one) || rtcCandidateLines(two) == rtcCandidateLines(one) {
		t.Fatalf("rotation rewrote or reused an old allocation: old %s current %s new %s", rtcCandidateLines(one), rtcCandidateLines(first.LocalDescription().SDP), rtcCandidateLines(two))
	}
	requests := proxy.Requests.Load()
	proxy.Drop()
	rtctest.Wait(t, "broken allocation cannot remain connected", func() bool {
		return first.ConnectionState() == webrtc.PeerConnectionStateFailed && second.ConnectionState() == webrtc.PeerConnectionStateFailed
	})
	if proxy.Requests.Load() != requests || server.Direct.Load() != 0 || strings.Contains(first.LocalDescription().SDP, "127.0.0.3") {
		t.Fatal("broken allocation silently changed routes")
	}
	firstDialer.Close()
	first.Close()
	secondDialer.Close()
	second.Close()
	rtctest.Wait(t, "broken allocation teardown", func() bool { return server.Active.Load() == 0 })
}

func rtcCandidateLines(description string) string {
	lines := []string{}
	for _, line := range strings.Split(description, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
