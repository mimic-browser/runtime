(async () => {
  const a = new RTCPeerConnection(rtcTestConfig);
  const b = new RTCPeerConnection(rtcTestConfig);
  globalThis.rtcProxyPeers = [a, b];
  const candidates = [];
  let complete;
  const gathering = new Promise((resolve) => (complete = resolve));
  a.onicecandidate = (event) => {
    if (event.candidate) candidates.push(event.candidate.toJSON());
    else complete();
  };
  const channel = a.createDataChannel('real-proxy-payload');
  const opened = new Promise((resolve) => (channel.onopen = resolve));
  const echoed = new Promise((resolve) => (channel.onmessage = (e) => resolve(e.data)));
  b.ondatachannel = (event) => {
    event.channel.onmessage = (message) => event.channel.send('echo:' + message.data);
  };
  const offer = await a.createOffer();
  await a.setLocalDescription(offer);
  await gathering;
  await b.setRemoteDescription(a.localDescription);
  const remoteGathering = new Promise((resolve) => {
    b.onicecandidate = (event) => {
      if (!event.candidate) resolve();
    };
  });
  await b.setLocalDescription(await b.createAnswer());
  await remoteGathering;
  await a.setRemoteDescription(b.localDescription);
  await opened;
  channel.send('real-data');
  if ((await echoed) !== 'echo:real-data') throw new Error('wrong payload');
  const stats = [...(await a.getStats()).values()];
  const locals = stats.filter((row) => row.type === 'local-candidate');
  if (!locals.length || locals.some((row) => row.candidateType !== 'relay'))
    throw new Error('missing relay stats');
  for (const candidate of candidates) {
    if (!candidate.candidate.includes(' typ relay')) throw new Error('non-relay event');
    const fields = candidate.candidate.split(' ');
    if (!locals.some((row) => row.ip === fields[4] && row.port === Number(fields[5])))
      throw new Error('event/stats mismatch');
    if (
      !a.localDescription.sdp.includes(
        candidate.candidate
          .replace(/^candidate:/, '')
          .split(' ')
          .slice(0, 8)
          .join(' '),
      )
    )
      throw new Error('event/SDP mismatch');
    if (candidate.usernameFragment !== /^a=ice-ufrag:([^\r\n]+)/m.exec(a.localDescription.sdp)[1])
      throw new Error('event/SDP ICE credential mismatch');
  }
  if (!stats.some((row) => row.type === 'candidate-pair' && row.state === 'succeeded'))
    throw new Error('missing actual candidate pair');
  const oldUfrag = /^a=ice-ufrag:([^\r\n]+)/m.exec(a.localDescription.sdp)[1];
  const configuration = a.getConfiguration();
  a.setConfiguration({ ...configuration, iceTransportPolicy: rtcTestPolicyUpdate });
  a.restartIce();
  const restarted = await a.createOffer();
  if (/^a=ice-ufrag:([^\r\n]+)/m.exec(restarted.sdp)[1] === oldUfrag)
    throw new Error('restart did not change credentials');
  await a.setLocalDescription(restarted);
  await new Promise((resolve) => {
    if (a.iceGatheringState === 'complete') resolve();
    else
      a.onicecandidate = (event) => {
        if (!event.candidate) resolve();
      };
  });
  if (a.localDescription.sdp.includes(' typ host') || a.localDescription.sdp.includes(' typ srflx'))
    throw new Error('policy update bypassed relay');
  const result = JSON.stringify({ candidates, sdp: a.localDescription.sdp, stats });
  if (!rtcLeaveOpen) {
    a.close();
    b.close();
  }
  return result;
})();
