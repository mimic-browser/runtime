// Candidate parsing and provisional transceiver identity. Native negotiation
// owns descriptions, credentials, gathering and every transport observation.
(() => {
  const parsedCandidate = (value) => {
    const s = rtcCandidateSlots.get(value);
    if (!s) throw new TypeError('Illegal invocation');
    const p = s.candidate.replace(/^candidate:/, '').split(/\s+/);
    if (p.length < 8 || p[6] !== 'typ') return {};
    const extra = {};
    for (let i = 8; i + 1 < p.length; i += 2) extra[p[i]] = p[i + 1];
    return {
      foundation: p[0],
      component: p[1] === '1' ? 'rtp' : p[1] === '2' ? 'rtcp' : null,
      protocol: p[2].toLowerCase(),
      priority: Number(p[3]),
      address: p[4],
      port: Number(p[5]),
      type: p[7],
      tcpType: extra.tcptype ?? null,
      relatedAddress: extra.raddr ?? null,
      relatedPort: extra.rport === undefined ? null : Number(extra.rport),
    };
  };
  for (const key of [
    'foundation',
    'component',
    'protocol',
    'priority',
    'address',
    'port',
    'type',
    'tcpType',
    'relatedAddress',
    'relatedPort',
  ])
    Object.defineProperty(RTCIceCandidate.prototype, key, {
      get() {
        return parsedCandidate(this)[key] ?? null;
      },
      enumerable: true,
      configurable: true,
    });
  const transceivers = new WeakMap(),
    senders = new WeakMap(),
    receivers = new WeakMap(),
    tracks = sharedMediaTrackSlots;
  const make = (type, map, state) => {
    const object = Object.create(globalThis[type].prototype);
    map.set(object, state);
    return object;
  };
  const attr = (type, map, name, read, write) => {
    const p = globalThis[type]?.prototype;
    if (!p) return;
    Object.defineProperty(p, name, {
      get() {
        const s = map.get(this);
        if (!s) throw new TypeError('Illegal invocation');
        return read(s);
      },
      ...(write
        ? {
            set(v) {
              const s = map.get(this);
              if (!s) throw new TypeError('Illegal invocation');
              write(s, v);
            },
          }
        : {}),
      enumerable: true,
      configurable: true,
    });
  };
  const state = (peer) => {
    const s = rtcPeerPrivate.get(peer);
    if (!s) throw new TypeError('Illegal invocation');
    if (!s.session) {
      s.session = { transceivers: [] };
    }
    return s;
  };
  const ensureOpen = (s) => {
    if (s.closed)
      throw platformDOMException('The RTCPeerConnection is closed.', 'InvalidStateError');
  };
  const direction = (value) => {
    const text = String(value);
    if (!['sendrecv', 'sendonly', 'recvonly', 'inactive'].includes(text))
      throw new TypeError('Invalid RTCRtpTransceiverDirection');
    return text;
  };
  const add = (peer, kind, init = {}) => {
    const s = state(peer);
    ensureOpen(s);
    if (!['audio', 'video'].includes(kind)) throw new TypeError('Invalid track kind');
    const track = make('MediaStreamTrack', tracks, {
      kind,
      id: host.internalRandomUUID(),
      enabled: true,
      muted: true,
      readyState: 'live',
    });
    const sender = make('RTCRtpSender', senders, { track: null }),
      receiver = make('RTCRtpReceiver', receivers, { track });
    const object = make('RTCRtpTransceiver', transceivers, {
      kind,
      sender,
      receiver,
      direction: direction(init.direction ?? 'sendrecv'),
      currentDirection: null,
      mid: null,
      stopped: false,
    });
    s.session.transceivers.push(object);
    return object;
  };
  for (const key of ['sender', 'receiver', 'direction', 'currentDirection', 'mid', 'stopped'])
    attr(
      'RTCRtpTransceiver',
      transceivers,
      key,
      (s) => s[key],
      key === 'direction'
        ? (s, v) => {
            if (s.stopped)
              throw platformDOMException('The transceiver is stopped.', 'InvalidStateError');
            s.direction = direction(v);
          }
        : null,
    );
  for (const [type, map] of [
    ['RTCRtpSender', senders],
    ['RTCRtpReceiver', receivers],
  ])
    attr(type, map, 'track', (s) => s.track);
  for (const key of ['kind', 'id', 'enabled', 'muted', 'readyState'])
    attr(
      'MediaStreamTrack',
      tracks,
      key,
      (s) => s[key],
      key === 'enabled' ? (s, v) => (s.enabled = !!v) : null,
    );
  const method = (name, fn) =>
    Object.defineProperty(RTCPeerConnection.prototype, name, {
      value: fn,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  method('addTransceiver', function (trackOrKind, init = {}) {
    return add(
      this,
      typeof trackOrKind === 'string' ? trackOrKind : tracks.get(trackOrKind)?.kind,
      init,
    );
  });
  method('getTransceivers', function () {
    return state(this).session.transceivers.slice();
  });
  method('getSenders', function () {
    return state(this).session.transceivers.map((t) => transceivers.get(t).sender);
  });
  method('getReceivers', function () {
    return state(this).session.transceivers.map((t) => transceivers.get(t).receiver);
  });
})();
