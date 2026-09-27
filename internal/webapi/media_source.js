// Container observations use one browser-owned source. These bindings cache
// wrappers and private brands, never another readyState or buffer membership.
if (typeof globalThis.MediaSource === 'function') {
  const previousMediaSource = globalThis.MediaSource;
  const sourceWrappers = new Map();
  const listWrappers = new Map();
  const bufferWrappers = new Map();
  const bufferBinding = (value) => requireRealmBinding(value, 'SourceBuffer');
  const bufferCall = (value, operation, args = []) =>
    callRealmBinding(value, bufferBinding(value), operation, args);
  const checkOperationResult = (result) => {
    if (result?.name === 'TypeError') throw new TypeError(result.message);
    if (result?.name) throw platformDOMException(result.message, result.name);
    return result;
  };
  const sourceBinding = (value) => requireRealmBinding(value, 'MediaSource');
  const sourceCall = (value, operation, args = []) =>
    callRealmBinding(value, sourceBinding(value), operation, args);
  const sourceOperation = (id, operation, ...args) => {
    const result = checkOperationResult(host.mediaSourceOperation(id, operation, ...args));
    return operation === 'addSourceBuffer' ? sourceBufferWrapper(id, result.value) : result?.value;
  };
  const sourceBufferWrapper = (sourceID, id) => {
    if (bufferWrappers.has(id)) return bufferWrappers.get(id);
    const buffer = new SourceBuffer(hostToken);
    registerRealmBinding(buffer, 'SourceBuffer', {
      identity: () => id,
      get: (name) => {
        const result = checkOperationResult(host.sourceBufferGet(sourceID, id, name));
        return name === 'buffered' ? new TimeRanges(hostToken) : result;
      },
      set: (name, value) => checkOperationResult(host.sourceBufferSet(sourceID, id, name, value)),
      append: (data) => checkOperationResult(host.sourceBufferAppend(sourceID, id, data)),
    });
    bufferWrappers.set(id, buffer);
    return buffer;
  };
  class TimeRanges {
    constructor(token, ranges = []) {
      if (token !== hostToken) throw new TypeError('Illegal constructor');
      registerRealmBinding(this, 'TimeRanges', {
        length: () => ranges.length,
        start: (index) => ranges[index][0],
        end: (index) => ranges[index][1],
      });
    }
    get length() {
      const binding = requireRealmBinding(this, 'TimeRanges');
      return callRealmBinding(this, binding, 'length', []);
    }
    start(index) {
      const binding = requireRealmBinding(this, 'TimeRanges');
      index = rangeIndex(
        'start',
        index,
        arguments.length,
        callRealmBinding(this, binding, 'length', []),
      );
      return callRealmBinding(this, binding, 'start', [index]);
    }
    end(index) {
      const binding = requireRealmBinding(this, 'TimeRanges');
      index = rangeIndex(
        'end',
        index,
        arguments.length,
        callRealmBinding(this, binding, 'length', []),
      );
      return callRealmBinding(this, binding, 'end', [index]);
    }
  }
  const rangeIndex = (name, index, argumentCount, length) => {
    const prefix = "Failed to execute '" + name + "' on 'TimeRanges': ";
    if (!argumentCount) throw new TypeError(prefix + '1 argument required, but only 0 present.');
    index = Number(index) >>> 0;
    if (index < length) return index;
    throw platformDOMException(
      prefix +
        'The index provided (' +
        index +
        (index === length
          ? ') is greater than or equal to the maximum bound ('
          : ') is greater than the maximum bound (') +
        length +
        ').',
      'IndexSizeError',
    );
  };
  class SourceBuffer extends EventTarget {
    constructor(token) {
      super();
      if (token !== hostToken) throw new TypeError('Illegal constructor');
    }
    get mode() {
      return bufferCall(this, 'get', ['mode']);
    }
    set mode(value) {
      bufferBinding(this);
      value = bindingString(value);
      if (value === 'segments' || value === 'sequence') bufferCall(this, 'set', ['mode', value]);
    }
    get updating() {
      return bufferCall(this, 'get', ['updating']);
    }
    get timestampOffset() {
      return bufferCall(this, 'get', ['timestampOffset']);
    }
    set timestampOffset(value) {
      bufferBinding(this);
      bufferCall(this, 'set', ['timestampOffset', Number(value)]);
    }
    get appendWindowStart() {
      return bufferCall(this, 'get', ['appendWindowStart']);
    }
    set appendWindowStart(value) {
      bufferBinding(this);
      bufferCall(this, 'set', ['appendWindowStart', Number(value)]);
    }
    get appendWindowEnd() {
      return bufferCall(this, 'get', ['appendWindowEnd']);
    }
    set appendWindowEnd(value) {
      bufferBinding(this);
      bufferCall(this, 'set', ['appendWindowEnd', Number(value)]);
    }
    get buffered() {
      return bufferCall(this, 'get', ['buffered']);
    }
    appendBuffer(data) {
      bufferBinding(this);
      if (!arguments.length)
        throw new TypeError(
          "Failed to execute 'appendBuffer' on 'SourceBuffer': 1 argument required, but only 0 present.",
        );
      let length;
      try {
        if (ArrayBuffer.isView(data)) {
          try {
            length = Object.getOwnPropertyDescriptor(DataView.prototype, 'byteLength').get.call(
              data,
            );
          } catch {
            length = Object.getOwnPropertyDescriptor(
              Object.getPrototypeOf(Uint8Array.prototype),
              'byteLength',
            ).get.call(data);
          }
        } else {
          length = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, 'byteLength').get.call(
            data,
          );
        }
      } catch {
        throw new TypeError(
          "Failed to execute 'appendBuffer' on 'SourceBuffer': Overload resolution failed.",
        );
      }
      bufferCall(this, 'append', [data]);
    }
  }
  const makeList = (id, name) => {
    const key = id + ':' + name;
    if (listWrappers.has(key)) return listWrappers.get(key);
    const target = new SourceBufferList(hostToken);
    const members = () => host.mediaSourceGet(id, name);
    const proxy = new Proxy(target, {
      get(object, key, receiver) {
        if (typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key)) {
          const value = members()[Number(key)];
          return value === undefined ? undefined : sourceBufferWrapper(id, value);
        }
        return Reflect.get(object, key, receiver);
      },
      has(object, key) {
        return (
          (typeof key === 'string' &&
            /^(0|[1-9]\d*)$/.test(key) &&
            Number(key) < members().length) ||
          Reflect.has(object, key)
        );
      },
      ownKeys(object) {
        return members()
          .map((_value, index) => String(index))
          .concat(Reflect.ownKeys(object));
      },
      getOwnPropertyDescriptor(object, key) {
        if (
          typeof key === 'string' &&
          /^(0|[1-9]\d*)$/.test(key) &&
          Number(key) < members().length
        ) {
          return {
            value: sourceBufferWrapper(id, members()[Number(key)]),
            writable: false,
            enumerable: true,
            configurable: true,
          };
        }
        return Reflect.getOwnPropertyDescriptor(object, key);
      },
    });
    registerRealmBinding(proxy, 'SourceBufferList', { length: () => members().length });
    listWrappers.set(key, proxy);
    return proxy;
  };
  class SourceBufferList extends EventTarget {
    constructor(token) {
      super();
      if (token !== hostToken) throw new TypeError('Illegal constructor');
    }
    get length() {
      const binding = requireRealmBinding(this, 'SourceBufferList');
      return callRealmBinding(this, binding, 'length', []);
    }
    [Symbol.iterator]() {
      requireRealmBinding(this, 'SourceBufferList');
      return Array.prototype.values.call(this);
    }
  }
  class MediaSource extends EventTarget {
    constructor() {
      super();
      recordAPIAccess('MediaSource.constructor', true);
      const id = host.mediaSourceCreate();
      sourceWrappers.set(id, this);
      registerRealmBinding(this, 'MediaSource', {
        get: (name) => host.mediaSourceGet(id, name),
        list: (name) => makeList(id, name),
        operation: (operation, ...args) => sourceOperation(id, operation, ...args),
        objectURL: (creator) => host.mediaSourceObjectURL(id, creator),
      });
    }
    get readyState() {
      return sourceCall(this, 'get', ['readyState']);
    }
    get duration() {
      return sourceCall(this, 'get', ['duration']);
    }
    set duration(value) {
      sourceBinding(this);
      const duration = Number(value);
      if (Number.isNaN(duration))
        throw new TypeError(
          "Failed to set the 'duration' property on 'MediaSource': The duration is not a number.",
        );
      if (duration < 0)
        throw new TypeError(
          "Failed to set the 'duration' property on 'MediaSource': The duration provided (" +
            duration +
            ') is less than the minimum bound (0).',
        );
      sourceCall(this, 'operation', ['duration', duration]);
    }
    get sourceBuffers() {
      return sourceCall(this, 'list', ['sourceBuffers']);
    }
    get activeSourceBuffers() {
      return sourceCall(this, 'list', ['activeSourceBuffers']);
    }
    addSourceBuffer(type) {
      sourceBinding(this);
      if (!arguments.length)
        throw new TypeError(
          "Failed to execute 'addSourceBuffer' on 'MediaSource': 1 argument required, but only 0 present.",
        );
      const contentType = bindingString(type);
      if (!MediaSource.isTypeSupported(contentType)) {
        throw platformDOMException(
          "Failed to execute 'addSourceBuffer' on 'MediaSource': The type provided ('" +
            contentType +
            "') is unsupported.",
          'NotSupportedError',
        );
      }
      return sourceCall(this, 'operation', ['addSourceBuffer', contentType]);
    }
    removeSourceBuffer(buffer) {
      sourceBinding(this);
      const binding = bufferBinding(buffer);
      const id = callRealmBinding(buffer, binding, 'identity', []);
      sourceCall(this, 'operation', ['removeSourceBuffer', id]);
    }
    endOfStream(error) {
      sourceBinding(this);
      if (error !== undefined) {
        error = bindingString(error);
        if (!['network', 'decode'].includes(error))
          throw new TypeError(
            "Failed to execute 'endOfStream' on 'MediaSource': The provided value '" +
              error +
              "' is not a valid enum value of type EndOfStreamError.",
          );
      }
      sourceCall(this, 'operation', ['endOfStream', error]);
    }
    setLiveSeekableRange(start, end) {
      sourceBinding(this);
      if (arguments.length < 2) throw new TypeError('setLiveSeekableRange requires two arguments');
      sourceCall(this, 'operation', ['setLiveSeekableRange', Number(start), Number(end)]);
    }
    clearLiveSeekableRange() {
      sourceCall(this, 'operation', ['clearLiveSeekableRange']);
    }
  }
  for (const key of Reflect.ownKeys(previousMediaSource)) {
    if (!['length', 'name', 'prototype'].includes(key)) {
      Object.defineProperty(
        MediaSource,
        key,
        Object.getOwnPropertyDescriptor(previousMediaSource, key),
      );
    }
  }
  Object.defineProperty(MediaSource.prototype, Symbol.toStringTag, {
    value: 'MediaSource',
    configurable: true,
  });
  Object.defineProperty(SourceBufferList.prototype, Symbol.toStringTag, {
    value: 'SourceBufferList',
    configurable: true,
  });
  createMediaSourceObjectURL = (object) => {
    let binding;
    try {
      binding = sourceBinding(object);
    } catch {
      return undefined;
    }
    return callRealmBinding(object, binding, 'objectURL', [host.selfRealmID()]);
  };
  for (const constructor of [SourceBuffer, TimeRanges]) {
    Object.defineProperty(constructor.prototype, Symbol.toStringTag, {
      value: constructor.name,
      configurable: true,
    });
  }
  Object.assign(globalThis, { MediaSource, SourceBufferList, SourceBuffer, TimeRanges });
  createMediaTimeRanges = (ranges) => new TimeRanges(hostToken, ranges);
  registerBootstrapCallback('installMediaSourceNotifier', (id, target, name) => {
    const object =
      target === 'source'
        ? sourceWrappers.get(id)
        : bufferWrappers.get(target) || listWrappers.get(id + ':' + target);
    if (object) dispatchTrusted(object, new Event(name));
  });
}
