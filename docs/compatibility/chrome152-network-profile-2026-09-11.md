# Chrome 152 direct TLS / QUIC observations

Reference: Windows x64, headful Chrome for Testing **152.0.7977.82**, fresh
profile, local trusted server. Reference executable SHA-256:
`ea36dd818a90176f1a70616f0363d9be527229389a6c073a0b1688b9e73f67e9`.
Full capture metadata and normalized observations are retained in
`internal/network/testdata/chrome152_network_wire.json`.

## Implemented and verified

The Chrome 152 QUIC path now uses the selected uTLS ClientHello, with live QUIC
transport parameters and real session events. It previously used the generic
TLS QUIC client. Cold, reused and resumed HTTP/3 requests complete against a
local server with certificate verification enabled, on IPv4 and IPv6.

| Observation | Verified result |
| --- | --- |
| TCP TLS 1.3 cold and resumed | Captured extension payloads and JA4 match the frozen reference; server confirms resumption |
| QUIC v1 cold and resumed | Normalized received ClientHellos match; server confirms resumption |
| QUIC ciphers and signatures | Separate QUIC list, rather than the TCP list; no TLS GREASE slots; empty legacy session ID |
| TLS extensions | Windows trust-anchor list, ALPN/ALPS, supported groups/versions, key-share sizes, compression, padding and ECH GREASE structure |
| Transport parameters | Flow-control windows, stream counts, idle timeout, datagrams, default omission, version information and Google connection options |
| Warm connection RTT | Remembered measured RTT, in microseconds, advertised as `0x3127` and used by loss recovery; bounded cache owned by the client |
| Initial packet / connection ID | IPv4 1250 bytes, IPv6 1230 bytes; empty source connection ID with per-connection UDP ownership |
| HTTP/3 control | SETTINGS, randomized GREASE, then per-request PRIORITY_UPDATE with the actual stream ID and priority |
| Connection reuse | Second same-origin request reuses the connection; closing idle connections permits session resumption |
| Protocol race | Existing selection-before-request behavior retained; losing-handshake cancellation and single POST delivery regressions pass |

JA4 for a DNS name with SNI:

| Connection | JA4 |
| --- | --- |
| TCP cold | `t13d1518h2_8daaf6152771_4980c97edce0` |
| TCP resumed | `t13d1519h2_8daaf6152771_3d1b1b7bef36` |
| QUIC cold | `q13d0313h3_55b375c5d22e_eb028bd37c08` |
| QUIC resumed | `q13d0314h3_55b375c5d22e_40246181ac92` |

SETTINGS order is `1=65536`, `6=262144`, `7=100`, `0x33=1`, then a fresh GREASE
setting. GREASE frame payload length is `(N % 4)` for frame type `31*N+33`;
the multi-connection frozen capture contains lengths 0, 1, 2 and 3. This also
agrees with QUICHE's
[HttpEncoder::SerializeGreasingFrame](https://github.com/google/quiche/blob/main/quiche/quic/core/http/http_encoder.cc).
The reference capture, not the moving upstream branch, supplies the expectations.

The Initial crypto-stream ordering bug for an absent SNI was fixed: a missing
extension no longer sorts ahead of a valid ECH offset and stalls the connection.
uTLS handshake errors now reach QUIC cleanup, and session-event draining permits
ticket storage and resumption. Failed address/socket setup closes its UDP socket.

## Validation

```text
go test ./internal/network ./chrome/152 github.com/bogdanfinn/tls-client github.com/bogdanfinn/quic-go-utls github.com/bogdanfinn/quic-go-utls/http3
go test -race ./internal/network github.com/bogdanfinn/tls-client
```

These checks pass. The QUIC package includes a cache-isolation regression;
the new root network tests exercise QUIC and HTTP/3 through actual sockets and captured bytes.
The protocol-race suite includes real QUIC, slow POST responses, streaming
bodies, cancellation and no replay after an uncertain application error.
Recursive testing of every upstream QUIC example is not a passing gate here:
the interop client requires an absent `golang.org/x/sync/errgroup` checksum.

Use `tools/networkprobe/README.md` to reproduce reference captures and comparison.
The loopback H3 reference needs `--origin-to-force-quic-on`; this flag and
temporary local trust are recorded rather than presented as ordinary public
Internet discovery. An unforced cold capture had the same normalized QUIC
ClientHello. Local tests cover the production direct H3 transport path.

## Remaining compatibility boundary

This is verified handshake and initial control-profile compatibility, **not
complete byte-for-byte Chrome network equivalence**. Random key material,
GREASE, permutations, tickets and measured timing intentionally vary.

The inherited dynamic QPACK receive gap was repaired on 2026-09-13. Each client
connection now owns a bounded dynamic table, consumes encoder instructions,
unblocks dependent field sections and sends insertion/section acknowledgments
and blocked-stream cancellations. The request encoder remains static-only.
RFC 9204 vectors and a real local HTTP/3 peer cover dynamic response headers,
trailers and table reuse across two POSTs, including instructions arriving after
the referring header block. Existing Chrome SETTINGS expectations are unchanged.
The net/http adapter now projects populated trailers after body EOF. See
[implementation and live results](google-signin-20260913.md).

QUIC CRYPTO fragmentation/coalescing, ACK scheduling, congestion control and
all packet-level timing have not been matched to Chrome. Zero-RTT application
data, Retry, ECH configuration negotiation and arbitrary experiment states are
not established by these cold/resumption tests. Proxy, loss, migration and path
validation scenarios remain outside the measured coverage.
