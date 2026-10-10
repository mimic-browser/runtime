(async () => {
  const globals = Object.getOwnPropertyNames(globalThis);
  const peer = new RTCPeerConnection();
  const receiver = peer.addTransceiver('audio').receiver;
  const peerTrack = receiver.track;
  const context = new AudioContext();
  try {
    const stream = context.createMediaStreamDestination().stream;
    const track = stream.getAudioTracks()[0];
    const id = track.id;
    track.enabled = false;
    const disabled = track.enabled === false;
    track.enabled = true;
    const transceiver = peer.addTransceiver(track);
    return {
      addedGlobals: Object.getOwnPropertyNames(globalThis).filter(
        (name) => !globals.includes(name),
      ),
      destination: {
        stable: stream.getTracks()[0] === track && track.id === id,
        id: typeof id === 'string' && id.length > 0,
        kind: track.kind,
        readyState: track.readyState,
        muted: track.muted,
        disabled,
        enabled: track.enabled,
      },
      peer: {
        stable: receiver.track === peerTrack,
        kind: peerTrack.kind,
        readyState: peerTrack.readyState,
        receiverKind: transceiver.receiver.track.kind,
      },
    };
  } finally {
    peer.close();
    await context.close();
  }
})();
