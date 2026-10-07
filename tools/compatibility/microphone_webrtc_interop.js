// Diagnostic: native microphone capture and bilateral Opus. No raw microphone
// samples are retained; the recorder stores settings, levels and packet counts.
globalThis.cameraInterop = {
  async prepare() {
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false },
    });
    const track = this.stream.getAudioTracks()[0];
    const clone = track.clone();
    clone.stop();
    this.local = {
      kind: track.kind,
      label: track.label,
      settings: track.getSettings(),
      capabilities: track.getCapabilities(),
      constraints: track.getConstraints(),
      stoppedSettings: clone.getSettings(),
    };
    this.context = new AudioContext({ sampleRate: 48000 });
    await this.context.resume();
    this.peer = new RTCPeerConnection();
    this.errors = [];
    this.peer.addEventListener('error', (event) => this.errors.push(event.message));
    this.peer.addTrack(track, this.stream);
    this.incoming = new Promise((resolve) => {
      this.peer.ontrack = (event) => {
        this.remoteTrack = event.track;
        this.receiver = event.receiver;
        this.remoteStream = event.streams[0];
        const element = document.createElement('audio');
        element.srcObject = event.streams[0];
        element.muted = true;
        document.body.appendChild(element);
        element.play().then(() => {
          const source = this.context.createMediaStreamSource(this.remoteStream);
          this.analyser = this.context.createAnalyser();
          this.analyser.fftSize = 512;
          source.connect(this.analyser);
          resolve();
        });
      };
    });
  },
  async gather() {
    if (this.peer.iceGatheringState !== 'complete') {
      await new Promise((resolve) => {
        const changed = () => {
          if (this.peer.iceGatheringState === 'complete') {
            this.peer.removeEventListener('icegatheringstatechange', changed);
            resolve();
          }
        };
        this.peer.addEventListener('icegatheringstatechange', changed);
        changed();
      });
    }
    return this.peer.localDescription.toJSON();
  },
  async offer() {
    await this.prepare();
    await this.peer.setLocalDescription(await this.peer.createOffer());
    return this.gather();
  },
  async answer(description) {
    await this.prepare();
    await this.peer.setRemoteDescription(description);
    await this.peer.setLocalDescription(await this.peer.createAnswer());
    return this.gather();
  },
  async observe() {
    await this.incoming;
    let peak = 0;
    const data = new Float32Array(512);
    for (let attempt = 0; attempt < 30; attempt++) {
      await new Promise((resolve) => setTimeout(resolve, 30));
      this.analyser.getFloatTimeDomainData(data);
      peak = Math.max(peak, ...data.map(Math.abs));
    }
    const stats = [...(await this.peer.getStats()).values()];
    const result = {
      connection: this.peer.connectionState,
      local: this.local,
      peak,
      errors: this.errors,
      remoteTrack: {
        kind: this.remoteTrack.kind,
        readyState: this.remoteTrack.readyState,
        muted: this.remoteTrack.muted,
        settings: this.remoteTrack.getSettings(),
        receiverIdentity: this.remoteTrack === this.receiver.track,
      },
      transport: stats.filter((stat) =>
        ['inbound-rtp', 'outbound-rtp', 'transport'].includes(stat.type),
      ),
    };
    return result;
  },
  async cleanup() {
    this.peer.close();
    this.stream.getTracks().forEach((track) => track.stop());
    await this.context.close();
    return this.remoteTrack.readyState;
  },
};
