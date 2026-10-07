# Native microphone boundary

The provider enumerates and opens native audio inputs lazily. Its capture owns
the native context and device independently of the startup context passed to
`Open`. Each `Read` returns an owned interleaved float block representing 20 ms
of 48 kHz mono/stereo PCM, converted from native S16 capture. Native callback
bytes never escape the callback. A two-block queue drops old input on overload;
`Read` is cancelable and `Close` is idempotent.

`github.com/gen2brain/malgo v0.11.26` compiles its miniaudio C header into the
executable. Windows uses WASAPI, macOS uses CoreAudio, and Linux uses PulseAudio
or ALSA through the OS's shared libraries. The null backend is excluded. There
is no enumeration at browser startup, global browser device cache, audio media
subprocess, downloaded payload, or additional audio DLL. The Go module checksum
pins the source. `tools/release/licenses/miniaudio.txt` retains the embedded
header's full license alternatives and is included in release notices along
with malgo's module license.

WebRTC Opus is implemented by the pure Go `github.com/pion/opus` source at
commit `44637de087b3f506170ab738ed96ac1be338006b`, pinned by the corresponding
pseudo-version and `go.sum`. The prior tagged release did not expose encoding.
Each connected sender owns an encoder, each received stream owns a decoder,
and neither is initialized by device enumeration or browser startup. The public
transport test and retained Chrome 152 diagnostic verify actual decoded audio,
not merely successful SDP negotiation. Echo/noise/gain/voice processing and
hardware speaker output remain unsupported.
