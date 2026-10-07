// A peer acquires native transport only when media is attached or a remote
// session must be negotiated. Offline offer observations retain their existing
// catalog path; a connected peer's state comes exclusively from the transport.
(() => {
  if (!cameraCaptureModel) return;
  const peers = new WeakMap(),
    peerWrappers = new Map(),
    senders = new WeakMap(),
    remoteStreams = new WeakMap();
  const senderLists = new WeakMap();
  const deferredChannels = new WeakMap(),
    channelSlots = new WeakMap(),
    channelWrappers = new Map();
  const attachChannel = (peer, channel) => {
    const state = rtcDataStates.get(channel);
    const threshold = channel.bufferedAmountLowThreshold;
    const record = failure(
      host.rtcMediaData(
        peers.get(peer),
        'create',
        state.label,
        JSON.stringify({
          ordered: state.ordered,
          maxPacketLifeTime: state.maxPacketLifeTime,
          maxRetransmits: state.maxRetransmits,
          protocol: state.protocol,
          negotiated: state.negotiated,
          id: state.id,
        }),
      ),
    );
    channelSlots.set(channel, { peer, id: record.id });
    channelWrappers.set(record.id, channel);
    failure(host.rtcMediaData(peers.get(peer), 'threshold', record.id, threshold));
  };
  const transceiverWrappers = new Map(),
    receiverSlots = new WeakMap(),
    transceiverSlots = new WeakMap();
  const failure = (result) => {
    if (result?.error) {
      if (result.error === 'TypeError') throw new TypeError(result.message);
      throw platformDOMException(result.message, result.error);
    }
    return result;
  };
  const ensure = (peer) => {
    const state = rtcPeerPrivate.get(peer);
    if (!state) throw new TypeError('Illegal invocation');
    if (state.closed)
      throw platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError');
    let id = peers.get(peer);
    if (!id) {
      const provisional = RTCPeerConnection.prototype.getTransceivers.call(peer);
      if (rtcPeerStates.get(peer).localDescription)
        throw platformDOMException(
          'Attach media before applying an offline offer',
          'NotSupportedError',
        );
      const config = peer.getConfiguration();
      delete config.certificates;
      id = failure(host.rtcMediaCreate(JSON.stringify(config))).id;
      peers.set(peer, id);
      peerWrappers.set(id, peer);
      senderLists.set(peer, []);
      for (const transceiver of provisional) {
        const sender = transceiver.sender,
          receiver = transceiver.receiver,
          track = receiver.track;
        const rows = failure(
            host.rtcMediaTrack(
              id,
              'addTransceiver',
              JSON.stringify({ direction: transceiver.direction, kind: track.kind }),
            ),
          ),
          row = rows.at(-1);
        senders.set(sender, { peer, id: row.senderID, track: null });
        senderLists.get(peer).push(sender);
        receiverSlots.set(receiver, {
          peer,
          track: cameraCaptureModel.makeTrack(row.receiverTrack, track),
        });
        transceiverSlots.set(transceiver, {
          peer,
          id: row.id,
          sender,
          receiver,
          stopped: false,
          row,
        });
        transceiverWrappers.set(row.id, transceiver);
      }
      for (const channel of deferredChannels.get(peer) || []) attachChannel(peer, channel);
      deferredChannels.delete(peer);
    }
    return id;
  };
  const operation = (peer, name, data) =>
    host.rtcMediaOperation(ensure(peer), name, JSON.stringify(data)).then(failure);
  const method = (prototype, name, value) =>
    Object.defineProperty(prototype, name, {
      value,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  const priorDataChannel = RTCPeerConnection.prototype.createDataChannel;
  method(RTCPeerConnection.prototype, 'createDataChannel', function (label, options = {}) {
    const channel = priorDataChannel.call(this, label, options);
    if (peers.has(this)) attachChannel(this, channel);
    else {
      let channels = deferredChannels.get(this);
      if (!channels) deferredChannels.set(this, (channels = []));
      channels.push(channel);
    }
    return channel;
  });
  for (const name of ['id', 'readyState', 'bufferedAmount', 'bufferedAmountLowThreshold']) {
    const prior = Object.getOwnPropertyDescriptor(RTCDataChannel.prototype, name);
    Object.defineProperty(RTCDataChannel.prototype, name, {
      get() {
        const slot = channelSlots.get(this);
        return slot
          ? host.rtcMediaData(peers.get(slot.peer), 'state', slot.id)[name]
          : prior.get.call(this);
      },
      ...(name === 'bufferedAmountLowThreshold'
        ? {
            set(value) {
              prior.set.call(this, value);
              const slot = channelSlots.get(this);
              if (slot)
                failure(
                  host.rtcMediaData(
                    peers.get(slot.peer),
                    'threshold',
                    slot.id,
                    prior.get.call(this),
                  ),
                );
            },
          }
        : {}),
      enumerable: true,
      configurable: true,
    });
  }
  const priorChannelSend = RTCDataChannel.prototype.send,
    priorChannelClose = RTCDataChannel.prototype.close;
  method(RTCDataChannel.prototype, 'send', function (value) {
    const slot = channelSlots.get(this);
    if (!slot) return priorChannelSend.call(this, value);
    let bytes,
      text = false;
    if (typeof value === 'string') {
      bytes = new TextEncoder().encode(value);
      text = true;
    } else if (value instanceof ArrayBuffer) bytes = new Uint8Array(value);
    else if (ArrayBuffer.isView(value))
      bytes = new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
    else throw platformDOMException('This data type is unsupported', 'NotSupportedError');
    let raw = '';
    for (let offset = 0; offset < bytes.length; offset += 4096)
      raw += String.fromCharCode(...bytes.subarray(offset, offset + 4096));
    failure(host.rtcMediaData(peers.get(slot.peer), 'send', slot.id, btoa(raw), text));
  });
  method(RTCDataChannel.prototype, 'close', function () {
    const slot = channelSlots.get(this);
    if (!slot) return priorChannelClose.call(this);
    failure(host.rtcMediaData(peers.get(slot.peer), 'close', slot.id));
  });
  const senderPrototype = RTCRtpSender.prototype;
  const syncTransceivers = (peer) => {
    const rows = failure(host.rtcMediaTrack(peers.get(peer), 'transceivers'));
    return rows.map((row) => {
      let object = transceiverWrappers.get(row.id);
      if (!object) {
        const receiver = Object.create(RTCRtpReceiver.prototype);
        receiverSlots.set(receiver, {
          peer,
          track: cameraCaptureModel.makeTrack(row.receiverTrack),
        });
        let sender = senderLists.get(peer).find((s) => senders.get(s).id === row.senderID);
        if (!sender) {
          sender = Object.create(senderPrototype);
          senders.set(sender, {
            peer,
            id: row.senderID,
            track: cameraCaptureModel.getTrack(row.senderTrackID),
          });
          senderLists.get(peer).push(sender);
        }
        object = Object.create(RTCRtpTransceiver.prototype);
        transceiverSlots.set(object, { peer, id: row.id, sender, receiver, stopped: false });
        transceiverWrappers.set(row.id, object);
      }
      transceiverSlots.get(object).row = row;
      return object;
    });
  };
  const priorReceiverTrack = Object.getOwnPropertyDescriptor(RTCRtpReceiver.prototype, 'track');
  Object.defineProperty(RTCRtpReceiver.prototype, 'track', {
    get() {
      return receiverSlots.get(this)?.track || priorReceiverTrack.get.call(this);
    },
    enumerable: true,
    configurable: true,
  });
  for (const name of ['sender', 'receiver', 'mid', 'direction', 'currentDirection', 'stopped']) {
    const prior = Object.getOwnPropertyDescriptor(RTCRtpTransceiver.prototype, name);
    Object.defineProperty(RTCRtpTransceiver.prototype, name, {
      get() {
        const slot = transceiverSlots.get(this);
        if (!slot) return prior.get.call(this);
        syncTransceivers(slot.peer);
        if (name === 'sender' || name === 'receiver' || name === 'stopped') return slot[name];
        if (name === 'currentDirection') {
          if (slot.stopped) return null;
          const state = host.rtcMediaState(peers.get(slot.peer));
          if (!state.currentLocalDescription || !state.currentRemoteDescription) return null;
          const description =
            state.currentLocalDescription.type === 'answer'
              ? state.currentLocalDescription
              : state.currentRemoteDescription;
          const section = description.sdp
            .split(/(?=^m=)/m)
            .find((s) => s.includes('a=mid:' + slot.row.mid + '\r\n'));
          const direction =
            section?.match(/^a=(sendrecv|sendonly|recvonly|inactive)\r?$/m)?.[1] || 'sendrecv';
          return description.type === 'answer' && description === state.currentRemoteDescription
            ? { sendonly: 'recvonly', recvonly: 'sendonly' }[direction] || direction
            : direction;
        }
        return slot.row[name];
      },
      ...(name === 'direction'
        ? {
            set(value) {
              const slot = transceiverSlots.get(this);
              if (!slot) return prior.set.call(this, value);
              if (String(value) !== slot.row.direction)
                throw platformDOMException(
                  'Changing negotiated direction is unsupported',
                  'NotSupportedError',
                );
            },
          }
        : {}),
      enumerable: true,
      configurable: true,
    });
  }
  const priorStop = RTCRtpTransceiver.prototype.stop;
  method(RTCRtpTransceiver.prototype, 'stop', function () {
    const slot = transceiverSlots.get(this);
    if (!slot) return priorStop?.call(this);
    failure(host.rtcMediaTrack(peers.get(slot.peer), 'stopTransceiver', slot.id));
    slot.stopped = true;
  });
  for (const [name, key] of [
    ['getTransceivers', null],
    ['getReceivers', 'receiver'],
  ]) {
    const prior = RTCPeerConnection.prototype[name];
    method(RTCPeerConnection.prototype, name, function () {
      if (!peers.has(this)) return prior.call(this);
      const list = syncTransceivers(this);
      return key ? list.map((t) => transceiverSlots.get(t)[key]) : list;
    });
  }
  const priorAddTransceiver = RTCPeerConnection.prototype.addTransceiver;
  method(RTCPeerConnection.prototype, 'addTransceiver', function (trackOrKind, init = {}) {
    if (!peers.has(this)) return priorAddTransceiver.call(this, trackOrKind, init);
    if (trackOrKind instanceof MediaStreamTrack) {
      const sender = this.addTrack(trackOrKind, ...(init.streams || []));
      return syncTransceivers(this).find((t) => t.sender === sender);
    }
    if (trackOrKind !== 'video' && trackOrKind !== 'audio')
      throw new TypeError('Expected audio or video');
    failure(
      host.rtcMediaTrack(
        ensure(this),
        'addTransceiver',
        JSON.stringify({ direction: init.direction || 'sendrecv', kind: trackOrKind }),
      ),
    );
    return syncTransceivers(this).at(-1);
  });
  const priorTrack = Object.getOwnPropertyDescriptor(senderPrototype, 'track');
  Object.defineProperty(senderPrototype, 'track', {
    get() {
      const state = senders.get(this);
      return state ? state.track : priorTrack.get.call(this);
    },
    configurable: true,
    enumerable: true,
  });
  for (const name of ['replaceTrack', 'getParameters', 'setParameters', 'getStats']) {
    const prior = senderPrototype[name];
    method(senderPrototype, name, function (value) {
      const state = senders.get(this);
      if (!state) {
        if (typeof prior === 'function') return prior.call(this, value);
        throw new TypeError('Illegal invocation');
      }
      if (name === 'getParameters')
        return host.rtcMediaTrack(peers.get(state.peer), 'parameters', state.id);
      if (name === 'getStats') return state.peer.getStats();
      if (name === 'setParameters')
        return platformPromiseReject(
          platformDOMException('Sender parameter changes are unsupported', 'NotSupportedError'),
        );
      try {
        if (value !== null && !(value instanceof MediaStreamTrack))
          throw new TypeError('Expected MediaStreamTrack or null');
        const id = value === null ? '' : cameraCaptureModel.trackID(value);
        failure(host.rtcMediaTrack(peers.get(state.peer), 'replace', state.id, id));
        state.track = value;
        return platformPromiseResolve();
      } catch (error) {
        return platformPromiseReject(error);
      }
    });
  }
  method(RTCPeerConnection.prototype, 'addTrack', function (track, ...streams) {
    const trackID = cameraCaptureModel.trackID(track);
    for (const stream of streams)
      if (!(stream instanceof MediaStream)) throw new TypeError('Expected MediaStream');
    const id = ensure(this),
      record = failure(
        host.rtcMediaTrack(id, 'add', trackID, streams[0]?.id || host.internalRandomUUID()),
      );
    let sender = senderLists.get(this).find((value) => senders.get(value).id === record.id);
    if (!sender) {
      sender = Object.create(senderPrototype);
      senderLists.get(this).push(sender);
    }
    senders.set(sender, { peer: this, id: record.id, track });
    return sender;
  });
  method(RTCPeerConnection.prototype, 'removeTrack', function (sender) {
    const state = senders.get(sender);
    if (!state || state.peer !== this)
      throw platformDOMException('Sender does not belong to this connection', 'InvalidAccessError');
    failure(host.rtcMediaTrack(ensure(this), 'remove', state.id));
    state.track = null;
  });
  for (const [name, action] of [
    ['createOffer', 'offer'],
    ['createAnswer', 'answer'],
    ['setLocalDescription', 'local'],
    ['setRemoteDescription', 'remote'],
    ['addIceCandidate', 'ice'],
    ['getStats', 'stats'],
  ]) {
    const prior = RTCPeerConnection.prototype[name];
    method(RTCPeerConnection.prototype, name, function (value) {
      if (!peers.has(this) && name !== 'setRemoteDescription') return prior.call(this, value);
      try {
        if (name === 'setLocalDescription' && value === undefined) {
          const state = host.rtcMediaState(ensure(this));
          return operation(
            this,
            state.signalingState === 'have-remote-offer' ? 'answer' : 'offer',
            {},
          )
            .then((description) => operation(this, 'local', description))
            .then(() => undefined);
        }
        return operation(this, action, value ?? (action === 'ice' ? null : {})).then((result) => {
          if (action === 'offer' || action === 'answer') return new RTCSessionDescription(result);
          if (action === 'stats') return new Map(Object.entries(result));
        });
      } catch (error) {
        return platformPromiseReject(error);
      }
    });
  }
  const priorSenders = RTCPeerConnection.prototype.getSenders;
  method(RTCPeerConnection.prototype, 'getSenders', function () {
    if (!peers.has(this)) return priorSenders.call(this);
    syncTransceivers(this);
    return senderLists.get(this).slice();
  });
  const priorConfiguration = RTCPeerConnection.prototype.setConfiguration;
  method(RTCPeerConnection.prototype, 'setConfiguration', function (value) {
    if (peers.has(this)) {
      const copy = { ...value };
      delete copy.certificates;
      failure(host.rtcMediaTrack(peers.get(this), 'configuration', JSON.stringify(copy)));
    }
    return priorConfiguration.call(this, value);
  });
  for (const name of [
    'localDescription',
    'currentLocalDescription',
    'pendingLocalDescription',
    'remoteDescription',
    'currentRemoteDescription',
    'pendingRemoteDescription',
    'signalingState',
    'iceGatheringState',
    'iceConnectionState',
    'connectionState',
    'canTrickleIceCandidates',
  ]) {
    const prior = Object.getOwnPropertyDescriptor(RTCPeerConnection.prototype, name);
    Object.defineProperty(RTCPeerConnection.prototype, name, {
      get() {
        if (!peers.has(this)) return prior.get.call(this);
        const state = host.rtcMediaState(peers.get(this));
        if (name === 'canTrickleIceCandidates')
          return state.remoteDescription
            ? /a=ice-options:.*trickle/.test(state.remoteDescription.sdp)
            : null;
        const value = state[name];
        return /Description$/.test(name) && value ? new RTCSessionDescription(value) : value;
      },
      configurable: true,
      enumerable: true,
    });
  }
  const priorClose = RTCPeerConnection.prototype.close;
  method(RTCPeerConnection.prototype, 'close', function () {
    const id = peers.get(this);
    if (id) failure(host.rtcMediaTrack(id, 'close'));
    priorClose.call(this);
  });
  for (const name of ['onnegotiationneeded', 'ontrack'])
    Object.defineProperty(RTCPeerConnection.prototype, name, {
      get() {
        return rtcPeerStates.get(this)[name] || null;
      },
      set(value) {
        rtcPeerStates.get(this)[name] = typeof value === 'function' ? value : null;
      },
      configurable: true,
      enumerable: true,
    });
  registerBootstrapCallback('installRTCMediaNotifier', (id, kind, data) => {
    const peer = peerWrappers.get(id);
    if (!peer) return;
    if (kind === 'datachannel') {
      const channel = priorDataChannel.call(peer, data.label, data);
      channelSlots.set(channel, { peer, id: data.id });
      channelWrappers.set(data.id, channel);
      const event = new Event('datachannel');
      Object.setPrototypeOf(event, RTCDataChannelEvent.prototype);
      Object.defineProperty(event, 'channel', { value: channel });
      dispatchTrusted(peer, event);
    } else if (kind.startsWith('data')) {
      const id = typeof data === 'string' ? data : data.id,
        channel = channelWrappers.get(id);
      if (!channel) return;
      if (kind === 'datamessage') {
        const raw = atob(data.data),
          bytes = new Uint8Array(raw.length);
        for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
        const value = data.text
          ? new TextDecoder().decode(bytes)
          : channel.binaryType === 'blob'
            ? new Blob([bytes])
            : bytes.buffer;
        dispatchTrusted(channel, new MessageEvent('message', { data: value }));
      } else {
        const name = {
          dataopen: 'open',
          dataclose: 'close',
          dataerror: 'error',
          databufferedamountlow: 'bufferedamountlow',
        }[kind];
        if (name) {
          rtcDataStates.get(channel).readyState = channel.readyState;
          dispatchTrusted(channel, new Event(name));
        }
      }
    } else if (kind === 'icecandidate') {
      const candidate = data ? new RTCIceCandidate(data) : null;
      dispatchTrusted(peer, new RTCPeerConnectionIceEvent('icecandidate', { candidate }));
    } else if (kind === 'track') {
      const transceiver = syncTransceivers(peer).find(
        (t) => transceiverSlots.get(t).id === data.transceiverID,
      );
      if (!transceiver) return;
      const receiver = transceiver.receiver,
        track = receiver.track;
      const event = new Event('track');
      Object.setPrototypeOf(event, RTCTrackEvent.prototype);
      const streams = [];
      if (data.streamID) {
        let cache = remoteStreams.get(peer);
        if (!cache) remoteStreams.set(peer, (cache = new Map()));
        let stream = cache.get(data.streamID);
        if (!stream) {
          stream = new MediaStream();
          Object.defineProperty(stream, 'id', { value: data.streamID });
          cache.set(data.streamID, stream);
        }
        stream.addTrack(track);
        streams.push(stream);
      }
      for (const [name, value] of Object.entries({ track, receiver, transceiver, streams }))
        Object.defineProperty(event, name, { value, enumerable: true });
      dispatchTrusted(peer, event);
    } else if (kind === 'error') {
      const event = new Event('error');
      Object.defineProperty(event, 'message', { value: data });
      dispatchTrusted(peer, event);
    } else dispatchTrusted(peer, new Event(kind));
  });
})();
