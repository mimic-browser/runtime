(async () => {
  const a = new RTCPeerConnection(),
    b = new RTCPeerConnection();
  const channel = a.createDataChannel('permission-free');
  const opened = new Promise((r) => (channel.onopen = r)),
    echoed = new Promise((r) => (channel.onmessage = (e) => r(e.data)));
  b.ondatachannel = (e) => (e.channel.onmessage = (m) => e.channel.send(m.data));
  const complete = (p) =>
    new Promise(
      (r) =>
        (p.onicecandidate = (e) => {
          if (!e.candidate) r();
        }),
    );
  let done = complete(a);
  await a.setLocalDescription(await a.createOffer());
  await done;
  await b.setRemoteDescription(a.localDescription);
  done = complete(b);
  await b.setLocalDescription(await b.createAnswer());
  await done;
  await a.setRemoteDescription(b.localDescription);
  await opened;
  channel.send('real');
  if ((await echoed) !== 'real') throw Error('DataChannel payload');
  const stats = [...(await a.getStats()).values()].filter(
    (s) => s.type === 'local-candidate' && s.candidateType === 'host',
  );
  if (!stats.length || stats.some((s) => s.ip !== '' || s.address !== ''))
    throw Error('host stats privacy');
  const candidates = a.localDescription.sdp
    .split('\r\n')
    .filter((l) => l.startsWith('a=candidate:'));
  if (
    !candidates.length ||
    candidates.some((l) => l.includes(' typ host') && !l.split(' ')[4].endsWith('.local'))
  )
    throw Error('real mDNS signaling');
  a.close();
  b.close();
  return true;
})();
