package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/net/http2"
)

func TestChrome152HTTP2PrioritiesFollowActiveDependencies(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	certificate := certServer.TLS.Certificates[0]
	certServer.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2"}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type observed struct {
		id, dependency uint32
		weight         uint16
		exclusive      bool
	}
	wire := make(chan []observed, 1)
	peerError := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peerError <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		preface := make([]byte, len(http2.ClientPreface))
		if _, err = io.ReadFull(conn, preface); err != nil {
			peerError <- err
			return
		}
		if string(preface) != http2.ClientPreface {
			peerError <- fmt.Errorf("invalid preface")
			return
		}
		framer := http2.NewFramer(conn, conn)
		if err = framer.WriteSettings(http2.Setting{ID: http2.SettingMaxConcurrentStreams, Val: 100}); err != nil {
			peerError <- err
			return
		}
		var rows []observed
		for len(rows) < 15 {
			frame, err := framer.ReadFrame()
			if err != nil {
				peerError <- err
				return
			}
			switch f := frame.(type) {
			case *http2.SettingsFrame:
				if !f.IsAck() {
					if err = framer.WriteSettingsAck(); err != nil {
						peerError <- err
						return
					}
				}
			case *http2.HeadersFrame:
				if !f.HasPriority() {
					peerError <- fmt.Errorf("stream %d has no priority", f.StreamID)
					return
				}
				rows = append(rows, observed{f.StreamID, f.Priority.StreamDep, uint16(f.Priority.Weight) + 1, f.Priority.Exclusive})
				if err = framer.WriteHeaders(http2.HeadersFrameParam{StreamID: f.StreamID, EndHeaders: true, EndStream: f.StreamID == 1 || f.StreamID >= 9, BlockFragment: []byte{0x88}}); err != nil {
					peerError <- err
					return
				}
				if f.StreamID == 7 {
					for _, id := range []uint32{3, 5, 7} {
						if err = framer.WriteData(id, true, nil); err != nil {
							peerError <- err
							return
						}
					}
				}
			}
		}
		wire <- rows
	}()
	transport, err := NewTLSClientTransport(profiles.Chrome_152, tlsclient.WithInsecureSkipVerify(), tlsclient.WithDisableHttp3())
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var held []*http.Response
	for index, resource := range []Request{
		{Initiator: Navigation}, {Initiator: Stylesheet}, {Initiator: Script},
		{Initiator: Image}, {Initiator: Other},
		{Initiator: Script, ScriptNonBlocking: true},
		{Initiator: Fetch, Kind: "font"},
		{Initiator: Script, ScriptNonBlocking: true, FetchPriority: "high"},
		{Initiator: Script, FetchPriority: "low"},
		{Initiator: Image, FetchPriority: "low"},
		{Initiator: Stylesheet, FetchPriority: "low"},
		{Initiator: Script, ScriptNonBlocking: true, FetchPriority: " high "},
		{Initiator: Image, FetchPriority: " high "},
		{Initiator: Stylesheet, FetchPriority: " low "},
		{Initiator: Script, ScriptNonBlocking: true, FetchPriority: "H\u0130GH"},
	} {
		requestContext := withBrowserRequestPriority(withBrowserHeaderLayout(ctx, resource.Initiator, nil), resource)
		request, _ := http.NewRequestWithContext(requestContext, "GET", "https://"+listener.Addr().String(), nil)
		request.Header.Set("Priority", "u=7") // Author wire data must not select the browser priority.
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 || index >= 4 {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		} else {
			held = append(held, response)
		}
		if index == 3 {
			for _, pending := range held {
				if _, err = io.Copy(io.Discard, pending.Body); err != nil {
					t.Fatal(err)
				}
				pending.Body.Close()
			}
		}
	}
	// Chrome 152.0.7977.82, directly launched headful with original webdriver=false.
	// Raw TLS-decrypted HEADERS capture; only the loopback self-signed certificate
	// was accepted through CDP. Priority classes are browser-owned, not HTTP header values.
	want := []observed{
		{1, 0, 256, true}, {3, 0, 256, true}, {5, 3, 220, true},
		{7, 5, 183, true}, {9, 0, 220, true}, {11, 0, 147, true},
		{13, 0, 256, true}, {15, 0, 220, true}, {17, 0, 220, true},
		{19, 0, 147, true}, {21, 0, 220, true},
		{23, 0, 147, true}, {25, 0, 183, true}, {27, 0, 256, true},
		{29, 0, 147, true},
	}
	select {
	case got := <-wire:
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("HTTP/2 priority graph = %v; want %v", got, want)
		}
	case err := <-peerError:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
