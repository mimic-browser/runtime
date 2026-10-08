# Mimic v0.2.3

Changes since v0.2.2:

- Configure a framework-created browser context with a managed environment profile, proxy, resource policy and media devices before its first user page. Configuration is atomic and rejects contexts that still own pages, uploads or active capture.
- Keep managed profiles immutable while accepting equivalent font and media initialization from automation clients. Independent contexts retain independent window bounds and resize behavior.
- Preserve CDP command arrival order during page initialization without blocking independent pages, Promise resolution or request interception. Closing nested targets now releases the child sessions before their parent.
- Complete navigation loading notifications on success, failure and explicit cancellation. Refresh the frontend DOM after document replacement and expose selector ancestor paths, frame security fields and numeric network connection IDs expected by native automation clients.
- Add read-only CDP computed-style inspection using the page's canonical style state, including inherited custom properties. CSS editing and full DevTools CSS agent behavior remain unsupported.
- Shut down the Unix runtime when its parent process exits.

Mimic remains a renderer-free public beta for Windows and Linux amd64. Browser automation support follows the documented CDP scope; these changes do not imply complete Chrome or framework compatibility. Full source changes: [v0.2.2...v0.2.3](https://github.com/mimic-browser/runtime/compare/v0.2.2...v0.2.3).
