// Capture sources and frames belong to the browser host. These maps own only
// canonical JS wrappers, stream membership, and video-element attachments.
const cameraCaptureModel = (() => {
  // A selected compatibility bundle may omit media constructors. Preserve that
  // exposure instead of making its unrelated realm bootstrap depend on capture.
  if (typeof MediaStreamTrack !== 'function' || typeof MediaStream !== 'function') return null;
  const tracks = new WeakMap(),
    streams = new WeakMap(),
    wrappers = new Map();
  const attachments = new Map();
  const trackState = (track) => {
    const id = tracks.get(track);
    if (!id) throw new TypeError('Illegal invocation');
    return host.cameraTrack(id, 'state');
  };
  const failure = (result) => {
    if (!result?.error) return result;
    if (result.error === 'OverconstrainedError') {
      const error = Object.create(OverconstrainedError.prototype);
      Object.defineProperties(error, {
        name: { value: 'OverconstrainedError', configurable: true },
        message: { value: result.message, configurable: true },
        constraint: { value: result.constraint, configurable: true },
      });
      throw error;
    }
    throw platformDOMException(result.message, result.error);
  };
  const makeTrack = (record, existing) => {
    if (wrappers.has(record.id)) return wrappers.get(record.id);
    const object = existing || new EventTarget();
    Object.setPrototypeOf(object, MediaStreamTrack.prototype);
    tracks.set(object, record.id);
    wrappers.set(record.id, object);
    return object;
  };
  const trackProto = MediaStreamTrack.prototype;
  // Keep the existing audio/RTC boundary for their tracks. Camera wrappers
  // observe source-owned state rather than maintaining a second copy of it.
  for (const name of ['id', 'kind', 'label', 'enabled', 'muted', 'readyState']) {
    const prior = Object.getOwnPropertyDescriptor(trackProto, name);
    Object.defineProperty(trackProto, name, {
      get() {
        if (tracks.has(this)) {
          const state = trackState(this);
          return name === 'id' ? state.publicID : state[name];
        }
        if (prior?.get) return prior.get.call(this);
        throw new TypeError('Illegal invocation');
      },
      ...(name === 'enabled'
        ? {
            set(value) {
              if (tracks.has(this)) host.cameraTrack(tracks.get(this), 'enabled', !!value);
              else if (prior?.set) prior.set.call(this, value);
              else throw new TypeError('Illegal invocation');
            },
          }
        : {}),
      configurable: true,
      enumerable: true,
    });
  }
  const installMethod = (prototype, name, value) => {
    Object.defineProperty(prototype, name, {
      value,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  };
  for (const [name, action] of [
    ['stop', 'stop'],
    ['clone', 'clone'],
    ['getSettings', 'state'],
    ['getCapabilities', 'capabilities'],
    ['getConstraints', 'state'],
  ]) {
    const prior = trackProto[name];
    installMethod(trackProto, name, function () {
      if (!tracks.has(this)) {
        if (typeof prior === 'function') return prior.apply(this, arguments);
        throw new TypeError('Illegal invocation');
      }
      const result = host.cameraTrack(tracks.get(this), action);
      if (name === 'clone') return makeTrack(result);
      if (name === 'getSettings') return result.settings;
      if (name === 'getConstraints') return result.constraints;
      if (name === 'stop') return;
      return result;
    });
  }
  const normalize = (value) => {
    if (value === true || value == null) return {};
    if (typeof value !== 'object') throw new TypeError('Expected media constraints');
    const result = {};
    for (const name of [
      'deviceId',
      'groupId',
      'width',
      'height',
      'frameRate',
      'aspectRatio',
      'facingMode',
      'resizeMode',
      'advanced',
      'sampleRate',
      'sampleSize',
      'channelCount',
      'latency',
      'echoCancellation',
      'noiseSuppression',
      'autoGainControl',
      'voiceIsolation',
    ]) {
      if (value[name] !== undefined) result[name] = value[name];
    }
    return result;
  };
  const priorApply = trackProto.applyConstraints;
  installMethod(trackProto, 'applyConstraints', function (constraints = {}) {
    if (!tracks.has(this)) return priorApply.call(this, constraints);
    try {
      failure(host.cameraTrack(tracks.get(this), 'apply', JSON.stringify(normalize(constraints))));
      return platformPromiseResolve();
    } catch (error) {
      return platformPromiseReject(error);
    }
  });
  for (const name of ['onended', 'onmute', 'onunmute'])
    Object.defineProperty(trackProto, name, {
      get() {
        return handlersFor(this)[name.slice(2)] || null;
      },
      set(value) {
        handlersFor(this)[name.slice(2)] = typeof value === 'function' ? value : null;
      },
      configurable: true,
      enumerable: true,
    });
  const Stream = globalThis.MediaStream;
  function MediaStream(value) {
    if (!new.target)
      throw new TypeError("Failed to construct 'MediaStream': Please use the 'new' operator");
    const object = new EventTarget();
    Object.setPrototypeOf(object, new.target.prototype);
    let members = [];
    if (value !== undefined)
      members = value instanceof MediaStream ? value.getTracks() : Array.from(value);
    for (const track of members)
      if (!(track instanceof MediaStreamTrack)) throw new TypeError('Expected MediaStreamTrack');
    streams.set(object, { id: host.internalRandomUUID(), tracks: [...new Set(members)] });
    return object;
  }
  MediaStream.prototype = Stream.prototype;
  Object.defineProperty(MediaStream.prototype, 'constructor', {
    value: MediaStream,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(globalThis, 'MediaStream', {
    value: MediaStream,
    writable: true,
    configurable: true,
  });
  if ('webkitMediaStream' in globalThis) globalThis.webkitMediaStream = MediaStream;
  for (const name of [
    'getTracks',
    'getVideoTracks',
    'getAudioTracks',
    'getTrackById',
    'addTrack',
    'removeTrack',
    'clone',
  ]) {
    const prior = MediaStream.prototype[name];
    installMethod(MediaStream.prototype, name, function (track) {
      const s = streams.get(this);
      if (!s) {
        if (typeof prior === 'function') return prior.apply(this, arguments);
        throw new TypeError('Illegal invocation');
      }
      if (name === 'getTracks') return s.tracks.slice();
      if (name === 'getVideoTracks') return s.tracks.filter((t) => t.kind === 'video');
      if (name === 'getAudioTracks') return s.tracks.filter((t) => t.kind === 'audio');
      if (name === 'getTrackById') return s.tracks.find((t) => t.id === String(track)) || null;
      if (name === 'clone') return new MediaStream(s.tracks.map((t) => t.clone()));
      if (!(track instanceof MediaStreamTrack)) throw new TypeError('Expected MediaStreamTrack');
      if (name === 'addTrack' && !s.tracks.includes(track)) s.tracks.push(track);
      if (name === 'removeTrack') s.tracks = s.tracks.filter((t) => t !== track);
    });
  }
  for (const name of ['id', 'active']) {
    const prior = Object.getOwnPropertyDescriptor(MediaStream.prototype, name);
    Object.defineProperty(MediaStream.prototype, name, {
      get() {
        const s = streams.get(this);
        if (!s) {
          if (prior?.get) return prior.get.call(this);
          throw new TypeError('Illegal invocation');
        }
        return name === 'id' ? s.id : s.tracks.some((t) => t.readyState === 'live');
      },
      configurable: true,
      enumerable: true,
    });
  }
  const attachedTrack = (element) => {
    const stream = mediaState(element).srcObject;
    return (
      stream?.getVideoTracks().find((track) => tracks.has(track) && track.readyState === 'live') ||
      null
    );
  };
  const attachedMediaTrack = (element) =>
    attachedTrack(element) ||
    mediaState(element)
      .srcObject?.getAudioTracks()
      .find((track) => tracks.has(track) && track.readyState === 'live') ||
    null;
  const frame = (element) => {
    const track = attachedTrack(element);
    if (!track) return null;
    const row = host.cameraTrack(tracks.get(track), 'frame');
    if (!row) return null;
    const raw = atob(row.pixels),
      pixels = new Uint8ClampedArray(raw.length);
    for (let i = 0; i < raw.length; i++) pixels[i] = raw.charCodeAt(i);
    return { ...row, pixels };
  };
  Object.defineProperty(HTMLMediaElement.prototype, 'srcObject', {
    get() {
      return mediaState(this).srcObject || null;
    },
    set(value) {
      if (value !== null && !(value instanceof MediaStream))
        throw new TypeError('Expected MediaStream or null');
      const state = mediaState(this);
      if (state.srcObject === value) return;
      state.srcObject = value;
      state.readyState = 0;
      state.networkState = value ? 2 : 0;
      state.duration = NaN;
      const priorAttachment = attachments.get(this);
      if (value) {
        attachments.set(this, {
          callbacks: priorAttachment?.callbacks || new Map(),
          nextCallback: priorAttachment?.nextCallback || 1,
          sequence: 0,
          presented: 0,
        });
        host.setTimer(() => deliver(this), 0, false);
      }
    },
    configurable: true,
    enumerable: true,
  });
  for (const name of ['videoWidth', 'videoHeight']) {
    const prior = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, name);
    Object.defineProperty(HTMLVideoElement.prototype, name, {
      get() {
        if (mediaState(this).srcObject) {
          const track = attachedTrack(this);
          const row = track ? host.cameraTrack(tracks.get(track), 'info') : null;
          return row ? row[name === 'videoWidth' ? 'width' : 'height'] : 0;
        }
        return prior.get.call(this);
      },
      configurable: true,
      enumerable: true,
    });
  }
  const priorPlay = HTMLMediaElement.prototype.play;
  installMethod(HTMLMediaElement.prototype, 'play', function () {
    if (!mediaState(this).srcObject) return priorPlay.call(this);
    if (!attachedMediaTrack(this))
      return platformPromiseReject(
        platformDOMException('Stream has no live captured track', 'NotSupportedError'),
      );
    const state = mediaState(this);
    state.paused = false;
    return new platformPromise((resolve, reject) => {
      state.pendingPlay.push({ resolve, reject });
      host.setTimer(() => deliver(this), 0, false);
    });
  });
  const deliver = (element) => {
    const entry = attachments.get(element);
    if (!entry) return;
    const track = attachedMediaTrack(element);
    if (!track) {
      for (const pending of mediaState(element).pendingPlay.splice(0))
        pending.reject(platformDOMException('The stream ended', 'AbortError'));
      return;
    }
    const state = mediaState(element);
    const row = host.cameraTrack(tracks.get(track), 'info');
    if (!row) return;
    if (!state.readyState) {
      state.readyState = 4;
      state.networkState = 1;
      state.duration = Infinity;
      for (const name of ['loadedmetadata', 'loadeddata', 'canplay', 'canplaythrough'])
        dispatchTrusted(element, new Event(name));
      if (element.autoplay) state.paused = false;
    }
    if (state.paused) return;
    const pending = state.pendingPlay.splice(0);
    if (pending.length) {
      dispatchTrusted(element, new Event('play'));
      dispatchTrusted(element, new Event('playing'));
      for (const p of pending) p.resolve();
    }
    if (!row || entry.sequence === row.sequence) return;
    entry.sequence = row.sequence;
    entry.presented++;
    if (entry.start === undefined) entry.start = row.time;
    state.currentTime = Math.max(0, row.time - entry.start);
    if (track.kind !== 'video') return;
    const callbacks = [...entry.callbacks.values()];
    entry.callbacks.clear();
    const now = performance.now();
    for (const callback of callbacks)
      callback(now, {
        presentationTime: now,
        expectedDisplayTime: now,
        width: row.width,
        height: row.height,
        mediaTime: state.currentTime,
        presentedFrames: entry.presented,
        processingDuration: 0,
      });
  };
  installMethod(HTMLVideoElement.prototype, 'requestVideoFrameCallback', function (callback) {
    if (typeof callback !== 'function') throw new TypeError('Expected callback');
    let entry = attachments.get(this);
    if (!entry) {
      entry = { callbacks: new Map(), nextCallback: 1, sequence: 0, presented: 0 };
      attachments.set(this, entry);
    }
    const id = entry.nextCallback++;
    entry.callbacks.set(id, callback);
    return id;
  });
  installMethod(HTMLVideoElement.prototype, 'cancelVideoFrameCallback', function (id) {
    attachments.get(this)?.callbacks.delete(Number(id));
  });
  registerBootstrapCallback('installCameraNotifier', (id, kind) => {
    const track = wrappers.get(id);
    if (!track) return;
    if (['ended', 'mute', 'unmute'].includes(kind)) dispatchTrusted(track, new Event(kind));
    for (const element of attachments.keys())
      if (mediaState(element).srcObject?.getTracks().includes(track)) deliver(element);
  });
  return {
    async open(constraints) {
      const opened = [];
      try {
        if (constraints.audio)
          opened.push(
            makeTrack(
              failure(await host.microphoneOpen(JSON.stringify(normalize(constraints.audio)))),
            ),
          );
        if (constraints.video)
          opened.push(
            makeTrack(failure(await host.cameraOpen(JSON.stringify(normalize(constraints.video))))),
          );
        return new MediaStream(opened);
      } catch (error) {
        for (const track of opened) track.stop();
        throw error;
      }
    },
    frame,
    audio(track, start = 0, count = 0, rate = 48000) {
      const id = tracks.get(track);
      if (!id || track.kind !== 'audio')
        throw platformDOMException('Audio source is not captured', 'NotSupportedError');
      const row = host.cameraTrack(id, 'pcm', start, count, rate);
      const raw = atob(row.pcm),
        bytes = new Uint8Array(raw.length);
      for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
      const view = new DataView(bytes.buffer);
      const samples = new Float32Array(raw.length / 4);
      for (let i = 0; i < samples.length; i++) samples[i] = view.getFloat32(i * 4, true);
      return { ...row, samples };
    },
    makeTrack,
    getTrack(id) {
      return wrappers.get(id) || null;
    },
    trackID(track) {
      const id = tracks.get(track);
      if (!id)
        throw platformDOMException('Only captured media tracks are supported', 'NotSupportedError');
      return id;
    },
  };
})();
