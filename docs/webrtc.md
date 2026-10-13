# WebRTC transport

Mimic uses one native Pion peer for offers, answers, ICE gathering, statistics,
DataChannels and supported media. Creating an offer does not open a camera or
microphone. Candidates describe actual sockets or TURN allocations; environment
profiles cannot select candidate addresses, counts, ports or gathering timing.

## Context proxies

A Context proxy enforces a stricter transport policy than normal Chrome's HTTP
proxy setting. Page scripts cannot remove this policy with `iceTransportPolicy`,
`setConfiguration` or `restartIce`.

| Context route | Available WebRTC transport |
| --- | --- |
| No proxy | Native UDP ICE, STUN, TURN, DTLS/SRTP/SCTP and DataChannels |
| HTTP proxy | TURN over TCP through authenticated HTTP CONNECT |
| HTTPS proxy | TURN over TCP through TLS-protected HTTP CONNECT |
| SOCKS5 proxy | TURN over TCP through authenticated SOCKS5 CONNECT |
| Any supported proxy | TURN over TLS through the same proxy tunnel |

Use an application-provided TURN server that supports UDP relay allocations over
a TCP or TLS control connection:

```javascript
const peer = new RTCPeerConnection({
  iceServers: [{
    urls: ['turns:turn.example.com:5349?transport=tcp'],
    username: turnUsername,
    credential: turnPassword,
  }],
});
const channel = peer.createDataChannel('messages');
await peer.setLocalDescription(await peer.createOffer());
// Exchange descriptions and trickled candidates with the remote peer.
```

Both the HTTPS proxy certificate and the TURN TLS certificate must pass their
own hostname and system-trust checks. Proxy credentials belong to Context proxy
configuration; TURN credentials belong to `iceServers`. TLS verification of the
proxy is independent of origin HTTP settings.

Plain TURN TCP and TURN TLS URLs for the same host and port are unsupported in
one proxied configuration: Pion's stream hook cannot distinguish those routes.
Use distinct endpoints for the two protocols.

With a proxy, local host gathering, direct ICE TCP/UDP, STUN, TURN UDP/DTLS,
multicast DNS and SOCKS5 UDP ASSOCIATE are unavailable. Unsupported server URLs
cannot open sockets or resolve ICE names on the host. TURN stream hostnames go
unchanged to the proxy for resolution. Resolving the configured proxy itself is
still necessary to connect to it.

Gathering can complete with no candidates when there is no usable TURN route.
A connection cannot succeed without an actual allocation and working ICE/DTLS
transport. Authentication failure, denied CONNECT, timeouts and TLS failure do
not retry directly. `getConfiguration()` preserves the author's requested
policy; the Context enforces relay transport internally.

Each allocation keeps its original tunnel and relay endpoint. Rotating proxies
affect new connections. Breaking an active tunnel causes transport failure;
Mimic does not silently migrate its allocation or substitute a new exit IP.
Renegotiation with an ICE restart can establish a new allocation where supported.
Closing the peer or discarding its document cancels pending proxy handshakes and
closes the allocation tunnels.

## Address privacy and compatibility limits

Without a proxy, peers created without camera or microphone permission advertise
real mDNS host candidates and expose blank host `address`/`ip` fields in stats.
Host privacy serialization also removes related interface addresses from local
candidate events and local SDP. If mDNS is unavailable, numeric host candidates
are withheld from signaling. Media permission at peer creation permits native
numeric host observations. Granting permission later does not replace existing
mDNS allocations.

With a proxy, only actual relay endpoints are exposed locally. The client socket
to the proxy is not an ICE endpoint and is not published as a related address.
Remote candidates supplied by the application remain remote signaling data.
Local candidate events, SDP and stats use the same allocation addresses and ports.

This is a functioning WebRTC transport, not complete Chrome SDP or conferencing
compatibility. Native offers advertise the implemented H264/Opus media boundary
rather than Chrome's full offline codec catalog. Native SDP ordering, ICE
priorities, candidate enumeration, some stats fields and lifecycle timing differ
from Chrome 152. Replacing a pending local offer and signaling rollback remain
unsupported by the native backend. ICE TCP, simulcast, insertable streams and unsupported media
codecs are outside the current implementation. See [media support](camera.md).

The frozen Chrome SDP catalog regression currently detects these native-offer
differences. Its expectations remain unchanged. Chrome's ordinary HTTP proxy can
still send direct STUN; Mimic intentionally applies stronger Context isolation.

## Reproducible network checks

Run only the focused transport and browser checks:

```text
go test ./internal/network -run '^(TestRTCProxyRelayEgressMatrix|TestRTCProxyFailuresNeverFallback|TestRTCIsolatedNetRejectsEveryDirectBoundary|TestRTCProxyRotationAndBrokenAllocation|TestRTCProxyCloseCancelsPendingHandshake|TestRTCProxyTURNAuthenticationFailure)$' -count=1
go test ./internal/browser -run '^(TestRTCProxyBrowserRoundTripAndTeardown|TestRTCProxyBrowserUnsupportedGathering|TestRTCWithoutProxyPermissionFreeDataChannel|TestRTCWithoutProxyRealSTUNAndRelayPolicy|TestRTCWithoutProxyTURNUDPDataChannel|TestRTCWithoutProxyPermissionDependentHosts)$' -count=1
```

The fixtures transfer real DataChannel payloads through TURN TCP/TLS and
HTTP/HTTPS/SOCKS5 proxies, including IPv6 relay endpoints. They count relay reads
and writes, independently distinguish IPv4 direct connections from proxy
connections by source address, observe forbidden IPv4/IPv6 UDP listeners and
check allocation teardown. HTTPS/TURN TLS positive fixtures install private
test trust roots in the package's dialer; production uses system trust.
These checks do not establish compatibility with every third-party proxy or
TURN service.
