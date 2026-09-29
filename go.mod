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

require (
	github.com/andybalholm/brotli v1.2.0
	github.com/bogdanfinn/fhttp v0.6.9
	github.com/bogdanfinn/tls-client v1.16.0
	github.com/bogdanfinn/utls v1.7.8-barnius
	github.com/dop251/goja v0.0.0-20260906210903-70ad66ec7ce4
	github.com/gen2brain/gav1d v0.2.5
	github.com/go-text/typesetting v0.3.4
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/klauspost/compress v1.18.2
	github.com/maclof/gov8 v0.1.1
	github.com/tdewolff/font v0.0.0-20241125190050-d899fdc808fc
	github.com/woozymasta/bcn v0.7.0
	golang.org/x/image v0.23.0
	golang.org/x/net v0.51.0
	golang.org/x/sys v0.47.0
)

require (
	github.com/bdandy/go-errors v1.2.2 // indirect
	github.com/bdandy/go-socks4 v1.2.3 // indirect
	github.com/bogdanfinn/quic-go-utls v1.0.10-utls // indirect
	github.com/bogdanfinn/websocket v1.5.6-barnius // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/tam7t/hpkp v0.0.0-20160821193359-2b70b4024ed5 // indirect
	github.com/tdewolff/parse/v2 v2.7.14-0.20240511005308-a1dd1e88845b // indirect
	golang.org/x/crypto v0.48.0 // indirect
)

require (
	github.com/buke/quickjs-go v0.7.7
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/text v0.34.0
)
