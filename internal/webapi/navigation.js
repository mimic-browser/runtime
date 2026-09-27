// Navigation wraps the canonical joint session history. Only wrapper identity,
// pending promises and event callbacks belong to this realm.
if (typeof Navigation === 'function') {
  const method = (p, n, f) => {
    markNative(f, n);
    Object.defineProperty(p, n, { value: f, writable: true, enumerable: true, configurable: true });
  };
  const getter = (p, n, f) => {
    markNative(f, n, 'get ');
    Object.defineProperty(p, n, { get: f, enumerable: true, configurable: true });
  };
  const brand = (map, o) => {
    if (!map.has(o)) throw new TypeError('Illegal invocation');
    return map.get(o);
  };
  const nav = new EventTarget();
  Object.setPrototypeOf(nav, Navigation.prototype);
  Object.setPrototypeOf(Navigation.prototype, EventTarget.prototype);
  replaceableWindow('navigation', () => nav);
  const check = (o) => {
    if (o !== nav) throw new TypeError('Illegal invocation');
  };
  const entries = new Map(),
    entrySlots = new WeakMap(),
    destinationSlots = new WeakMap(),
    eventData = new WeakMap();
  let current = null,
    list = [],
    pending = null,
    transition = null,
    activation = null;
  // Browser-owned storage uses the same immutable wire graph as messages and
  // structuredClone; deserialization constructs objects in the receiving realm.
  const encode = (value) => {
    const reference = referenceGet(value);
    if (reference) {
      const reply = host.navigationEncodeReference(
        reference.frame,
        reference.realm,
        reference.handle,
      );
      if (!reply[0]) throw platformDOMException(reply[2], reply[1]);
      return reply[1];
    }
    return cloneCodec.encode(value);
  };
  const decode = (raw) => (raw ? cloneCodec.decode(raw) : undefined);
  cloneCrossRealmHistoryState = (value) => decode(encode(value));
  const refresh = () => {
    const state = host.navigationEntries(),
      seen = new Set();
    list = [];
    for (const row of state.entries) {
      let e = entries.get(row.id);
      if (!e) {
        e = new EventTarget();
        Object.setPrototypeOf(e, NavigationHistoryEntry.prototype);
        entries.set(row.id, e);
      }
      entrySlots.set(e, row);
      list.push(e);
      seen.add(row.id);
    }
    for (const [id, e] of entries) if (!seen.has(id)) entrySlots.get(e).index = -1;
    current = entries.get(state.current) || null;
    if (!activation && current) {
      let from = null;
      if (state.activationFrom) {
        const row = state.activationFrom;
        from = entries.get(row.id);
        if (!from) {
          from = new EventTarget();
          Object.setPrototypeOf(from, NavigationHistoryEntry.prototype);
          entries.set(row.id, from);
          entrySlots.set(from, row);
        }
      }
      activation = Object.create(NavigationActivation.prototype);
      Object.defineProperties(activation, {
        entry: { value: current, enumerable: true },
        from: { value: from, enumerable: true },
        navigationType: { value: state.activationType || 'push', enumerable: true },
      });
    }
  };
  for (const name of ['key', 'id', 'url', 'index', 'sameDocument'])
    getter(NavigationHistoryEntry.prototype, name, function () {
      const s = brand(entrySlots, this);
      if (!host.historyIsActive())
        return name === 'index' ? -1 : name === 'sameDocument' ? false : '';
      refresh();
      return s === entrySlots.get(this) ? s[name] : entrySlots.get(this)[name];
    });
  method(NavigationHistoryEntry.prototype, 'getState', function () {
    brand(entrySlots, this);
    if (!host.historyIsActive()) return undefined;
    refresh();
    return decode(entrySlots.get(this).state);
  });
  Object.setPrototypeOf(NavigationHistoryEntry.prototype, EventTarget.prototype);
  for (const [prototype, type] of [
    [NavigationHistoryEntry.prototype, 'dispose'],
    [Navigation.prototype, 'navigate'],
    [Navigation.prototype, 'navigatesuccess'],
    [Navigation.prototype, 'navigateerror'],
    [Navigation.prototype, 'currententrychange'],
  ])
    Object.defineProperty(prototype, 'on' + type, {
      get() {
        return eventHandlerRecord(this, type).value;
      },
      set(v) {
        setEventHandlerValue(this, type, v);
      },
      enumerable: true,
      configurable: true,
    });
  method(Navigation.prototype, 'entries', function () {
    check(this);
    refresh();
    return list.slice();
  });
  getter(Navigation.prototype, 'currentEntry', function () {
    check(this);
    refresh();
    return current;
  });
  getter(Navigation.prototype, 'canGoBack', function () {
    check(this);
    refresh();
    return !!current && list.indexOf(current) > 0;
  });
  getter(Navigation.prototype, 'canGoForward', function () {
    check(this);
    refresh();
    return !!current && list.indexOf(current) < list.length - 1;
  });
  getter(Navigation.prototype, 'transition', function () {
    check(this);
    return transition;
  });
  getter(Navigation.prototype, 'activation', function () {
    check(this);
    if (!host.historyIsActive()) return null;
    refresh();
    return activation;
  });
  for (const name of ['url', 'key', 'id', 'index', 'sameDocument'])
    getter(NavigationDestination.prototype, name, function () {
      return brand(destinationSlots, this)[name];
    });
  method(NavigationDestination.prototype, 'getState', function () {
    return decode(brand(destinationSlots, this).state);
  });
  class NavigateEvent extends Event {
    constructor(type, init) {
      if (arguments.length < 2) throw new TypeError('2 arguments required');
      super(type, init);
      if (!init.destination || !init.signal)
        throw new TypeError('destination and signal are required');
      eventData.set(this, { ...init, handlers: [], intercepted: false, dispatching: false });
    }
    intercept(options = {}) {
      const s = brand(eventData, this);
      if (!s.dispatching || this.defaultPrevented)
        throw platformDOMException('The event is not being dispatched.', 'InvalidStateError');
      if (!s.canIntercept)
        throw platformDOMException('The navigation cannot be intercepted.', 'SecurityError');
      if (options.handler !== undefined && typeof options.handler !== 'function')
        throw new TypeError('handler must be callable');
      if (
        options.focusReset !== undefined &&
        !['after-transition', 'manual'].includes(String(options.focusReset))
      )
        throw new TypeError('Invalid focusReset');
      if (
        options.scroll !== undefined &&
        !['after-transition', 'manual'].includes(String(options.scroll))
      )
        throw new TypeError('Invalid scroll');
      s.intercepted = true;
      if (options.handler) s.handlers.push(options.handler);
    }
    scroll() {
      const s = brand(eventData, this);
      if (!s.intercepted || s.dispatching || !s.committed)
        throw platformDOMException('The navigation is not committed.', 'InvalidStateError');
      s.scrolled = true;
    }
  }
  for (const [key, value] of Object.entries({
    navigationType: 'push',
    canIntercept: false,
    userInitiated: false,
    hashChange: false,
    downloadRequest: null,
    formData: null,
    info: undefined,
    hasUAVisualTransition: false,
    sourceElement: null,
  }))
    getter(NavigateEvent.prototype, key, function () {
      const s = brand(eventData, this);
      return s[key] === undefined ? value : s[key];
    });
  for (const key of ['destination', 'signal'])
    getter(NavigateEvent.prototype, key, function () {
      return brand(eventData, this)[key];
    });
  Object.defineProperty(NavigateEvent.prototype, Symbol.toStringTag, {
    value: 'NavigateEvent',
    configurable: true,
  });
  markNative(NavigateEvent, 'NavigateEvent');
  Object.defineProperty(window, 'NavigateEvent', {
    value: NavigateEvent,
    writable: true,
    configurable: true,
  });
  const changeEvents = new WeakMap();
  class NavigationCurrentEntryChangeEvent extends Event {
    constructor(type, init) {
      if (arguments.length < 2 || !init?.from) throw new TypeError('from is required');
      super(type, init);
      const kind = init.navigationType ?? null;
      if (kind !== null && !['push', 'replace', 'reload', 'traverse'].includes(String(kind)))
        throw new TypeError('Invalid navigationType');
      changeEvents.set(this, { from: init.from, navigationType: kind });
    }
  }
  for (const key of ['from', 'navigationType'])
    getter(NavigationCurrentEntryChangeEvent.prototype, key, function () {
      return brand(changeEvents, this)[key];
    });
  Object.defineProperty(NavigationCurrentEntryChangeEvent.prototype, Symbol.toStringTag, {
    value: 'NavigationCurrentEntryChangeEvent',
    configurable: true,
  });
  markNative(NavigationCurrentEntryChangeEvent, 'NavigationCurrentEntryChangeEvent');
  Object.defineProperty(window, 'NavigationCurrentEntryChangeEvent', {
    value: NavigationCurrentEntryChangeEvent,
    writable: true,
    configurable: true,
  });
  const defer = () => {
    let resolve, reject;
    const promise = new platformPromise((a, b) => {
      resolve = a;
      reject = b;
    });
    promise.catch(() => {});
    return { promise, resolve, reject };
  };
  const error = () => platformDOMException('Navigation was aborted.', 'AbortError');
  const fail = (op, e) => {
    if (op.done) return;
    op.done = true;
    op.controller.abort(e);
    op.committed.reject(e);
    op.finished.reject(e);
    if (pending === op) {
      pending = null;
      transition = null;
    }
    const event = new ErrorEvent('navigateerror', { error: e, message: String(e.message || e) });
    dispatchNative(nav, event);
  };
  const begin = (url, kind, state, info, reason, destEntry) => {
    refresh();
    if (pending) fail(pending, error());
    const op = {
      from: current,
      kind,
      state,
      committed: defer(),
      finished: defer(),
      controller: new AbortController(),
      done: false,
    };
    pending = op;
    const target = new URL(url, location.href),
      old = new URL(location.href),
      same =
        reason === 'history' ||
        (target.origin === old.origin &&
          target.pathname === old.pathname &&
          target.search === old.search &&
          target.hash !== old.hash) ||
        !!destEntry?.sameDocument;
    const destination = Object.create(NavigationDestination.prototype);
    destinationSlots.set(destination, {
      url: target.href,
      key: destEntry?.key || '',
      id: destEntry?.id || '',
      index: destEntry?.index ?? -1,
      sameDocument: same,
      state,
    });
    op.event = new NavigateEvent('navigate', {
      cancelable: true,
      navigationType: kind,
      destination,
      signal: op.controller.signal,
      canIntercept: target.origin === old.origin,
      userInitiated: false,
      hashChange:
        reason !== 'history' &&
        target.origin === old.origin &&
        target.pathname === old.pathname &&
        target.search === old.search &&
        target.hash !== old.hash,
      info,
    });
    const s = eventData.get(op.event);
    s.dispatching = true;
    dispatchNative(nav, op.event);
    s.dispatching = false;
    if (op.event.defaultPrevented) fail(op, error());
    return op;
  };
  const committed = (op) => {
    if (!op || op.done) return;
    refresh();
    const e = new NavigationCurrentEntryChangeEvent('currententrychange', {
      navigationType: op.kind,
      from: op.from,
    });
    dispatchNative(nav, e);
    for (const old of entries.values())
      if (entrySlots.get(old).index === -1 && !entrySlots.get(old).disposed) {
        entrySlots.get(old).disposed = true;
        dispatchNative(old, new Event('dispose'));
      }
    eventData.get(op.event).committed = true;
    op.committed.resolve(current);
    const s = eventData.get(op.event);
    if (s.intercepted) {
      transition = Object.create(NavigationTransition.prototype);
      Object.defineProperties(transition, {
        navigationType: { value: op.kind, enumerable: true },
        from: { value: op.from, enumerable: true },
        finished: { value: op.finished.promise, enumerable: true },
      });
    }
    let handlers;
    try {
      handlers = s.handlers.map((f) => f());
    } catch (e) {
      fail(op, e);
      return;
    }
    platformPromiseAll(handlers).then(
      () => {
        if (op.done) return;
        op.done = true;
        if (pending === op) {
          pending = null;
          transition = null;
        }
        dispatchNative(nav, new Event('navigatesuccess'));
        op.finished.resolve(current);
      },
      (e) => fail(op, e),
    );
  };
  registerBootstrapCallback(
    'installNavigation',
    (phase, value) => {
      if (phase === 'start') {
        refresh();
        if (!current) return true;
        if (value.reason === 'traverse' && pending?.kind === 'traverse') return true;
        if (pending?.awaitingCrossDocument) {
          pending.awaitingCrossDocument = false;
          return true;
        }
        const op = begin(
          value.url,
          value.navigationType,
          value.key ? entrySlots.get(list.find((e) => e.key === value.key))?.state || '' : '',
          undefined,
          value.reason,
          value.key ? list.find((e) => e.key === value.key) : undefined,
        );
        if (
          !op.done &&
          eventData.get(op.event).intercepted &&
          (value.reason === 'crossDocument' || value.reason === 'reload')
        ) {
          host.navigationCommit(value.url, value.navigationType !== 'push', '');
          committed(op);
          return false;
        }
        return !op.done;
      }
      if (phase === 'commit') {
        if (!pending) {
          refresh();
          return;
        }
        committed(pending);
      }
    },
    (value) => {
      try {
        return [true, encode(value)];
      } catch (e) {
        return [false, e.name, e.message];
      }
    },
    decode,
  );
  method(Navigation.prototype, 'updateCurrentEntry', function (options) {
    check(this);
    if (arguments.length < 1 || options?.state === undefined)
      throw new TypeError('state is required');
    refresh();
    if (!current) throw platformDOMException('No current entry', 'InvalidStateError');
    const stored = encode(options.state);
    host.navigationState(stored);
    refresh();
    const e = new NavigationCurrentEntryChangeEvent('currententrychange', {
      navigationType: null,
      from: current,
    });
    dispatchNative(nav, e);
  });
  const rejected = (e) => {
    const a = platformPromiseReject(e),
      b = platformPromiseReject(e);
    a.catch(() => {});
    b.catch(() => {});
    return { committed: a, finished: b };
  };
  const result = (op) => ({ committed: op.committed.promise, finished: op.finished.promise });
  method(Navigation.prototype, 'navigate', function (url, options = {}) {
    check(this);
    if (!arguments.length) throw new TypeError('1 argument required');
    let target, state;
    try {
      target = new URL(String(url), location.href);
      state = encode(options.state);
      if (
        options.history !== undefined &&
        !['auto', 'push', 'replace'].includes(String(options.history))
      )
        throw new TypeError('Invalid history');
    } catch (e) {
      return rejected(e);
    }
    refresh();
    if (!host.historyIsActive() || !current)
      return rejected(platformDOMException('Document is not active', 'InvalidStateError'));
    const kind =
        options.history === 'replace' ||
        ((options.history === undefined || options.history === 'auto') &&
          host.navigationDefaultReplace(target.href))
          ? 'replace'
          : 'push',
      op = begin(target.href, kind, state, options.info, 'navigate');
    if (op.done) return result(op);
    const s = eventData.get(op.event);
    if (s.intercepted || s.destination.sameDocument) {
      host.navigationCommit(target.href, kind === 'replace', state);
      committed(op);
    } else {
      op.awaitingCrossDocument = true;
      host.navigate(target.href, kind === 'replace', false);
    }
    return result(op);
  });
  const traverse = (key, options = {}) => {
    refresh();
    const to = list.find((e) => e.key === key);
    if (!to) return rejected(platformDOMException('Invalid key', 'InvalidStateError'));
    if (to === current)
      return {
        committed: platformPromiseResolve(current),
        finished: platformPromiseResolve(current),
      };
    const op = begin(to.url, 'traverse', entrySlots.get(to).state, options.info, 'traverse', to);
    if (!op.done) host.navigationTraverse(key);
    return result(op);
  };
  method(Navigation.prototype, 'traverseTo', function (key, options) {
    check(this);
    if (!arguments.length) throw new TypeError('1 argument required');
    return traverse(String(key), options);
  });
  method(Navigation.prototype, 'back', function (options) {
    check(this);
    refresh();
    return traverse(list[list.indexOf(current) - 1]?.key, options);
  });
  method(Navigation.prototype, 'forward', function (options) {
    check(this);
    refresh();
    return traverse(list[list.indexOf(current) + 1]?.key, options);
  });
  method(Navigation.prototype, 'reload', function (options = {}) {
    check(this);
    refresh();
    let state;
    try {
      state = options.state === undefined ? entrySlots.get(current)?.state : encode(options.state);
    } catch (e) {
      return rejected(e);
    }
    const op = begin(location.href, 'reload', state, options.info, 'reload');
    if (!op.done) {
      if (eventData.get(op.event).intercepted) {
        host.navigationState(state);
        committed(op);
      } else {
        op.awaitingCrossDocument = true;
        host.navigate(location.href, true, true);
      }
    }
    return result(op);
  });
}
