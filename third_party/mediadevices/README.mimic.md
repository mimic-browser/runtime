# Narrow native camera dependency

Source: `github.com/pion/mediadevices` v0.10.0, copied from the checksum-verified
Go module. Original license: [LICENSE](LICENSE).

Mimic retains camera adapters, their required frame readers, property types and
AVFoundation bindings. Audio, screen capture, codec wrappers, WebRTC track
plumbing, examples, and upstream tests are excluded. This replacement is a
camera provider dependency, not a second media runtime.

Local changes remove import-time enumeration and expose `Discover` with fresh
independently owned device adapters. Mimic owns lazy discovery, COM thread
initialization, format selection and capture lifetime. The Windows adapter uses
owned frame bytes, a bounded nonblocking queue, explicit close notifications,
and native format/fps validation. Native enumeration and allocation bounds are
checked before interpreting platform structures. Linux/macOS transient frame
timeouts remain distinct from a closed source.

The Windows callback lookup is only native callback dispatch metadata. Readers
of independent cameras are not serialized by a global browser or Page lock.
There is no process-wide mutable device list or observer running at startup.
