# Web platform compatibility

Mimic models script-observable browser behavior against frozen Chrome 152.
The focused fixtures in `internal/browser/testdata/*_chrome152.json` retain
the reference observations and capture provenance. Their paired JavaScript
fixtures cover Window behavior and, where applicable, dedicated Workers.

## Shared mechanisms

- URL construction and component setters use the WHATWG URL parser, including
  host canonicalization, IDNA, default ports, special-scheme backslashes and
  relative resolution. `URLSearchParams` retains its identity across updates.
- Filtered `:nth-child()` and `:nth-last-child()` count matching siblings using
  the same selector engine as queries, nested selectors and stylesheet matching.
  Shadow host rules use the owning shadow root and its adopted stylesheets.
- Generated pseudo-element content reads the live cascade and attributes.
  Registered custom properties validate syntax and apply inheritance and initial
  values through variable substitution. Computed style maps expose live reads
  with numeric, keyword and unparsed value objects.
- Range extraction moves fully selected nodes and preserves their identity;
  partial text and ancestor boundaries determine extraction and surrounding.
- IndexedDB activity follows the innermost executing Page task, including nested
  realm execution. Storage mutations dispatch asynchronous events to other
  eligible same-origin documents, excluding unchanged writes and the source.
- Text decoding supports the Encoding Standard label table through Go's
  `x/text` codecs, with streaming state, BOM handling and fatal decoding.
  BufferSource consumers use intrinsic view boundaries rather than author
  overrides of `buffer`, `byteOffset` or `byteLength`.

## Cryptography

Window and Worker share one WebCrypto implementation. Key material is private;
operations check algorithm compatibility, usages and extractability. The
implementation uses Go's standard cryptography packages and `x/crypto`:

- SHA-1, SHA-256, SHA-384 and SHA-512 digests;
- HMAC generation, raw/JWK import and export, signing and verification;
- AES-GCM, AES-CBC, AES-CTR and AES-KW generation and raw/JWK import/export,
  encryption/decryption or key wrapping as appropriate;
- PBKDF2 and HKDF derivation, including derived symmetric keys;
- RSA-OAEP public SPKI import and encryption.

This does not imply support for every WebCrypto algorithm. In particular,
asymmetric key generation, private RSA operations and asymmetric signing are
outside this implementation's scope.

## Boundaries

Mimic does not render transition snapshots. `startViewTransition()` runs and
awaits the author's DOM update callback, preserves promise identity and callback
errors, and supports skipping. Snapshot readiness rejects with
`NotSupportedError` for a visible document, or `InvalidStateError` for a hidden
document. Successful DOM updates still fulfill `updateCallbackDone` and
`finished`.

Computed Typed OM currently provides numeric, keyword, base and unparsed value
observations. Full arithmetic and transform objects and writable Typed OM are
not implemented.

## Focused validation

```sh
go test ./internal/browser -run '^(TestWebCryptoURLSelectorsChromeOracle|TestCSSDOMObservationsChromeOracle|TestEncodingStorageCryptoChromeOracle|TestWorkerCryptoEncodingChromeOracle|TestIndexedDBUpgradeUsesInnermostPageTask|TestViewTransitionDOMUpdateLifecycle|TestBufferSourceUsesIntrinsicViewBounds|TestDefaultFormStateReachesStyleBeforeJavaScriptReads)$' -count=1
```

These tests run the supported Goja and V8 engines and compare the captured
observations or check focused lifecycle invariants. They are not a claim of
complete Web Platform Tests conformance.
