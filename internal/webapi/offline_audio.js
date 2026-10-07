/*
 * Copyright (C) 2011, 2012 Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1.  Redistributions of source code must retain the above copyright
 *     notice, this list of conditions and the following disclaimer.
 * 2.  Redistributions in binary form must reproduce the above copyright
 *     notice, this list of conditions and the following disclaimer in the
 *     documentation and/or other materials provided with the distribution.
 * 3.  Neither the name of Apple Computer, Inc. ("Apple") nor the names of
 *     its contributors may be used to endorse or promote products derived
 *     from this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY APPLE AND ITS CONTRIBUTORS "AS IS" AND ANY
 * EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 * DISCLAIMED. IN NO EVENT SHALL APPLE OR ITS CONTRIBUTORS BE LIABLE FOR ANY
 * DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
 * LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
 * ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
 * THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */
// DSP adapted from Chromium 152 periodic_wave.cc and dynamics_compressor.cc.
// CPU-side PCM observations. No audio device, native renderer or global graph.
(() => {
  if (
    typeof globalThis.OfflineAudioContext !== 'function' ||
    typeof globalThis.AudioBuffer !== 'function'
  )
    return;
  const buffers = new WeakMap(),
    contexts = new WeakMap(),
    nodes = new WeakMap(),
    params = new WeakMap(),
    completions = new WeakMap(),
    waves = new WeakMap();
  const exception = (name, message) => platformDOMException(message || name, name);
  const unsupported = (name) => {
    host.semanticMissingAt('offline_audio.js:34', 'WebAudio.' + name);
    throw exception('NotSupportedError', 'Unsupported audio operation: ' + name);
  };
  const requireSlot = (map, value) => {
    const s = map.get(value);
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const finite = (value) => {
    value = Number(value);
    if (!Number.isFinite(value)) throw new TypeError('Expected finite number');
    return value;
  };
  const float = (value) => {
    value = Math.fround(finite(value));
    if (!Number.isFinite(value)) throw new TypeError('Expected finite float');
    return value;
  };
  const time = (value) => {
    value = finite(value);
    if (value < 0) throw new RangeError('Negative time');
    return value;
  };
  let synchronizeOwner = () => {};
  const method = (type, name, value) => {
    const implementation = value;
    value = function (...args) {
      synchronizeOwner(this);
      return Reflect.apply(implementation, this, args);
    };
    if (!globalThis[type]) return;
    const prior = Object.getOwnPropertyDescriptor(globalThis[type].prototype, name);
    Object.defineProperty(value, 'name', { value: name, configurable: true });
    if (prior?.value)
      Object.defineProperty(value, 'length', { value: prior.value.length, configurable: true });
    markNative(value, name);
    if (lazyDomainInterfaces.get(type) === 'audio' && prior?.value) {
      bindLazyBehavior(type, name, value);
      return;
    }
    Object.defineProperty(globalThis[type].prototype, name, {
      value,
      writable: true,
      configurable: true,
      enumerable: true,
    });
  };
  const getter = (type, name, map, key = name, set) => {
    if (!globalThis[type]) return;
    const get = function () {
      const s = requireSlot(map, this);
      return typeof key === 'function' ? key(s) : s[key];
    };
    markNative(get, name, 'get ');
    if (set) {
      const write = set;
      set = function (value) {
        synchronizeOwner(this);
        return Reflect.apply(write, this, [value]);
      };
      markNative(set, name, 'set ');
    }
    const prior = Object.getOwnPropertyDescriptor(globalThis[type].prototype, name);
    if (lazyDomainInterfaces.get(type) === 'audio' && prior) {
      if (get) bindLazyBehavior(type, name, get, 'get');
      if (set) bindLazyBehavior(type, name, set, 'set');
      return;
    }
    Object.defineProperty(globalThis[type].prototype, name, {
      get,
      set,
      configurable: true,
      enumerable: true,
    });
  };
  const install = (name, ctor) => {
    const facade = globalThis[name];
    Object.setPrototypeOf(ctor, Object.getPrototypeOf(facade));
    ctor.prototype = facade.prototype;
    if (lazyDomainInterfaces.get(name) === 'audio') {
      bindLazyBehavior(name, 'constructor', ctor);
      return;
    }
    Object.defineProperty(ctor.prototype, 'constructor', {
      value: ctor,
      writable: true,
      configurable: true,
    });
    markNative(ctor, name);
    Object.defineProperty(globalThis, name, { value: ctor, writable: true, configurable: true });
  };
  const dimensions = (options) => {
    if (!options || options.length === undefined || options.sampleRate === undefined)
      throw new TypeError('Missing audio dimensions');
    const length = Number(options.length) >>> 0,
      numberOfChannels =
        options.numberOfChannels === undefined ? 1 : Number(options.numberOfChannels) >>> 0,
      sampleRate = float(options.sampleRate);
    if (
      !length ||
      !numberOfChannels ||
      numberOfChannels > 32 ||
      sampleRate < 3000 ||
      sampleRate > 768000
    )
      throw exception('NotSupportedError', 'Invalid audio dimensions');
    if (length * numberOfChannels > 16 * 1024 * 1024) unsupported('PCM storage limit');
    return { length, numberOfChannels, sampleRate };
  };
  function AudioBuffer(options) {
    if (!new.target) throw new TypeError('Expected new');
    const s = dimensions(options),
      object = Object.create(new.target.prototype);
    s.channels = Array.from({ length: s.numberOfChannels }, () => new Float32Array(s.length));
    buffers.set(object, s);
    return object;
  }
  install('AudioBuffer', AudioBuffer);
  for (const key of ['length', 'sampleRate', 'numberOfChannels'])
    getter('AudioBuffer', key, buffers);
  getter('AudioBuffer', 'duration', buffers, (s) => s.length / s.sampleRate);
  const channel = (s, index) => {
    index = Number(index) >>> 0;
    if (index >= s.numberOfChannels) throw exception('IndexSizeError');
    return s.channels[index];
  };
  method('AudioBuffer', 'getChannelData', function getChannelData(index) {
    const s = requireSlot(buffers, this);
    if (!arguments.length) throw new TypeError('Expected channel');
    return channel(s, index);
  });
  for (const name of ['copyFromChannel', 'copyToChannel'])
    method('AudioBuffer', name, function (data, index, offset = 0) {
      const s = requireSlot(buffers, this);
      if (arguments.length < 2 || !(data instanceof Float32Array))
        throw new TypeError('Expected Float32Array and channel');
      if (!data.length) return;
      const target = channel(s, index);
      offset = Number(offset) >>> 0;
      if (offset >= s.length) return;
      const count = Math.min(data.length, s.length - offset);
      if (name === 'copyFromChannel') data.set(target.subarray(offset, offset + count));
      else target.set(data.subarray(0, count), offset);
    });
  function OfflineAudioCompletionEvent(type, init) {
    if (!new.target || arguments.length < 2 || !init || init.renderedBuffer === undefined)
      throw new TypeError('Expected completion buffer');
    requireSlot(buffers, init.renderedBuffer);
    const event = new Event(type, init);
    Object.setPrototypeOf(event, new.target.prototype);
    completions.set(event, { renderedBuffer: init.renderedBuffer });
    return event;
  }
  install('OfflineAudioCompletionEvent', OfflineAudioCompletionEvent);
  getter('OfflineAudioCompletionEvent', 'renderedBuffer', completions);
  const emit = (object, type, buffer) => {
    const event = buffer
      ? new OfflineAudioCompletionEvent(type, { renderedBuffer: buffer })
      : new Event(type);
    dispatchEventCore(object, event, true);
  };
  const makeNode = (context, type, inputs, outputs, extra = {}) => {
    requireSlot(contexts, context);
    const object = new EventTarget();
    Object.setPrototypeOf(object, globalThis[type].prototype);
    nodes.set(object, {
      object,
      context,
      type,
      numberOfInputs: inputs,
      numberOfOutputs: outputs,
      channelCount: 2,
      channelCountMode: 'max',
      channelInterpretation: 'speakers',
      incoming: [],
      ...extra,
    });
    requireSlot(contexts, context).nodes.push(object);
    return object;
  };
  const makeParam = (
    context,
    value,
    rate = 'a-rate',
    min = -3.4028234663852886e38,
    max = 3.4028234663852886e38,
  ) => {
    const object = Object.create(AudioParam.prototype);
    params.set(object, {
      object,
      context,
      value: Math.fround(value),
      defaultValue: Math.fround(value),
      minValue: min,
      maxValue: max,
      automationRate: rate,
      fixedRate: rate === 'k-rate',
      timeline: [],
      incoming: [],
    });
    requireSlot(contexts, context).params.push(object);
    return object;
  };
  for (const key of ['defaultValue', 'minValue', 'maxValue']) getter('AudioParam', key, params);
  getter(
    'AudioParam',
    'value',
    params,
    (s) => {
      const c = contexts.get(s.context);
      synchronizeOwner(s.context);
      return c.realtime
        ? parameterAt(s.object, Math.floor(c.currentTime * c.sampleRate), c.sampleRate)
        : s.value;
    },
    function (value) {
      const s = requireSlot(params, this);
      s.value = Math.max(s.minValue, Math.min(s.maxValue, float(value)));
      s.initialChange = true;
    },
  );
  getter('AudioParam', 'automationRate', params, 'automationRate', function (value) {
    const s = requireSlot(params, this);
    value = String(value);
    if (!['a-rate', 'k-rate'].includes(value)) throw new TypeError('Invalid automation rate');
    if (s.fixedRate && value !== s.automationRate) throw exception('InvalidStateError');
    s.automationRate = value;
  });
  const curveAt = (event, when, rate, frame = when * rate) => {
    const position = Math.max(
        0,
        Math.min(
          event.values.length - 1,
          (frame - event.time * rate) * ((event.values.length - 1) / (event.duration * rate)),
        ),
      ),
      index = Math.min(event.values.length - 2, Math.floor(position));
    return Math.fround(
      event.values[index] + (event.values[index + 1] - event.values[index]) * (position - index),
    );
  };
  const eventEnd = (event) =>
    event.kind === 'curve'
      ? Math.min(event.time + event.duration, event.clip ?? Infinity)
      : event.time;
  function timelineValue(s, when, frame, limit) {
    const rate = contexts.get(s.context).sampleRate,
      events = limit === undefined ? s.timeline : s.timeline.slice(0, limit);
    // Stable time ordering preserves insertion order for unlike events at the
    // same instant. Find the active segment without rescanning every past event
    // for every output sample.
    let left = 0,
      right = events.length;
    while (left < right) {
      const middle = (left + right) >>> 1;
      if (events[middle].time <= when) left = middle + 1;
      else right = middle;
    }
    const previous = events[left - 1],
      next = events[left];
    let value = s.value,
      start = 0;
    if (previous) {
      if (previous.kind === 'curve') {
        const end = eventEnd(previous);
        if (when < end) return curveAt(previous, when, rate, frame);
        value = curveAt(previous, end, rate);
        start = end;
      } else if (previous.kind === 'target') {
        const index = events.indexOf(previous),
          initial = timelineValue(s, previous.time, previous.time * rate, index),
          elapsed = Math.max(0, when - previous.time);
        value =
          previous.timeConstant === 0
            ? previous.value
            : Math.fround(
                previous.value +
                  (initial - previous.value) * Math.exp(-elapsed / previous.timeConstant),
              );
        start = when;
      } else {
        value = previous.value;
        start = previous.time;
      }
    }
    if (next && (next.kind === 'linear' || next.kind === 'exponential')) {
      const fraction = Math.max(0, (when - start) / (next.time - start));
      if (next.kind === 'linear') return Math.fround(value + (next.value - value) * fraction);
      return value * next.value <= 0
        ? value
        : Math.fround(value * Math.pow(next.value / value, fraction));
    }
    return value;
  }
  function addAutomation(s, event) {
    const next = s.timeline.filter((e) => e.time !== event.time || e.kind !== event.kind);
    if (
      next.some(
        (e) =>
          (e.kind === 'curve' && event.time >= e.time && event.time < eventEnd(e)) ||
          (event.kind === 'curve' && e.time >= event.time && e.time < eventEnd(event)),
      )
    )
      throw exception('NotSupportedError', 'Overlapping automation curve');
    next.push(event);
    next.sort((a, b) => a.time - b.time);
    s.timeline = next;
  }
  for (const [name, kind] of [
    ['setValueAtTime', 'set'],
    ['linearRampToValueAtTime', 'linear'],
    ['exponentialRampToValueAtTime', 'exponential'],
  ])
    method('AudioParam', name, function (value, when) {
      const s = requireSlot(params, this);
      if (arguments.length < 2) throw new TypeError('Expected value and time');
      value = float(value);
      when = time(when);
      if (kind === 'exponential' && value === 0) throw new RangeError('Zero exponential target');
      addAutomation(s, { kind, time: when, value });
      return this;
    });
  method('AudioParam', 'setValueCurveAtTime', function setValueCurveAtTime(values, when, duration) {
    const s = requireSlot(params, this);
    if (arguments.length < 3) throw new TypeError('Expected curve, time and duration');
    values = Float32Array.from(values, float);
    when = time(when);
    duration = time(duration);
    if (duration === 0) throw new RangeError('Zero curve duration');
    if (values.length < 2) throw exception('InvalidStateError', 'Curve requires two values');
    addAutomation(s, { kind: 'curve', time: when, duration, values });
    return this;
  });
  method('AudioParam', 'cancelScheduledValues', function cancelScheduledValues(when) {
    const s = requireSlot(params, this);
    when = time(when);
    s.timeline = s.timeline.filter(
      (e) => e.time < when && !(e.kind === 'curve' && eventEnd(e) > when),
    );
    return this;
  });
  method('AudioParam', 'cancelAndHoldAtTime', function cancelAndHoldAtTime(when) {
    const s = requireSlot(params, this);
    when = time(when);
    const value = timelineValue(s, when),
      next = s.timeline.find((e) => e.time > when),
      active = s.timeline.find((e) => e.kind === 'curve' && e.time < when && eventEnd(e) > when);
    s.timeline = s.timeline.filter((e) => e.time < when);
    if (active) active.clip = when;
    s.timeline.push({
      kind: !active && next && ['linear', 'exponential'].includes(next.kind) ? next.kind : 'set',
      time: when,
      value,
    });
    return this;
  });
  method('AudioParam', 'setTargetAtTime', function setTargetAtTime() {
    const s = requireSlot(params, this);
    if (arguments.length < 3) throw new TypeError('Expected target, time and time constant');
    const value = float(arguments[0]),
      when = time(arguments[1]),
      timeConstant = finite(arguments[2]);
    if (timeConstant < 0) throw new RangeError('Negative time constant');
    addAutomation(s, { kind: 'target', time: when, value, timeConstant });
    return this;
  });
  const parameterAt = (object, frame, rate) => {
    const s = requireSlot(params, object);
    if (s.automationRate === 'k-rate') frame = Math.floor(frame / 128) * 128;
    const c = contexts.get(s.context),
      modulation = c.parameterSampler ? c.parameterSampler(s, frame) : 0;
    return Math.max(
      s.minValue,
      Math.min(s.maxValue, Math.fround(timelineValue(s, frame / rate, frame) + modulation)),
    );
  };
  for (const key of ['context', 'numberOfInputs', 'numberOfOutputs'])
    getter('AudioNode', key, nodes);
  getter('AudioNode', 'channelCount', nodes, 'channelCount', function (value) {
    const s = requireSlot(nodes, this);
    value = Number(value) >>> 0;
    if (value < 1 || value > 32 || (s.type === 'DynamicsCompressorNode' && value > 2))
      throw exception('NotSupportedError');
    s.channelCount = value;
  });
  for (const [key, values] of [
    ['channelCountMode', ['max', 'clamped-max', 'explicit']],
    ['channelInterpretation', ['speakers', 'discrete']],
  ])
    getter('AudioNode', key, nodes, key, function (value) {
      const s = requireSlot(nodes, this);
      value = String(value);
      if (!values.includes(value)) return;
      if (s.type === 'DynamicsCompressorNode' && key === 'channelCountMode' && value === 'max')
        throw exception('NotSupportedError');
      s[key] = value;
    });
  getter('AudioDestinationNode', 'maxChannelCount', nodes, 'channelCount');
  method('AudioNode', 'connect', function connect(destination, output = 0, input = 0) {
    const a = requireSlot(nodes, this),
      b = nodes.get(destination);
    if (!b) {
      const parameter = params.get(destination);
      if (parameter) {
        output = Number(output) >>> 0;
        if (a.context !== parameter.context) throw exception('InvalidAccessError');
        if (output >= a.numberOfOutputs) throw exception('IndexSizeError');
        if (!parameter.incoming.some((edge) => edge.source === this && edge.output === output))
          parameter.incoming.push({ source: this, output });
        return;
      }
      throw new TypeError('Expected AudioNode');
    }
    output = Number(output) >>> 0;
    input = Number(input) >>> 0;
    if (a.context !== b.context) throw exception('InvalidAccessError');
    if (output >= a.numberOfOutputs || input >= b.numberOfInputs) throw exception('IndexSizeError');
    if (
      !b.incoming.some(
        (edge) => edge.source === this && edge.output === output && edge.input === input,
      )
    )
      b.incoming.push({ source: this, output, input });
    return destination;
  });
  method('AudioNode', 'disconnect', function disconnect(destination) {
    const a = requireSlot(nodes, this),
      all = requireSlot(contexts, a.context).nodes;
    if (arguments.length === 0) {
      for (const node of all) {
        const s = nodes.get(node);
        s.incoming = s.incoming.filter((edge) => edge.source !== this);
      }
      for (const parameter of requireSlot(contexts, a.context).params)
        params.get(parameter).incoming = params
          .get(parameter)
          .incoming.filter((edge) => edge.source !== this);
      return;
    }
    if (typeof destination === 'number') {
      if (destination >>> 0 >= a.numberOfOutputs) throw exception('IndexSizeError');
      for (const node of all) {
        const s = nodes.get(node);
        s.incoming = s.incoming.filter(
          (edge) => edge.source !== this || edge.output !== destination >>> 0,
        );
      }
      return;
    }
    const b = nodes.get(destination);
    const parameter = params.get(destination);
    if (!b && !parameter) throw new TypeError('Expected AudioNode or AudioParam');
    const incoming = b ? b.incoming : parameter.incoming;
    if (!incoming.some((edge) => edge.source === this)) throw exception('InvalidAccessError');
    const output = arguments.length > 1 ? Number(arguments[1]) >>> 0 : null;
    const input = b && arguments.length > 2 ? Number(arguments[2]) >>> 0 : null;
    if (output !== null && output >= a.numberOfOutputs) throw exception('IndexSizeError');
    if (input !== null && input >= b.numberOfInputs) throw exception('IndexSizeError');
    const keep = (edge) =>
      edge.source !== this ||
      (output !== null && edge.output !== output) ||
      (input !== null && edge.input !== input);
    if (b) b.incoming = b.incoming.filter(keep);
    else parameter.incoming = parameter.incoming.filter(keep);
  });
  const makeSource = (context) =>
    makeNode(context, 'AudioBufferSourceNode', 0, 1, {
      buffer: null,
      playbackRate: makeParam(context, 1, 'k-rate'),
      detune: makeParam(context, 0, 'k-rate'),
      loop: false,
      loopStart: 0,
      loopEnd: 0,
      started: false,
      startTime: 0,
      offset: 0,
      duration: Infinity,
      stopTime: Infinity,
      onended: null,
    });
  function AudioBufferSourceNode(context, options = {}) {
    if (!new.target) throw new TypeError('Expected new');
    const object = makeSource(context);
    Object.setPrototypeOf(object, new.target.prototype);
    options = options ?? {};
    for (const key of [
      'buffer',
      'loop',
      'loopStart',
      'loopEnd',
      'channelCount',
      'channelCountMode',
      'channelInterpretation',
    ])
      if (options[key] !== undefined) object[key] = options[key];
    for (const key of ['playbackRate', 'detune'])
      if (options[key] !== undefined) object[key].value = options[key];
    return object;
  }
  install('AudioBufferSourceNode', AudioBufferSourceNode);
  getter('AudioBufferSourceNode', 'buffer', nodes, 'buffer', function (value) {
    const s = requireSlot(nodes, this);
    if (value !== null) requireSlot(buffers, value);
    if (s.bufferAssigned && value !== null) throw exception('InvalidStateError');
    if (value !== null) s.bufferAssigned = true;
    s.buffer = value;
  });
  for (const key of ['playbackRate', 'detune']) getter('AudioBufferSourceNode', key, nodes);
  for (const key of ['loop', 'loopStart', 'loopEnd'])
    getter('AudioBufferSourceNode', key, nodes, key, function (value) {
      requireSlot(nodes, this)[key] = key === 'loop' ? !!value : finite(value);
    });
  getter('AudioScheduledSourceNode', 'onended', nodes, 'onended', function (value) {
    requireSlot(nodes, this).onended = typeof value === 'function' ? value : null;
  });
  method('AudioBufferSourceNode', 'start', function start(when = 0, offset = 0, duration) {
    const s = requireSlot(nodes, this);
    when = time(when);
    offset = time(offset);
    duration = duration === undefined ? Infinity : time(duration);
    if (s.started) throw exception('InvalidStateError');
    s.started = true;
    s.startTime = when;
    s.offset = offset;
    s.duration = duration;
  });
  method('AudioScheduledSourceNode', 'stop', function stop(when = 0) {
    const s = requireSlot(nodes, this);
    when = time(when);
    if (!s.started) throw exception('InvalidStateError');
    s.stopTime = when;
  });
  const makeGain = (context) =>
    makeNode(context, 'GainNode', 1, 1, { gain: makeParam(context, 1) });
  function GainNode(context, options = {}) {
    if (!new.target) throw new TypeError('Expected new');
    const object = makeGain(context);
    Object.setPrototypeOf(object, new.target.prototype);
    options = options ?? {};
    if (options.gain !== undefined) object.gain.value = options.gain;
    for (const key of ['channelCount', 'channelCountMode', 'channelInterpretation'])
      if (options[key] !== undefined) object[key] = options[key];
    return object;
  }
  install('GainNode', GainNode);
  getter('GainNode', 'gain', nodes);
  // Band-limited periodic PCM follows Chrome 152's pitch ranges and Fourier
  // coefficients. Float32 mixed-radix arithmetic follows the frozen RustFFT
  // path without requiring Rust or native audio/FFT code.
  const f = Math.fround,
    clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
  const waveSize = (rate) => (rate <= 24000 ? 2048 : rate <= 88200 ? 4096 : 16384);
  function makeWave(context, real, imag, disableNormalization) {
    const c = requireSlot(contexts, context);
    real = Float32Array.from(real, float);
    imag = Float32Array.from(imag, float);
    if (real.length !== imag.length || real.length < 2) throw exception('IndexSizeError');
    const object = Object.create(PeriodicWave.prototype),
      size = waveSize(c.sampleRate),
      count = Math.min(real.length, size / 2);
    waves.set(object, {
      context,
      size,
      rate: c.sampleRate,
      real: real.slice(0, count),
      imag: imag.slice(0, count),
      tables: new Map(),
      normalization: disableNormalization ? 0.5 : null,
      ranges: 3 * Math.log2(size),
    });
    return object;
  }
  function PeriodicWave(context, options = {}) {
    if (!new.target) throw new TypeError('Expected new');
    options = options ?? {};
    let real = options.real,
      imag = options.imag;
    if (real === undefined && imag === undefined) {
      real = [0, 0];
      imag = [0, 1];
    } else if (real === undefined) real = new Float32Array(imag.length);
    else if (imag === undefined) imag = new Float32Array(real.length);
    const object = makeWave(context, real, imag, !!options.disableNormalization);
    Object.setPrototypeOf(object, new.target.prototype);
    return object;
  }
  install('PeriodicWave', PeriodicWave);
  function basicWave(context, type) {
    const c = contexts.get(context);
    if (!c.waves) c.waves = new Map();
    if (c.waves.has(type)) return c.waves.get(type);
    const size = waveSize(c.sampleRate) / 2,
      real = new Float32Array(size),
      imag = new Float32Array(size);
    for (let n = 1; n < size; n++) {
      const p = f(2 / f(n * f(Math.PI)));
      imag[n] =
        type === 'sine'
          ? n === 1
            ? 1
            : 0
          : type === 'square'
            ? n & 1
              ? f(2 * p)
              : 0
            : type === 'sawtooth'
              ? p * (n & 1 ? 1 : -1)
              : n & 1
                ? f(2 * f(p * p)) * (((n - 1) >> 1) & 1 ? -1 : 1)
                : 0;
    }
    const wave = makeWave(context, real, imag, false);
    c.waves.set(type, wave);
    return wave;
  }
  // Portable Float32 projection of RustFFT 6.4.1's AVX power-of-two plan and
  // Chromium rustfft_ffi.rs real reconstruction. See vendor/rustfft/NOTICE.
  // Complex multiplication preserves the separate rounded imaginary product
  // followed by fused multiply-add; reversing its operands changes rounding.
  function inverseRealFFT(real, imag) {
    const n = real.length * 2,
      m = n / 2,
      add = (a, b) => [f(a[0] + b[0]), f(a[1] + b[1])],
      sub = (a, b) => [f(a[0] - b[0]), f(a[1] - b[1])],
      rot = (a) => [-a[1], a[0]],
      scale = (a, s) => [f(a[0] * s), f(a[1] * s)];
    const mul = (a, b) => [f(a[0] * b[0] - f(a[1] * b[1])), f(a[0] * b[1] + f(a[1] * b[0]))],
      plainMul = (a, b) => [f(f(a[0] * b[0]) - f(a[1] * b[1])), f(f(a[0] * b[1]) + f(a[1] * b[0]))];
    const tw = (i, len) => {
        const angle = ((-2 * Math.PI) / len) * i;
        return [f(Math.cos(angle)), -f(Math.sin(angle))];
      },
      root = f(Math.SQRT1_2),
      eighth = (a, third = false) => scale(third ? sub(rot(a), a) : add(rot(a), a), root);
    function b4(a) {
      const x = add(a[0], a[2]),
        y = sub(a[0], a[2]),
        z = add(a[1], a[3]),
        t = rot(sub(a[1], a[3]));
      return [add(x, z), add(y, t), sub(x, z), sub(y, t)];
    }
    function b8(a) {
      const x = b4([a[0], a[2], a[4], a[6]]),
        y = b4([a[1], a[3], a[5], a[7]]);
      y[1] = eighth(y[1]);
      y[2] = rot(y[2]);
      y[3] = eighth(y[3], true);
      return [...x.map((v, i) => add(v, y[i])), ...x.map((v, i) => sub(v, y[i]))];
    }
    function b32(a) {
      const rows = Array.from({ length: 8 }, (_, j) => b4([a[j], a[j + 8], a[j + 16], a[j + 24]]));
      for (let j = 1; j < 8; j++)
        for (let k = 1; k < 4; k++) {
          const index = j * k,
            v = rows[j][k];
          if (index === 4) rows[j][k] = eighth(v);
          else if (index === 8) rows[j][k] = rot(v);
          else if (index === 12) rows[j][k] = eighth(v, true);
          else {
            let t =
              index > 16
                ? scale(tw(index - 16, 32), -1)
                : index > 8
                  ? rot(tw(index - 8, 32))
                  : tw(index, 32);
            rows[j][k] = mul(v, t);
          }
        }
      const out = new Array(32);
      for (let k = 0; k < 4; k++) {
        const values = b8(rows.map((row) => row[k]));
        for (let j = 0; j < 8; j++) out[k + 4 * j] = values[j];
      }
      return out;
    }
    function mixed(a, radix, inner, twiddleFirst) {
      const length = a.length,
        width = length / radix,
        rows = Array.from({ length: radix }, () => new Array(width));
      for (let j = 0; j < width; j++) {
        const input = Array.from({ length: radix }, (_, r) => a[j + r * width]),
          v = radix === 4 ? b4(input) : b8(input);
        rows[0][j] = v[0];
        for (let r = 1; r < radix; r++)
          rows[r][j] = twiddleFirst ? mul(tw(j * r, length), v[r]) : mul(v[r], tw(j * r, length));
      }
      const out = new Array(length);
      for (let r = 0; r < radix; r++) {
        const v = inner(rows[r]);
        for (let j = 0; j < width; j++) out[j * radix + r] = v[j];
      }
      return out;
    }
    // Table sizes 2048/4096/16384 require complex plans 1024/2048/8192:
    // a 256 (8 x 32) base followed by the planner's radix-4/radix-8 stages.
    const b256 = (a) => mixed(a, 8, b32, false);
    function fft(a) {
      if (a.length === 256) return b256(a);
      const radix = a.length === 1024 || a.length === 8192 ? 4 : 8;
      return mixed(a, radix, fft, true);
    }
    const z = new Array(m);
    z[0] = [f(0.5 * f(real[0] + imag[0])), f(0.5 * f(real[0] - imag[0]))];
    for (let k = 1; k < m / 2; k++) {
      const a = [real[k], imag[k]],
        b = [real[m - k], -imag[m - k]],
        even = scale(add(a, b), 0.5),
        angle = (-2 * Math.PI * k) / n,
        t = [f(0.5 * Math.cos(angle)), -f(0.5 * Math.sin(angle))],
        odd = rot(plainMul(t, sub(a, b)));
      z[k] = add(even, odd);
      const v = sub(even, odd);
      z[m - k] = [v[0], -v[1]];
    }
    z[m / 2] = [real[m / 2], -imag[m / 2]];
    const v = fft(z),
      out = new Float32Array(n);
    for (let i = 0; i < m; i++) {
      out[i * 2] = f(v[i][0] / m);
      out[i * 2 + 1] = f(v[i][1] / m);
    }
    return out;
  }
  function waveTable(w, range) {
    if (w.tables.has(range)) return w.tables.get(range);
    if (w.normalization === null && range !== 0) waveTable(w, 0);
    const owner = contexts.get(w.context);
    owner.waveStorage = (owner.waveStorage || 0) + w.size;
    if (owner.waveStorage > 16 * 1024 * 1024) unsupported('wavetable storage limit');
    const size = w.size,
      re = new Float32Array(size / 2),
      im = new Float32Array(size / 2),
      partials = Math.floor((f(Math.pow(2, f(-range / 3))) * size) / 2);
    for (let i = 1; i < Math.min(w.real.length, partials + 1); i++) {
      re[i] = f(w.real[i] * size);
      im[i] = f(-w.imag[i] * size);
    }
    const table = inverseRealFFT(re, im);
    if (w.normalization === null) {
      let peak = 0;
      for (const value of table) peak = Math.max(peak, Math.abs(value));
      w.normalization = peak ? f(1 / peak) : 0.5;
    }
    for (let i = 0; i < size; i++) table[i] = f(table[i] * w.normalization);
    w.tables.set(range, table);
    return table;
  }
  function waveSample(w, phase, frequency, increment) {
    const pitch = clamp(
        f(
          1 +
            f(
              f(
                f(Math.log2(frequency ? f(Math.abs(frequency) / f(w.rate / w.size)) : 0.5)) * 1200,
              ) / 400,
            ),
        ),
        0,
        w.ranges - 1,
      ),
      index = Math.floor(pitch),
      blend = f(pitch - index),
      higher = waveTable(w, index),
      lower = waveTable(w, Math.min(index + 1, w.ranges - 1)),
      base = Math.floor(phase),
      mask = w.size - 1;
    if (Math.abs(increment) >= 0.3) {
      const t = f(f(phase) - base),
        sample = (table) =>
          f(table[base & mask] + f(t * f(table[(base + 1) & mask] - table[base & mask]))),
        hi = sample(higher),
        lo = sample(lower);
      return f(hi + f(blend * f(lo - hi)));
    }
    const t = phase - base,
      radius = Math.abs(increment) >= 0.16 ? 1 : 2;
    let hi = 0,
      lo = 0;
    for (let i = -radius; i <= radius; i++) {
      let weight = 1;
      for (let j = -radius; j <= radius; j++) if (i !== j) weight *= (t - j) / (i - j);
      hi += weight * higher[(base + i) & mask];
      lo += weight * lower[(base + i) & mask];
    }
    return f((1 - blend) * hi + blend * lo);
  }
  const makeOscillator = (context) => {
    const rate = requireSlot(contexts, context).sampleRate;
    return makeNode(context, 'OscillatorNode', 0, 1, {
      oscillatorType: 'sine',
      wave: null,
      frequency: makeParam(context, 440, 'a-rate', -rate / 2, rate / 2),
      detune: makeParam(context, 0, 'a-rate', -153600, 153600),
      started: false,
      startTime: 0,
      stopTime: Infinity,
      onended: null,
    });
  };
  function OscillatorNode(context, options = {}) {
    if (!new.target) throw new TypeError('Expected new');
    const object = makeOscillator(context);
    Object.setPrototypeOf(object, new.target.prototype);
    options = options ?? {};
    if (
      options.type !== undefined &&
      !['sine', 'square', 'sawtooth', 'triangle', 'custom'].includes(String(options.type))
    )
      throw new TypeError('Invalid oscillator type');
    for (const key of ['type', 'channelCount', 'channelCountMode', 'channelInterpretation'])
      if (options[key] !== undefined && !(key === 'type' && options.periodicWave !== undefined))
        object[key] = options[key];
    for (const key of ['frequency', 'detune'])
      if (options[key] !== undefined) {
        object[key].value = options[key];
        params.get(object[key]).initialChange = false;
      }
    if (options.periodicWave !== undefined) object.setPeriodicWave(options.periodicWave);
    return object;
  }
  install('OscillatorNode', OscillatorNode);
  getter('OscillatorNode', 'type', nodes, 'oscillatorType', function (value) {
    const n = requireSlot(nodes, this);
    value = String(value);
    if (value === 'custom') throw exception('InvalidStateError');
    if (!['sine', 'square', 'sawtooth', 'triangle'].includes(value)) return;
    n.oscillatorType = value;
    n.wave = null;
  });
  for (const key of ['frequency', 'detune']) getter('OscillatorNode', key, nodes);
  method('OscillatorNode', 'setPeriodicWave', function setPeriodicWave(wave) {
    const n = requireSlot(nodes, this);
    requireSlot(waves, wave);
    n.wave = wave;
    n.oscillatorType = 'custom';
  });
  method('AudioScheduledSourceNode', 'start', function start(when = 0) {
    const n = requireSlot(nodes, this);
    when = time(when);
    if (n.started) throw exception('InvalidStateError');
    n.started = true;
    n.startTime = when;
  });
  const compressorParams = {
    threshold: [-24, -100, 0],
    knee: [30, 0, 40],
    ratio: [12, 1, 20],
    attack: [0.003, 0, 1],
    release: [0.25, 0, 1],
  };
  const makeCompressor = (context) => {
    const extra = { channelCountMode: 'clamped-max', reduction: 0 };
    for (const [key, [value, min, max]] of Object.entries(compressorParams))
      extra[key] = makeParam(context, value, 'k-rate', min, max);
    return makeNode(context, 'DynamicsCompressorNode', 1, 1, extra);
  };
  function DynamicsCompressorNode(context, options = {}) {
    if (!new.target) throw new TypeError('Expected new');
    const object = makeCompressor(context);
    Object.setPrototypeOf(object, new.target.prototype);
    options = options ?? {};
    for (const key of Object.keys(compressorParams))
      if (options[key] !== undefined) object[key].value = options[key];
    for (const key of ['channelCount', 'channelCountMode', 'channelInterpretation'])
      if (options[key] !== undefined) object[key] = options[key];
    return object;
  }
  install('DynamicsCompressorNode', DynamicsCompressorNode);
  for (const key of [...Object.keys(compressorParams), 'reduction'])
    getter('DynamicsCompressorNode', key, nodes);
  function OfflineAudioContext(channels, length, sampleRate) {
    if (!new.target || arguments.length === 0) throw new TypeError('Expected offline dimensions');
    if (typeof channels !== 'object' && arguments.length < 3)
      throw new TypeError('Expected three arguments');
    const s = {
      ...dimensions(
        typeof channels === 'object'
          ? channels
          : { numberOfChannels: channels, length, sampleRate },
      ),
      state: 'suspended',
      currentTime: 0,
      nodes: [],
      params: [],
      started: false,
      renderFrame: 0,
      suspensions: new Map(),
      renderContinuation: null,
      onstatechange: null,
      oncomplete: null,
    };
    const object = new EventTarget();
    Object.setPrototypeOf(object, new.target.prototype);
    s.object = object;
    contexts.set(object, s);
    s.destination = makeNode(object, 'AudioDestinationNode', 1, 0, {
      channelCount: s.numberOfChannels,
      channelCountMode: 'explicit',
    });
    return object;
  }
  install('OfflineAudioContext', OfflineAudioContext);
  Object.defineProperty(OfflineAudioContext, 'length', { value: 1, configurable: true });
  for (const key of ['sampleRate', 'currentTime', 'state', 'destination'])
    getter('BaseAudioContext', key, contexts);
  getter('OfflineAudioContext', 'length', contexts);
  for (const [type, key] of [
    ['BaseAudioContext', 'onstatechange'],
    ['OfflineAudioContext', 'oncomplete'],
  ])
    getter(type, key, contexts, key, function (value) {
      requireSlot(contexts, this)[key] = typeof value === 'function' ? value : null;
    });
  for (const name of Object.getOwnPropertyNames(BaseAudioContext.prototype)) {
    const d = Object.getOwnPropertyDescriptor(BaseAudioContext.prototype, name);
    if (name !== 'constructor' && typeof d.value === 'function')
      method('BaseAudioContext', name, function () {
        requireSlot(contexts, this);
        unsupported(name);
      });
  }
  method('BaseAudioContext', 'createBuffer', function createBuffer(channels, length, sampleRate) {
    requireSlot(contexts, this);
    if (arguments.length < 3) throw new TypeError('Expected three arguments');
    return new AudioBuffer({ numberOfChannels: channels, length, sampleRate });
  });
  method('BaseAudioContext', 'createBufferSource', function createBufferSource() {
    return makeSource(this);
  });
  method('BaseAudioContext', 'createGain', function createGain() {
    return makeGain(this);
  });
  method('BaseAudioContext', 'createOscillator', function createOscillator() {
    return makeOscillator(this);
  });
  method('BaseAudioContext', 'createDynamicsCompressor', function createDynamicsCompressor() {
    return makeCompressor(this);
  });
  method(
    'BaseAudioContext',
    'createPeriodicWave',
    function createPeriodicWave(real, imag, options = {}) {
      requireSlot(contexts, this);
      if (arguments.length < 2) throw new TypeError('Expected coefficients');
      return makeWave(this, real, imag, !!options?.disableNormalization);
    },
  );
  const decodeWave = (context, bytes) => {
    const c = requireSlot(contexts, context),
      view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength),
      text = (offset, length) => String.fromCharCode(...bytes.subarray(offset, offset + length));
    if (bytes.length < 44 || text(0, 4) !== 'RIFF' || text(8, 4) !== 'WAVE')
      throw exception('EncodingError', 'Unable to decode audio data');
    let format = 0,
      channels = 0,
      sourceRate = 0,
      bits = 0,
      block = 0,
      dataOffset = -1,
      dataLength = 0;
    for (let offset = 12; offset + 8 <= bytes.length; ) {
      const kind = text(offset, 4),
        length = view.getUint32(offset + 4, true),
        start = offset + 8;
      if (start + length > bytes.length) throw exception('EncodingError');
      if (kind === 'fmt ' && length >= 16) {
        format = view.getUint16(start, true);
        channels = view.getUint16(start + 2, true);
        sourceRate = view.getUint32(start + 4, true);
        block = view.getUint16(start + 12, true);
        bits = view.getUint16(start + 14, true);
        if (format === 65534 && length >= 40) format = view.getUint16(start + 24, true);
      } else if (kind === 'data') {
        dataOffset = start;
        dataLength = length;
      }
      offset = start + length + (length & 1);
    }
    if (
      ![1, 3].includes(format) ||
      channels < 1 ||
      channels > 32 ||
      sourceRate < 3000 ||
      sourceRate > 768000 ||
      !block ||
      dataOffset < 0 ||
      ![8, 16, 24, 32].includes(bits) ||
      (format === 3 && bits !== 32)
    )
      throw exception('EncodingError', 'Unsupported audio encoding');
    const sourceLength = Math.floor(dataLength / block),
      targetLength = Math.max(1, Math.round((sourceLength * c.sampleRate) / sourceRate)),
      output = new AudioBuffer({
        numberOfChannels: channels,
        length: targetLength,
        sampleRate: c.sampleRate,
      }),
      read = (frame, channel) => {
        const offset = dataOffset + frame * block + channel * (bits / 8);
        if (format === 3) return view.getFloat32(offset, true);
        if (bits === 8) return (view.getUint8(offset) - 128) / 128;
        if (bits === 16) {
          const value = view.getInt16(offset, true);
          return value < 0 ? value / 32768 : value / 32767;
        }
        if (bits === 24) {
          let value =
            view.getUint8(offset) |
            (view.getUint8(offset + 1) << 8) |
            (view.getUint8(offset + 2) << 16);
          if (value & 0x800000) value -= 0x1000000;
          return value / 8388608;
        }
        return view.getInt32(offset, true) / 2147483648;
      };
    for (let channel = 0; channel < channels; channel++) {
      const target = output.getChannelData(channel);
      for (let frame = 0; frame < targetLength; frame++) {
        const position = (frame * sourceRate) / c.sampleRate,
          lower = Math.min(sourceLength - 1, Math.floor(position)),
          upper = Math.min(sourceLength - 1, lower + 1),
          fraction = position - lower;
        target[frame] = Math.fround(
          read(lower, channel) + (read(upper, channel) - read(lower, channel)) * fraction,
        );
      }
    }
    return output;
  };
  method('BaseAudioContext', 'decodeAudioData', function decodeAudioData(data, success, failure) {
    try {
      requireSlot(contexts, this);
      if (!(data instanceof ArrayBuffer)) throw new TypeError('Expected ArrayBuffer');
      const bytes = new Uint8Array(data).slice(),
        context = this;
      if (typeof host.detachArrayBuffer === 'function') host.detachArrayBuffer(data);
      else if (typeof structuredClone === 'function') structuredClone(data, { transfer: [data] });
      return new platformPromise((resolve, reject) =>
        setTimeout(() => {
          try {
            const buffer = decodeWave(context, bytes);
            if (typeof success === 'function') success(buffer);
            resolve(buffer);
          } catch (error) {
            const failureError =
              error?.name === 'EncodingError' ? error : exception('EncodingError');
            if (typeof failure === 'function') failure(failureError);
            reject(failureError);
          }
        }, 0),
      );
    } catch (error) {
      return platformPromiseReject(error);
    }
  });
  method('OfflineAudioContext', 'suspend', function suspend(when) {
    try {
      const c = requireSlot(contexts, this);
      when = time(when);
      const frame = Math.ceil((when * c.sampleRate) / 128) * 128;
      if (c.state === 'closed' || frame >= c.length || c.suspensions.has(frame))
        throw exception('InvalidStateError');
      return new platformPromise((resolve, reject) =>
        c.suspensions.set(frame, { resolve, reject }),
      );
    } catch (error) {
      return platformPromiseReject(error);
    }
  });
  method('OfflineAudioContext', 'resume', function resume() {
    try {
      const c = requireSlot(contexts, this);
      if (!c.started || c.state !== 'suspended' || !c.renderContinuation)
        throw exception('InvalidStateError');
      c.state = 'running';
      c.resumeEventPending = true;
      const continuation = c.renderContinuation;
      c.renderContinuation = null;
      setTimeout(continuation, 0);
      return platformPromiseResolve();
    } catch (error) {
      return platformPromiseReject(error);
    }
  });
  let prepareAudioNode = () => {},
    processAudioNode = () => {},
    commitAudioNode = () => {};
  function render(context, startFrame = 0, frameCount) {
    const c = requireSlot(contexts, context),
      cache = new Map(),
      visiting = new Set();
    if (nodes.get(c.destination).channelCount !== c.numberOfChannels)
      unsupported('destination channel reconfiguration');
    let storage = 0,
      work = 0;
    const allocate = (count) => {
      storage += count * processedFrames;
      if (storage > 16 * 1024 * 1024) unsupported('graph storage limit');
      return Array.from({ length: count }, () => new Float32Array(processedFrames));
    };
    const processedFrames = Math.ceil((frameCount ?? c.length) / 128) * 128;
    const endFrame = startFrame + processedFrames;
    function sourceOutput(object, output) {
      const n = nodes.get(object);
      n.ended = false;
      if (!n.started) return;
      const b = n.buffer && buffers.get(n.buffer),
        start = n.startTime * c.sampleRate,
        first = Math.ceil(start),
        stop = Math.ceil(n.stopTime * c.sampleRate);
      if (!b) {
        n.ended = first < endFrame;
        return;
      }
      // The rate ratio is formed before scaling: reassociation changes rounding
      // observed by the mixed-rate phase oracle near Float32 midpoints.
      const stepAt = (frame) => {
        const speed =
            parameterAt(n.playbackRate, frame, c.sampleRate) *
            Math.pow(2, parameterAt(n.detune, frame, c.sampleRate) / 1200),
          value = (b.sampleRate / c.sampleRate) * speed;
        if (!Number.isFinite(value) || Math.abs(value) > 1024)
          unsupported('resampling ratio limit');
        if (n.loop && value < 0) unsupported('reverse loop');
        return value;
      };
      let step = stepAt(first);
      if (!Number.isFinite(step) || Math.abs(step) > 1024) unsupported('resampling ratio limit');
      const offset = Math.min(b.length, Math.round(n.offset * b.sampleRate)),
        duration = n.duration * b.sampleRate;
      const loopStart = Math.max(0, n.loopStart * b.sampleRate),
        loopEnd = n.loopEnd > 0 ? Math.min(b.length, n.loopEnd * b.sampleRate) : b.length;
      if (
        n.loop &&
        (step < 0 ||
          !Number.isInteger(loopStart) ||
          !Number.isInteger(loopEnd) ||
          loopStart >= loopEnd ||
          loopStart >= b.length ||
          offset >= b.length)
      )
        unsupported('loop interval');
      // Starts occur at ceil(frame), while their fractional remainder advances the
      // source phase. Offset itself is rounded to the nearest source sample.
      let position = n.renderPosition ?? offset + (first - start) * step,
        consumed = n.renderConsumed ?? (first - start) * Math.abs(step);
      for (let frame = Math.max(first, startFrame); frame <= endFrame; frame++) {
        if (++work > 16 * 1024 * 1024) unsupported('source evaluation limit');
        if (frame < endFrame && frame % 128 === 0) step = stepAt(frame);
        if (frame >= stop || consumed >= duration) {
          n.ended = true;
          break;
        }
        if (n.loop && position >= loopEnd)
          position = loopStart + ((position - loopStart) % (loopEnd - loopStart));
        if (position < 0 || position >= b.length) {
          n.ended = true;
          break;
        }
        if (frame === endFrame) break;
        if (output && frame < endFrame) {
          let lower = Math.floor(position),
            upper = lower + 1;
          if (n.loop && upper >= loopEnd) upper = loopStart;
          // Native linear interpolation extrapolates the last pair at the non-loop
          // edge; a loop instead interpolates toward its first sample.
          else if (upper >= b.length) {
            lower = Math.max(0, b.length - 2);
            upper = b.length - 1;
          }
          const fraction = position - lower;
          for (let ch = 0; ch < output.length; ch++) {
            const values = b.channels[ch],
              a = values[lower];
            output[ch][frame - startFrame] =
              fraction === 0 ? a : a + (values[upper] - a) * fraction;
          }
        }
        // Carry phase across frames and render quanta; recomputing from elapsed
        // time loses observable rounding, even when the difference is tiny.
        position += step;
        consumed += Math.abs(step);
      }
      n.renderPosition = position;
      n.renderConsumed = consumed;
    }
    function oscillatorOutput(object, output) {
      const n = nodes.get(object);
      n.ended = false;
      if (!n.started) return;
      const first = Math.ceil(n.startTime * c.sampleRate),
        stop = Math.ceil(n.stopTime * c.sampleRate),
        w = waves.get(n.wave || basicWave(n.context, n.oscillatorType)),
        scale = f(w.size / w.rate),
        wrap = (v) => v - Math.floor(v / w.size) * w.size;
      let phase = n.renderPhase ?? 0;
      for (
        let quantum = Math.max(startFrame, Math.floor(first / 128) * 128);
        quantum < Math.min(endFrame, stop);
        quantum += 128
      ) {
        const from = Math.max(first, quantum),
          end = Math.min(quantum + 128, stop, endFrame),
          count = Math.max(0, end - from);
        const changing = (key) => {
          const p = params.get(n[key]);
          return (
            p.automationRate === 'a-rate' &&
            ((quantum === 0 && p.initialChange) ||
              p.timeline.some(
                (e) =>
                  (e.time * c.sampleRate >= quantum && e.time * c.sampleRate < quantum + 128) ||
                  (['linear', 'exponential'].includes(e.kind) && e.time * c.sampleRate >= quantum),
              ))
          );
        };
        const accurate = changing('frequency') || changing('detune'),
          frequencyAt = (frame) => {
            const detune = f(Math.pow(2, parameterAt(n.detune, frame, c.sampleRate) / 1200)),
              value = f(
                parameterAt(n.frequency, frame, c.sampleRate) *
                  (detune < 1.1754943508222875e-38 ? 0 : detune),
              );
            return Number.isNaN(value)
              ? c.sampleRate / 2
              : clamp(value, -c.sampleRate / 2, c.sampleRate / 2);
          },
          constant = frequencyAt(from),
          increment = f(constant * scale);
        if (from === first)
          phase = accurate ? 0 : (first - n.startTime * c.sampleRate) * constant * scale;
        phase = wrap(phase);
        // Chrome's constant-rate path advances four Float32 phase lanes and
        // reanchors their double-precision origin at every render quantum. This
        // rounding is observable near the steep edge of a band-limited square.
        const vectorCount = !accurate && increment >= f(0.3) ? count - (count % 4) : 0,
          origin = phase,
          lanes = Array.from({ length: 4 }, (_, i) => wrap(f(phase + f(i * increment))));
        for (let i = 0; i < count; i++) {
          if (++work > 16 * 1024 * 1024) unsupported('source evaluation limit');
          const frame = from + i,
            frequency = accurate ? frequencyAt(frame) : constant,
            step = accurate ? f(frequency * scale) : increment;
          if (i < vectorCount) {
            phase = lanes[i % 4];
            lanes[i % 4] = wrap(f(phase + f(4 * increment)));
          } else if (i === vectorCount && vectorCount)
            phase = wrap(origin + f(vectorCount * increment));
          output[0][frame - startFrame] = waveSample(w, phase, frequency, step);
          if (i >= vectorCount) phase = wrap(phase + step);
        }
        if (vectorCount) phase = wrap(origin + f(count * increment));
      }
      n.ended = stop <= endFrame;
      n.renderPhase = phase;
    }
    // Chrome 152 compressor: linked-channel peak detector, soft knee, 6 ms
    // lookahead, 32-frame envelope divisions and adaptive release. The delayed
    // PCM and envelope belong to this node evaluation, never to a global device.
    function compress(n, result) {
      work += processedFrames * result.length;
      if (work > 16 * 1024 * 1024) unsupported('compressor evaluation limit');
      // Chrome multiplies by the Float32 0.05 constant before powf. Division by
      // 20 is algebraically equivalent but changes the rounded envelope and PCM.
      const db = (x) => f(20 * f(Math.log10(x))),
        linear = (x) => f(Math.pow(10, f(f(0.05) * x))),
        safe = (x, d) => (Number.isFinite(x) ? x : d);
      const previous = n.compressorState;
      let detector = previous?.detector ?? 0,
        gain = previous?.gain ?? 1,
        meter = previous?.meter ?? 1,
        maxAttack = previous?.maxAttack ?? -1,
        read = previous?.read ?? 0,
        write = previous?.write ?? Math.min(1023, Math.floor(f(f(0.006) * c.sampleRate)));
      const delay = previous?.delay ?? result.map(() => new Float32Array(1024)),
        meterRelease = f(1 - Math.exp(-1 / (f(0.325) * c.sampleRate)));
      const zones = [0.09, 0.16, 0.42, 0.98].map(f),
        coefficients = [
          [
            0.9999999999999998, 1.8432219684323923e-16, -1.9373394351676423e-16,
            8.824516011816245e-18,
          ],
          [-1.5788320352845888, 2.3305837032074286, -0.9141194204840429, 0.1623677525612032],
          [0.5334142869106424, -1.272736789213631, 0.9258856042207512, -0.18656310191776226],
          [0.08783463138207234, -0.1694162967925622, 0.08588057951595272, -0.00429891410546283],
          [-0.042416883008123074, 0.1115693827987602, -0.09764676325265872, 0.028494263462021576],
        ].map((row) => row.reduce((sum, v, i) => f(sum + f(f(v) * zones[i])), 0));
      for (let quantum = 0; quantum < processedFrames; quantum += 128) {
        const get = (key) => {
            const p = params.get(n[key]);
            return clamp(
              parameterAt(n[key], quantum + startFrame, c.sampleRate),
              p.minValue,
              p.maxValue,
            );
          },
          threshold = get('threshold'),
          knee = get('knee'),
          ratio = get('ratio'),
          attackFrames = f(Math.max(f(0.001), get('attack')) * c.sampleRate),
          releaseFrames = f(c.sampleRate * get('release')),
          satFrames = f(f(0.0025) * c.sampleRate),
          a = coefficients.map((v) => f(v * releaseFrames));
        const slope = f(1 / ratio),
          lt = linear(threshold),
          kneeDB = f(threshold + knee),
          kt = linear(kneeDB);
        const curve = (x, k) => (x < lt ? x : f(lt + f(f(1 - f(Math.exp(f(-k * f(x - lt))))) / k)));
        let minK = f(0.1),
          maxK = 10000,
          k = 5;
        const x2 = f(kt * 1.001),
          dbx2 = db(x2);
        for (let i = 0; i < 15; i++) {
          const derivative =
            kt < lt ? 1 : f(f(db(curve(x2, k)) - db(curve(kt, k))) / f(dbx2 - kneeDB));
          if (derivative < slope) maxK = k;
          else minK = k;
          k = f(Math.sqrt(f(minK * maxK)));
        }
        const ky = db(curve(kt, k)),
          saturate = (x) => (x < kt ? curve(x, k) : linear(f(ky + f(slope * f(db(x) - kneeDB))))),
          post = f(Math.pow(f(1 / saturate(1)), f(0.6)));
        for (let division = 0; division < 128; division += 32) {
          detector = safe(detector, 1);
          const desired = f(f(Math.asin(detector)) / f(Math.PI / 2)),
            releasing = desired > gain;
          let diff = desired === 0 ? (releasing ? -1 : 1) : db(f(gain / desired)),
            envelope;
          if (releasing) {
            maxAttack = -1;
            diff = safe(diff, -1);
            const x = f(0.25 * f(clamp(diff, -12, 0) + 12)),
              x2 = f(x * x),
              x3 = f(x2 * x),
              x4 = f(x2 * x2),
              frames = f(f(f(f(a[0] + f(a[1] * x)) + f(a[2] * x2)) + f(a[3] * x3)) + f(a[4] * x4));
            envelope = linear(f(5 / frames));
          } else {
            diff = safe(diff, 1);
            maxAttack = maxAttack === -1 ? diff : Math.max(maxAttack, diff);
            envelope = f(1 - f(Math.pow(f(0.25 / Math.max(0.5, maxAttack)), f(1 / attackFrames))));
          }
          for (let j = 0; j < 32; j++) {
            const frame = quantum + division + j;
            let input = 0;
            for (let ch = 0; ch < result.length; ch++) {
              const value = result[ch][frame];
              delay[ch][write] = value;
              input = Math.max(input, Math.abs(value));
            }
            const attenuation = input <= f(0.0001) ? 1 : f(saturate(input) / input),
              attenDB = Math.max(2, -db(attenuation)),
              satRate = f(linear(f(attenDB / satFrames)) - 1);
            detector = Math.min(
              1,
              f(detector + f(f(attenuation - detector) * (attenuation > detector ? satRate : 1))),
            );
            detector = safe(detector, 1);
            gain =
              envelope < 1
                ? f(gain + f(f(desired - gain) * envelope))
                : Math.min(1, f(gain * envelope));
            const warped = f(Math.sin(f(f(Math.PI / 2) * gain))),
              total = f(post * warped),
              realDB = db(warped);
            meter = realDB < meter ? realDB : f(meter + f(f(realDB - meter) * meterRelease));
            if (frame < processedFrames)
              for (let ch = 0; ch < result.length; ch++)
                result[ch][frame] = f(delay[ch][read] * total);
            read = (read + 1) & 1023;
            write = (write + 1) & 1023;
          }
        }
      }
      n.reduction = meter;
      n.compressorState = { detector, gain, meter, maxAttack, read, write, delay };
    }
    function nodeOutput(object, outputIndex = 0) {
      const cached = cache.get(object)?.[outputIndex];
      if (cached) return cached;
      if (visiting.has(object)) {
        const state = nodes.get(object);
        if (state.type === 'DelayNode' && state.feedbackOutput) return state.feedbackOutput;
        unsupported('feedback graphs');
      }
      visiting.add(object);
      const n = nodes.get(object);
      prepareAudioNode(n, startFrame, processedFrames, allocate);
      let result;
      if (n.type === 'DelayNode' && parameterAt(n.delayTime, startFrame, c.sampleRate) > 0) {
        result = n.feedbackOutput;
      } else if (n.type === 'AudioBufferSourceNode') {
        const b = n.buffer && buffers.get(n.buffer);
        result = allocate(b ? b.numberOfChannels : 1);
        sourceOutput(object, result);
      } else if (n.type === 'ConstantSourceNode') {
        result = allocate(1);
        if (n.started) {
          for (let frame = startFrame; frame < endFrame; frame++)
            if (
              frame >= Math.ceil(n.startTime * c.sampleRate) &&
              frame < Math.ceil(n.stopTime * c.sampleRate)
            )
              result[0][frame - startFrame] = parameterAt(n.offset, frame, c.sampleRate);
          n.ended = Math.ceil(n.stopTime * c.sampleRate) <= endFrame;
        }
      } else if (n.type === 'MediaStreamAudioSourceNode' && n.captureTrack) {
        const position = n.captureOrigin + (startFrame / c.sampleRate) * 48000;
        const capture =
          n.captureWindow ||
          cameraCaptureModel.audio(n.captureTrack, position, processedFrames, c.sampleRate);
        const offset = n.captureWindow ? startFrame - n.captureWindow.start : 0;
        result = allocate(capture.channels);
        for (let frame = 0; frame < processedFrames; frame++)
          for (let channel = 0; channel < capture.channels; channel++)
            result[channel][frame] =
              capture.samples[(offset + frame) * capture.channels + channel] ?? 0;
      } else if (n.type === 'OscillatorNode') {
        result = allocate(1);
        oscillatorOutput(object, result);
      } else if (n.type === 'ChannelSplitterNode') {
        const inputs = n.incoming
            .filter((edge) => edge.input === 0)
            .map((edge) => nodeOutput(edge.source, edge.output)),
          maximum = Math.max(1, ...inputs.map((value) => value.length)),
          mixed = allocate(maximum);
        for (const input of inputs)
          for (let channel = 0; channel < maximum; channel++)
            for (let frame = 0; frame < processedFrames; frame++)
              mixed[channel][frame] = Math.fround(
                mixed[channel][frame] + (input[channel]?.[frame] ?? 0),
              );
        result = [mixed[outputIndex] || new Float32Array(processedFrames)];
      } else if (n.type === 'ChannelMergerNode') {
        result = allocate(n.numberOfInputs);
        for (let port = 0; port < n.numberOfInputs; port++)
          for (const edge of n.incoming.filter((edge) => edge.input === port)) {
            const input = nodeOutput(edge.source, edge.output);
            for (let frame = 0; frame < processedFrames; frame++) {
              let value = 0;
              for (const channel of input) value += channel[frame] / input.length;
              result[port][frame] = Math.fround(result[port][frame] + value);
            }
          }
      } else {
        const inputs = n.incoming
            .filter((edge) => edge.input === 0)
            .map((edge) => nodeOutput(edge.source, edge.output)),
          maximum = Math.max(1, ...inputs.map((v) => v.length));
        let count =
          n.channelCountMode === 'explicit'
            ? n.channelCount
            : n.channelCountMode === 'clamped-max'
              ? Math.min(n.channelCount, maximum)
              : maximum;
        if (
          n.channelInterpretation === 'speakers' &&
          inputs.some((v) => v.length !== count && (count > 2 || v.length > 2))
        )
          unsupported('multichannel speaker conversion');
        result = allocate(count);
        work += inputs.length * count * processedFrames;
        if (work > 16 * 1024 * 1024) unsupported('graph mixing limit');
        for (const input of inputs)
          for (let ch = 0; ch < count; ch++)
            for (let frame = 0; frame < processedFrames; frame++) {
              const value =
                n.channelInterpretation === 'discrete'
                  ? (input[ch]?.[frame] ?? 0)
                  : count === 1 && input.length === 2
                    ? Math.fround(
                        Math.fround(input[0][frame] * 0.5) + Math.fround(input[1][frame] * 0.5),
                      )
                    : input[Math.min(ch, input.length - 1)][frame];
              result[ch][frame] = Math.fround(result[ch][frame] + value);
            }
        processAudioNode(n, result, startFrame, processedFrames);
        if (n.type === 'DynamicsCompressorNode') compress(n, result);
        if (n.type === 'GainNode')
          for (let frame = 0; frame < processedFrames; frame++) {
            const gain = parameterAt(n.gain, frame + startFrame, c.sampleRate);
            for (const ch of result) ch[frame] = Math.fround(ch[frame] * gain);
          }
      }
      visiting.delete(object);
      const outputs = cache.get(object) || [];
      outputs[outputIndex] = result;
      cache.set(object, outputs);
      return result;
    }
    c.parameterSampler = (parameter, frame) => {
      let value = 0;
      const index = frame - startFrame;
      for (const edge of parameter.incoming) {
        const output = nodeOutput(edge.source, edge.output);
        for (const channel of output) value += (channel[index] ?? 0) / output.length;
      }
      return value;
    };
    const samples = nodeOutput(c.destination);
    for (const object of c.nodes) {
      const n = nodes.get(object);
      if (n.type !== 'DelayNode' || parameterAt(n.delayTime, startFrame, c.sampleRate) <= 0)
        continue;
      const inputs = n.incoming
          .filter((edge) => edge.input === 0)
          .map((edge) => nodeOutput(edge.source, edge.output)),
        channels = Math.max(1, ...inputs.map((input) => input.length)),
        mixed = allocate(channels);
      for (const input of inputs)
        for (let channel = 0; channel < channels; channel++)
          for (let frame = 0; frame < processedFrames; frame++)
            mixed[channel][frame] = Math.fround(
              mixed[channel][frame] + (input[channel]?.[frame] ?? input[0]?.[frame] ?? 0),
            );
      commitAudioNode(n, mixed, startFrame, processedFrames);
    }
    if (c.realtime)
      for (const node of c.nodes) if (nodes.get(node).type === 'AnalyserNode') nodeOutput(node);
    const buffer = new AudioBuffer({
      numberOfChannels: c.numberOfChannels,
      length: frameCount ?? c.length,
      sampleRate: c.sampleRate,
    });
    for (let ch = 0; ch < samples.length; ch++) buffer.copyToChannel(samples[ch], ch);
    c.parameterSampler = null;
    return buffer;
  }
  method('OfflineAudioContext', 'startRendering', function startRendering() {
    try {
      const c = requireSlot(contexts, this);
      if (c.started) throw exception('InvalidStateError');
      c.started = true;
      c.state = 'running';
      return new platformPromise((resolve, reject) => {
        const buffer = new AudioBuffer({
          numberOfChannels: c.numberOfChannels,
          length: c.length,
          sampleRate: c.sampleRate,
        });
        const finish = () => {
          try {
            c.currentTime = (Math.ceil(c.length / 128) * 128) / c.sampleRate;
            if (!c.runningEventEmitted || c.resumeEventPending) {
              emit(this, 'statechange');
              c.runningEventEmitted = true;
              c.resumeEventPending = false;
            }
            c.state = 'closed';
            for (const object of c.nodes) {
              const n = nodes.get(object);
              if (
                ['AudioBufferSourceNode', 'OscillatorNode', 'ConstantSourceNode'].includes(
                  n.type,
                ) &&
                n.ended
              )
                emit(object, 'ended');
            }
            emit(this, 'complete', buffer);
            resolve(buffer);
          } catch (error) {
            c.state = 'closed';
            reject(error);
          }
          setTimeout(() => emit(this, 'statechange'), 0);
        };
        const step = () => {
          try {
            if (c.renderFrame >= c.length) return finish();
            const suspension = c.suspensions.get(c.renderFrame);
            if (suspension) {
              c.suspensions.delete(c.renderFrame);
              if (!c.runningEventEmitted) {
                emit(this, 'statechange');
                c.runningEventEmitted = true;
              }
              c.state = 'suspended';
              c.renderContinuation = step;
              suspension.resolve();
              setTimeout(() => emit(this, 'statechange'), 0);
              return;
            }
            const count = Math.min(128, c.length - c.renderFrame),
              quantum = render(this, c.renderFrame, count);
            for (let channel = 0; channel < c.numberOfChannels; channel++)
              buffer.copyToChannel(quantum.getChannelData(channel), channel, c.renderFrame);
            c.renderFrame += 128;
            c.currentTime = Math.min(c.length, c.renderFrame) / c.sampleRate;
            setTimeout(step, 0);
          } catch (error) {
            c.state = 'closed';
            reject(error);
          }
        };
        setTimeout(step, 0);
      });
    } catch (e) {
      return platformPromiseReject(e);
    }
  });
  /* shared_audio_nodes */
  /* shared_realtime_audio */
})();
