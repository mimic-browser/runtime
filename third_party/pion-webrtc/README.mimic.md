# Pion WebRTC transport integration

This directory contains the production Go sources of
`github.com/pion/webrtc/v4` v4.2.22, under its original MIT license. Tests,
examples and upstream project tooling are excluded. Dependency versions remain
those of the pinned module.

The upstream module checksum is
`h1:eoRTHzLKW5tI9o5iahw3s4KL4IHPESsfIF06zWi3JAo=`; its `go.mod` checksum is
`h1:JBu1ba8lUEX4jbYFKofglMQJG7f0rJk7UepKjI3GaLE=`. Both are preserved in
the repository's root `go.sum`.

Mimic's optional SettingEngine hooks let native offer and answer generation
apply serialization conventions before recording the accepted SDP, reserve
unapplied offer MIDs, and avoid gathering ICE for descriptions with no media.
The data description operations option also stages actual ICE credentials while
generating offers, installs them only when an offer is accepted, permits pending
offer replacement, and supports data-only rollback while the active transport
has not restarted. Candidate observations remain bound to their credential
generation. Generated native ICE credentials use the measured Chrome character
lengths and cryptographic randomness, including initial answerer credentials.
Media rollback and rollback after applying an ICE restart are
explicitly unsupported rather than partially restoring descriptions.

Defaults retain upstream offer and description behavior. Candidate population
rejects mismatched credential generations for every caller. The native
descriptions, transceiver MIDs and ICE agent remain authoritative; there is no
translated SDP shadow state. Modified upstream files are `peerconnection.go`,
`settingengine.go`, `icegatherer.go`, `icetransport.go` and `sdp.go`; additions use the `mimic_`
filename prefix.
