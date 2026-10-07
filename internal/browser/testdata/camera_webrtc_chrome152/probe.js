// Diagnostic probe: actual OBS video in both directions, with native H264 and
// network transport. The recorder supplies offer/answer descriptions separately.
globalThis.cameraInterop = {
  async prepare() {
    const devices = await navigator.mediaDevices.enumerateDevices();
    const camera = devices.find((device) => device.label === 'OBS Virtual Camera');
    if (!camera) throw new Error('OBS Virtual Camera is required');
    this.stream = await navigator.mediaDevices.getUserMedia({
      video: {
        deviceId: { exact: camera.deviceId },
        width: { ideal: 640 },
        height: { ideal: 360 },
        frameRate: { ideal: 30 },
      },
    });
    this.peer = new RTCPeerConnection();
    this.peer.addTrack(this.stream.getVideoTracks()[0], this.stream);
    this.incoming = new Promise((resolve) => {
      this.peer.ontrack = (event) => {
        this.remoteTrack = event.track;
        const video = document.createElement('video');
        video.muted = true;
        video.srcObject = event.streams[0] || new MediaStream([event.track]);
        document.body.appendChild(video);
        video.play().then(() => {
          this.video = video;
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
    const canvas = document.createElement('canvas');
    canvas.width = this.video.videoWidth;
    canvas.height = this.video.videoHeight;
    const drawing = canvas.getContext('2d');
    drawing.drawImage(this.video, 0, 0);
    const stats = [...(await this.peer.getStats()).values()];
    const result = {
      connection: this.peer.connectionState,
      dimensions: [canvas.width, canvas.height],
      pixel: [...drawing.getImageData(0, 0, 1, 1).data],
      remoteTrack: {
        id: this.remoteTrack.id,
        readyState: this.remoteTrack.readyState,
        muted: this.remoteTrack.muted,
        settings: this.remoteTrack.getSettings(),
      },
      transport: stats.filter((stat) =>
        ['inbound-rtp', 'outbound-rtp', 'transport'].includes(stat.type),
      ),
    };
    this.peer.close();
    result.remoteAfterClose = this.remoteTrack.readyState;
    this.stream.getTracks().forEach((track) => track.stop());
    return result;
  },
};
