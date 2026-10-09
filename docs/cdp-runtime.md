# CDP execution and object ownership

The Runtime adapter keeps values inside their JavaScript realm. A protocol
session owns one private object table per realm; neither the table nor its
helper functions are properties of the page global. Object IDs identify that
session and realm and cannot be passed to another session or execution world.
`getProperties` uses captured descriptor operations and preserves accessors
without invoking them. Child property handles inherit the parent's object
group. Releasing a handle or group removes debugger references while ordinary
JavaScript references remain valid. The V8 adapter also releases temporary
embedder roots, including evaluation results after the private table retains
them. Navigation invalidates handles and prunes tables even when page-script
references keep an old realm alive.

Primitive protocol values preserve undefined, null, NaN, infinities, negative
zero, BigInt and symbols. Returning an object by value uses recursive property
serialization rather than JSON.stringify on the user object: Date remains an
empty object, user toJSON hooks are not called, and cycles or unsupported nested
values produce protocol errors. These distinctions, descriptor flags and
binding lifetime were measured against Chrome 152.0.7977.82.

An isolated world is a separate JavaScript runtime and wrapper set over the
frame's authoritative DOM. It has its own globals and DOM expandos, and belongs
to the document rather than the debugger session. Parent and child Window
lookups select the same named world. DOM mutation records cross worlds by
canonical node IDs and are reconstructed using each world's own wrappers.
Document parsing and inserted script execution belong to the main document
realm, including when a utility world calls document.write. All realm queues
are selected by the same Page event loop and are destroyed with their document.

Pending protocol promises release the session and Page command locks. They
wait for a Page broadcast after microtask checkpoints or realm teardown; the
existing Page pump continues to own task execution and clock advancement.
Multiple pending evaluations therefore cannot consume one another's scheduler
wakeups or advance the clock more than once. Another command on the same
session can resolve the pending promise. An explicit evaluation timeout is
honored; otherwise the session lifetime governs cancellation.

Console events retain their original argument values independently for every
session. Runtime bindings install only their requested public function name,
can target a specific realm or execution-context name, and apply to future
matching contexts. Removing a binding stops delivery to that session and leaves
the JavaScript function installed, as Chrome does.

The optional V8 command-line evaluator creates a native Inspector only for the
requested evaluation and closes it before returning. Its result is transferred
into the existing realm/session object table; native Inspector handles never
escape. Selected-node wrappers and the console-group last result are bounded
session/realm references. No command-line helpers are permanently installed on
the author global. See [Chrome DevTools](devtools.md) for usage and limitations.

The general Runtime object adapter remains a Mimic projection. Deep
serialization is rejected explicitly. Object previews, debugger call-frame
evaluation, precise native stack locations, and the complete set of internal
inspector properties are not implemented. Ordinary property descriptors and
the prototype link are available; subtype descriptions are not a complete
replacement for V8 inspector metadata.

Network request overrides belong to a Page's Loader. Frames, isolated worlds
and dedicated workers use that same policy; other Pages in the context keep
their own headers, offline state and cache-read preference. The context still
owns cached responses, blobs, accepted client hints and connection records.
Chrome 152 measurements show that cache bypass still stores fresh responses
in the shared cache. Refreshing a Vary variant replaces its prior representation
and does not retain superseded response bodies.

Offline mode changes the live navigator.onLine observation in Window and worker
realms. Window connectivity events are trusted tasks on the Page event loop;
workers observe the flag without additional connectivity events, matching the
measured reference. Local data and blob loads continue to work while offline.
Requests carry their client's security origin separately from the source
document URL. An inherited about:blank frame therefore uses its real origin
for CORS, credentials and Fetch Metadata without inventing a Referer header or
mistaking an author-provided base URL for its origin.
