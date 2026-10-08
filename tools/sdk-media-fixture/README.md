# SDK media fixture

This standalone test executable uses the production browser and CDP code with
injected deterministic camera/microphone providers. It never enumerates or opens
native hardware. Build once per runtime source change and reuse it:

```sh
go build -o /tmp/mimic-sdk-media-fixture ./tools/sdk-media-fixture
/tmp/mimic-sdk-media-fixture --browser-mode headless --listen 127.0.0.1:0
```

Run inside WSL/Linux in the current SDK qualification workflow. The fixture
requires V8, headless mode and loopback binding. It prints a Mimic CDP endpoint
and an HTTP fixture endpoint. `Browser.close` ends the process and its listeners.

The private catalog contains cameras A/B (red/blue frames) and microphones A/B
(440/660 Hz PCM). All camera inputs are 8×4 at 30 fps; the public media profile
can exercise production crop/scale processing. The labels are `Private native
camera A`, `Private native camera B`, `Private native microphone A` and `Private
native microphone B`. Public device identities are configured normally over CDP.

`GET /state` reports private source open/close counters. `POST /control` accepts
`{"missing":{"native-camera-b":true},"fail":{"native-microphone-b":true}}`
to emulate disappearance or a backend failure. Backend errors deliberately
contain private paths so SDK tests can verify that Web errors sanitize details
while CDP diagnostics retain them. Controls are test-only loopback endpoints,
not a production runtime contract or a fake-device CLI feature.

The SDK's Node/Python/Rust qualification uses this fixture to prove capture source
selection independently from public device identity, without physical capture,
permission prompts, headful windows or a new Chrome oracle run.
