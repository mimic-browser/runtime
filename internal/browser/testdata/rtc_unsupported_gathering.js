(async () => {
  const configuration = {
    iceTransportPolicy: rtcTestPolicy,
    iceServers: [
      { urls: rtcTestSTUN },
      { urls: rtcTestTURN, username: 'rtc', credential: 'secret' },
    ],
  };
  const p = new RTCPeerConnection(configuration),
    source = new RTCPeerConnection();
  const events = [];
  let finish;
  const done = new Promise((r) => (finish = r));
  p.onicecandidate = (e) => {
    if (e.candidate) events.push(e.candidate.candidate);
    else finish();
  };
  if (rtcTestRemote) {
    source.createDataChannel('source');
    await p.setRemoteDescription(await source.createOffer());
    await p.setLocalDescription(await p.createAnswer());
  } else {
    p.createDataChannel('local');
    await p.setLocalDescription(await p.createOffer());
  }
  await done;
  if (!rtcTestRemote) {
    await source.setRemoteDescription(p.localDescription);
    await source.setLocalDescription(await source.createAnswer());
    await p.setRemoteDescription(source.localDescription);
  }
  p.setConfiguration({ ...configuration, iceTransportPolicy: 'all' });
  const oldUfrag = /^a=ice-ufrag:([^\r\n]+)/m.exec(p.localDescription.sdp)[1];
  const regathered = new Promise((resolve) => (finish = resolve));
  p.restartIce();
  await p.setLocalDescription();
  await regathered;
  const stats = [...(await p.getStats()).values()].filter((r) => r.type.includes('candidate'));
  const ok =
    events.length === 0 &&
    stats.length === 0 &&
    !p.localDescription.sdp.includes('a=candidate:') &&
    p.iceGatheringState === 'complete' &&
    /^a=ice-ufrag:([^\r\n]+)/m.exec(p.localDescription.sdp)[1] !== oldUfrag &&
    p.connectionState !== 'connected';
  p.close();
  source.close();
  if (!((await p.getStats()) instanceof Map)) throw new Error('closed stats report');
  return ok;
})();
