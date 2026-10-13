(async () => {
  const peer = new RTCPeerConnection({ iceTransportPolicy: 'relay' });
  try {
    peer.createDataChannel('queue');
    const rejectedAnswer = peer.createAnswer().then(
      () => false,
      () => true,
    );
    const offer = peer.createOffer();
    if (!(await rejectedAnswer)) throw new Error('Answer without offer succeeded');
    const description = await offer;
    const applied = peer.setLocalDescription(description);
    if (peer.signalingState !== 'stable')
      throw new Error('Description changed state before returning to the caller');
    const stats = peer.getStats();
    await applied;
    if (peer.signalingState !== 'have-local-offer')
      throw new Error('Local description did not apply');
    if (!((await stats) instanceof Map)) throw new Error('Queued stats failed');
    const remote = new RTCPeerConnection({ iceTransportPolicy: 'relay' });
    try {
      const received = remote.setRemoteDescription(description);
      const answered = remote.setLocalDescription();
      await received;
      await answered;
      if (remote.localDescription.type !== 'answer' || remote.signalingState !== 'stable')
        throw new Error('Implicit description did not use the queued signaling state');
    } finally {
      remote.close();
    }
    return true;
  } finally {
    peer.close();
  }
})();
