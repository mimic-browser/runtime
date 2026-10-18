package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRTCOfferUsesEnvironmentCodecCatalog(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		for _, kind := range []string{"audio", "video"} {
			for _, direction := range []string{"sendrecv", "recvonly"} {
				result, err := p.Evaluate(context.Background(), fmt.Sprintf(`(async()=>{
                const peer = new RTCPeerConnection();
                try { peer.addTransceiver(%q, {direction:%q}); return (await peer.createOffer()).sdp.split('\r\n').find(line=>line.startsWith('m=%s ')); }
                finally { peer.close(); }
            })()`, kind, direction, kind))
				if err != nil {
					t.Fatal(err)
				}
				media := p.environmentView().Capabilities.Media.RTP.Media(kind, direction)
				want := strings.TrimSpace(fmt.Sprint(media.Payloads))
				want = strings.Trim(want, "[]")
				expected := "m=" + kind + " 9 UDP/TLS/RTP/SAVPF " + want
				if result != expected {
					t.Fatalf("%s %s offer: got %v; want %s", kind, direction, result, expected)
				}
			}
		}
	})
}

func TestRTCOfferCodecLinesMatchFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	raw, err := os.ReadFile("testdata/rtp_direction_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string][]string
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	cameraTestPages(t, func(t *testing.T, p *Page) {
		for _, kind := range []string{"audio", "video"} {
			for _, direction := range []string{"sendrecv", "recvonly"} {
				value, err := p.Evaluate(context.Background(), fmt.Sprintf(`(async()=>{
                  const peer = new RTCPeerConnection();
                  try { peer.addTransceiver(%q, {direction:%q});
                    const lines = (await peer.createOffer()).sdp.split('\r\n');
                    return JSON.stringify(lines.filter(s=>/^(m=|a=(extmap:|rtpmap:|fmtp:|rtcp-fb:|rtcp-xr:))/.test(s)));
                  } finally { peer.close(); }
                })()`, kind, direction))
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
					t.Fatal(err)
				}
				want := expected[kind+"|"+direction]
				if !reflect.DeepEqual(got, want) {
					for index := range got {
						if index >= len(want) || got[index] != want[index] {
							t.Fatalf("%s %s SDP line %d: got %q; want %q (lengths %d/%d)", kind, direction, index, got[index], func() string {
								if index < len(want) {
									return want[index]
								}
								return "<missing>"
							}(), len(got), len(want))
						}
					}
					t.Fatalf("%s %s SDP line count: got %d; want %d", kind, direction, len(got), len(want))
				}
			}
		}
	})
}
