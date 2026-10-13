(async () => {
  const peer = new RTCPeerConnection();
  let finish;
  const complete = new Promise((resolve) => (finish = resolve));
  const candidates = [];
  peer.onicecandidate = (event) => {
    if (event.candidate) candidates.push(event.candidate);
    else finish();
  };
  peer.createDataChannel('permission-gather');
  await peer.setLocalDescription(await peer.createOffer());
  await complete;
  const hosts = candidates.filter((candidate) => candidate.type === 'host');
  const stats = [...(await peer.getStats()).values()].filter(
    (row) => row.type === 'local-candidate' && row.candidateType === 'host',
  );
  const permitted = rtcNumericHostsPermitted;
  const result =
    hosts.length > 0 &&
    stats.length > 0 &&
    hosts.every((candidate) => candidate.address.endsWith('.local') !== permitted) &&
    stats.every((row) => Boolean(row.ip && row.address) === permitted);
  peer.close();
  return result;
})();
