// Stateful observations added to the shared offline/realtime PCM evaluator.
const nodeOptions = (object, options, parameters = []) => {
  options = options ?? {};
  for (const key of ['channelCount', 'channelCountMode', 'channelInterpretation'])
    if (options[key] !== undefined) object[key] = options[key];
  for (const key of parameters) if (options[key] !== undefined) object[key].value = options[key];
  return object;
};
function DelayNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const maximum = options?.maxDelayTime === undefined ? 1 : finite(options.maxDelayTime);
  if (maximum <= 0 || maximum >= 180) throw new RangeError('Invalid maxDelayTime');
  const object = makeNode(context, 'DelayNode', 1, 1, {
    delayTime: makeParam(context, 0, 'a-rate', 0, maximum),
    maxDelayTime: maximum,
    delayBuffers: [],
    delayWrite: 0,
    feedbackOutput: null,
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options, ['delayTime']);
}
install('DelayNode', DelayNode);
getter('DelayNode', 'delayTime', nodes);
method('BaseAudioContext', 'createDelay', function createDelay(maxDelayTime) {
  return new DelayNode(this, maxDelayTime === undefined ? {} : { maxDelayTime });
});
function ChannelSplitterNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const count = options?.numberOfOutputs === undefined ? 6 : Number(options.numberOfOutputs) >>> 0;
  if (count < 1 || count > 32) throw exception('IndexSizeError');
  const object = makeNode(context, 'ChannelSplitterNode', 1, count, {
    channelCount: count,
    channelCountMode: 'explicit',
    channelInterpretation: 'discrete',
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return object;
}
install('ChannelSplitterNode', ChannelSplitterNode);
method('BaseAudioContext', 'createChannelSplitter', function createChannelSplitter(count) {
  return new ChannelSplitterNode(this, count === undefined ? {} : { numberOfOutputs: count });
});
function ChannelMergerNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const count = options?.numberOfInputs === undefined ? 6 : Number(options.numberOfInputs) >>> 0;
  if (count < 1 || count > 32) throw exception('IndexSizeError');
  const object = makeNode(context, 'ChannelMergerNode', count, 1, {
    channelCount: 1,
    channelCountMode: 'explicit',
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return object;
}
install('ChannelMergerNode', ChannelMergerNode);
method('BaseAudioContext', 'createChannelMerger', function createChannelMerger(count) {
  return new ChannelMergerNode(this, count === undefined ? {} : { numberOfInputs: count });
});
function StereoPannerNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'StereoPannerNode', 1, 1, {
    channelCount: 2,
    channelCountMode: 'clamped-max',
    pan: makeParam(context, 0, 'a-rate', -1, 1),
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options, ['pan']);
}
install('StereoPannerNode', StereoPannerNode);
getter('StereoPannerNode', 'pan', nodes);
method('BaseAudioContext', 'createStereoPanner', function createStereoPanner() {
  return new StereoPannerNode(this);
});
function WaveShaperNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'WaveShaperNode', 1, 1, { curve: null, oversample: 'none' });
  Object.setPrototypeOf(object, new.target.prototype);
  nodeOptions(object, options);
  if (options?.curve !== undefined) object.curve = options.curve;
  if (options?.oversample !== undefined) object.oversample = options.oversample;
  return object;
}
install('WaveShaperNode', WaveShaperNode);
getter(
  'WaveShaperNode',
  'curve',
  nodes,
  (n) => n.curve?.slice() ?? null,
  function (value) {
    const n = requireSlot(nodes, this);
    if (value === null) n.curve = null;
    else {
      value = Float32Array.from(value, float);
      if (value.length < 2) throw exception('InvalidStateError');
      n.curve = value;
    }
  },
);
getter('WaveShaperNode', 'oversample', nodes, 'oversample', function (value) {
  value = String(value);
  if (!['none', '2x', '4x'].includes(value)) return;
  requireSlot(nodes, this).oversample = value;
});
method('BaseAudioContext', 'createWaveShaper', function createWaveShaper() {
  return new WaveShaperNode(this);
});
function ConvolverNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'ConvolverNode', 1, 1, {
    channelCount: 2,
    channelCountMode: 'clamped-max',
    buffer: null,
    normalize: true,
    convolverHistory: [],
  });
  Object.setPrototypeOf(object, new.target.prototype);
  nodeOptions(object, options);
  if (options?.disableNormalization !== undefined) object.normalize = !options.disableNormalization;
  if (options?.buffer !== undefined) object.buffer = options.buffer;
  return object;
}
install('ConvolverNode', ConvolverNode);
getter('ConvolverNode', 'buffer', nodes, 'buffer', function (value) {
  const n = requireSlot(nodes, this);
  if (value !== null) {
    const buffer = requireSlot(buffers, value);
    if (![1, 2, 4].includes(buffer.numberOfChannels)) throw exception('NotSupportedError');
    if (n.buffer) throw exception('InvalidStateError');
  }
  n.buffer = value;
});
getter('ConvolverNode', 'normalize', nodes, 'normalize', function (value) {
  requireSlot(nodes, this).normalize = !!value;
});
method('BaseAudioContext', 'createConvolver', function createConvolver() {
  return new ConvolverNode(this);
});

const listenerFor = (context) => {
  const c = requireSlot(contexts, context);
  if (c.listener) return c.listener;
  const listener = Object.create(AudioListener.prototype),
    state = {
      context,
      positionX: makeParam(context, 0, 'a-rate'),
      positionY: makeParam(context, 0, 'a-rate'),
      positionZ: makeParam(context, 0, 'a-rate'),
      forwardX: makeParam(context, 0, 'a-rate'),
      forwardY: makeParam(context, 0, 'a-rate'),
      forwardZ: makeParam(context, -1, 'a-rate'),
      upX: makeParam(context, 0, 'a-rate'),
      upY: makeParam(context, 1, 'a-rate'),
      upZ: makeParam(context, 0, 'a-rate'),
    };
  listenerSlots.set(listener, state);
  c.listener = listener;
  return listener;
};
const listenerSlots = new WeakMap();
getter('BaseAudioContext', 'listener', contexts, (c) => listenerFor(c.object));
for (const key of [
  'positionX',
  'positionY',
  'positionZ',
  'forwardX',
  'forwardY',
  'forwardZ',
  'upX',
  'upY',
  'upZ',
])
  getter('AudioListener', key, listenerSlots);
for (const [name, keys] of [
  ['setPosition', ['positionX', 'positionY', 'positionZ']],
  ['setOrientation', ['forwardX', 'forwardY', 'forwardZ', 'upX', 'upY', 'upZ']],
])
  method('AudioListener', name, function (...values) {
    const state = requireSlot(listenerSlots, this);
    if (values.length < keys.length) throw new TypeError('Not enough arguments');
    keys.forEach((key, index) => (state[key].value = finite(values[index])));
  });
function PannerNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'PannerNode', 1, 1, {
    channelCount: 2,
    channelCountMode: 'clamped-max',
    panningModel: 'equalpower',
    distanceModel: 'inverse',
    refDistance: 1,
    maxDistance: 10000,
    rolloffFactor: 1,
    coneInnerAngle: 360,
    coneOuterAngle: 360,
    coneOuterGain: 0,
    positionX: makeParam(context, 0, 'a-rate'),
    positionY: makeParam(context, 0, 'a-rate'),
    positionZ: makeParam(context, 0, 'a-rate'),
    orientationX: makeParam(context, 1, 'a-rate'),
    orientationY: makeParam(context, 0, 'a-rate'),
    orientationZ: makeParam(context, 0, 'a-rate'),
  });
  Object.setPrototypeOf(object, new.target.prototype);
  nodeOptions(object, options, [
    'positionX',
    'positionY',
    'positionZ',
    'orientationX',
    'orientationY',
    'orientationZ',
  ]);
  for (const key of [
    'panningModel',
    'distanceModel',
    'refDistance',
    'maxDistance',
    'rolloffFactor',
    'coneInnerAngle',
    'coneOuterAngle',
    'coneOuterGain',
  ])
    if (options?.[key] !== undefined) object[key] = options[key];
  return object;
}
install('PannerNode', PannerNode);
for (const key of [
  'positionX',
  'positionY',
  'positionZ',
  'orientationX',
  'orientationY',
  'orientationZ',
])
  getter('PannerNode', key, nodes);
for (const [key, values] of [
  ['panningModel', ['equalpower', 'HRTF']],
  ['distanceModel', ['linear', 'inverse', 'exponential']],
])
  getter('PannerNode', key, nodes, key, function (value) {
    value = String(value);
    if (!values.includes(value)) return;
    requireSlot(nodes, this)[key] = value;
  });
for (const key of [
  'refDistance',
  'maxDistance',
  'rolloffFactor',
  'coneInnerAngle',
  'coneOuterAngle',
  'coneOuterGain',
])
  getter('PannerNode', key, nodes, key, function (value) {
    value = finite(value);
    if ((key === 'refDistance' || key === 'maxDistance') && value <= 0)
      throw new RangeError('Positive distance required');
    if ((key === 'rolloffFactor' || key === 'coneOuterGain') && value < 0)
      throw new RangeError('Negative value');
    if (key === 'coneOuterGain' && value > 1) throw new RangeError('Gain exceeds one');
    requireSlot(nodes, this)[key] = value;
  });
for (const [name, keys] of [
  ['setPosition', ['positionX', 'positionY', 'positionZ']],
  ['setOrientation', ['orientationX', 'orientationY', 'orientationZ']],
])
  method('PannerNode', name, function (...values) {
    const n = requireSlot(nodes, this);
    if (values.length < 3) throw new TypeError('Not enough arguments');
    keys.forEach((key, index) => (n[key].value = finite(values[index])));
  });
method('BaseAudioContext', 'createPanner', function createPanner() {
  return new PannerNode(this);
});

// Media-backed nodes expose stable ownership and graph behavior without
// opening a media or audio output backend. Destination streams are Page-owned
// observable objects with one live audio track; source nodes render silence
// until a future media decoder supplies PCM to the same graph model.
const audioMediaElements = new WeakMap(),
  audioStreams = new WeakMap(),
  sharedMediaTrackSlots = new WeakMap();
globalThis.__mimicMediaTrackSlots = sharedMediaTrackSlots;
const mediaTrackDescriptor = (name) =>
  typeof MediaStreamTrack === 'function'
    ? Object.getOwnPropertyDescriptor(MediaStreamTrack.prototype, name)
    : null;
const priorTrackKind = mediaTrackDescriptor('kind'),
  priorTrackReadyState = mediaTrackDescriptor('readyState');
if (typeof MediaStreamTrack === 'function') {
  for (const [name, prior, fallback] of [
    ['kind', priorTrackKind, 'audio'],
    ['readyState', priorTrackReadyState, 'live'],
  ])
    Object.defineProperty(MediaStreamTrack.prototype, name, {
      get() {
        const state = sharedMediaTrackSlots.get(this);
        if (state) return state[name] || fallback;
        if (prior?.get) return prior.get.call(this);
        throw new TypeError('Illegal invocation');
      },
      enumerable: prior?.enumerable ?? true,
      configurable: true,
    });
}
if (typeof MediaStream === 'function') {
  for (const name of ['getTracks', 'getAudioTracks']) {
    const prior = MediaStream.prototype[name];
    method('MediaStream', name, function () {
      const state = audioStreams.get(this);
      if (state) return state.tracks.slice();
      if (typeof prior === 'function') return prior.call(this);
      throw new TypeError('Illegal invocation');
    });
  }
}
const makeAudioDestinationStream = () => {
  const track = Object.create(MediaStreamTrack.prototype),
    stream = Object.create(MediaStream.prototype);
  sharedMediaTrackSlots.set(track, {
    kind: 'audio',
    id: host.internalRandomUUID(),
    enabled: true,
    muted: false,
    readyState: 'live',
  });
  audioStreams.set(stream, { tracks: [track] });
  return stream;
};
function MediaStreamAudioDestinationNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'MediaStreamAudioDestinationNode', 1, 0, {
    channelCount: 2,
    channelCountMode: 'explicit',
    stream: makeAudioDestinationStream(),
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options);
}
install('MediaStreamAudioDestinationNode', MediaStreamAudioDestinationNode);
getter('MediaStreamAudioDestinationNode', 'stream', nodes);
method('AudioContext', 'createMediaStreamDestination', function createMediaStreamDestination() {
  return new MediaStreamAudioDestinationNode(this);
});
function MediaStreamAudioSourceNode(context, options = {}) {
  if (!new.target || !options?.mediaStream) throw new TypeError('Expected mediaStream');
  const stream = options.mediaStream;
  if (!audioStreams.has(stream) && !(stream instanceof MediaStream))
    throw new TypeError('Expected MediaStream');
  const captureTrack = stream
    .getAudioTracks()
    .slice()
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))[0];
  if (!captureTrack) throw exception('InvalidStateError');
  // Destination catalog streams retain their existing graph boundary. Native
  // captured streams supply PCM through their canonical capture track.
  const capture = audioStreams.has(stream) ? null : cameraCaptureModel.audio(captureTrack);
  const object = makeNode(context, 'MediaStreamAudioSourceNode', 0, 1, {
    mediaStream: stream,
    captureTrack: capture ? captureTrack : null,
    captureOrigin: capture
      ? capture.end - capture.blockFrames - contexts.get(context).currentTime * capture.sampleRate
      : 0,
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options);
}
install('MediaStreamAudioSourceNode', MediaStreamAudioSourceNode);
getter('MediaStreamAudioSourceNode', 'mediaStream', nodes);
method('AudioContext', 'createMediaStreamSource', function createMediaStreamSource(stream) {
  if (!arguments.length) throw new TypeError('Expected MediaStream');
  return new MediaStreamAudioSourceNode(this, { mediaStream: stream });
});
function MediaElementAudioSourceNode(context, options = {}) {
  if (!new.target || !options?.mediaElement) throw new TypeError('Expected mediaElement');
  const element = options.mediaElement;
  if (audioMediaElements.has(element)) throw exception('InvalidStateError');
  const object = makeNode(context, 'MediaElementAudioSourceNode', 0, 1, {
    mediaElement: element,
  });
  Object.setPrototypeOf(object, new.target.prototype);
  audioMediaElements.set(element, object);
  return nodeOptions(object, options);
}
install('MediaElementAudioSourceNode', MediaElementAudioSourceNode);
getter('MediaElementAudioSourceNode', 'mediaElement', nodes);
method('AudioContext', 'createMediaElementSource', function createMediaElementSource(element) {
  if (!arguments.length) throw new TypeError('Expected media element');
  return new MediaElementAudioSourceNode(this, { mediaElement: element });
});
const processorEvents = new WeakMap();
function ScriptProcessorNode(context, bufferSize, inputChannels, outputChannels) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(
    context,
    'ScriptProcessorNode',
    inputChannels ? 1 : 0,
    outputChannels ? 1 : 0,
    {
      bufferSize,
      inputChannels,
      outputChannels,
      onaudioprocess: null,
    },
  );
  Object.setPrototypeOf(object, new.target.prototype);
  processorEvents.set(object, true);
  return object;
}
install('ScriptProcessorNode', ScriptProcessorNode);
getter('ScriptProcessorNode', 'bufferSize', nodes);
getter('ScriptProcessorNode', 'onaudioprocess', nodes, 'onaudioprocess', function (value) {
  requireSlot(nodes, this).onaudioprocess = typeof value === 'function' ? value : null;
});
method(
  'BaseAudioContext',
  'createScriptProcessor',
  function createScriptProcessor(bufferSize = 0, inputChannels = 2, outputChannels = 2) {
    bufferSize = Number(bufferSize) >>> 0;
    inputChannels = Number(inputChannels) >>> 0;
    outputChannels = Number(outputChannels) >>> 0;
    if (
      (bufferSize && ![256, 512, 1024, 2048, 4096, 8192, 16384].includes(bufferSize)) ||
      inputChannels > 32 ||
      outputChannels > 32 ||
      (!inputChannels && !outputChannels)
    )
      throw exception('IndexSizeError');
    if (!bufferSize) bufferSize = 2048;
    return new ScriptProcessorNode(this, bufferSize, inputChannels, outputChannels);
  },
);

const workletSlots = new WeakMap(),
  parameterMapSlots = new WeakMap();
const audioWorkletPrototype = Object.create(Object.prototype);
Object.defineProperty(audioWorkletPrototype, Symbol.toStringTag, {
  value: 'AudioWorklet',
  configurable: true,
});
const workletFor = (context) => {
  const c = requireSlot(contexts, context);
  if (c.audioWorklet) return c.audioWorklet;
  const object = Object.create(audioWorkletPrototype);
  workletSlots.set(object, { context, processors: new Map(), modules: new Set() });
  c.audioWorklet = object;
  return object;
};
getter('BaseAudioContext', 'audioWorklet', contexts, (c) => workletFor(c.object));
const addWorkletModule = function addModule(url) {
  const worklet = requireSlot(workletSlots, this),
    address = String(url);
  return fetch(address)
    .then((response) => {
      if (!response.ok) throw platformDOMException('Unable to load worklet module', 'AbortError');
      return response.text();
    })
    .then((source) => {
      const registrations = [
        ...source.matchAll(/registerProcessor\s*\(\s*(['"])([^'"\\]+)\1\s*,/g),
      ];
      if (!registrations.length)
        throw platformDOMException('No processor registered', 'AbortError');
      let descriptors = [];
      const literal =
        /parameterDescriptors\s*\(\s*\)\s*\{\s*return\s+(\[[\s\S]*?\])\s*;?\s*\}/.exec(source)?.[1];
      if (literal && /^[\s\[\]{},:.'"+\-\w\d]*$/.test(literal)) {
        try {
          descriptors = Function('return (' + literal + ')')();
        } catch {}
      }
      for (const registration of registrations) {
        const name = registration[2];
        if (worklet.processors.has(name))
          throw platformDOMException('Duplicate processor name', 'NotSupportedError');
        worklet.processors.set(name, descriptors);
      }
      worklet.modules.add(address);
    });
};
markNative(addWorkletModule, 'addModule');
Object.defineProperty(audioWorkletPrototype, 'addModule', {
  value: addWorkletModule,
  writable: true,
  enumerable: true,
  configurable: true,
});
const makeParameterMap = (values) => {
  const object = Object.create(AudioParamMap.prototype);
  parameterMapSlots.set(object, values);
  return object;
};
for (const name of ['get', 'has', 'keys', 'values', 'entries'])
  method('AudioParamMap', name, function (...args) {
    return requireSlot(parameterMapSlots, this)[name](...args);
  });
method('AudioParamMap', 'forEach', function forEach(callback, thisArg) {
  if (typeof callback !== 'function') throw new TypeError('Expected callback');
  const map = requireSlot(parameterMapSlots, this);
  map.forEach((value, key) => callback.call(thisArg, value, key, this));
});
getter('AudioParamMap', 'size', parameterMapSlots, (map) => map.size);
Object.defineProperty(AudioParamMap.prototype, Symbol.iterator, {
  value: function () {
    return requireSlot(parameterMapSlots, this).entries();
  },
  writable: true,
  configurable: true,
});
function AudioWorkletNode(context, name, options = {}) {
  if (!new.target || arguments.length < 2) throw new TypeError('Expected context and name');
  const worklet = requireSlot(workletSlots, workletFor(context)),
    descriptors = worklet.processors.get(String(name));
  if (!descriptors) throw exception('InvalidStateError', 'Processor is not registered');
  options = options ?? {};
  const inputs = options.numberOfInputs === undefined ? 1 : Number(options.numberOfInputs) >>> 0,
    outputs = options.numberOfOutputs === undefined ? 1 : Number(options.numberOfOutputs) >>> 0,
    channelCounts =
      options.outputChannelCount === undefined
        ? null
        : Array.from(options.outputChannelCount, (value) => Number(value) >>> 0);
  if (
    inputs > 32 ||
    outputs > 32 ||
    (!inputs && !outputs) ||
    (channelCounts &&
      (channelCounts.length !== outputs || channelCounts.some((value) => value < 1 || value > 32)))
  )
    throw exception('NotSupportedError');
  const parameters = new Map(),
    data = options.parameterData || {};
  for (const descriptor of descriptors) {
    const parameter = makeParam(
      context,
      descriptor.defaultValue === undefined ? 0 : descriptor.defaultValue,
      descriptor.automationRate || 'a-rate',
      descriptor.minValue === undefined ? -3.4028234663852886e38 : descriptor.minValue,
      descriptor.maxValue === undefined ? 3.4028234663852886e38 : descriptor.maxValue,
    );
    if (Object.hasOwn(data, descriptor.name)) parameter.value = data[descriptor.name];
    parameters.set(String(descriptor.name), parameter);
  }
  const channel = new MessageChannel(),
    object = makeNode(context, 'AudioWorkletNode', inputs, outputs, {
      parameters: makeParameterMap(parameters),
      port: channel.port1,
      processorPort: channel.port2,
      onprocessorerror: null,
      outputChannelCount: channelCounts,
    });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options);
}
install('AudioWorkletNode', AudioWorkletNode);
for (const key of ['port', 'parameters']) getter('AudioWorkletNode', key, nodes);
getter('AudioWorkletNode', 'onprocessorerror', nodes, 'onprocessorerror', function (value) {
  requireSlot(nodes, this).onprocessorerror = typeof value === 'function' ? value : null;
});
function ConstantSourceNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'ConstantSourceNode', 0, 1, {
    offset: makeParam(context, 1),
    started: false,
    startTime: 0,
    stopTime: Infinity,
    onended: null,
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options, ['offset']);
}
install('ConstantSourceNode', ConstantSourceNode);
getter('ConstantSourceNode', 'offset', nodes);
method('BaseAudioContext', 'createConstantSource', function createConstantSource() {
  return new ConstantSourceNode(this);
});
function AnalyserNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const object = makeNode(context, 'AnalyserNode', 1, 1, {
    fftSize: 2048,
    minDecibels: -100,
    maxDecibels: -30,
    smoothingTimeConstant: 0.8,
    history: new Float32Array(32768),
    historyFrame: 0,
    analysisVersion: 0,
    frequencyVersion: -1,
    spectrum: null,
  });
  Object.setPrototypeOf(object, new.target.prototype);
  nodeOptions(object, options);
  for (const name of ['fftSize', 'minDecibels', 'maxDecibels', 'smoothingTimeConstant'])
    if (options?.[name] !== undefined) object[name] = options[name];
  return object;
}
install('AnalyserNode', AnalyserNode);
getter('AnalyserNode', 'fftSize', nodes, 'fftSize', function (value) {
  const n = requireSlot(nodes, this);
  value = Number(value) >>> 0;
  if (value < 32 || value > 32768 || value & (value - 1)) throw exception('IndexSizeError');
  if (value !== n.fftSize) {
    n.fftSize = value;
    n.spectrum = null;
    n.frequencyVersion = -1;
  }
});
getter('AnalyserNode', 'frequencyBinCount', nodes, (n) => n.fftSize / 2);
for (const name of ['minDecibels', 'maxDecibels', 'smoothingTimeConstant'])
  getter('AnalyserNode', name, nodes, name, function (value) {
    const n = requireSlot(nodes, this);
    value = finite(value);
    if (
      (name === 'minDecibels' && value >= n.maxDecibels) ||
      (name === 'maxDecibels' && value <= n.minDecibels) ||
      (name === 'smoothingTimeConstant' && (value < 0 || value > 1))
    )
      throw exception('IndexSizeError');
    n[name] = value;
  });
const analyserTime = (n) => {
  const out = new Float32Array(n.fftSize);
  for (let i = 0; i < out.length; i++)
    out[i] = n.history[(n.historyFrame - out.length + i + 32768) & 32767];
  return out;
};
const analyserFrequency = (n) => {
  if (n.frequencyVersion === n.analysisVersion && n.spectrum) return n.spectrum;
  const input = analyserTime(n),
    N = input.length,
    re = new Float32Array(N),
    im = new Float32Array(N);
  for (let i = 0; i < N; i++)
    re[i] =
      input[i] *
      (0.42 - 0.5 * Math.cos((2 * Math.PI * i) / N) + 0.08 * Math.cos((4 * Math.PI * i) / N));
  for (let i = 1, j = 0; i < N; i++) {
    let bit = N >> 1;
    for (; j & bit; bit >>= 1) j ^= bit;
    j ^= bit;
    if (i < j) {
      const v = re[i];
      re[i] = re[j];
      re[j] = v;
    }
  }
  for (let size = 2; size <= N; size *= 2)
    for (let base = 0; base < N; base += size)
      for (let j = 0; j < size / 2; j++) {
        const a = base + j,
          b = a + size / 2,
          angle = (-2 * Math.PI * j) / size,
          wr = Math.cos(angle),
          wi = Math.sin(angle),
          tr = re[b] * wr - im[b] * wi,
          ti = re[b] * wi + im[b] * wr;
        re[b] = re[a] - tr;
        im[b] = im[a] - ti;
        re[a] += tr;
        im[a] += ti;
      }
  const previous = n.spectrum || new Float32Array(N / 2),
    out = new Float32Array(N / 2);
  for (let i = 0; i < out.length; i++)
    out[i] =
      n.smoothingTimeConstant * previous[i] +
      ((1 - n.smoothingTimeConstant) * Math.hypot(re[i], im[i])) / N;
  n.spectrum = out;
  n.frequencyVersion = n.analysisVersion;
  return out;
};
for (const name of [
  'getFloatTimeDomainData',
  'getByteTimeDomainData',
  'getFloatFrequencyData',
  'getByteFrequencyData',
])
  method('AnalyserNode', name, function (array) {
    const n = requireSlot(nodes, this),
      floating = name.includes('Float'),
      frequency = name.includes('Frequency');
    if (!(array instanceof (floating ? Float32Array : Uint8Array)))
      throw new TypeError('Invalid typed array');
    const values = frequency ? analyserFrequency(n) : analyserTime(n);
    for (let i = 0; i < Math.min(array.length, values.length); i++) {
      const value = frequency ? 20 * Math.log10(values[i]) : values[i];
      array[i] = floating
        ? value
        : Math.max(
            0,
            Math.min(
              255,
              Math.floor(
                frequency
                  ? (255 * (value - n.minDecibels)) / (n.maxDecibels - n.minDecibels)
                  : 128 * (1 + value),
              ),
            ),
          );
    }
  });
method('BaseAudioContext', 'createAnalyser', function createAnalyser() {
  return new AnalyserNode(this);
});
processAudioNode = (n, result, startFrame, frames) => {
  if (n.type === 'AnalyserNode') {
    for (let i = 0; i < frames; i++) {
      let value = 0;
      for (const channel of result) value += channel[i] / result.length;
      n.history[n.historyFrame & 32767] = value;
      n.historyFrame++;
    }
    n.analysisVersion++;
  }
};
const previousPrepareAudioNode = prepareAudioNode;
prepareAudioNode = (n, startFrame, frames, allocate) => {
  previousPrepareAudioNode(n, startFrame, frames, allocate);
  if (n.type !== 'DelayNode') return;
  const rate = contexts.get(n.context).sampleRate,
    length = Math.ceil(n.maxDelayTime * rate) + 128,
    channels = Math.max(1, n.delayBuffers.length || n.channelCount);
  if (n.feedbackLatency === undefined) {
    const all = contexts.get(n.context).nodes,
      start = n;
    let frontier = [n.object],
      cyclic = false;
    const seen = new Set(frontier);
    while (frontier.length && !cyclic) {
      const source = frontier.shift();
      for (const object of all) {
        const target = nodes.get(object);
        if (!target.incoming.some((edge) => edge.source === source)) continue;
        if (target === start) {
          cyclic = true;
          break;
        }
        if (!seen.has(object)) {
          seen.add(object);
          frontier.push(object);
        }
      }
    }
    n.feedbackLatency = cyclic ? 128 : 0;
  }
  while (n.delayBuffers.length < channels) n.delayBuffers.push(new Float32Array(length));
  const output = allocate(channels);
  for (let channel = 0; channel < channels; channel++)
    for (let frame = 0; frame < frames; frame++) {
      const delay = Math.min(
        length - 1,
        Math.max(
          0,
          Math.round(parameterAt(n.delayTime, startFrame + frame, rate) * rate) + n.feedbackLatency,
        ),
      );
      output[channel][frame] =
        delay === 0 ? 0 : n.delayBuffers[channel][(n.delayWrite + frame - delay + length) % length];
    }
  n.feedbackOutput = output;
};
const previousGeneralNodeProcessor = processAudioNode;
processAudioNode = (n, result, startFrame, frames) => {
  previousGeneralNodeProcessor(n, result, startFrame, frames);
  const rate = contexts.get(n.context).sampleRate;
  if (n.type === 'DelayNode') {
    commitAudioNode(n, result, startFrame, frames);
  } else if (n.type === 'StereoPannerNode') {
    const input = result.map((channel) => channel.slice());
    if (result.length < 2) result.push(new Float32Array(frames));
    for (let frame = 0; frame < frames; frame++) {
      const pan = parameterAt(n.pan, startFrame + frame, rate),
        angle = ((pan + 1) * Math.PI) / 4,
        left = Math.cos(angle),
        right = Math.sin(angle);
      if (input.length === 1) {
        result[0][frame] = Math.fround(input[0][frame] * left);
        result[1][frame] = Math.fround(input[0][frame] * right);
      } else if (pan <= 0) {
        result[0][frame] = Math.fround(
          input[0][frame] + input[1][frame] * Math.sin((-pan * Math.PI) / 2),
        );
        result[1][frame] = Math.fround(input[1][frame] * Math.cos((pan * Math.PI) / 2));
      } else {
        result[0][frame] = Math.fround(input[0][frame] * Math.cos((pan * Math.PI) / 2));
        result[1][frame] = Math.fround(
          input[1][frame] + input[0][frame] * Math.sin((pan * Math.PI) / 2),
        );
      }
    }
  } else if (n.type === 'WaveShaperNode' && n.curve) {
    const curve = n.curve;
    for (const channel of result)
      for (let frame = 0; frame < frames; frame++) {
        const x = Math.max(-1, Math.min(1, channel[frame])),
          position = ((x + 1) * (curve.length - 1)) / 2,
          index = Math.min(curve.length - 2, Math.floor(position)),
          fraction = position - index;
        channel[frame] = Math.fround(curve[index] + (curve[index + 1] - curve[index]) * fraction);
      }
  } else if (n.type === 'ConvolverNode') {
    const impulse = n.buffer && buffers.get(n.buffer);
    if (!impulse) {
      for (const channel of result) channel.fill(0);
      return;
    }
    const peak = Math.max(
        1e-12,
        ...impulse.channels.map((channel) => {
          let value = 0;
          for (const sample of channel) value = Math.max(value, Math.abs(sample));
          return value;
        }),
      ),
      scale = n.normalize ? 1 / peak : 1;
    for (let channel = 0; channel < result.length; channel++) {
      const coefficients = impulse.channels[Math.min(channel, impulse.channels.length - 1)],
        history = n.convolverHistory[channel] || (n.convolverHistory[channel] = []),
        input = result[channel].slice();
      for (let frame = 0; frame < frames; frame++) {
        history.unshift(input[frame]);
        if (history.length > coefficients.length) history.length = coefficients.length;
        let value = 0;
        for (let k = 0; k < history.length; k++) value += history[k] * coefficients[k];
        result[channel][frame] = Math.fround(value * scale);
      }
    }
  } else if (n.type === 'PannerNode') {
    const listener = listenerSlots.get(listenerFor(n.context));
    for (let frame = 0; frame < frames; frame++) {
      const index = startFrame + frame,
        dx = parameterAt(n.positionX, index, rate) - parameterAt(listener.positionX, index, rate),
        dy = parameterAt(n.positionY, index, rate) - parameterAt(listener.positionY, index, rate),
        dz = parameterAt(n.positionZ, index, rate) - parameterAt(listener.positionZ, index, rate),
        distance = Math.hypot(dx, dy, dz),
        normalized = Math.max(n.refDistance, Math.min(n.maxDistance, distance)),
        attenuation =
          n.distanceModel === 'linear'
            ? Math.max(
                0,
                1 -
                  (n.rolloffFactor * (normalized - n.refDistance)) /
                    Math.max(1e-12, n.maxDistance - n.refDistance),
              )
            : n.distanceModel === 'exponential'
              ? (normalized / n.refDistance) ** -n.rolloffFactor
              : n.refDistance / (n.refDistance + n.rolloffFactor * (normalized - n.refDistance)),
        pan = distance ? Math.max(-1, Math.min(1, dx / distance)) : 0,
        angle = ((pan + 1) * Math.PI) / 4;
      let mono = 0;
      for (const channel of result) mono += channel[frame] / result.length;
      if (result.length < 2) result.push(new Float32Array(frames));
      result[0][frame] = Math.fround(mono * attenuation * Math.cos(angle));
      result[1][frame] = Math.fround(mono * attenuation * Math.sin(angle));
    }
  } else if (n.type === 'AudioWorkletNode') {
    // Processor code belongs to an isolated rendering realm. Without a native
    // output backend, unmodelled processor writes observe the initialized
    // zero-filled output buses rather than leaking the input through.
    for (const channel of result) channel.fill(0);
  }
};
const previousCommitAudioNode = commitAudioNode;
commitAudioNode = (n, result, startFrame, frames) => {
  previousCommitAudioNode(n, result, startFrame, frames);
  if (n.type !== 'DelayNode') return;
  const rate = contexts.get(n.context).sampleRate,
    length = Math.ceil(n.maxDelayTime * rate) + 128;
  while (n.delayBuffers.length < result.length) n.delayBuffers.push(new Float32Array(length));
  for (let frame = 0; frame < frames; frame++) {
    for (let channel = 0; channel < result.length; channel++)
      n.delayBuffers[channel][n.delayWrite] = result[channel][frame];
    n.delayWrite = (n.delayWrite + 1) % length;
  }
};
// Additional node processors share their state with frequency-response queries.
const filterTypes = [
  'lowpass',
  'highpass',
  'bandpass',
  'lowshelf',
  'highshelf',
  'peaking',
  'notch',
  'allpass',
];
function BiquadFilterNode(context, options = {}) {
  if (!new.target) throw new TypeError('Expected new');
  const rate = requireSlot(contexts, context).sampleRate;
  const object = makeNode(context, 'BiquadFilterNode', 1, 1, {
    filterType: 'lowpass',
    frequency: makeParam(context, 350, 'a-rate', 0, rate / 2),
    detune: makeParam(context, 0),
    Q: makeParam(context, 1),
    gain: makeParam(context, 0),
    filterState: [],
  });
  Object.setPrototypeOf(object, new.target.prototype);
  nodeOptions(object, options, ['frequency', 'detune', 'Q', 'gain']);
  if (options?.type !== undefined) object.type = options.type;
  return object;
}
install('BiquadFilterNode', BiquadFilterNode);
for (const key of ['frequency', 'detune', 'Q', 'gain']) getter('BiquadFilterNode', key, nodes);
getter('BiquadFilterNode', 'type', nodes, 'filterType', function (value) {
  const n = requireSlot(nodes, this);
  value = String(value);
  if (filterTypes.includes(value)) n.filterType = value;
});
const biquadCoefficients = (n, frame) => {
  const rate = contexts.get(n.context).sampleRate;
  const frequency = Math.max(
      0,
      Math.min(
        rate / 2,
        parameterAt(n.frequency, frame, rate) * 2 ** (parameterAt(n.detune, frame, rate) / 1200),
      ),
    ),
    q = parameterAt(n.Q, frame, rate),
    gain = parameterAt(n.gain, frame, rate),
    w = (2 * Math.PI * frequency) / rate,
    cos = Math.cos(w),
    sin = Math.sin(w),
    A = Math.fround(Math.exp(Math.fround((gain / 40) * Math.LN10)));
  const alpha =
      sin /
      (2 *
        (['lowpass', 'highpass'].includes(n.filterType)
          ? Math.fround(Math.exp(Math.fround((q / 20) * Math.LN10)))
          : Math.max(1e-12, q))),
    beta = Math.SQRT2 * Math.sqrt(A) * sin;
  let b0,
    b1,
    b2,
    a0 = 1 + alpha,
    a1 = -2 * cos,
    a2 = 1 - alpha;
  switch (n.filterType) {
    case 'lowpass':
      b0 = (1 - cos) / 2;
      b1 = 1 - cos;
      b2 = b0;
      break;
    case 'highpass':
      b0 = (1 + cos) / 2;
      b1 = -(1 + cos);
      b2 = b0;
      break;
    case 'bandpass':
      b0 = alpha;
      b1 = 0;
      b2 = -alpha;
      break;
    case 'notch':
      b0 = 1;
      b1 = -2 * cos;
      b2 = 1;
      break;
    case 'allpass':
      b0 = 1 - alpha;
      b1 = -2 * cos;
      b2 = 1 + alpha;
      break;
    case 'peaking':
      b0 = 1 + alpha * A;
      b1 = -2 * cos;
      b2 = 1 - alpha * A;
      a0 = 1 + alpha / A;
      a2 = 1 - alpha / A;
      break;
    case 'lowshelf':
      b0 = A * (A + 1 - (A - 1) * cos + beta);
      b1 = 2 * A * (A - 1 - (A + 1) * cos);
      b2 = A * (A + 1 - (A - 1) * cos - beta);
      a0 = A + 1 + (A - 1) * cos + beta;
      a1 = -2 * (A - 1 + (A + 1) * cos);
      a2 = A + 1 + (A - 1) * cos - beta;
      break;
    case 'highshelf':
      b0 = A * (A + 1 + (A - 1) * cos + beta);
      b1 = -2 * A * (A - 1 + (A + 1) * cos);
      b2 = A * (A + 1 + (A - 1) * cos - beta);
      a0 = A + 1 - (A - 1) * cos + beta;
      a1 = 2 * (A - 1 - (A + 1) * cos);
      a2 = A + 1 - (A - 1) * cos - beta;
      break;
  }
  return { b: [b0 / a0, b1 / a0, b2 / a0], a: [1, a1 / a0, a2 / a0] };
};
const response = (context, coefficients, frequencies, magnitude, phase) => {
  if (
    !(frequencies instanceof Float32Array) ||
    !(magnitude instanceof Float32Array) ||
    !(phase instanceof Float32Array)
  )
    throw new TypeError('Expected Float32Array');
  if (frequencies.length !== magnitude.length || frequencies.length !== phase.length)
    throw exception('InvalidAccessError');
  const rate = contexts.get(context).sampleRate;
  for (let i = 0; i < frequencies.length; i++) {
    const hz = frequencies[i];
    if (hz < 0 || hz > rate / 2 || !Number.isFinite(hz)) {
      magnitude[i] = phase[i] = NaN;
      continue;
    }
    // Frequency-response input is normalized through a float sample buffer.
    const w = -Math.PI * Math.fround(hz / (rate / 2));
    const polynomial = (values) => {
      const real = Math.cos(w),
        imaginary = Math.sin(w);
      let re = 0,
        im = 0;
      for (let k = values.length - 1; k >= 0; k--) {
        const next = re * real - im * imaginary + values[k];
        im = re * imaginary + im * real;
        re = next;
      }
      return [re, im];
    };
    const b = polynomial(coefficients.b),
      a = polynomial(coefficients.a),
      den = a[0] * a[0] + a[1] * a[1],
      re = (b[0] * a[0] + b[1] * a[1]) / den,
      im = (b[1] * a[0] - b[0] * a[1]) / den;
    magnitude[i] = Math.hypot(re, im);
    phase[i] = Math.atan2(im, re);
  }
};
method('BiquadFilterNode', 'getFrequencyResponse', function getFrequencyResponse(f, m, p) {
  const n = requireSlot(nodes, this);
  response(
    n.context,
    biquadCoefficients(
      n,
      Math.floor(contexts.get(n.context).currentTime * contexts.get(n.context).sampleRate),
    ),
    f,
    m,
    p,
  );
});
method('BaseAudioContext', 'createBiquadFilter', function createBiquadFilter() {
  return new BiquadFilterNode(this);
});
function IIRFilterNode(context, options) {
  if (!new.target || !options) throw new TypeError('Expected coefficients');
  const b = Array.from(options.feedforward, finite),
    a = Array.from(options.feedback, finite);
  if (!b.length || b.length > 20 || !a.length || a.length > 20)
    throw exception('NotSupportedError');
  if (!b.some((v) => v !== 0) || a[0] === 0) throw exception('InvalidStateError');
  const object = makeNode(context, 'IIRFilterNode', 1, 1, {
    coefficients: { b: b.map((v) => v / a[0]), a: a.map((v) => v / a[0]) },
    filterState: [],
  });
  Object.setPrototypeOf(object, new.target.prototype);
  return nodeOptions(object, options);
}
install('IIRFilterNode', IIRFilterNode);
method('BaseAudioContext', 'createIIRFilter', function createIIRFilter(feedforward, feedback) {
  return new IIRFilterNode(this, { feedforward, feedback });
});
method('IIRFilterNode', 'getFrequencyResponse', function getFrequencyResponse(f, m, p) {
  const n = requireSlot(nodes, this);
  response(n.context, n.coefficients, f, m, p);
});
const previousNodeProcessor = processAudioNode;
processAudioNode = (n, result, startFrame, frames) => {
  previousNodeProcessor(n, result, startFrame, frames);
  if (n.type === 'BiquadFilterNode' || n.type === 'IIRFilterNode') {
    for (let frame = 0; frame < frames; frame++) {
      const coefficients =
        n.type === 'BiquadFilterNode' ? biquadCoefficients(n, startFrame + frame) : n.coefficients;
      for (let ch = 0; ch < result.length; ch++) {
        const state = n.filterState[ch] || (n.filterState[ch] = { x: [], y: [] }),
          input = result[ch][frame];
        state.x.unshift(input);
        state.x.length = coefficients.b.length;
        let output = 0;
        for (let i = 0; i < coefficients.b.length; i++)
          output += coefficients.b[i] * (state.x[i] || 0);
        for (let i = 1; i < coefficients.a.length; i++)
          output -= coefficients.a[i] * (state.y[i - 1] || 0);
        // Biquad feedback stores the emitted float sample, while IIR retains
        // double precision internal history.
        state.y.unshift(n.type === 'BiquadFilterNode' ? Math.fround(output) : output);
        state.y.length = coefficients.a.length - 1;
        result[ch][frame] = output;
      }
    }
  }
};
