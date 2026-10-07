// Diagnostic observations only: no overrides of browser APIs or device state.
// The recorder selects the native device ID after retaining enumeration.
globalThis.mediaProfileProbe = {
  stream: null,
  peer: null,
  receiver: null,
  async open(deviceId) {
    this.stream = await navigator.mediaDevices.getUserMedia({
      video: {
        deviceId: { exact: deviceId },
        width: { ideal: 1280 },
        height: { ideal: 720 },
        frameRate: { ideal: 30 },
      },
    });
    this.track = this.stream.getVideoTracks()[0];
    this.video = document.createElement('video');
    this.video.muted = true;
    this.video.srcObject = this.stream;
    document.body.appendChild(this.video);
    await this.video.play();
    return this.observe();
  },
  observe() {
    return {
      label: this.track.label,
      settings: this.track.getSettings(),
      constraints: this.track.getConstraints(),
      capabilities: this.track.getCapabilities(),
      video: { width: this.video.videoWidth, height: this.video.videoHeight },
    };
  },
  async constraints(constraints) {
    const before = this.observe();
    try {
      await this.track.applyConstraints(constraints);
      // Wait for a frame carrying the new dimensions, rather than assuming
      // metadata and the already-presented frame change at the same instant.
      await this.frames(this.video, 2);
      return { requested: constraints, before, after: this.observe() };
    } catch (error) {
      return {
        requested: constraints,
        before,
        after: this.observe(),
        error: { name: error.name, message: error.message, constraint: error.constraint },
      };
    }
  },
  async clone() {
    const clone = this.track.clone();
    try {
      const before = { original: this.observe(), clone: clone.getSettings() };
      await clone.applyConstraints({ width: { exact: 640 }, height: { exact: 480 } });
      return {
        before,
        after: { original: this.observe(), clone: clone.getSettings() },
        independentIdentity: clone !== this.track && clone.id !== this.track.id,
        label: clone.label,
        capabilities: clone.getCapabilities(),
      };
    } finally {
      clone.stop();
    }
  },
  async cloneStart() {
    this.transitionClone = this.track.clone();
    await this.transitionClone.applyConstraints({ width: { exact: 640 }, height: { exact: 480 } });
    return {
      clone: this.transitionClone.getSettings(),
      constraints: this.transitionClone.getConstraints(),
      original: this.observe(),
    };
  },
  async cloneDelay() {
    await new Promise((resolve) => setTimeout(resolve, 150));
    return { clone: this.transitionClone.getSettings(), original: this.observe() };
  },
  async cloneAttach() {
    const video = document.createElement('video');
    video.muted = true;
    video.srcObject = new MediaStream([this.transitionClone]);
    document.body.appendChild(video);
    await video.play();
    const frames = await this.frames(video, 3);
    return { clone: this.transitionClone.getSettings(), original: this.observe(), frames };
  },
  frames(video, count) {
    return new Promise((resolve, reject) => {
      const canvas = document.createElement('canvas');
      canvas.width = 64;
      canvas.height = 36;
      const drawing = canvas.getContext('2d', { willReadFrequently: true });
      const rows = [];
      let callback;
      const timer = setTimeout(() => {
        video.cancelVideoFrameCallback(callback);
        reject(new Error('Frame observation timed out'));
      }, 12000);
      const collect = (now, metadata) => {
        drawing.drawImage(video, 0, 0, canvas.width, canvas.height);
        const pixels = drawing.getImageData(0, 0, canvas.width, canvas.height).data;
        const repeated = drawing.getImageData(0, 0, canvas.width, canvas.height).data;
        let hash = 2166136261;
        let sum = 0;
        let sumSquares = 0;
        let coherentReadback = true;
        for (let i = 0; i < pixels.length; i++) {
          hash = Math.imul(hash ^ pixels[i], 16777619) >>> 0;
          coherentReadback &&= pixels[i] === repeated[i];
          if (i % 4 !== 3) {
            sum += pixels[i];
            sumSquares += pixels[i] * pixels[i];
          }
        }
        const samples = (pixels.length / 4) * 3;
        rows.push({
          now,
          ...metadata,
          downsampleHash: hash,
          mean: sum / samples,
          variance: sumSquares / samples - (sum / samples) ** 2,
          coherentReadback,
        });
        if (rows.length === count) {
          clearTimeout(timer);
          resolve(rows);
        } else {
          callback = video.requestVideoFrameCallback(collect);
        }
      };
      callback = video.requestVideoFrameCallback(collect);
    });
  },
  async transport() {
    const sender = (this.peer = new RTCPeerConnection({ iceServers: [] }));
    const receiver = (this.receiver = new RTCPeerConnection({ iceServers: [] }));
    const remoteVideo = document.createElement('video');
    remoteVideo.muted = true;
    document.body.appendChild(remoteVideo);
    const incoming = new Promise((resolve, reject) => {
      receiver.ontrack = async (event) => {
        try {
          remoteVideo.srcObject = new MediaStream([event.track]);
          await remoteVideo.play();
          resolve();
        } catch (error) {
          reject(error);
        }
      };
    });
    const gather = async (peer) => {
      if (peer.iceGatheringState !== 'complete') {
        await new Promise((resolve, reject) => {
          const timer = setTimeout(() => reject(new Error('ICE gather timed out')), 6000);
          const changed = () => {
            if (peer.iceGatheringState === 'complete') {
              clearTimeout(timer);
              peer.removeEventListener('icegatheringstatechange', changed);
              resolve();
            }
          };
          peer.addEventListener('icegatheringstatechange', changed);
          changed();
        });
      }
      return peer.localDescription;
    };
    sender.addTrack(this.track, this.stream);
    await sender.setLocalDescription(await sender.createOffer());
    await receiver.setRemoteDescription(await gather(sender));
    await receiver.setLocalDescription(await receiver.createAnswer());
    await sender.setRemoteDescription(await gather(receiver));
    await incoming;
    const frames = await this.frames(remoteVideo, 60);
    const snapshot = async (peer) =>
      [...(await peer.getStats()).values()].filter((row) =>
        ['outbound-rtp', 'inbound-rtp', 'media-source', 'codec'].includes(row.type),
      );
    return {
      senderConnection: sender.connectionState,
      receiverConnection: receiver.connectionState,
      sender: await snapshot(sender),
      receiver: await snapshot(receiver),
      frames,
    };
  },
  close() {
    this.transitionClone?.stop();
    this.peer?.close();
    this.receiver?.close();
    this.stream?.getTracks().forEach((track) => track.stop());
  },
};
