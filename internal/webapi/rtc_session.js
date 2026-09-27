// Session descriptions are projections of peer-owned negotiation state. No
// media device, socket, native codec or graphics backend is needed to offer.
const rtcSessionModel = (() => {
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
    tracks = globalThis.__mimicMediaTrackSlots || new WeakMap();
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
      const random = rtcRandomBytes(32);
      s.session = {
        id: String(BigInt('0x' + rtcHex(random.slice(0, 8))) % 9223372036854775807n || 1n),
        version: 1,
        nextMid: 0,
        transceivers: [],
        fingerprint: rtcHex(rtcRandomBytes(32), ':'),
        ufrag: rtcBase64(random.slice(8, 11)),
        pwd: rtcBase64(random.slice(11, 29)),
      };
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
  const offer = (peer, options = {}) => {
    const s = state(peer);
    ensureOpen(s);
    const session = s.session;
    for (const [kind, key] of [
      ['audio', 'offerToReceiveAudio'],
      ['video', 'offerToReceiveVideo'],
    ])
      if (options[key] && !session.transceivers.some((t) => transceivers.get(t).kind === kind))
        add(peer, kind, { direction: 'recvonly' });
    if (options.iceRestart) {
      const random = rtcRandomBytes(21);
      session.ufrag = rtcBase64(random.slice(0, 3));
      session.pwd = rtcBase64(random.slice(3));
    }
    s.iceUfrag = session.ufrag;
    const sections = session.transceivers.map((t) => ({ object: t, ...transceivers.get(t) }));
    if (s.hasDataChannel) sections.push({ kind: 'application', mid: session.dataMid ?? null });
    for (const section of sections)
      if (section.mid === null) section.mid = String(session.nextMid++);
    const lines = [
      'v=0',
      'o=- ' + session.id + ' ' + ++session.version + ' IN IP4 127.0.0.1',
      's=-',
      't=0 0',
    ];
    if (sections.length) lines.push('a=group:BUNDLE ' + sections.map((s) => s.mid).join(' '));
    lines.push('a=extmap-allow-mixed', 'a=msid-semantic: WMS');
    const extensionIDs = new Map(),
      usedExtensionIDs = new Set();
    for (const section of sections) {
      const application = section.kind === 'application',
        media = application ? null : rtcSessionModel.media(section.kind, section.direction);
      lines.push(
        application
          ? 'm=application 9 UDP/DTLS/SCTP webrtc-datachannel'
          : 'm=' + section.kind + ' 9 UDP/TLS/RTP/SAVPF ' + media.payloads.join(' '),
        'c=IN IP4 0.0.0.0',
      );
      if (!application) lines.push('a=rtcp:9 IN IP4 0.0.0.0');
      lines.push(
        'a=ice-ufrag:' + session.ufrag,
        'a=ice-pwd:' + session.pwd,
        'a=ice-options:trickle',
        'a=fingerprint:sha-256 ' + session.fingerprint,
        'a=setup:actpass',
        'a=mid:' + section.mid,
      );
      if (application) lines.push('a=sctp-port:5000', 'a=max-message-size:262144');
      else {
        const extensions = media.extensions.map((line) => {
          const [, preferred, uri] = /^a=extmap:(\d+) (.+)$/.exec(line);
          let id = extensionIDs.get(uri);
          if (id === undefined) {
            id = Number(preferred);
            if (usedExtensionIDs.has(id)) {
              id = 14;
              while (usedExtensionIDs.has(id)) id--;
            }
            extensionIDs.set(uri, id);
            usedExtensionIDs.add(id);
          }
          return 'a=extmap:' + id + ' ' + uri;
        });
        lines.push(
          ...extensions,
          'a=' + section.direction,
          'a=rtcp-mux',
          'a=rtcp-rsize',
          ...media.attributes,
        );
      }
    }
    return { sdp: lines.join('\r\n') + '\r\n', type: 'offer' };
  };
  method('createOffer', function (options = {}) {
    try {
      return platformPromiseResolve(offer(this, options ?? {}));
    } catch (e) {
      return platformPromiseReject(e);
    }
  });
  const applyLocal = (peer, description) => {
    const session = state(peer).session;
    let at = 0;
    for (const section of description.sdp.split(/(?=^m=)/m).slice(1)) {
      const kind = /^m=(\w+)/.exec(section)?.[1],
        mid = /^a=mid:([^\r\n]+)/m.exec(section)?.[1] ?? null;
      if (kind === 'application') session.dataMid = mid;
      else if (session.transceivers[at]) transceivers.get(session.transceivers[at++]).mid = mid;
    }
  };
  const expandCandidates = (sdp, candidates, configuration) => {
    const sections = sdp.split(/(?=^m=)/m).slice(1);
    const original = candidates.splice(0);
    for (let index = 0; index < sections.length; index++) {
      if (configuration.bundlePolicy === 'max-bundle' && index) break;
      const mid = /^a=mid:([^\r\n]+)/m.exec(sections[index])?.[1] ?? String(index);
      for (const candidate of original) {
        const s = rtcCandidateSlots.get(candidate);
        candidates.push(
          new RTCIceCandidate({
            ...s,
            sdpMid: mid,
            sdpMLineIndex: index,
            candidate: s.candidate.replace(
              /^(candidate:\S+ \S+ \S+ \S+ \S+ )(\d+)/,
              (_, prefix, port) => prefix + (Number(port) + index * 2),
            ),
          }),
        );
      }
    }
  };
  const candidateDescription = (value, candidates) => {
    const sections = value.sdp.split(/(?=^m=)/m),
      head = sections.shift();
    return new RTCSessionDescription({
      type: value.type,
      sdp:
        head +
        sections
          .map((section, index) => {
            const additions = candidates
              .filter((c) => rtcCandidateSlots.get(c).sdpMLineIndex === index)
              .map(
                (c) =>
                  'a=' +
                  rtcCandidateSlots.get(c).candidate.replace(/ ufrag [^ ]+(?= network-cost)/, '') +
                  '\r\n',
              )
              .join('');
            const at = section.indexOf('a=ice-ufrag:');
            return at < 0
              ? section + additions
              : section.slice(0, at) + additions + section.slice(at);
          })
          .join(''),
    });
  };
  return {
    offer,
    applyLocal,
    expandCandidates,
    candidateDescription,
    media: (kind, direction) => host.rtpMedia(kind, direction),
    state,
    transceivers,
  };
})();
delete globalThis.__mimicMediaTrackSlots;
