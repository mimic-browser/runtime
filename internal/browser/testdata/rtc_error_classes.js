(async () => {
  const peer = new RTCPeerConnection({ iceTransportPolicy: 'relay' });
  const errors = {};
  const capture = async (name, action) => {
    try {
      await action();
      errors[name] = 'success';
    } catch (error) {
      errors[name] = error.name;
    }
  };
  try {
    await capture('answerWithoutOffer', () => peer.createAnswer());
    await capture('iceWithoutRemote', () =>
      peer.addIceCandidate({
        candidate: 'candidate:1 1 udp 2113937151 127.0.0.1 12345 typ host',
        sdpMid: '0',
      }),
    );
    await capture('localAnswerWithoutOffer', () =>
      peer.setLocalDescription({ type: 'answer', sdp: 'v=0\r\n' }),
    );
    await capture('malformedRemote', () =>
      peer.setRemoteDescription({ type: 'offer', sdp: 'garbage' }),
    );
    peer.close();
    await capture('offerClosed', () => peer.createOffer());
    await capture('localClosed', () => peer.setLocalDescription());
    return JSON.stringify(errors);
  } finally {
    peer.close();
  }
})();
