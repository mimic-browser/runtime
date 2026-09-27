// Web Animations have an observable control lifecycle even when the runtime
// does not render intermediate frames. Keep one coherent clock/state model and
// complete effects deterministically; sampled visual interpolation remains an
// explicit boundary of the non-rendering runtime.
const webAnimations = (() => {
  const animations = new Set(),
    animationsByTarget = new WeakMap(),
    animationState = new WeakMap(),
    effectState = new WeakMap();
  const member = (prototype, name, value) => {
    Object.defineProperty(value, 'name', { value: name, configurable: true });
    markNative(value, name);
    Object.defineProperty(prototype, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const accessor = (prototype, name, get, set) => {
    if (get) Object.defineProperty(get, 'name', { value: 'get ' + name, configurable: true });
    if (set) Object.defineProperty(set, 'name', { value: 'set ' + name, configurable: true });
    markNative(get, name, 'get ');
    markNative(set, name, 'set ');
    Object.defineProperty(prototype, name, { get, set, enumerable: true, configurable: true });
  };
  const number = (value, fallback = 0) => {
    value = Number(value);
    return Number.isFinite(value) ? value : fallback;
  };
  const timing = (input) => {
    if (typeof input === 'number') input = { duration: input };
    else input = input && typeof input === 'object' ? input : {};
    const duration = number(input.duration),
      iterations = number(input.iterations, 1),
      iterationStart = number(input.iterationStart);
    if (duration < 0 || iterations < 0 || iterationStart < 0)
      throw new TypeError('Invalid effect timing');
    return {
      delay: number(input.delay),
      direction: String(input.direction || 'normal'),
      duration,
      endDelay: number(input.endDelay),
      easing: String(input.easing || 'linear'),
      fill: String(input.fill || 'none'),
      iterationStart,
      iterations,
    };
  };
  const keyframes = (input) => {
    if (Array.isArray(input)) return input.map((frame) => ({ ...frame }));
    if (!input || typeof input !== 'object') return [];
    const result = [];
    let length = 0;
    for (const [name, value] of Object.entries(input))
      if (!['offset', 'easing', 'composite'].includes(name))
        length = Math.max(length, Array.isArray(value) ? value.length : 1);
    for (let index = 0; index < length; index++) {
      const frame = {};
      for (const [name, value] of Object.entries(input))
        frame[name] = Array.isArray(value) ? value[Math.min(index, value.length - 1)] : value;
      result.push(frame);
    }
    return result;
  };
  const properties = (frames) => {
    const result = new Map();
    for (const frame of frames)
      for (const name of Object.keys(frame))
        if (!['offset', 'easing', 'composite'].includes(name)) result.set(cssName(name), name);
    return result;
  };
  const effectFor = (value) => {
    const state = effectState.get(value);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const stateFor = (value) => {
    const state = animationState.get(value);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const clear = (state) => {
    if (state.timer !== null) {
      clearTimeout(state.timer);
      state.timer = null;
    }
  };
  const track = (animation) => {
    // Time-driven observations cannot use a mutation-only scalar projection.
    host.disableStyleProjectionCache();
    styleObservationDynamic = true;
    const state = stateFor(animation),
      effect = state.effect && effectState.get(state.effect),
      target = effect?.target;
    if (!effect) return;
    effect.animation = animation;
    if (!target) return;
    let set = animationsByTarget.get(target);
    if (!set) {
      set = new Set();
      animationsByTarget.set(target, set);
    }
    set.add(animation);
  };
  const untrack = (animation) => {
    const state = stateFor(animation),
      effect = state.effect && effectState.get(state.effect),
      set = effect?.target && animationsByTarget.get(effect.target);
    set?.delete(animation);
    if (effect?.animation === animation) effect.animation = null;
  };
  const total = (state) => {
    if (!state.effect) return 0;
    const timing = effectFor(state.effect).timing;
    return Math.max(0, timing.delay + timing.duration * timing.iterations + timing.endDelay);
  };
  const complete = (animation, state) => {
    if (state.playState === 'idle' || state.playState === 'finished') return;
    clear(state);
    state.currentTime = total(state);
    state.playState = 'finished';
    state.pending = false;
    state.resolveFinished(animation);
    queueMicrotask(() => {
      const event = new Event('finish');
      animation.dispatchEvent(event);
      if (typeof state.onfinish === 'function') state.onfinish.call(animation, event);
    });
  };
  const schedule = (animation, state) => {
    clear(state);
    if (state.playState !== 'running') return;
    const remaining = Math.max(0, total(state) - (state.currentTime || 0));
    state.timer = setTimeout(
      () => complete(animation, state),
      remaining / Math.max(Math.abs(state.playbackRate), Number.EPSILON),
    );
  };
  if (typeof globalThis.KeyframeEffect === 'function') {
    member(globalThis.KeyframeEffect.prototype, 'getTiming', function () {
      return { ...effectFor(this).timing };
    });
    member(globalThis.KeyframeEffect.prototype, 'getComputedTiming', function () {
      const state = effectFor(this),
        localTime = state.animation ? stateFor(state.animation).currentTime : null,
        { delay, duration, iterations, iterationStart } = state.timing,
        activeDuration = duration * iterations;
      let progress = null,
        currentIteration = null;
      if (localTime !== null && localTime >= delay && localTime <= delay + activeDuration) {
        const position = duration ? Math.max(0, (localTime - delay) / duration) : iterations,
          current = Math.min(Math.floor(position), Math.max(0, Math.ceil(iterations) - 1));
        currentIteration = Math.floor(iterationStart + current);
        progress = duration ? position - current : 1;
        if (localTime === delay + activeDuration && activeDuration > 0) progress = 1;
      }
      return {
        ...state.timing,
        activeDuration,
        endTime: delay + activeDuration + state.timing.endDelay,
        localTime,
        progress,
        currentIteration,
      };
    });
    member(globalThis.KeyframeEffect.prototype, 'getKeyframes', function () {
      const frames = effectFor(this).keyframes;
      return frames.map((frame, index) => {
        const result = {
          offset: frame.offset == null ? null : Number(frame.offset),
          easing: String(frame.easing || 'linear'),
          composite: String(frame.composite || 'auto'),
        };
        for (const [name, value] of Object.entries(frame))
          if (!['offset', 'easing', 'composite'].includes(name)) result[name] = String(value);
        result.computedOffset =
          frame.offset == null
            ? frames.length === 1
              ? 1
              : index / (frames.length - 1)
            : Number(frame.offset);
        return result;
      });
    });
    member(globalThis.KeyframeEffect.prototype, 'setKeyframes', function (value) {
      const state = effectFor(this),
        frames = keyframes(value);
      state.keyframes = frames;
      state.properties = properties(frames);
    });
    member(globalThis.KeyframeEffect.prototype, 'updateTiming', function (value = {}) {
      const state = effectFor(this);
      state.timing = timing({ ...state.timing, ...value });
    });
    for (const name of ['target', 'pseudoElement', 'composite', 'iterationComposite'])
      accessor(
        globalThis.KeyframeEffect.prototype,
        name,
        function () {
          return (
            effectFor(this)[name] ??
            (name === 'target' ? null : name === 'pseudoElement' ? null : 'replace')
          );
        },
        function (value) {
          const state = effectFor(this);
          if (name === 'target') {
            const owners = Array.from(animations).filter(
              (animation) => stateFor(animation).effect === this,
            );
            for (const animation of owners) untrack(animation);
            state[name] = value;
            for (const animation of owners) track(animation);
          } else state[name] = value;
        },
      );
    const old = globalThis.KeyframeEffect,
      prototype = old.prototype;
    const KeyframeEffect = function (target, frames, options = {}) {
      if (!new.target)
        throw new TypeError("Failed to construct 'KeyframeEffect': Please use the 'new' operator");
      if (target !== null && !(target instanceof Element))
        throw new TypeError(
          "Failed to construct 'KeyframeEffect': parameter 1 is not of type 'Element'",
        );
      const effect = Object.create(prototype),
        normalized = keyframes(frames);
      effectState.set(effect, {
        target,
        keyframes: normalized,
        properties: properties(normalized),
        timing: timing(options),
        pseudoElement: null,
        composite: 'replace',
        iterationComposite: 'replace',
      });
      return effect;
    };
    Object.defineProperty(KeyframeEffect, 'name', { value: 'KeyframeEffect' });
    Object.defineProperty(KeyframeEffect, 'length', { value: 1 });
    markNative(KeyframeEffect, 'KeyframeEffect');
    Object.defineProperty(KeyframeEffect, 'prototype', { value: prototype });
    Object.defineProperty(prototype, 'constructor', {
      value: KeyframeEffect,
      writable: true,
      configurable: true,
    });
    globalThis.KeyframeEffect = KeyframeEffect;
  }
  if (typeof globalThis.Animation === 'function') {
    for (const name of ['id', 'timeline'])
      accessor(
        globalThis.Animation.prototype,
        name,
        function () {
          return stateFor(this)[name];
        },
        function (value) {
          stateFor(this)[name] = name === 'id' ? String(value) : value;
        },
      );
    accessor(
      globalThis.Animation.prototype,
      'effect',
      function () {
        return stateFor(this).effect;
      },
      function (value) {
        const state = stateFor(this);
        untrack(this);
        state.effect = value;
        track(this);
      },
    );
    accessor(
      globalThis.Animation.prototype,
      'startTime',
      function () {
        return stateFor(this).startTime;
      },
      function (value) {
        const state = stateFor(this);
        state.startTime = value == null ? null : number(value);
        state.pending = false;
        if (state.startTime !== null)
          state.currentTime = Math.max(
            0,
            (performance.now() - state.startTime) * state.playbackRate,
          );
        schedule(this, state);
      },
    );
    accessor(
      globalThis.Animation.prototype,
      'currentTime',
      function () {
        const state = stateFor(this);
        if (state.playState === 'running' && state.startTime !== null)
          return Math.min(
            total(state),
            Math.max(0, (performance.now() - state.startTime) * state.playbackRate),
          );
        return state.currentTime;
      },
      function (value) {
        const state = stateFor(this);
        state.currentTime = value == null ? null : number(value);
        if (state.startTime !== null && state.currentTime !== null)
          state.startTime = performance.now() - state.currentTime / state.playbackRate;
        schedule(this, state);
      },
    );
    accessor(
      globalThis.Animation.prototype,
      'playbackRate',
      function () {
        return stateFor(this).playbackRate;
      },
      function (value) {
        stateFor(this).playbackRate = number(value, 1);
      },
    );
    for (const name of [
      'playState',
      'replaceState',
      'pending',
      'finished',
      'ready',
      'onfinish',
      'oncancel',
      'onremove',
    ])
      accessor(
        globalThis.Animation.prototype,
        name,
        function () {
          return stateFor(this)[name];
        },
        ['onfinish', 'oncancel', 'onremove'].includes(name)
          ? function (value) {
              stateFor(this)[name] = value;
            }
          : undefined,
      );
    member(globalThis.Animation.prototype, 'play', function () {
      const state = stateFor(this);
      if (state.playState === 'idle') {
        animations.add(this);
        track(this);
      }
      if (state.playState === 'finished' || state.currentTime === null) state.currentTime = 0;
      state.playState = 'running';
      state.pending = true;
      if (state.startTime === null)
        state.startTime = performance.now() - (state.currentTime || 0) / state.playbackRate;
      queueMicrotask(() => {
        state.pending = false;
        schedule(this, state);
      });
    });
    member(globalThis.Animation.prototype, 'pause', function () {
      const state = stateFor(this);
      state.currentTime = this.currentTime;
      state.playState = 'paused';
      state.pending = true;
      clear(state);
      queueMicrotask(() => {
        state.pending = false;
      });
    });
    member(globalThis.Animation.prototype, 'finish', function () {
      complete(this, stateFor(this));
    });
    member(globalThis.Animation.prototype, 'cancel', function () {
      const state = stateFor(this);
      if (state.playState === 'idle') return;
      clear(state);
      state.startTime = null;
      state.currentTime = null;
      state.playState = 'idle';
      state.pending = false;
      animations.delete(this);
      untrack(this);
      queueMicrotask(() => {
        const event = new Event('cancel');
        this.dispatchEvent(event);
        if (typeof state.oncancel === 'function') state.oncancel.call(this, event);
      });
    });
    member(globalThis.Animation.prototype, 'reverse', function () {
      this.playbackRate = -this.playbackRate;
      this.play();
    });
    member(globalThis.Animation.prototype, 'updatePlaybackRate', function (value) {
      this.playbackRate = value;
    });
    member(globalThis.Animation.prototype, 'persist', function () {
      stateFor(this).replaceState = 'persisted';
    });
    member(globalThis.Animation.prototype, 'commitStyles', function () {});
    const old = globalThis.Animation,
      prototype = old.prototype;
    const Animation = function (effect = null, timeline = document.timeline || null) {
      if (!new.target)
        throw new TypeError("Failed to construct 'Animation': Please use the 'new' operator");
      if (effect !== null && !effectState.has(effect))
        throw new TypeError(
          "Failed to construct 'Animation': parameter 1 is not of type 'AnimationEffect'",
        );
      return createAnimation(effect, timeline, false);
    };
    Object.defineProperty(Animation, 'name', { value: 'Animation' });
    Object.defineProperty(Animation, 'length', { value: 0 });
    markNative(Animation, 'Animation');
    Object.defineProperty(Animation, 'prototype', { value: prototype });
    Object.defineProperty(prototype, 'constructor', {
      value: Animation,
      writable: true,
      configurable: true,
    });
    globalThis.Animation = Animation;
  }
  const promisePair = () => {
    let resolve, reject;
    const promise = new platformPromise((yes, no) => {
      resolve = yes;
      reject = no;
    });
    return { promise, resolve, reject };
  };
  function createAnimation(effect, timeline, autoplay) {
    const animation = Object.create(globalThis.Animation.prototype),
      readyPair = promisePair(),
      finishedPair = promisePair();
    const state = {
      effect,
      id: '',
      timeline,
      startTime: null,
      currentTime: autoplay ? 0 : null,
      playbackRate: 1,
      playState: autoplay ? 'running' : 'idle',
      replaceState: 'active',
      pending: autoplay,
      finished: finishedPair.promise,
      ready: readyPair.promise,
      onfinish: null,
      oncancel: null,
      onremove: null,
      timer: null,
      resolveFinished: finishedPair.resolve,
      rejectFinished: finishedPair.reject,
    };
    animationState.set(animation, state);
    if (autoplay) {
      animations.add(animation);
      track(animation);
    }
    queueMicrotask(() => {
      if (state.playState === 'running') {
        state.startTime = performance.now();
        state.pending = false;
        schedule(animation, state);
      }
      readyPair.resolve(animation);
    });
    return animation;
  }
  const animate = function (frames, options = {}) {
    if (!(this instanceof Element) || !elementSlot(this)) throw new TypeError('Illegal invocation');
    const effect = new globalThis.KeyframeEffect(this, frames, options);
    return createAnimation(effect, document.timeline || null, true);
  };
  member(Element.prototype, 'animate', animate);
  member(Element.prototype, 'getAnimations', function () {
    if (!(this instanceof Element) || !elementSlot(this)) throw new TypeError('Illegal invocation');
    return Array.from(animationsByTarget.get(this) || []);
  });
  member(Document.prototype, 'getAnimations', function () {
    if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
    return Array.from(animations);
  });
  if (typeof globalThis.DocumentTimeline === 'function') {
    const timelinePrototype = globalThis.DocumentTimeline.prototype,
      timeline = Object.create(timelinePrototype),
      origin = performance.now();
    accessor(timelinePrototype, 'currentTime', function () {
      return performance.now() - origin;
    });
    Object.defineProperty(Document.prototype, 'timeline', {
      get() {
        if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
        return timeline;
      },
      enumerable: true,
      configurable: true,
    });
  }
  const frameValue = (state, name) => {
    const frames = state.keyframes.filter((frame) => frame[name] !== undefined);
    if (!frames.length) return undefined;
    const animation = state.animation;
    if (!animation) return undefined;
    const control = stateFor(animation),
      time = animation.currentTime;
    if (time === null) return undefined;
    const { delay, duration, iterations, fill, direction } = state.timing,
      end = duration * iterations;
    let progress;
    if (time < delay) {
      if (!['backwards', 'both'].includes(fill)) return undefined;
      progress = 0;
    } else if (time >= delay + end) {
      if (control.playState !== 'finished' && !['forwards', 'both'].includes(fill))
        return undefined;
      progress = direction === 'reverse' ? 0 : 1;
    } else if (duration === 0) progress = 1;
    else {
      const position = (time - delay) / duration,
        iteration = Math.min(Math.floor(position), Math.max(0, iterations - 1)),
        part = position - iteration;
      progress =
        direction === 'reverse' ||
        (direction === 'alternate' && iteration % 2 === 1) ||
        (direction === 'alternate-reverse' && iteration % 2 === 0)
          ? 1 - part
          : part;
    }
    const offsets = frames.map((frame, index) =>
      frame.offset == null
        ? frames.length === 1
          ? 1
          : index / (frames.length - 1)
        : Math.max(0, Math.min(1, Number(frame.offset))),
    );
    let right = offsets.findIndex((offset) => offset >= progress);
    if (right < 0) right = frames.length - 1;
    const left = Math.max(0, right - 1),
      span = offsets[right] - offsets[left],
      portion = span ? Math.max(0, Math.min(1, (progress - offsets[left]) / span)) : 1,
      a = String(frames[left][name]),
      b = String(frames[right][name]);
    const an = /^(-?(?:\d+\.?\d*|\.\d+))(.*)$/.exec(a),
      bn = /^(-?(?:\d+\.?\d*|\.\d+))(.*)$/.exec(b);
    if (an && bn && an[2] === bn[2])
      return cssSerializeNumber(Number(an[1]) + (Number(bn[1]) - Number(an[1])) * portion) + an[2];
    return portion < 0.5 ? a : b;
  };
  webAnimationComputedValue = (element, name) => {
    let value;
    for (const animation of animationsByTarget.get(element) || []) {
      const state = stateFor(animation);
      if (!state.effect) continue;
      const effect = effectFor(state.effect),
        candidate = effect.properties.get(name);
      if (candidate !== undefined) {
        const sampled = frameValue(effect, candidate);
        if (sampled !== undefined) value = sampled;
      }
    }
    return value;
  };
  return { animationState, effectState };
})();
