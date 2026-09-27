// Intersection observations use the same approximate boxes as DOM geometry.
// This is not a paint/layout engine: transforms, scrolling and cross-frame
// clipping remain outside that model. Never replace the boxes with an
// unconditional "visible" notification just to trigger lazy loaders.
const intersectionObserverSlots = new WeakMap(),
  intersectionEntrySlots = new WeakMap();
const intersectionObservers = new Set();
let renderingTimer = null,
  animationFrameSequence = 0;
let lastIntersectionVersion = null,
  intersectionTargetsChanged = false;
const animationFrameCallbacks = new Map(),
  pendingAnimationFrames = new Set();
const requestRenderingFrame = (callback) => {
  if (typeof callback !== 'function') throw new TypeError('callback is not a function');
  const id = ++animationFrameSequence;
  animationFrameCallbacks.set(id, callback);
  pendingAnimationFrames.add(id);
  scheduleIntersectionUpdate();
  return id;
};
const cancelRenderingFrame = (id) => {
  id = Number(id);
  animationFrameCallbacks.delete(id);
  pendingAnimationFrames.delete(id);
};
const intersectionState = (observer) => {
  const state = intersectionObserverSlots.get(observer);
  if (!state) throw new TypeError('Illegal invocation');
  return state;
};
const intersectionMargin = (value) => {
  const tokens = String(value).trim().split(/\s+/);
  if (
    tokens.length < 1 ||
    tokens.length > 4 ||
    tokens.some((v) => !/^[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:px|%)$/.test(v))
  )
    throw platformDOMException('rootMargin must be specified in pixels or percent.', 'SyntaxError');
  const parts = tokens.map((v) => ({
    value: Number.parseFloat(v),
    unit: v.endsWith('%') ? '%' : 'px',
  }));
  const [a, b = a, c = a, d = b] = parts;
  return [a, b, c, d];
};
class IntersectionObserverEntry {
  constructor(init) {
    if (!init || !(init.target instanceof Element))
      throw new TypeError('IntersectionObserverEntry requires a target Element');
    const rect = (value) =>
      value == null
        ? null
        : new DOMRectReadOnly(
            Number(value.x) || 0,
            Number(value.y) || 0,
            Number(value.width) || 0,
            Number(value.height) || 0,
          );
    intersectionEntrySlots.set(this, {
      time: Number(init.time) || 0,
      target: init.target,
      rootBounds: rect(init.rootBounds),
      boundingClientRect: rect(init.boundingClientRect),
      intersectionRect: rect(init.intersectionRect),
      isIntersecting: !!init.isIntersecting,
      intersectionRatio: Number(init.intersectionRatio) || 0,
      isVisible: !!init.isVisible,
    });
  }
}
for (const key of [
  'time',
  'target',
  'rootBounds',
  'boundingClientRect',
  'intersectionRect',
  'isIntersecting',
  'intersectionRatio',
  'isVisible',
])
  def(IntersectionObserverEntry.prototype, key, {
    get() {
      const state = intersectionEntrySlots.get(this);
      if (!state) throw new TypeError('Illegal invocation');
      return state[key];
    },
  });
Object.defineProperty(IntersectionObserverEntry.prototype, Symbol.toStringTag, {
  value: 'IntersectionObserverEntry',
  configurable: true,
});
const intersectionParent = (element) =>
  element.parentElement || shadowSlots.get(syntheticParents.get(element))?.host || null;
const intersectionRect = (x = 0, y = 0, width = 0, height = 0) => ({
  x,
  y,
  width,
  height,
  left: x,
  top: y,
  right: x + width,
  bottom: y + height,
});
const intersectionSample = (state, target, time, eligibility) => {
  const zero = intersectionRect();
  const eligible = (element) => {
    if (!element) return true;
    if (eligibility.has(element)) return eligibility.get(element);
    const parent = intersectionParent(element),
      display =
        blitzStyleValue(element, 'display') ??
        computedCSSDeclarations(element).find((e) => e.name === 'display')?.value,
      valid =
        !blitzSkippedContent(element) &&
        element.isConnected &&
        element.ownerDocument === document &&
        display !== 'none' &&
        (display || !element.hasAttribute('hidden')) &&
        eligible(parent);
    eligibility.set(element, valid);
    return valid;
  };
  let valid = eligible(target);
  if (state.root)
    valid =
      valid && (state.root === document || (state.root.isConnected && state.root.contains(target)));
  let box = zero;
  if (valid) box = clientRectFor(target);
  let root = zero,
    intersection = zero,
    isIntersecting = false;
  if (valid) {
    const viewport = host.viewport(),
      base =
        state.root instanceof Element
          ? clientRectFor(state.root)
          : intersectionRect(0, 0, viewport.width, viewport.height);
    const [top, right, bottom, left] = state.margin.map(
      (part) => part.value * (part.unit === '%' ? base.width / 100 : 1),
    );
    root = intersectionRect(
      base.x - left,
      base.y - top,
      Math.max(0, base.width + left + right),
      Math.max(0, base.height + top + bottom),
    );
    let x = Math.max(root.left, box.left),
      y = Math.max(root.top, box.top),
      endX = Math.min(root.right, box.right),
      endY = Math.min(root.bottom, box.bottom);
    for (
      let ancestor = intersectionParent(target);
      ancestor && ancestor !== state.root;
      ancestor = intersectionParent(ancestor)
    ) {
      const native = blitzStyleValue(ancestor, 'overflow-x'),
        entries = native === null ? computedCSSDeclarations(ancestor) : null,
        get = (name) =>
          entries
            ? entries.find((e) => e.name === name)?.value || ''
            : blitzStyleValue(ancestor, name) || '',
        overflow = get('overflow');
      const clipX = /^(hidden|clip|scroll|auto)$/.test(get('overflow-x') || overflow),
        clipY = /^(hidden|clip|scroll|auto)$/.test(get('overflow-y') || overflow);
      if (clipX || clipY) {
        const bounds = clientRectFor(ancestor);
        if (clipX) {
          x = Math.max(x, bounds.left);
          endX = Math.min(endX, bounds.right);
        }
        if (clipY) {
          y = Math.max(y, bounds.top);
          endY = Math.min(endY, bounds.bottom);
        }
      }
    }
    isIntersecting = endX >= x && endY >= y;
    if (isIntersecting) intersection = intersectionRect(x, y, endX - x, endY - y);
  }
  const area = box.width * box.height,
    ratio = area ? (intersection.width * intersection.height) / area : isIntersecting ? 1 : 0;
  return new IntersectionObserverEntry({
    time,
    target,
    rootBounds: root,
    boundingClientRect: box,
    intersectionRect: intersection,
    isIntersecting,
    intersectionRatio: ratio,
  });
};
const scheduleIntersectionUpdate = () => {
  if (renderingTimer !== null || (!intersectionObservers.size && !pendingAnimationFrames.size))
    return;
  // One sampled update for the realm, not a timer (or a Go crossing) per target.
  // Compute all entries before calling author code so a callback cannot mutate
  // a later observer's sample or poison a shared computed-style read cache.
  renderingTimer = host.setTimer(
    () => {
      renderingTimer = null;
      const timestamp = host.performanceNow(),
        frames = Array.from(pendingAnimationFrames);
      pendingAnimationFrames.clear();
      // A rendering opportunity runs its rAF callbacks before sampling layout
      // intersections. Each callback remains a scheduler task with its own
      // microtask checkpoint, and cancellation can suppress a later callback.
      for (const id of frames)
        host.queueIntersectionObserver(() => {
          const callback = animationFrameCallbacks.get(id);
          animationFrameCallbacks.delete(id);
          if (callback) callback(timestamp);
        });
      host.queueIntersectionObserver(() => {
        if (!intersectionObservers.size) return;
        const version =
          host.observationVersion() +
          ':' +
          (constructedStyleSheets.revision?.() || 0) +
          ':' +
          compatibilityElementState.observationVersion() +
          ':' +
          compatibilityScrolling.revision();
        // Synthetic shadow attachment and membership changes now invalidate the
        // canonical observation epoch too. An unchanged shadow tree does not
        // require recomputing every observed box at every rendering opportunity.
        if (!intersectionTargetsChanged && version === lastIntersectionVersion) return;
        lastIntersectionVersion = version;
        intersectionTargetsChanged = false;
        withStyleReadCache(() => {
          const time = host.performanceNow(),
            eligibility = new WeakMap();
          for (const observer of intersectionObservers) {
            const state = intersectionState(observer);
            for (const [target, previous] of state.targets) {
              const entry = intersectionSample(state, target, time, eligibility);
              const threshold = state.thresholds.findIndex(
                (value) => value > entry.intersectionRatio,
              );
              if (
                !previous ||
                previous.threshold !== threshold ||
                previous.intersects !== entry.isIntersecting
              ) {
                state.records.push(entry);
                state.targets.set(target, { threshold, intersects: entry.isIntersecting });
              }
            }
            if (state.records.length && !state.queued) {
              state.queued = true;
              host.queueIntersectionObserver(() => {
                state.queued = false;
                const records = state.records.splice(0);
                if (records.length) state.callback.call(observer, records, observer);
              });
            }
          }
        });
      });
      scheduleIntersectionUpdate();
    },
    16,
    false,
  );
};
const removeIntersectionObserver = (observer) => {
  intersectionObservers.delete(observer);
  if (!intersectionObservers.size && !pendingAnimationFrames.size && renderingTimer !== null) {
    host.clearTimer(renderingTimer);
    renderingTimer = null;
  }
};
class IntersectionObserver {
  constructor(callback, options = {}) {
    if (typeof callback !== 'function')
      throw new TypeError('IntersectionObserver callback must be callable');
    options = options || {};
    const root = options.root ?? null;
    if (root !== null && !(root instanceof Element) && !(root instanceof Document))
      throw new TypeError('IntersectionObserver root must be an Element or Document');
    const thresholds = (
      options.threshold === undefined
        ? [0]
        : Array.isArray(options.threshold)
          ? options.threshold
          : [options.threshold]
    )
      .map(Number)
      .sort((a, b) => a - b);
    if (thresholds.some((v) => !Number.isFinite(v) || v < 0 || v > 1))
      throw new RangeError('Threshold values must be numbers between 0 and 1');
    if (!thresholds.length) thresholds.push(0);
    const margin = intersectionMargin(options.rootMargin ?? '0px'),
      scrollMargin = intersectionMargin(options.scrollMargin ?? '0px');
    if (options.trackVisibility || scrollMargin.some((p) => p.value !== 0))
      host.semanticMissingAt(
        'intersection_observer.js',
        'IntersectionObserver.visibilityAndScrollMargin',
      );
    intersectionObserverSlots.set(this, {
      callback,
      root,
      margin,
      scrollMargin,
      thresholds,
      records: [],
      targets: new Map(),
      queued: false,
    });
  }
  get root() {
    return intersectionState(this).root;
  }
  get rootMargin() {
    return intersectionState(this)
      .margin.map((p) => p.value + p.unit)
      .join(' ');
  }
  get scrollMargin() {
    return intersectionState(this)
      .scrollMargin.map((p) => p.value + p.unit)
      .join(' ');
  }
  get thresholds() {
    return intersectionState(this).thresholds.slice();
  }
  observe(target) {
    const state = intersectionState(this);
    if (!(target instanceof Element))
      throw new TypeError('IntersectionObserver target must be an Element');
    if (state.targets.has(target)) return;
    state.targets.set(target, null);
    intersectionTargetsChanged = true;
    intersectionObservers.add(this);
    scheduleIntersectionUpdate();
  }
  unobserve(target) {
    const state = intersectionState(this);
    if (!(target instanceof Element))
      throw new TypeError('IntersectionObserver target must be an Element');
    state.targets.delete(target);
    if (!state.targets.size) removeIntersectionObserver(this);
  }
  disconnect() {
    const state = intersectionState(this);
    state.targets.clear();
    removeIntersectionObserver(this);
  }
  takeRecords() {
    return intersectionState(this).records.splice(0);
  }
}
