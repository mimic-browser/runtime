# BrowserContext ResourcePolicy

ResourcePolicy is an opt-in cost policy for network resources. Without a policy,
resource loading follows the existing Chrome 152 compatibility path. A policy is
owned by one BrowserContext; its Pages, frames, workers and WebSockets use the
same generation. It never changes animations, timers or scheduler behavior.

Pass a JSON document for the current Mimic release with `--resource-policy path.json`, through
`browser.Options.ResourcePolicyJSON`, or as `resourcePolicy` in
`Mimic.createContext`. Existing contexts can be updated with
`Context.UpdateResourcePolicy` or `Mimic.updateResourcePolicy`. Updates are
atomic. A load captures its generation before it starts; redirects retain it.
An update does not cancel loads or clear existing cache entries.

```json
{
  "rules": [
    {
      "id": "drop-visual-assets",
      "match": { "kinds": ["image", "font", "media", "favicon"] },
      "work": { "cacheRead": false, "network": false }
    }
  ]
}
```

Rules are evaluated in order. The first match wins; unspecified work fields
preserve the ordinary behavior. A rule can match kinds, hosts, exact HTTP
origins (including scheme and effective port), URL glob, owner, mechanism and
top-level site. A schemeful `topLevelSite` such as `https://example.com` uses
the registrable domain; a bare hostname matches the exact top-level host.
The kind comes from the cause of the
request: `fetch("photo.png")` is Fetch, even if the response is an image.
Leading-dot host rules match subdomains. Explicit rules precede preset rules.

`cacheRead` controls use of an existing HTTP representation or document image
reuse; `network` controls a new network acquisition. With cache reading allowed
and network disabled, a cache hit succeeds while a miss fails. A document
preload cannot bypass the cache-read decision. Redirect targets are checked
again using the captured generation. The `noVisualAssets`, `headersOnly`,
`dataExtraction` and `noSpeculativeLoads` presets compile to ordinary rules.

`body` can be `full`, `none` or `prefix`; prefix requires a positive
`prefixBytes`. The original GET remains a GET. With `none` or `prefix`, the
loader closes the body after the permitted bytes when bytes remain. Fetch
resolves with the admitted status and headers, but its body stream errors when
read (including through a clone). This permits status/header-only operations
without inventing an empty JSON payload. CORS, redirects and cookies still use
the normal loader. Script, image and XHR consumers require a complete body and
observe a load failure; direct Loader callers receive the partial response and
policy error. A partial response is not cached or retained for CDP body retrieval.
No later API access silently fetches the rest. For a prefix image, intrinsic
dimensions may be extracted into a diagnostic `policyMetadata` trace event;
their source is marked `prefix`, and the image still fails to load.

`decode:false` on an image reads only intrinsic metadata, keeps no encoded body
in the runtime image resource, and denies later pixel materialization. If this
metadata cannot be parsed, the image fails explicitly. Media currently has no
frame decoder, so the flag adds no media work to suppress. `cacheRetain:false`
prevents HTTP cache storage; `debugRetain:false` prevents CDP response-body
history storage. Neither erases the image's minimal runtime state.

MediaSource object URLs refer to browser-owned source state, not an HTTP body or
an empty Blob response. Attachment evaluates the same whole-resource rule as
local Blob media: denying both cache reading and network work blocks it, while
reportOnly records that decision and permits attachment. The local decision
does not acquire a connection or charge HTTP response/body/retention budgets.
Body, decode and retention controls do not erase source state or caller-owned
append input. If a script fetches append input, that Fetch load separately uses
its ordinary resource policy. MediaSource currently models the container
lifecycle, empty appends, and single-track AVC/AAC MP4 initialization metadata.
Coded media fragments remain an explicit unsupported boundary. Initialization
input is discarded after parsing; only observable metadata is retained.
Open-state live seekable range operations are also
unsupported. It does not decode or retain video frames.

`reportOnly:true` executes the ordinary full path and records which network
requests would have been blocked. It does not claim bytes saved for a resource
whose size was never observed. `Mimic.getResourcePolicyStats` reports request
counts, cache hits, policy failures, body bytes consumed and encoded network
body bytes consumed. `knownAvoidedBodyReadBytes` counts only logical body reads
whose size was observed or given by `Content-Length`; it is not a wire-traffic
savings claim. `unknownAvoidedBodyRequests` preserves blocked cases without a
known response size. `wireBytesKnown` remains zero where the transport cannot
report exact on-wire bytes; buffered bytes may exceed the intentional read
boundary. `wouldBypassCache` records report-only decisions to ignore an actual
cache hit.

The implemented Context budgets are `maxRequests`, `maxConcurrent`,
`maxResponseBytes`, `maxBodyBytes`, `maxDecodedBytes`, and
`maxRetainedBytes` (all zero when
unlimited). Request and concurrency budgets
count network acquisitions, not cache hits. Response size is checked before
body reading when Content-Length is available and during full reading when it
is not. The body budget is charged to bytes actually returned by the network
body reader (encoded representation) and to bytes copied from a cache hit; it
does not claim to limit transport buffering. A chunked/unknown-length body that
reaches the exact byte boundary is conservatively reported as budget-limited:
the loader cannot prove EOF without attempting another read. The retained
budget counts unique
immutable response-body storage shared by HTTP cache and CDP history, and
releases its charge when the last owner closes. It does not include the
minimum runtime image state. `reportOnly` records budget violations without
changing delivery. `maxDecodedBytes` limits cumulative logical RGBA pixel
work before image validation and again before late pixel materialization; it
is not an RSS/allocator cap and does not currently cover media frames (which
Mimic does not decode). The schema marks the wire budget as
unsupported; nonzero values are rejected rather than silently ignored.

The policy is a deliberate fidelity tradeoff. The absence of policy must retain
all existing compatibility behavior and tests. New rules should be verified
against the target workload, especially when stopping document, script, style,
worker or Fetch bodies.
