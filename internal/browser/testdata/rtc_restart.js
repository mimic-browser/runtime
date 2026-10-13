(async () => {
  const a = new RTCPeerConnection(),
    b = new RTCPeerConnection();
  const check = (condition, message) => {
    if (!condition) throw new Error(message);
  };
  const attributes = (description, name) => {
    const prefix = 'a=' + name + ':';
    const values = description.sdp
      .split('\r\n')
      .filter((line) => line.startsWith(prefix))
      .map((line) => line.slice(prefix.length));
    check(values.length > 0 && values.every(Boolean), 'Missing SDP attribute ' + name);
    return values;
  };
  const mids = (description) => attributes(description, 'mid');
  const ufrag = (description) => {
    const ufrags = attributes(description, 'ice-ufrag'),
      passwords = attributes(description, 'ice-pwd');
    check(
      ufrags.every((value) => /^[A-Za-z0-9+/]{4}$/.test(value)),
      'Native ICE username fragment shape: ' + ufrags.map((value) => value.length).join(','),
    );
    check(
      passwords.every((value) => /^[A-Za-z0-9+/]{24}$/.test(value)),
      'Native ICE password shape: ' + passwords.map((value) => value.length).join(','),
    );
    return ufrags[0];
  };
  const waitFor = (promise, stage) =>
    new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('Timed out waiting for ' + stage)), 5000);
      promise.then(
        (value) => {
          clearTimeout(timer);
          resolve(value);
        },
        (error) => {
          clearTimeout(timer);
          reject(error);
        },
      );
    });
  let generation = 0;
  const gathered = async (peer, description) => {
    const complete = new Promise((resolve) => {
      peer.onicecandidate = (event) => {
        if (!event.candidate) resolve();
      };
    });
    await peer.setLocalDescription(description);
    const applied = peer.localDescription;
    check(ufrag(applied) === ufrag(description), 'Gathering returned another ICE generation');
    // Answer gathering may finish while the remote offer is being applied,
    // before this listener exists. The native SDP marker belongs to these
    // credentials; a previously published gathering state does not.
    if (!applied.sdp.split('\r\n').includes('a=end-of-candidates'))
      await waitFor(complete, 'ICE gathering generation ' + ++generation);
    return peer.localDescription;
  };
  try {
    const channel = a.createDataChannel('restart');
    const opened = new Promise((resolve) => (channel.onopen = resolve));
    b.ondatachannel = (event) =>
      (event.channel.onmessage = (message) => event.channel.send(message.data));
    const echo = async (payload) => {
      const received = new Promise(
        (resolve) => (channel.onmessage = (event) => resolve(event.data)),
      );
      channel.send(payload);
      check(
        (await waitFor(received, payload)) === payload,
        'Native DataChannel did not echo ' + payload,
      );
    };
    const offer = await gathered(a, await a.createOffer());
    await b.setRemoteDescription(offer);
    const answer = await gathered(b, await b.createAnswer());
    await a.setRemoteDescription(answer);
    await waitFor(opened, 'initial DataChannel open');
    await echo('before-restart');
    const currentLocal = a.currentLocalDescription.sdp,
      currentRemote = a.currentRemoteDescription.sdp,
      currentAnswer = b.currentLocalDescription.sdp,
      stableMids = mids(a.localDescription).join(',');
    await a.setLocalDescription(await a.createOffer());
    await a.setLocalDescription({ type: 'rollback' });
    check(a.signalingState === 'stable', 'Rollback did not restore stable state');
    check(a.pendingLocalDescription === null, 'Rollback retained pending local offer');
    check(a.pendingRemoteDescription === null, 'Rollback retained pending remote description');
    check(a.localDescription.sdp === currentLocal, 'Rollback did not restore current local SDP');
    check(a.remoteDescription.sdp === currentRemote, 'Rollback did not restore current remote SDP');
    await echo('after-rollback');
    a.restartIce();
    const restarted = await a.createOffer(),
      repeated = await a.createOffer();
    check(ufrag(restarted) !== ufrag(offer), 'ICE restart reused the current credentials');
    check(ufrag(repeated) !== ufrag(restarted), 'Repeated restart offer lost restart intent');
    check(mids(repeated).join(',') === stableMids, 'Restart changed negotiated MIDs');
    await echo('restart-created-but-not-applied');
    const fresh = await gathered(a, repeated);
    check(
      a.currentLocalDescription.sdp === currentLocal,
      'Pending restart changed current local SDP',
    );
    check(
      a.currentRemoteDescription.sdp === currentRemote,
      'Pending restart changed current remote SDP',
    );
    const pending = a.pendingLocalDescription.sdp,
      beforeRejectedLocal = a.currentLocalDescription.sdp,
      beforeRejectedRemote = a.currentRemoteDescription.sdp;
    let rollbackFailure;
    try {
      await a.setLocalDescription({ type: 'rollback' });
    } catch (error) {
      rollbackFailure = error;
    }
    check(rollbackFailure?.name === 'NotSupportedError', 'Applied restart rollback boundary');
    check(a.signalingState === 'have-local-offer', 'Rejected rollback changed signaling');
    check(a.pendingLocalDescription.sdp === pending, 'Rejected rollback changed pending SDP');
    check(
      a.currentLocalDescription.sdp === beforeRejectedLocal,
      'Rejected rollback changed current SDP',
    );
    check(
      a.currentRemoteDescription.sdp === beforeRejectedRemote,
      'Rejected rollback changed remote SDP',
    );
    await b.setRemoteDescription(fresh);
    check(
      b.currentLocalDescription.sdp === currentAnswer,
      'Remote restart changed the current local answer SDP',
    );
    await a.setRemoteDescription(await gathered(b, await b.createAnswer()));
    check(a.signalingState === 'stable', 'Restart negotiation did not complete');
    await echo('after-rollback-and-restart');
    return true;
  } finally {
    a.close();
    b.close();
  }
})();
