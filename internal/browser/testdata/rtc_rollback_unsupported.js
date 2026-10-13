(async () => {
  const a = new RTCPeerConnection({ iceTransportPolicy: 'relay' }),
    b = new RTCPeerConnection({ iceTransportPolicy: 'relay' });
  try {
    const transceiver = a.addTransceiver('audio', { direction: 'recvonly' });
    const offer = await a.createOffer();
    await a.setLocalDescription(offer);
    await b.setRemoteDescription(offer);
    if (b.getTransceivers()[0].mid !== transceiver.mid)
      throw new Error('Remote offer MID was not visible before the answer');
    const pending = a.pendingLocalDescription.sdp;
    let rejected;
    try {
      await a.setLocalDescription({ type: 'rollback' });
    } catch (error) {
      rejected = error;
    }
    if (rejected?.name !== 'NotSupportedError')
      throw new Error('Media rollback must explicitly reject at the supported boundary');
    if (
      a.signalingState !== 'have-local-offer' ||
      a.pendingLocalDescription.sdp !== pending ||
      a.currentLocalDescription !== null ||
      a.currentRemoteDescription !== null
    )
      throw new Error('Rejected media rollback corrupted descriptions');
    return true;
  } finally {
    a.close();
    b.close();
  }
})();
