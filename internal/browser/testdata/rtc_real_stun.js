(async () => {
  const p = new RTCPeerConnection({
    iceTransportPolicy: rtcTestPolicy,
    iceServers: [{ urls: rtcTestSTUN }],
  });
  let finish;
  const done = new Promise((r) => (finish = r)),
    events = [];
  p.onicecandidate = (e) => {
    if (e.candidate) events.push(e.candidate);
    else finish();
  };
  p.createDataChannel('actual-stun');
  await p.setLocalDescription(await p.createOffer());
  await done;
  const stats = [...(await p.getStats()).values()].filter((s) => s.type === 'local-candidate');
  const srflx = events.filter((c) => c.type === 'srflx');
  const expected = rtcTestPolicy === 'all';
  const ok = expected
    ? srflx.length > 0 &&
      srflx.every(
        (c) =>
          c.address === '127.0.0.1' &&
          c.relatedAddress === '0.0.0.0' &&
          c.relatedPort === 0 &&
          stats.some((s) => s.ip === c.address && s.port === c.port) &&
          p.localDescription.sdp.includes(
            c.candidate
              .replace(/^candidate:/, '')
              .split(' ')
              .slice(0, 8)
              .join(' '),
          ),
      )
    : events.length === 0 && stats.length === 0;
  const diagnostics = JSON.stringify({
    events: events.map((c) => c.toJSON()),
    sdp: p.localDescription.sdp,
    stats,
  });
  p.close();
  return ok ? true : diagnostics;
})();
