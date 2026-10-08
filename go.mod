module github.com/moreveal/mimic

go 1.26.4

// Keep the winning HTTP/3 race transport in the browser-session pool. The
// upstream v1.16.0 racer recreates it after the first response, forcing an
// observable second connection for the next same-origin resource.
replace github.com/bogdanfinn/tls-client => ./third_party/tls-client

// Expose connection-only HTTP/3 setup so protocol selection cannot replay requests.
replace github.com/bogdanfinn/quic-go-utls => ./third_party/quic-go-utls

// QUIC presets retain live transport parameters and TLS session events.
replace github.com/bogdanfinn/utls => ./third_party/utls

// Native HTMLDDA flags and complete accessor descriptors for document.all.
replace github.com/maclof/gov8 => ./third_party/gov8

// Keep horizontal font metrics while releasing unused static glyph outlines.
replace github.com/go-text/typesetting => ./third_party/go-text-typesetting

// Bundle only the platform camera drivers; no external capture process or codecs.
replace github.com/pion/mediadevices => ./third_party/mediadevices

require (
	github.com/andybalholm/brotli v1.2.5
	github.com/bogdanfinn/fhttp v0.6.9
	github.com/bogdanfinn/quic-go-utls v1.0.10-utls
	github.com/bogdanfinn/tls-client v1.16.0
	github.com/bogdanfinn/utls v1.7.8-barnius
	github.com/bogdanfinn/websocket v1.5.6-barnius
	github.com/buke/quickjs-go v0.7.7
	github.com/dop251/goja v0.0.0-20260926152631-39ec2650adc9
	github.com/ebitengine/purego v0.10.0
	github.com/gen2brain/gav1d v0.2.5
	github.com/gen2brain/malgo v0.11.26
	github.com/go-text/typesetting v0.3.4
	github.com/go-webengine/engine v0.4.3
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/klauspost/compress v1.18.7
	github.com/maclof/gov8 v0.1.1
	github.com/pion/interceptor v0.1.49
	github.com/pion/mediadevices v0.10.0
	github.com/pion/opus v0.1.1-0.20261005072002-44637de087b3
	github.com/pion/rtcp v1.2.18
	github.com/pion/rtp v1.10.5
	github.com/pion/webrtc/v4 v4.2.22
	github.com/tdewolff/font v0.0.0-20241125190050-d899fdc808fc
	github.com/woozymasta/bcn v0.7.0
	github.com/y9o/go-openh264 v0.2.0
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.46.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
)

require (
	github.com/ajroetker/go-highway v0.0.4 // indirect
	github.com/bdandy/go-errors v1.2.2 // indirect
	github.com/bdandy/go-socks4 v1.2.3 // indirect
	github.com/blackjack/webcam v0.6.1 // indirect
	github.com/breml/rootcerts v0.3.7 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/evanw/esbuild v0.28.2 // indirect
	github.com/go-browserhttp/browserhttp v0.2.0 // indirect
	github.com/go-gfx/gfx v0.34.0 // indirect
	github.com/go-images/gif v0.1.0 // indirect
	github.com/go-images/images v0.0.0-20260927173152-87444e36aac4 // indirect
	github.com/go-images/jpeg v0.2.0 // indirect
	github.com/go-images/jpeg2000 v0.1.0 // indirect
	github.com/go-images/png v0.1.0 // indirect
	github.com/go-opentype/fonts v0.10.0 // indirect
	github.com/go-opentype/opentype v0.13.1-0.20260927180318-ae6327b14eac // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/go-webengine/esbuildsandbox v0.1.0 // indirect
	github.com/go-widgets/painter v0.13.0 // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/pion/datachannel v1.6.3 // indirect
	github.com/pion/dtls/v3 v3.1.9 // indirect
	github.com/pion/ice/v4 v4.4.4 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.2.1 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.11.3 // indirect
	github.com/pion/sdp/v3 v3.0.20 // indirect
	github.com/pion/srtp/v3 v3.1.0 // indirect
	github.com/pion/stun/v4 v4.0.1 // indirect
	github.com/pion/transport/v5 v5.1.1 // indirect
	github.com/pion/turn/v5 v5.1.2 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/sergeymakinen/go-bmp v1.0.0 // indirect
	github.com/sergeymakinen/go-ico v1.0.0 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/tam7t/hpkp v0.0.0-20160821193359-2b70b4024ed5 // indirect
	github.com/tannevaled/gobig2 v0.2.0 // indirect
	github.com/tdewolff/parse/v2 v2.7.14-0.20240511005308-a1dd1e88845b // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/time v0.14.0 // indirect
)
