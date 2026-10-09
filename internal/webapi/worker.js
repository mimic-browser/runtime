(function (host) {
  'use strict';
  /* shared_promise_intrinsics */
  let platformDOMException;
  let bufferSourceBytes;
  const webIDLNumber = (value) => +value;
  let cloneCodec;
  const listenerTargets = new WeakMap(),
    handlers = { message: null, messageerror: null, error: null },
    handlerRecords = {};
  const targetListeners = (target) => {
    let result = listenerTargets.get(target);
    if (!result) {
      result = new Map();
      listenerTargets.set(target, result);
    }
    return result;
  };
  class EventTarget {
    addEventListener(type, listener, options = {}) {
      if (
        typeof listener !== 'function' &&
        !(listener && typeof listener.handleEvent === 'function')
      )
        return;
      const listeners = targetListeners(this == null ? globalThis : this),
        key = String(type),
        capture = typeof options === 'boolean' ? options : !!options?.capture,
        values = listeners.get(key) || [];
      if (!values.some((x) => x.listener === listener && x.capture === capture && !x.removed))
        values.push({ listener, capture, once: !!options?.once, removed: false });
      listeners.set(key, values);
    }
    removeEventListener(type, listener, options = {}) {
      const listeners = targetListeners(this == null ? globalThis : this),
        key = String(type),
        capture = typeof options === 'boolean' ? options : !!options?.capture,
        values = listeners.get(key) || [];
      for (const record of values)
        if (record.listener === listener && record.capture === capture) record.removed = true;
      listeners.set(
        key,
        values.filter((record) => !record.removed),
      );
    }
    dispatchEvent(event) {
      return dispatchWorkerEvent(this == null ? globalThis : this, event, false);
    }
  }
  const eventSlots = new WeakMap(),
    messageEventSlots = new WeakMap(),
    errorEventSlots = new WeakMap();
  class Event {
    constructor(type, init = {}) {
      eventSlots.set(this, {
        type: String(type),
        trusted: false,
        bubbles: !!init.bubbles,
        cancelable: !!init.cancelable,
        defaultPrevented: false,
        target: null,
        currentTarget: null,
        phase: 0,
        immediate: false,
      });
      Object.defineProperty(this, 'isTrusted', {
        get: () => !!eventSlots.get(this).trusted,
        enumerable: true,
        configurable: false,
      });
    }
    get type() {
      return eventSlots.get(this).type;
    }
    get bubbles() {
      return eventSlots.get(this).bubbles;
    }
    get cancelable() {
      return eventSlots.get(this).cancelable;
    }
    get defaultPrevented() {
      return eventSlots.get(this).defaultPrevented;
    }
    get target() {
      return eventSlots.get(this).target;
    }
    get currentTarget() {
      return eventSlots.get(this).currentTarget;
    }
    get eventPhase() {
      return eventSlots.get(this).phase;
    }
    preventDefault() {
      const state = eventSlots.get(this);
      if (state.cancelable) state.defaultPrevented = true;
    }
    stopImmediatePropagation() {
      eventSlots.get(this).immediate = true;
    }
  }
  class ErrorEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      errorEventSlots.set(this, {
        message: String(init.message || ''),
        filename: String(init.filename || ''),
        lineno: Number(init.lineno || 0) >>> 0,
        colno: Number(init.colno || 0) >>> 0,
        error: init.error === undefined ? null : init.error,
      });
    }
    get message() {
      return errorEventSlots.get(this).message;
    }
    get filename() {
      return errorEventSlots.get(this).filename;
    }
    get lineno() {
      return errorEventSlots.get(this).lineno;
    }
    get colno() {
      return errorEventSlots.get(this).colno;
    }
    get error() {
      return errorEventSlots.get(this).error;
    }
  }
  Object.defineProperty(ErrorEvent.prototype, Symbol.toStringTag, {
    value: 'ErrorEvent',
    configurable: true,
  });
  function setHandler(type, value) {
    value = typeof value === 'function' ? value : null;
    handlers[type] = value;
    if (!value && handlerRecords[type]) {
      EventTarget.prototype.removeEventListener.call(globalThis, type, handlerRecords[type]);
      delete handlerRecords[type];
    }
    if (value && !handlerRecords[type]) {
      const listener = function (event) {
        const handler = handlers[type];
        if (!handler) return;
        if (type === 'error' && errorEventSlots.has(event)) {
          if (
            handler.call(
              globalThis,
              event.message,
              event.filename,
              event.lineno,
              event.colno,
              event.error,
            ) === true
          )
            event.preventDefault();
        } else handler.call(globalThis, event);
      };
      handlerRecords[type] = listener;
      EventTarget.prototype.addEventListener.call(globalThis, type, listener);
    }
  }
  function dispatchWorkerEvent(target, event, trusted) {
    const state = eventSlots.get(event);
    if (!state) throw new TypeError('Illegal invocation');
    state.trusted = trusted;
    state.target = target;
    state.currentTarget = target;
    state.phase = 2;
    try {
      for (const record of (targetListeners(target).get(event.type) || []).slice()) {
        if (record.removed) continue;
        if (state.immediate) break;
        if (record.once)
          EventTarget.prototype.removeEventListener.call(
            target,
            event.type,
            record.listener,
            record.capture,
          );
        try {
          if (typeof record.listener === 'function') record.listener.call(target, event);
          else record.listener.handleEvent(event);
        } catch (error) {
          reportWorkerException(error);
        }
      }
    } finally {
      state.currentTarget = null;
      state.phase = 0;
      state.immediate = false;
    }
    return !state.defaultPrevented;
  }
  let reportingException = false;
  function reportWorkerException(error) {
    let details = host.describeException?.(error);
    if (!details) {
      let message = 'Uncaught';
      try {
        message += ' ' + String(error);
      } catch {}
      details = { message, filename: '', lineno: 0, colno: 0 };
      host.semanticMissingAt(
        'worker.js:exception-location',
        'WorkerGlobalScope.error.sourceLocation',
      );
    }
    if (reportingException) {
      host.reportUnhandledException?.(details.message);
      return;
    }
    reportingException = true;
    try {
      const event = new ErrorEvent('error', { ...details, error, cancelable: true });
      errorEventSlots.get(event).error = error;
      if (dispatchWorkerEvent(globalThis, event, true))
        host.reportUnhandledException?.(details.message);
    } finally {
      reportingException = false;
    }
  }
  // await uses the engine's intrinsic Promise job queue. User replacements of
  // Promise.resolve/then/species cannot add calls or reorder this microtask.
  const enqueueMicrotask = async (callback) => {
    await 0;
    try {
      callback();
    } catch (error) {
      reportWorkerException(error);
    }
  };
  function queueMicrotask(callback) {
    if (this != null && this !== globalThis) throw new TypeError('Illegal invocation');
    if (arguments.length === 0)
      throw new TypeError(
        "Failed to execute 'queueMicrotask' on 'WorkerGlobalScope': 1 argument required, but only 0 present.",
      );
    if (typeof callback !== 'function')
      throw new TypeError(
        "Failed to execute 'queueMicrotask' on 'WorkerGlobalScope': parameter 1 is not of type 'Function'.",
      );
    enqueueMicrotask(callback);
  }
  class MessageEvent extends Event {
    constructor(type, init = {}) {
      super(type);
      messageEventSlots.set(this, {
        data: init.data,
        origin: init.origin || '',
        source: null,
        ports: [],
      });
    }
    get data() {
      return messageEventSlots.get(this).data;
    }
    get origin() {
      return messageEventSlots.get(this).origin;
    }
    get source() {
      return messageEventSlots.get(this).source;
    }
    get ports() {
      return messageEventSlots.get(this).ports.slice();
    }
  }
  class WorkerNavigator {
    constructor() {
      throw new TypeError('Illegal constructor');
    }
  }
  class WorkerLocation {
    constructor() {
      throw new TypeError('Illegal constructor');
    }
    toString() {
      return this.href;
    }
  }
  class Crypto {
    constructor() {
      throw new TypeError('Illegal constructor');
    }
    getRandomValues(view) {
      if (
        !ArrayBuffer.isView(view) ||
        view instanceof Float32Array ||
        view instanceof Float64Array ||
        view instanceof DataView
      )
        throw new TypeError(
          "Failed to execute 'getRandomValues' on 'Crypto': parameter 1 is not of type 'ArrayBufferView'.",
        );
      if (view.byteLength > 65536)
        throw platformDOMException(
          'The ArrayBufferView byte length exceeds 65536 bytes',
          'QuotaExceededError',
        );
      const bytes = host.randomBytes(view.byteLength),
        raw = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
      for (let i = 0; i < raw.length; i++) raw[i] = bytes[i];
      return view;
    }
    randomUUID() {
      return host.randomUUID();
    }
  }
  /* shared_webcrypto */
  /* shared_performance */
  const hostToken = host.token();
  const bindingString = (value) => {
    if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
    return String(value);
  };
  const realmBindings = new WeakMap(),
    bindingGet = WeakMap.prototype.get.bind(realmBindings);
  const referenceGet = () => undefined;
  const registerRealmBinding = (value, kind, operations) =>
    realmBindings.set(value, { kind, operations });
  const requireRealmBinding = (value, kind) => {
    const b = bindingGet(value);
    if (b?.kind !== kind) throw new TypeError('Illegal invocation');
    return b;
  };
  const callRealmBinding = (receiver, binding, operation, args) =>
    binding.operations[operation](...args);
  /* shared_trusted_types */
  const readableSlots = new WeakMap(),
    readerSlots = new WeakMap();
  class ReadableStreamDefaultController {
    constructor(stream) {
      this.stream = stream;
    }
    enqueue(value) {
      const state = readableSlots.get(this.stream);
      if (state.reads.length) state.reads.shift()({ value, done: false });
      else state.queue.push(value);
    }
    close() {
      const state = readableSlots.get(this.stream);
      state.closed = true;
      while (state.reads.length) state.reads.shift()({ value: undefined, done: true });
    }
    error(error) {
      const state = readableSlots.get(this.stream);
      state.error = error;
      while (state.rejects.length) state.rejects.shift()(error);
    }
  }
  class ReadableStreamDefaultReader {
    constructor(stream) {
      const state = readableSlots.get(stream);
      if (!state || state.locked) throw new TypeError('ReadableStream is locked');
      state.locked = true;
      readerSlots.set(this, stream);
    }
    read() {
      const stream = readerSlots.get(this),
        state = readableSlots.get(stream);
      if (state.queue.length)
        return platformPromiseResolve({ value: state.queue.shift(), done: false });
      if (state.error) return platformPromiseReject(state.error);
      if (state.closed) return platformPromiseResolve({ value: undefined, done: true });
      return new platformPromise((resolve, reject) => {
        state.reads.push(resolve);
        state.rejects.push(reject);
      });
    }
    releaseLock() {
      const stream = readerSlots.get(this);
      if (stream) {
        readableSlots.get(stream).locked = false;
        readerSlots.delete(this);
      }
    }
    cancel(reason) {
      const stream = readerSlots.get(this);
      return stream
        ? stream.cancel(reason)
        : platformPromiseReject(new TypeError('Reader has been released'));
    }
  }
  class ReadableStream {
    constructor(source = {}) {
      const state = {
        queue: [],
        reads: [],
        rejects: [],
        closed: false,
        error: null,
        locked: false,
        source,
      };
      readableSlots.set(this, state);
      const controller = new ReadableStreamDefaultController(this);
      try {
        platformPromiseResolve(
          typeof source.start === 'function' ? source.start(controller) : undefined,
        ).catch((error) => controller.error(error));
      } catch (error) {
        controller.error(error);
      }
    }
    get locked() {
      return readableSlots.get(this).locked;
    }
    getReader() {
      return new ReadableStreamDefaultReader(this);
    }
    cancel(reason) {
      const state = readableSlots.get(this);
      state.closed = true;
      state.queue.length = 0;
      return platformPromiseResolve(
        typeof state.source.cancel === 'function' ? state.source.cancel(reason) : undefined,
      );
    }
  }
  Object.assign(globalThis, {
    EventTarget,
    Event,
    MessageEvent,
    ErrorEvent,
    WorkerNavigator,
    WorkerLocation,
    Crypto,
    SubtleCrypto,
    CryptoKey,
    Performance,
    TrustedHTML,
    TrustedScript,
    TrustedScriptURL,
    TrustedTypePolicy,
    TrustedTypePolicyFactory,
    ReadableStream,
    ReadableStreamDefaultController,
    ReadableStreamDefaultReader,
  });
  const navigatorData = host.navigator(),
    workerSubtle = Object.create(SubtleCrypto.prototype);
  const workerNavigator = Object.create(WorkerNavigator.prototype),
    workerLocation = Object.create(WorkerLocation.prototype),
    workerCrypto = Object.create(Crypto.prototype),
    workerPerformance = installPerformanceObject(Object.create(Performance.prototype)),
    workerTrustedTypes = new TrustedTypePolicyFactory(host.token());
  for (const name of [
    'userAgent',
    'appVersion',
    'platform',
    'languages',
    'language',
    'hardwareConcurrency',
    'deviceMemory',
    'onLine',
  ])
    Object.defineProperty(WorkerNavigator.prototype, name, {
      get() {
        const value = name === 'onLine' ? host.navigator().onLine : navigatorData[name];
        return Array.isArray(value) ? value.slice() : value;
      },
      enumerable: true,
      configurable: true,
    });
  for (const name of [
    'href',
    'origin',
    'protocol',
    'host',
    'hostname',
    'port',
    'pathname',
    'search',
    'hash',
  ])
    Object.defineProperty(WorkerLocation.prototype, name, {
      get() {
        return host.location()[name];
      },
      enumerable: true,
      configurable: true,
    });
  for (const [prototype, tag] of [
    [EventTarget.prototype, 'EventTarget'],
    [Event.prototype, 'Event'],
    [MessageEvent.prototype, 'MessageEvent'],
    [WorkerNavigator.prototype, 'WorkerNavigator'],
    [WorkerLocation.prototype, 'WorkerLocation'],
    [Crypto.prototype, 'Crypto'],
    [SubtleCrypto.prototype, 'SubtleCrypto'],
    [Performance.prototype, 'Performance'],
    [TrustedHTML.prototype, 'TrustedHTML'],
    [TrustedScript.prototype, 'TrustedScript'],
    [TrustedScriptURL.prototype, 'TrustedScriptURL'],
    [TrustedTypePolicy.prototype, 'TrustedTypePolicy'],
    [TrustedTypePolicyFactory.prototype, 'TrustedTypePolicyFactory'],
    [ReadableStream.prototype, 'ReadableStream'],
    [ReadableStreamDefaultController.prototype, 'ReadableStreamDefaultController'],
    [ReadableStreamDefaultReader.prototype, 'ReadableStreamDefaultReader'],
  ])
    Object.defineProperty(prototype, Symbol.toStringTag, { value: tag, configurable: true });
  if (host.isSecureContext())
    Object.defineProperty(Crypto.prototype, 'subtle', {
      get() {
        return workerSubtle;
      },
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(globalThis, 'trustedTypes', {
    value: workerTrustedTypes,
    writable: false,
    enumerable: true,
    configurable: true,
  });
  if (!host.isSecureContext()) {
    delete Crypto.prototype.randomUUID;
  }
  const workerTimer = (fn, delay, args, repeat) => {
    fn = trustedTimerArgument(fn);
    delay = webIDLNumber(delay);
    const name = repeat ? 'setInterval' : 'setTimeout';
    const source =
      typeof fn === 'function'
        ? null
        : trustedConvert(
            fn,
            'TrustedScript',
            'WorkerGlobalScope ' + name,
            "Failed to execute '" + name + "' on 'WorkerGlobalScope': ",
          );
    return host.setTimer(source === null ? fn : source, delay, repeat, reportWorkerException, args);
  };
  const operations = {
    importScripts(...urls) {
      urls = urls.map((value) =>
        trustedTypeOf(value) === 'TrustedScriptURL' ? value : trustedString(value),
      );
      const values = urls.map((value) =>
        trustedConvert(
          value,
          'TrustedScriptURL',
          'WorkerGlobalScope importScripts',
          "Failed to execute 'importScripts' on 'WorkerGlobalScope': ",
        ),
      );
      const error = host.importScripts(values);
      if (error) throw platformDOMException(error.message, error.name);
    },
    queueMicrotask,
    addEventListener: (...args) => EventTarget.prototype.addEventListener.apply(globalThis, args),
    removeEventListener: (...args) =>
      EventTarget.prototype.removeEventListener.apply(globalThis, args),
    dispatchEvent: (...args) => EventTarget.prototype.dispatchEvent.apply(globalThis, args),
    postMessage: (data) => {
      const wire = cloneCodec.encode(data);
      host.postMessage(wire, cloneCodec.trace(wire));
    },
    close: () => host.close(),
    setTimeout: (fn, delay = 0, ...args) => workerTimer(fn, delay, args, false),
    setInterval: (fn, delay = 0, ...args) => workerTimer(fn, delay, args, true),
    clearTimeout: (id) => host.clearTimer(webIDLNumber(id)),
    clearInterval: (id) => host.clearTimer(webIDLNumber(id)),
  };
  Object.assign(globalThis, operations);
  globalThis.__deliver = (data) =>
    dispatchWorkerEvent(
      globalThis,
      new MessageEvent('message', { data: cloneCodec.decode(data) }),
      true,
    );
  globalThis.__applyWorkerExposure = (exposure) => {
    const expected = new Map(
      (exposure.properties || []).map((property) => [property.name, property]),
    );
    for (const name of Object.getOwnPropertyNames(globalThis)) {
      if (name.startsWith('__') || expected.has(name)) continue;
      const descriptor = Object.getOwnPropertyDescriptor(globalThis, name);
      if (descriptor && descriptor.configurable) delete globalThis[name];
    }
    for (const property of exposure.properties || []) {
      if (Object.prototype.hasOwnProperty.call(globalThis, property.name)) continue;
      let value;
      if (property.valueType === 'function') {
        const functionName = property.functionName || property.name;
        value = {
          [functionName]: function () {
            return host.semanticMissingAt(
              'worker.js:74',
              'DedicatedWorkerGlobalScope.' + property.name,
            );
          },
        }[functionName];
        if (property.functionLength !== null && property.functionLength !== undefined)
          Object.defineProperty(value, 'length', {
            value: property.functionLength,
            configurable: true,
          });
      } else if (property.valueType === 'boolean') value = false;
      else if (property.valueType === 'number') value = 0;
      else if (property.valueType === 'string') value = '';
      else value = {};
      Object.defineProperty(globalThis, property.name, {
        value,
        writable: property.writable !== false,
        enumerable: property.enumerable,
        configurable: property.configurable,
      });
    }
  };
  globalThis.__applyWorkerPrototypeExposure = (exposure) => {
    for (const [interfaceName, properties] of Object.entries(exposure.prototypes || {})) {
      const ctor = globalThis[interfaceName],
        prototype = ctor && ctor.prototype;
      if (!prototype) continue;
      const expected = new Set(properties.map((property) => property.name));
      for (const name of Object.getOwnPropertyNames(prototype)) {
        if (!expected.has(name)) {
          const descriptor = Object.getOwnPropertyDescriptor(prototype, name);
          if (descriptor && descriptor.configurable) delete prototype[name];
        }
      }
    }
  };
  globalThis.__finishWorkerSurface = () => {
    const hostToken = host.token(),
      markNative = () => {};
    const expose = (name, value) =>
      Object.defineProperty(globalThis, name, { value, writable: true, configurable: true });
    const dispatchTrusted = (target, event) => dispatchWorkerEvent(target, event, true);
    /* shared_worker_fetch */
    cloneCodec = createStructuredCloneCodec((value) => {
      if (blobSlots.has(value))
        throw platformDOMException(
          'Platform serialization of Blob and File is not supported.',
          'NotSupportedError',
        );
      return value === globalThis || eventSlots.has(value);
    });
    globalThis.structuredClone = cloneCodec.clone;
    globalThis.__mimicClonePlatforms = clonePlatformCodecs;

    globalThis.__mimicEvalSourceResolver = evalSourceResolver;
    const workerPrototype = globalThis.WorkerGlobalScope && globalThis.WorkerGlobalScope.prototype;
    const dedicatedPrototype =
      globalThis.DedicatedWorkerGlobalScope && globalThis.DedicatedWorkerGlobalScope.prototype;
    if (workerPrototype) {
      Object.defineProperty(workerPrototype, 'structuredClone', {
        value: cloneCodec.clone,
        writable: true,
        enumerable: true,
        configurable: true,
      });
      delete globalThis.structuredClone;
      Object.defineProperty(workerPrototype, 'fetch', {
        value: globalThis.fetch,
        writable: true,
        enumerable: true,
        configurable: true,
      });
      delete globalThis.fetch;
      Object.defineProperty(workerPrototype, Symbol.toStringTag, {
        value: 'WorkerGlobalScope',
        configurable: true,
      });
      for (const name of [
        'importScripts',
        'setTimeout',
        'setInterval',
        'clearTimeout',
        'clearInterval',
      ])
        Object.defineProperty(workerPrototype, name, {
          value: operations[name],
          writable: true,
          enumerable: true,
          configurable: true,
        });
      for (const name of ['onerror', 'onmessageerror'])
        Object.defineProperty(workerPrototype, name, {
          get() {
            return handlers[name.slice(2)];
          },
          set(value) {
            setHandler(name.slice(2), value);
          },
          enumerable: true,
          configurable: true,
        });
      Object.defineProperty(workerPrototype, 'self', {
        get() {
          return globalThis;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'navigator', {
        get() {
          return workerNavigator;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'location', {
        get() {
          return workerLocation;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'crypto', {
        get() {
          return workerCrypto;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'performance', {
        get() {
          return workerPerformance;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'trustedTypes', {
        get() {
          return workerTrustedTypes;
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'isSecureContext', {
        get() {
          return host.isSecureContext();
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(workerPrototype, 'queueMicrotask', {
        value: queueMicrotask,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    }
    if (dedicatedPrototype) {
      Object.defineProperty(dedicatedPrototype, Symbol.toStringTag, {
        value: 'DedicatedWorkerGlobalScope',
        configurable: true,
      });
      for (const name of ['postMessage', 'close'])
        Object.defineProperty(dedicatedPrototype, name, {
          value: operations[name],
          writable: true,
          enumerable: true,
          configurable: true,
        });
      Object.defineProperty(dedicatedPrototype, 'onmessage', {
        get() {
          return handlers.message;
        },
        set(value) {
          setHandler('message', value);
        },
        enumerable: true,
        configurable: true,
      });
      Object.setPrototypeOf(globalThis, dedicatedPrototype);
      Object.defineProperty(globalThis, Symbol.toStringTag, {
        value: 'DedicatedWorkerGlobalScope',
        configurable: true,
      });
    }
    for (const name of Object.keys(operations)) delete globalThis[name];
    // A top-level `onmessage = fn` assignment is handled by the ECMAScript
    // global environment, which does not reliably perform [[Set]] through a
    // host-installed prototype accessor in every embeddable engine. Keep the
    // DedicatedWorkerGlobalScope handler as an own global accessor, matching
    // Chrome's observable worker global and preserving ordinary script syntax.
    Object.defineProperty(globalThis, 'onmessage', {
      get() {
        return handlers.message;
      },
      set(value) {
        setHandler('message', value);
      },
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(globalThis, 'onmessageerror', {
      get() {
        return handlers.messageerror;
      },
      set(value) {
        setHandler('messageerror', value);
      },
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(globalThis, 'postMessage', {
      value: operations.postMessage,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(globalThis, 'close', {
      value: operations.close,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    if (!host.isSecureContext()) {
      delete Crypto.prototype.randomUUID;
      delete WorkerNavigator.prototype.deviceMemory;
    }
    delete globalThis.performance;
    delete globalThis.queueMicrotask;
    Object.defineProperty(globalThis, 'onerror', {
      get() {
        return handlers.error;
      },
      set(value) {
        setHandler('error', value);
      },
      enumerable: true,
      configurable: true,
    });
    delete globalThis.InternalError;
  };
})(__workerHost);
