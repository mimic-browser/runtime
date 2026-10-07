# Generated CDP protocol

`tools/generate_cdp.py` projects the complete machine-readable protocol exposed
by the pinned Chrome 152.0.7977.82 browser into:

- `internal/cdp/protocol_registry_generated.go`: all command parameters, results,
  event fields and referenced type descriptors, including nested arrays and
  cross-domain references. The registry has 58 domains, 665 commands, 234 events,
  and 611 named types. Runtime validation needs no JSON-schema parsing at startup.
- `internal/cdp/protocol_types_generated.go`: Go wire types for every named type,
  command parameters/result and event. Optional values preserve omission; arbitrary
  JavaScript values preserve explicit `null`. Integer fields accept Chrome's
  integral JSON doubles, including `1.0` and `1e0`, with int32 range checks.
- `internal/cdp/protocol_inventory_generated.json`: the full surface annotated
  with an explicit implementation manifest. Generated shape and implemented
  behavior are separate fields; no handler is fabricated from the schema.

The retained input `internal/cdp/protocol/chrome152.json` is Chrome's complete
`/json/protocol` response. Its adjacent `.source.json` records SHA-256, exact
Chrome/Chromium/V8 versions, platform and capture provenance. These inputs are
outside the frozen `chrome/152/generated` directory.

The historical `chrome/152/generated/cdp.json` is preserved unchanged. Its browser
input was Chromium's legacy `browser_protocol-1.3.json`, which omits current
automation domains and commands. Its simplified V8 PDL parser also omitted types,
misread flags/arrays, and folded the Schema domain into Runtime. It is not an
adequate source for validating modern automation requests. The complete retained
Chrome response avoids both losses without weakening the frozen artifact checks.

## Offline generation and verification

```powershell
python tools/generate_cdp.py
python tools/generate_cdp.py --check
python tools/generate_compat.py --check
python -m unittest discover -s tools -p test_generate_cdp.py
go test ./internal/cdp -run 'TestProtocol|TestGeneratedProtocol'
```

Generation and checks perform no network requests. `--check` verifies source
provenance and its hash, resolves every reference, rejects duplicate definitions
or unsupported schema syntax, and compares every generated output byte. The
existing full compatibility check also runs the new CDP check. The generator
does not rewrite source hashes or implementation claims during verification.

`internal/cdp/protocol_support.json` is the handwritten implementation manifest:

```json
{
  "Runtime.evaluate": {
    "status": "partial",
    "notes": "Describe actual supported behavior and remaining limitations.",
    "tests": ["TestRuntimeEvaluation"]
  }
}
```

Permitted statuses are `unsupported`, `partial`, and `implemented`. Entries absent
from the manifest remain unsupported. Unknown Chrome command/event names fail generation.
`implemented` is an explicit maintainer claim; recorded tests provide evidence
but are not a claim of complete Chrome equivalence. Regenerate the inventory after
editing the manifest. Adding a command handler also requires adding its manifest
entry; a generated descriptor alone never enables a command.

### Required handler-change checklist

Keep these changes together whenever a handler is added, removed, or its behavior
changes:

1. Update `protocol_support.json` with the supported scope, remaining limitations,
   and repository test references (`path#test-name`). Use `partial` when the full
   command contract is not supported.
2. Run `python tools/generate_cdp.py` and commit the generated projections with
   the manifest. The runtime inventory and published coverage table must describe
   the same support state.
3. Run `python tools/generate_cdp.py --check`, the focused handler tests, and
   `go test ./internal/cdp -run '^(TestProtocolSupportManifestHasLiveEvidenceAndNoLostHandlers|TestProtocolCoverageUsesCompleteGeneratedInventory)$' -count=1`.

The registry is intentionally reviewed rather than inferred from dispatch code:
the presence of a handler establishes reachability, not semantic compatibility.
Keep implementation claims here instead of adding parallel handwritten lists.

### Mimic extensions

The same support manifest can declare `Mimic.<command>` extensions with explicit
scope, limitations and focused test evidence. They appear in the inventory's
separate `extensions` array and in their own generated coverage section.
`Mimic.getCompatibilityMatrix` returns these rows as `extensions`, with
`kind: "extension"` and `wireSchemaGenerated: false`. Extension names must use
the validated Mimic namespace; they do not alter the frozen Chrome schema,
wire descriptors or Chrome command/event counts. Their handlers validate their
own public parameters. Adding an extension still requires a manifest entry,
regeneration and the focused coverage checks above.

## Parameter boundary

`validateCommand(method, rawParams)` reports unknown methods as `-32601` and
invalid wire parameters as `-32602`, with the failing field in error data.
Required fields, scalar types, references, arrays and nested objects are checked.
Chrome's optional-field and base64 behavior is retained: unknown fields are
ignored recursively, explicit `null` does not erase a typed optional field,
and binary values require padded base64 without whitespace. Parameterless
commands ignore their `params`; commands with optional parameters accept an
omitted/null object but reject an array.

Enum strings are decoded as strings. Allowed values, argument combinations,
resource existence, session scope, ownership, and command effects remain the
handler's responsibility. Unknown enum values are not uniformly rejected by
Chrome's generated decoder. Error data identifies the failing field, but does
not reproduce Chrome's CBOR byte offsets. A successful schema validation means
only that dispatch may inspect the request; it does not imply a supported command.

`internal/cdp/testdata/protocol_params_chrome152.json` retains focused headful
Chrome observations covering these decoder boundaries. Tests distinguish actual
wire decoder failures from command-specific semantic failures, such as a valid
Fetch request issued while the Fetch domain is disabled.

## Intentional source refresh

Start the exact pinned Chrome with a fresh controlled profile, no command-line
feature overrides and the configured headful 1280x800 window. Then run:

```powershell
python tools/capture_cdp_protocol.py --endpoint http://127.0.0.1:19433 --fresh-controlled-profile
python tools/generate_cdp.py
```

The capture tool requires `websockets`, creates and closes its own page, checks
the exact Chrome/V8/Chromium versions and browser mode, then refreshes the source,
provenance and parameter observations. It never closes the shared browser. Review
the resulting schema and observation differences before accepting a refresh.
