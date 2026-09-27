// Realtime contexts use the same PCM graph and parameter timelines as offline
// rendering. A Page timer advances complete quanta; no device or global clock.
const realtimeContexts = new Set();
const currentClock = () => performance.now() / 1000;
const stateEvent = (object) => setTimeout(() => emit(object, 'statechange'), 0);
const contextFor = (object) =>
  contexts.has(object) ? object : (nodes.get(object) || params.get(object))?.context;
const advance = (object) => {
  const c = contexts.get(object);
  if (!c?.realtime || c.state !== 'running' || c.advancing) return;
  const target =
    Math.floor(
      ((c.clockFrame / c.sampleRate + Math.max(0, currentClock() - c.clockOrigin)) * c.sampleRate) /
        128,
    ) * 128;
  c.advancing = true;
  try {
    while (c.renderFrame < target) {
      render(object, c.renderFrame, 128);
      c.renderFrame += 128;
      c.currentTime = c.renderFrame / c.sampleRate;
      for (const node of c.nodes) {
        const n = nodes.get(node);
        if (n.ended && !n.endedPosted) {
          n.endedPosted = true;
          setTimeout(() => emit(node, 'ended'), 0);
        }
      }
    }
  } finally {
    c.advancing = false;
  }
};
synchronizeOwner = (object) => {
  const context = contextFor(object);
  if (context) advance(context);
};
const scheduleAudio = (object) => {
  const c = contexts.get(object);
  if (c.timer !== null || c.state !== 'running') return;
  c.timer = setTimeout(() => {
    c.timer = null;
    advance(object);
    scheduleAudio(object);
  }, 10);
};
const startContext = (object) => {
  const c = contexts.get(object);
  if (c.state === 'closed') return;
  c.state = 'running';
  c.clockOrigin = currentClock();
  c.clockFrame = c.renderFrame;
  stateEvent(object);
  scheduleAudio(object);
  const pending = c.pendingResume.splice(0);
  for (const pendingResume of pending) pendingResume.resolve();
};
const activated = () => !!navigator.userActivation?.hasBeenActive;
for (const event of ['mousedown', 'keydown', 'touchend'])
  globalThis.addEventListener(
    event,
    (e) => {
      if (!e.isTrusted) return;
      for (const object of realtimeContexts) {
        const c = contexts.get(object);
        if (c.state === 'suspended' && !c.userSuspended) startContext(object);
      }
    },
    true,
  );
function AudioContext(options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  options = options ?? {};
  const device = host.audioDevice();
  const sampleRate =
    options.sampleRate === undefined ? device.sampleRate : float(options.sampleRate);
  if (sampleRate < 3000 || sampleRate > 768000) throw exception('NotSupportedError');
  const hint = options.latencyHint ?? 'interactive';
  if (typeof hint === 'string' && !['interactive', 'balanced', 'playback'].includes(hint))
    throw new TypeError('Invalid latencyHint');
  if (typeof hint !== 'string') finite(hint);
  const minimumFrames = Math.max(128, Math.round(sampleRate * device.bufferDuration));
  const requestedFrames =
    typeof hint === 'string'
      ? minimumFrames * (hint === 'playback' ? 2 : 1)
      : Math.max(
          minimumFrames,
          Math.ceil((Number(hint) * sampleRate) / minimumFrames) * minimumFrames,
        );
  const object = new EventTarget();
  Object.setPrototypeOf(object, new.target.prototype);
  const c = {
    object,
    realtime: true,
    state: 'suspended',
    sampleRate,
    numberOfChannels: device.channels,
    currentTime: 0,
    nodes: [],
    params: [],
    onstatechange: null,
    onsinkchange: null,
    renderFrame: 0,
    clockFrame: 0,
    clockOrigin: currentClock(),
    timer: null,
    pendingResume: [],
    userSuspended: false,
    baseLatency: Math.min(device.maxBufferFrames, requestedFrames) / sampleRate,
    sinkId: '',
  };
  contexts.set(object, c);
  c.destination = makeNode(object, 'AudioDestinationNode', 1, 0, {
    channelCount: device.channels,
    channelCountMode: 'explicit',
    maxChannelCount: device.channels,
  });
  if (options.sinkId !== undefined) {
    if (typeof options.sinkId === 'object' && options.sinkId !== null) {
      if (options.sinkId.type !== 'none') throw new TypeError('Invalid sink type');
      c.sinkId = Object.freeze({ type: 'none' });
    } else if (String(options.sinkId) !== '')
      throw exception('NotFoundError', 'Audio output device not found');
  }
  realtimeContexts.add(object);
  if (activated()) startContext(object);
  return object;
}
install('AudioContext', AudioContext);
if ('webkitAudioContext' in globalThis)
  Object.defineProperty(globalThis, 'webkitAudioContext', {
    value: AudioContext,
    writable: true,
    configurable: true,
  });
getter('BaseAudioContext', 'currentTime', contexts, (c) => {
  if (c.realtime) advance(c.object);
  return c.currentTime;
});
getter('AudioContext', 'baseLatency', contexts, (c) => c.baseLatency);
getter('AudioContext', 'outputLatency', contexts, (c) =>
  c.state === 'running' && c.renderFrame > 0 ? c.baseLatency : 0,
);
getter('AudioContext', 'sinkId', contexts, 'sinkId');
getter('AudioContext', 'onsinkchange', contexts, 'onsinkchange', function (value) {
  requireSlot(contexts, this).onsinkchange = typeof value === 'function' ? value : null;
});
method('AudioContext', 'suspend', function suspend() {
  try {
    const c = requireSlot(contexts, this);
    if (c.state === 'closed') throw exception('InvalidStateError');
    c.userSuspended = true;
    c.state = 'suspended';
    if (c.timer !== null) {
      clearTimeout(c.timer);
      c.timer = null;
    }
    stateEvent(this);
    return platformPromiseResolve();
  } catch (e) {
    return platformPromiseReject(e);
  }
});
method('AudioContext', 'resume', function resume() {
  try {
    const c = requireSlot(contexts, this);
    if (c.state === 'closed') throw exception('InvalidStateError');
    if (c.state === 'running') return platformPromiseResolve();
    c.userSuspended = false;
    return new platformPromise((resolve, reject) => {
      c.pendingResume.push({ resolve, reject });
      if (activated()) startContext(this);
    });
  } catch (e) {
    return platformPromiseReject(e);
  }
});
method('AudioContext', 'close', function close() {
  try {
    const c = requireSlot(contexts, this);
    if (c.state === 'closed') throw exception('InvalidStateError');
    c.state = 'closed';
    if (c.timer !== null) {
      clearTimeout(c.timer);
      c.timer = null;
    }
    realtimeContexts.delete(this);
    for (const pending of c.pendingResume.splice(0)) pending.reject(exception('InvalidStateError'));
    stateEvent(this);
    return platformPromiseResolve();
  } catch (e) {
    return platformPromiseReject(e);
  }
});
method('AudioContext', 'getOutputTimestamp', function getOutputTimestamp() {
  const c = requireSlot(contexts, this);
  return {
    contextTime: Math.max(0, c.currentTime - c.baseLatency),
    performanceTime: c.currentTime ? performance.now() : 0,
  };
});
method('AudioContext', 'setSinkId', function setSinkId(value) {
  try {
    const c = requireSlot(contexts, this);
    if (c.state === 'closed') throw exception('InvalidStateError');
    let sink;
    if (typeof value === 'object' && value !== null) {
      if (value.type !== 'none') throw new TypeError('Invalid sink type');
      sink = Object.freeze({ type: 'none' });
    } else {
      sink = String(value);
      if (sink !== '') throw exception('NotFoundError');
    }
    if (JSON.stringify(sink) !== JSON.stringify(c.sinkId)) {
      c.sinkId = sink;
      setTimeout(() => emit(this, 'sinkchange'), 0);
    }
    return platformPromiseResolve();
  } catch (e) {
    return platformPromiseReject(e);
  }
});
