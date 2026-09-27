// Shared Window/Worker projection of the agent-owned Performance timeline.
// The schema contains measured interface shape only; mutable browser state and
// timeline/observer membership belong to the host, not these wrapper caches.
const perfWorker = host.performance('worker'),
  perfSlots = new WeakMap(),
  perfWrappers = new Map(),
  perfConstructors = {};
const perfString = (value) => {
  if (typeof value === 'symbol') throw new TypeError('Cannot convert a Symbol value to a string');
  return String(value);
};
const perfNumber = (value) => {
  const n = +value;
  if (!Number.isFinite(n)) throw new TypeError('The provided double value is non-finite.');
  return n;
};
const perfDict = (value) => {
  if (value == null) return {};
  if (typeof value !== 'object' && typeof value !== 'function')
    throw new TypeError('The provided value is not of type dictionary.');
  return value;
};
const perfRequired = (args, n) => {
  if (args.length < n) throw new TypeError(n + ' argument required');
};
const perfFail = (name, message) => {
  throw platformDOMException(message || name, name);
};
const perfBases = {},
  perfSchema = /* performance_interface_schema */;
const perfIsA = (actual, expected) => {
  while (actual) {
    if (actual === expected) return true;
    actual = perfBases[actual];
  }
  return false;
};
const perfLocal = (object, kind) => {
  const s = perfSlots.get(object);
  if (!s || !perfIsA(s.kind, kind)) throw new TypeError('Illegal invocation');
  return s;
};
const perfBound = (object, kind, operation, args = []) => {
  if (!perfWorker) {
    const binding = requireRealmBinding(object, kind);
    return callRealmBinding(object, binding, operation, args);
  }
  return perfLocal(object, kind).operations[operation](...args);
};
const perfRegister = (object, kind, operations) => {
  const slot = perfSlots.get(object) || { kind };
  slot.operations = operations;
  perfSlots.set(object, slot);
  if (!perfWorker) registerRealmBinding(object, kind, operations);
};
const perfData = (object, kind) => {
  const slot = perfLocal(object, kind);
  return slot.data || (slot.owner ? slot.owner.read() : host.performance('entry', slot.id));
};
const perfBaseJSONFields = ['name', 'entryType', 'startTime', 'duration', 'navigationId'];
const perfResourceJSONFields = [
  'initiatorType',
  'deliveryType',
  'nextHopProtocol',
  'renderBlockingStatus',
  'contentType',
  'contentEncoding',
  'workerStart',
  'workerRouterEvaluationStart',
  'workerCacheLookupStart',
  'workerMatchedSourceType',
  'workerFinalSourceType',
  'redirectStart',
  'redirectEnd',
  'fetchStart',
  'domainLookupStart',
  'domainLookupEnd',
  'connectStart',
  'secureConnectionStart',
  'connectEnd',
  'requestStart',
  'responseStart',
  'firstInterimResponseStart',
  'finalResponseHeadersStart',
  'responseEnd',
  'transferSize',
  'encodedBodySize',
  'decodedBodySize',
  'responseStatus',
  'serverTiming',
];
const perfNavigationJSONFields = [
  'unloadEventStart',
  'unloadEventEnd',
  'domInteractive',
  'domContentLoadedEventStart',
  'domContentLoadedEventEnd',
  'domComplete',
  'loadEventStart',
  'loadEventEnd',
  'type',
  'redirectCount',
  'activationStart',
  'criticalCHRestart',
  'notRestoredReasons',
  'confidence',
];
const perfLegacyFields = [
  'navigationStart',
  'unloadEventStart',
  'unloadEventEnd',
  'redirectStart',
  'redirectEnd',
  'fetchStart',
  'domainLookupStart',
  'domainLookupEnd',
  'connectStart',
  'connectEnd',
  'secureConnectionStart',
  'requestStart',
  'responseStart',
  'responseEnd',
  'domLoading',
  'domInteractive',
  'domContentLoadedEventStart',
  'domContentLoadedEventEnd',
  'domComplete',
  'loadEventStart',
  'loadEventEnd',
];
const perfLegacyJSONFields = [
  'connectStart',
  'secureConnectionStart',
  'unloadEventEnd',
  'domainLookupStart',
  'domainLookupEnd',
  'responseStart',
  'connectEnd',
  'responseEnd',
  'requestStart',
  'domLoading',
  'redirectStart',
  'loadEventEnd',
  'domComplete',
  'navigationStart',
  'loadEventStart',
  'domContentLoadedEventEnd',
  'unloadEventStart',
  'redirectEnd',
  'domInteractive',
  'fetchStart',
  'domContentLoadedEventStart',
];
const perfStrings = new Set([
  'name',
  'entryType',
  'initiatorType',
  'deliveryType',
  'nextHopProtocol',
  'contentType',
  'contentEncoding',
  'workerMatchedSourceType',
  'workerFinalSourceType',
  'description',
  'containerType',
  'containerSrc',
  'containerId',
  'containerName',
]);
const perfTypeClass = {
  mark: 'PerformanceMark',
  measure: 'PerformanceMeasure',
  resource: 'PerformanceResourceTiming',
  navigation: 'PerformanceNavigationTiming',
  'visibility-state': 'VisibilityStateEntry',
  longtask: 'PerformanceLongTaskTiming',
  taskattribution: 'TaskAttributionTiming',
  event: 'PerformanceEventTiming',
  'first-input': 'PerformanceEventTiming',
  paint: 'PerformancePaintTiming',
};
const perfDefault = (field, data) =>
  field in data
    ? data[field]
    : perfStrings.has(field)
      ? ''
      : field === 'renderBlockingStatus'
        ? 'non-blocking'
        : field === 'notRestoredReasons' || field === 'target'
          ? null
          : 0;
function perfRead(object, kind, field) {
  const s = perfLocal(object, kind),
    data = perfData(object, kind);
  if (field === 'detail') return s.detail;
  if (field === 'serverTiming') {
    if (!s.serverTiming)
      s.serverTiming = Array.from(data.serverTiming || [], (data) =>
        perfObject('PerformanceServerTiming', data),
      );
    return Object.freeze(s.serverTiming.slice());
  }
  if (field === 'attribution') {
    if (!s.attribution)
      s.attribution = Object.freeze(
        Array.from(data.attribution || [], (data) => perfObject('TaskAttributionTiming', data)),
      );
    return s.attribution;
  }
  if (field === 'confidence')
    return data.confidence ? perfObject('PerformanceTimingConfidence', data.confidence) : null;
  if (field === 'target') {
    const target = s.owner ? s.owner.target(data.targetNode) : wrap(data.targetNode);
    return target?.isConnected ? target : null;
  }
  return perfDefault(field, data);
}
function perfJSON(object, kind) {
  perfLocal(object, kind);
  let fields = perfBaseJSONFields;
  if (perfIsA(kind, 'PerformanceResourceTiming')) fields = fields.concat(perfResourceJSONFields);
  if (kind === 'PerformanceNavigationTiming') fields = fields.concat(perfNavigationJSONFields);
  if (kind === 'PerformanceServerTiming') fields = ['name', 'duration', 'description'];
  if (kind === 'PerformanceTimingConfidence') fields = ['randomizedTriggerRate', 'value'];
  if (kind === 'PerformanceLongTaskTiming') fields = fields.concat('attribution');
  if (kind === 'TaskAttributionTiming')
    fields = fields.concat(['containerType', 'containerSrc', 'containerId', 'containerName']);
  if (kind === 'PerformanceEventTiming')
    fields = fields.concat(['interactionId', 'processingStart', 'processingEnd', 'cancelable']);
  const result = {};
  for (const field of fields)
    Object.defineProperty(result, field, {
      value: perfRead(object, kind, field),
      enumerable: true,
      writable: true,
      configurable: true,
    });
  return result;
}
function perfObject(kind, data, owner) {
  const object = Object.create(perfConstructors[kind].prototype),
    id = data._id;
  perfSlots.set(object, {
    kind,
    id,
    owner,
    data: kind === 'PerformanceNavigationTiming' ? null : data,
  });
  if (kind === 'PerformanceMark' || kind === 'PerformanceMeasure')
    perfSlots.get(object).detail = owner ? owner.detail : host.performance('detail', id);
  if (!perfWorker)
    registerRealmBinding(object, perfIsA(kind, 'PerformanceEntry') ? 'PerformanceEntry' : kind, {
      get: (requested, field) => perfRead(object, requested, field),
      json: (requested) => perfJSON(object, requested),
    });
  return object;
}
function makePerformanceEntry(data, creationRealm) {
  if (typeof creationRealm === 'number') creationRealm = undefined; // Array.map index is not a realm.
  let object = perfWrappers.get(data._id);
  if (!object) {
    const kind = perfTypeClass[data.entryType] || 'PerformanceEntry';
    object = creationRealm
      ? perfBound(creationRealm.performance, 'Performance', 'projectEntry', [
          kind,
          data,
          {
            read: () => host.performance('entry', data._id),
            target: (id) => wrap(id),
            detail: host.performance('detail', data._id),
          },
        ])
      : perfObject(kind, data);
    perfWrappers.set(data._id, object);
  }
  return object;
}
const perfPrune = () => {
  for (const id of host.performance('prune') || []) perfWrappers.delete(id);
};
const perfEntryGet = (object, kind, field) => {
  if (perfSlots.has(object)) return perfRead(object, kind, field);
  if (!perfWorker)
    return perfBound(object, perfIsA(kind, 'PerformanceEntry') ? 'PerformanceEntry' : kind, 'get', [
      kind,
      field,
    ]);
  throw new TypeError('Illegal invocation');
};
const perfEntryJSON = (object, kind) => {
  if (perfSlots.has(object)) return perfJSON(object, kind);
  if (!perfWorker)
    return perfBound(
      object,
      perfIsA(kind, 'PerformanceEntry') ? 'PerformanceEntry' : kind,
      'json',
      [kind],
    );
  throw new TypeError('Illegal invocation');
};
function perfClone(value) {
  if (value === undefined || value === null) return null;
  if (!perfWorker) return cloneHistoryState(value);
  if (host.performanceClone) {
    const reply = host.performanceClone(value, (v) => v === globalThis || perfUncloneable(v));
    if (!reply[0]) perfFail('DataCloneError', reply[1]);
    return reply[1];
  }
  const seen = new Map(),
    copy = (v) => {
      if (typeof v === 'function' || typeof v === 'symbol') perfFail('DataCloneError');
      if (v === null || typeof v !== 'object') return v;
      if (seen.has(v)) return seen.get(v);
      if (v === globalThis || perfUncloneable(v)) perfFail('DataCloneError');
      let out = Array.isArray(v)
        ? []
        : v instanceof Map
          ? new Map()
          : v instanceof Set
            ? new Set()
            : v instanceof Date
              ? new Date(v.getTime())
              : v instanceof ArrayBuffer
                ? v.slice(0)
                : {};
      seen.set(v, out);
      if (v instanceof Map) for (const [k, x] of v) out.set(copy(k), copy(x));
      else if (v instanceof Set) for (const x of v) out.add(copy(x));
      else
        for (const k of Object.keys(v))
          Object.defineProperty(out, k, {
            value: copy(v[k]),
            enumerable: true,
            configurable: true,
            writable: true,
          });
      return out;
    };
  return copy(value);
}
function perfMarkOptions(value) {
  const o = perfDict(value),
    detail = o.detail,
    start = o.startTime;
  return { detail, startTime: start === undefined ? undefined : perfNumber(start) };
}
function perfCreateMark(name, options, buffer) {
  if (!perfWorker && perfLegacyFields.includes(name))
    perfFail('SyntaxError', "'" + name + "' is part of the PerformanceTiming interface");
  const start = options.startTime === undefined ? host.performanceNow() : options.startTime;
  if (start < 0) throw new TypeError('startTime cannot be negative');
  const mark = makePerformanceEntry(
    host.performance('add', name, 'mark', start, 0, perfClone(options.detail), buffer),
  );
  if (!buffer) perfPrune();
  return mark;
}
const perfTimeValue = (value) =>
  typeof value === 'number' ? perfNumber(value) : perfString(value);
function perfMeasureOptions(value) {
  if (value === undefined) return {};
  if (value === null || typeof value === 'object' || typeof value === 'function') {
    const o = perfDict(value),
      detail = o.detail,
      rawDuration = o.duration,
      duration = rawDuration === undefined ? undefined : perfNumber(rawDuration),
      rawEnd = o.end,
      end = rawEnd === undefined ? undefined : perfTimeValue(rawEnd),
      rawStart = o.start,
      start = rawStart === undefined ? undefined : perfTimeValue(rawStart);
    return { detail, duration, end, start, dictionary: true };
  }
  return { start: perfString(value) };
}
function perfResolveTime(value) {
  if (typeof value === 'number') {
    if (value < 0) throw new TypeError('Timestamp cannot be negative');
    return value;
  }
  const mark = host.performance('latest', value);
  if (mark !== null) return mark;
  if (!perfWorker && perfLegacyFields.includes(value)) {
    if (value === 'navigationStart') return 0;
    const nav = host.performance('navigation'),
      time = nav[value];
    if (!time && !perfCompletedZeroPhase(nav, value)) perfFail('InvalidAccessError');
    return time;
  }
  perfFail('SyntaxError', "The mark '" + value + "' does not exist.");
}
function perfCreateMeasure(name, o, third) {
  const count = [o.start, o.end, o.duration].filter((v) => v !== undefined).length;
  if (o.dictionary && (count > 0 || o.detail !== undefined) && third !== undefined)
    throw new TypeError('endMark cannot be supplied with options');
  if (
    count === 3 ||
    (o.duration !== undefined && count === 1) ||
    (o.detail !== undefined && o.start === undefined && o.end === undefined)
  )
    throw new TypeError('Invalid start/end/duration combination');
  let start, end;
  if (third !== undefined) end = perfResolveTime(third);
  else if (o.end !== undefined) end = perfResolveTime(o.end);
  else if (o.duration !== undefined) end = undefined;
  else end = host.performanceNow();
  if (o.start !== undefined) start = perfResolveTime(o.start);
  else if (o.duration !== undefined) start = end - o.duration;
  else start = 0;
  if (end === undefined) end = start + o.duration;
  return makePerformanceEntry(
    host.performance('add', name, 'measure', start, end - start, perfClone(o.detail), true),
  );
}
const perfLists = new WeakMap(),
  perfObservers = new WeakMap();
function perfList(entries) {
  const list = Object.create(PerformanceObserverEntryList.prototype);
  perfLists.set(list, entries);
  if (!perfWorker)
    registerRealmBinding(list, 'PerformanceObserverEntryList', { entries: () => entries.slice() });
  return list;
}
const perfListRequire = (list) => {
  const entries = perfLists.get(list);
  if (entries) return entries;
  if (!perfWorker) return Array.from(perfBound(list, 'PerformanceObserverEntryList', 'entries'));
  throw new TypeError('Illegal invocation');
};
const perfObserverRequire = (observer) => {
  const slot = perfObservers.get(observer);
  if (!slot) throw new TypeError('Illegal invocation');
  return slot.id;
};
const perfSupported = Object.freeze(
  perfWorker
    ? ['mark', 'measure', 'resource']
    : [
        'element',
        'event',
        'first-input',
        'interaction-contentful-paint',
        'largest-contentful-paint',
        'layout-shift',
        'long-animation-frame',
        'longtask',
        'mark',
        'measure',
        'navigation',
        'paint',
        'resource',
        'soft-navigation',
        'visibility-state',
      ],
);
function perfObserve(observer, args) {
  const id = perfObserverRequire(observer),
    o = perfDict(args[0]);
  const buffered = !!o.buffered,
    rawThreshold = o.durationThreshold,
    threshold = rawThreshold === undefined ? undefined : perfNumber(rawThreshold),
    rawTypes = o.entryTypes;
  let types;
  if (rawTypes !== undefined) {
    if (rawTypes == null || typeof rawTypes[Symbol.iterator] !== 'function')
      throw new TypeError('entryTypes is not iterable');
    types = Array.from(rawTypes, perfString);
  }
  const rawType = o.type,
    type = rawType === undefined ? undefined : perfString(rawType);
  if ((types !== undefined && type !== undefined) || (types === undefined && type === undefined))
    throw new TypeError('An observe() call must include either entryTypes or type arguments.');
  const mode = types === undefined ? 'single' : 'multiple';
  types = (types === undefined ? [type] : types).filter((type) => perfSupported.includes(type));
  const eventThreshold =
    threshold === undefined ? 104 : Math.max(16, Math.round(threshold / 8) * 8);
  const error = host.performance(
    'observe',
    id,
    mode,
    types,
    buffered,
    type,
    eventThreshold,
    perfObservers.get(observer).deliver,
  );
  if (error) perfFail(error);
}
const perfMethods = {
  Performance: {
    now(args) {
      return perfBound(this, 'Performance', 'now');
    },
    toJSON(args) {
      return perfBound(this, 'Performance', 'json');
    },
    getEntries(args) {
      return perfBound(this, 'Performance', 'entries');
    },
    getEntriesByType(args) {
      const binding = !perfWorker
        ? requireRealmBinding(this, 'Performance')
        : perfLocal(this, 'Performance');
      perfRequired(args, 1);
      const type = perfString(args[0]);
      return perfWorker
        ? binding.operations.entriesByType(type)
        : callRealmBinding(this, binding, 'entriesByType', [type]);
    },
    getEntriesByName(args) {
      const binding = !perfWorker
        ? requireRealmBinding(this, 'Performance')
        : perfLocal(this, 'Performance');
      perfRequired(args, 1);
      const name = perfString(args[0]),
        type = args[1] === undefined ? undefined : perfString(args[1]);
      return perfWorker
        ? binding.operations.entriesByName(name, type)
        : callRealmBinding(this, binding, 'entriesByName', [name, type]);
    },
    mark(args) {
      const binding = !perfWorker
        ? requireRealmBinding(this, 'Performance')
        : perfLocal(this, 'Performance');
      perfRequired(args, 1);
      const name = perfString(args[0]),
        o = perfMarkOptions(args[1]);
      return perfWorker
        ? binding.operations.mark(name, o)
        : callRealmBinding(this, binding, 'mark', [name, o]);
    },
    measure(args) {
      const binding = !perfWorker
        ? requireRealmBinding(this, 'Performance')
        : perfLocal(this, 'Performance');
      perfRequired(args, 1);
      const name = perfString(args[0]),
        o = perfMeasureOptions(args[1]),
        end = args[2] === undefined ? undefined : perfString(args[2]);
      return perfWorker
        ? binding.operations.measure(name, o, end)
        : callRealmBinding(this, binding, 'measure', [name, o, end]);
    },
    clearMarks(args) {
      perfBound(this, 'Performance', 'brand');
      return perfBound(this, 'Performance', 'clear', [
        'mark',
        args[0] === undefined ? undefined : perfString(args[0]),
      ]);
    },
    clearMeasures(args) {
      perfBound(this, 'Performance', 'brand');
      return perfBound(this, 'Performance', 'clear', [
        'measure',
        args[0] === undefined ? undefined : perfString(args[0]),
      ]);
    },
    clearResourceTimings(args) {
      return perfBound(this, 'Performance', 'clear', ['resource']);
    },
    setResourceTimingBufferSize(args) {
      perfBound(this, 'Performance', 'brand');
      perfRequired(args, 1);
      return perfBound(this, 'Performance', 'bufferSize', [+args[0] >>> 0]);
    },
  },
  PerformanceObserver: {
    observe(args) {
      return perfObserve(this, args);
    },
    disconnect(args) {
      host.performance('disconnect', perfObserverRequire(this));
      perfPrune();
    },
    takeRecords(args) {
      const entries = host
        .performance('records', perfObserverRequire(this))
        .map(makePerformanceEntry);
      perfPrune();
      return entries;
    },
  },
  PerformanceObserverEntryList: {
    getEntries(args) {
      return perfListRequire(this).slice();
    },
    getEntriesByType(args) {
      const entries = perfListRequire(this);
      perfRequired(args, 1);
      const type = perfString(args[0]);
      return entries.filter((e) => perfEntryGet(e, 'PerformanceEntry', 'entryType') === type);
    },
    getEntriesByName(args) {
      const entries = perfListRequire(this);
      perfRequired(args, 1);
      const name = perfString(args[0]),
        type = args[1] === undefined ? undefined : perfString(args[1]);
      return entries.filter(
        (e) =>
          perfEntryGet(e, 'PerformanceEntry', 'name') === name &&
          (type === undefined || perfEntryGet(e, 'PerformanceEntry', 'entryType') === type),
      );
    },
  },
};
for (const [name, schema] of Object.entries(perfSchema)) {
  if (perfWorker && !schema.worker) continue;
  perfBases[name] = schema.parent;
  const C = {
    [name]: function (...args) {
      if (!new.target) throw new TypeError("Please use the 'new' operator");
      if (name === 'PerformanceMark') {
        perfRequired(args, 1);
        const mark = perfCreateMark(perfString(args[0]), perfMarkOptions(args[1]), false);
        Object.setPrototypeOf(mark, new.target.prototype);
        return mark;
      }
      if (name === 'PerformanceObserver') {
        perfRequired(args, 1);
        if (typeof args[0] !== 'function')
          throw new TypeError('PerformanceObserverCallback must be callable');
        const callback = args[0],
          observer = this;
        const callbackReference = perfWorker ? null : referenceGet(callback),
          creationRealm = callbackReference ? remoteWindow(callbackReference.frame) : undefined;
        const deliver = function (records, dropped) {
          const entries = records.map((data) => makePerformanceEntry(data, creationRealm));
          perfPrune();
          try {
            callback.call(
              observer,
              perfList(entries),
              observer,
              dropped == null ? {} : { droppedEntriesCount: dropped },
            );
          } catch (error) {
            host.reportUnhandledException?.(error);
          }
        };
        const id = host.performance('observerCreate');
        perfObservers.set(this, { id, deliver });
        return;
      }
      throw new TypeError('Illegal constructor');
    },
  }[name];
  Object.defineProperty(C, 'length', { value: schema.length, configurable: true });
  C.prototype = Object.create(
    perfConstructors[schema.parent]?.prototype ||
      (schema.parent === 'EventTarget' ? EventTarget.prototype : Object.prototype),
  );
  if (schema.parent && schema.parent !== 'Object')
    Object.setPrototypeOf(
      C,
      perfConstructors[schema.parent] ||
        (schema.parent === 'EventTarget' ? EventTarget : Function.prototype),
    );
  Object.defineProperty(C, 'prototype', { writable: false });
  perfConstructors[name] = C;
  for (const member of perfWorker ? schema.workerMembers : schema.members) {
    const key = member.key;
    if (key === 'Symbol(Symbol.toStringTag)') {
      Object.defineProperty(C.prototype, Symbol.toStringTag, { value: name, configurable: true });
      continue;
    }
    if (key === 'Symbol(Symbol.iterator)') {
      Object.defineProperty(C.prototype, Symbol.iterator, {
        value: C.prototype.entries,
        writable: true,
        configurable: true,
      });
      continue;
    }
    if (key === 'constructor') {
      Object.defineProperty(C.prototype, key, { value: C, writable: true, configurable: true });
      continue;
    }
    if (member.kind === 'function') {
      const fn = {
        [key](...args) {
          if (perfMethods[name]?.[key]) return perfMethods[name][key].call(this, args);
          if (key === 'toJSON') return perfEntryJSON(this, name);
          throw new TypeError('Illegal invocation');
        },
      }[key];
      Object.defineProperty(fn, 'length', { value: member.length, configurable: true });
      Object.defineProperty(C.prototype, key, {
        value: fn,
        writable: true,
        enumerable: true,
        configurable: true,
      });
    } else if (member.get) {
      const get = {
        get value() {
          if (name === 'Performance') return perfBound(this, 'Performance', 'get', [key]);
          if (name === 'PerformanceTiming' || name === 'PerformanceNavigation')
            return perfBound(this, name, 'get', [key]);
          return perfEntryGet(this, name, key);
        },
      };
      const descriptor = {
        get: Object.getOwnPropertyDescriptor(get, 'value').get,
        enumerable: true,
        configurable: true,
      };
      if (member.set)
        descriptor.set = function (value) {
          return perfBound(this, 'Performance', 'set', [key, value]);
        };
      Object.defineProperty(C.prototype, key, descriptor);
    } else if (name === 'PerformanceNavigation') {
      const value = { TYPE_NAVIGATE: 0, TYPE_RELOAD: 1, TYPE_BACK_FORWARD: 2, TYPE_RESERVED: 255 }[
        key
      ];
      Object.defineProperty(C.prototype, key, { value, enumerable: true });
      Object.defineProperty(C, key, { value, enumerable: true });
    }
  }
  Object.defineProperty(globalThis, name, { value: C, writable: true, configurable: true });
}
const {
  Performance,
  PerformanceEntry,
  PerformanceMark,
  PerformanceMeasure,
  PerformanceResourceTiming,
  PerformanceNavigationTiming,
  PerformanceServerTiming,
  PerformanceObserver,
  PerformanceObserverEntryList,
  PerformanceTiming,
  PerformanceNavigation,
} = perfConstructors;
Object.defineProperty(PerformanceObserver, 'supportedEntryTypes', {
  get() {
    return perfSupported;
  },
  enumerable: true,
  configurable: true,
});
const perfMemorySlots = new WeakMap(),
  perfMemoryPrototype = {};
for (const key of ['totalJSHeapSize', 'usedJSHeapSize', 'jsHeapSizeLimit'])
  Object.defineProperty(perfMemoryPrototype, key, {
    get() {
      const s = perfMemorySlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s[key];
    },
    enumerable: true,
    configurable: true,
  });
Object.defineProperty(perfMemoryPrototype, Symbol.toStringTag, {
  value: 'MemoryInfo',
  configurable: true,
});
function perfMemory() {
  const object = Object.create(perfMemoryPrototype);
  perfMemorySlots.set(object, host.performanceMemory());
  return object;
}
const perfCompletedZeroPhase = (nav, field) =>
  nav.responseEnd > 0 &&
  [
    'fetchStart',
    'domainLookupStart',
    'domainLookupEnd',
    'connectStart',
    'connectEnd',
    'requestStart',
    'responseStart',
  ].includes(field);
function perfLegacy(kind) {
  const object = Object.create(perfConstructors[kind].prototype),
    get = (field) => {
      const nav = host.performance('navigation') || {},
        origin = Math.trunc(host.performanceTimeOrigin());
      if (kind === 'PerformanceNavigation')
        return field === 'type'
          ? { navigate: 0, reload: 1, back_forward: 2 }[nav.type] || 0
          : nav.redirectCount || 0;
      if (field === 'navigationStart') return origin;
      const value = field === 'domLoading' ? nav.responseEnd : nav[field];
      const began = perfCompletedZeroPhase(nav, field);
      return value || began ? Math.trunc(host.performanceTimeOrigin() + (value || 0)) : 0;
    };
  perfRegister(object, kind, {
    get,
    json: () => {
      const result = {};
      for (const field of kind === 'PerformanceTiming'
        ? perfLegacyJSONFields
        : ['type', 'redirectCount'])
        result[field] = get(field);
      return result;
    },
  });
  return object;
}
for (const kind of ['PerformanceTiming', 'PerformanceNavigation'])
  if (perfConstructors[kind])
    Object.defineProperty(perfConstructors[kind].prototype, 'toJSON', {
      value: function () {
        return perfBound(this, kind, 'json');
      },
      writable: true,
      enumerable: true,
      configurable: true,
    });
function installPerformanceObject(object) {
  const timing = perfWorker ? null : perfLegacy('PerformanceTiming'),
    navigation = perfWorker ? null : perfLegacy('PerformanceNavigation');
  let handler = null,
    handlerListener = null;
  const operations = {
    brand: () => {},
    projectEntry: perfObject,
    timeOrigin: () => host.performanceTimeOrigin(),
    now: () => host.performanceNow(),
    entries: () => host.performance('entries', '', '', false).map(makePerformanceEntry),
    entriesByType: (type) =>
      type === '' ? [] : host.performance('entries', type, '', false).map(makePerformanceEntry),
    entriesByName: (name, type) =>
      type === ''
        ? []
        : host.performance('entries', type || '', name, true).map(makePerformanceEntry),
    mark: (name, options) => perfCreateMark(name, options, true),
    measure: perfCreateMeasure,
    clear: (type, name) => {
      host.performance('clear', type, name || '', name !== undefined);
      perfPrune();
    },
    bufferSize: (size) => {
      host.performance('bufferSize', size);
    },
    json: () =>
      perfWorker
        ? { timeOrigin: host.performanceTimeOrigin() }
        : {
            timeOrigin: host.performanceTimeOrigin(),
            timing: perfBound(timing, 'PerformanceTiming', 'json'),
            navigation: perfBound(navigation, 'PerformanceNavigation', 'json'),
          },
    get: (key) =>
      key === 'timeOrigin'
        ? host.performanceTimeOrigin()
        : key === 'timing'
          ? timing
          : key === 'navigation'
            ? navigation
            : key === 'memory'
              ? perfMemory()
              : key === 'onresourcetimingbufferfull'
                ? perfWorker
                  ? handler
                  : eventHandlerRecord(object, 'resourcetimingbufferfull')?.value || null
                : key === 'interactionCount'
                  ? host.performance('interactionCount')
                  : key === 'eventCounts'
                    ? perfEventCounts()
                    : undefined,
    set: (key, value) => {
      if (key === 'onresourcetimingbufferfull') {
        if (!perfWorker) {
          setEventHandlerValue(object, 'resourcetimingbufferfull', value);
          return;
        }
        handler = typeof value === 'function' ? value : null;
        if (!handler && handlerListener) {
          object.removeEventListener('resourcetimingbufferfull', handlerListener);
          handlerListener = null;
        }
        if (handler && !handlerListener) {
          handlerListener = function (event) {
            if (handler?.call(this, event) === false) event.preventDefault();
          };
          object.addEventListener('resourcetimingbufferfull', handlerListener);
        }
      }
    },
  };
  perfRegister(object, 'Performance', operations);
  const full = () => {
    const event = new Event('resourcetimingbufferfull');
    if (perfWorker) dispatchWorkerEvent(object, event, true);
    else dispatchTrusted(object, event);
  };
  if (!perfWorker) registerBootstrapCallback('performanceInstallBuffer', full);
  else host.performanceInstallBuffer(full);
  return object;
}
let perfCounts;
function perfEventCounts() {
  if (perfCounts) return perfCounts;
  const keys = [
    'pointerdown',
    'touchend',
    'input',
    'keydown',
    'mouseleave',
    'mouseenter',
    'drop',
    'beforeinput',
    'pointerenter',
    'dragend',
    'pointercancel',
    'compositionupdate',
    'mousedown',
    'dragleave',
    'dragover',
    'mouseup',
    'pointerover',
    'lostpointercapture',
    'mouseover',
    'gotpointercapture',
    'dblclick',
    'keyup',
    'keypress',
    'pointerup',
    'compositionstart',
    'auxclick',
    'dragstart',
    'touchstart',
    'compositionend',
    'pointerout',
    'dragenter',
    'touchcancel',
    'click',
    'contextmenu',
    'mouseout',
    'pointerleave',
  ];
  const proto = perfConstructors.EventCounts.prototype;
  const check = (value) => {
    if (value !== perfCounts) throw new TypeError('Illegal invocation');
  };
  for (const [name, fn] of Object.entries({
    get(key) {
      check(this);
      perfRequired(arguments, 1);
      key = perfString(key);
      return keys.includes(key) ? host.performance('eventCount', key) : undefined;
    },
    has(key) {
      check(this);
      perfRequired(arguments, 1);
      return keys.includes(perfString(key));
    },
    keys() {
      check(this);
      return keys.values();
    },
    values() {
      check(this);
      return keys.map((k) => host.performance('eventCount', k)).values();
    },
    entries() {
      check(this);
      return keys.map((k) => [k, host.performance('eventCount', k)]).values();
    },
    forEach(callback, thisArg = undefined) {
      check(this);
      if (typeof callback !== 'function') throw new TypeError('Callback is not callable');
      for (const key of keys)
        callback.call(thisArg, host.performance('eventCount', key), key, this);
    },
  }))
    Object.defineProperty(proto, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  Object.defineProperty(proto, Symbol.iterator, {
    value: proto.entries,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(proto, 'size', {
    get() {
      check(this);
      return keys.length;
    },
    enumerable: true,
    configurable: true,
  });
  return (perfCounts = Object.create(proto));
}
if (!perfWorker)
  Object.defineProperty(globalThis, '__mimicNotifyPerformanceObservers', {
    value: () => {},
    configurable: true,
  });
if (!perfWorker) perfEventCounts();
const perfUncloneable = (value) =>
  perfSlots.has(value) ||
  perfObservers.has(value) ||
  perfLists.has(value) ||
  perfMemorySlots.has(value) ||
  (perfCounts !== undefined && value === perfCounts);
if (!perfWorker) historyCloneBrandRejectors.push(perfUncloneable);

function finalizePerformanceBindings() {
  if (
    !Object.prototype.hasOwnProperty.call(Performance.prototype, 'measureUserAgentSpecificMemory')
  )
    return;
  Object.defineProperty(Performance.prototype, 'measureUserAgentSpecificMemory', {
    value: async function measureUserAgentSpecificMemory() {
      perfBound(this, 'Performance', 'brand');
      host.semanticMissingAt?.('performance.js', 'Performance.measureUserAgentSpecificMemory');
      throw platformDOMException(
        'Agent-cluster memory attribution is not implemented',
        'NotSupportedError',
      );
    },
    writable: true,
    enumerable: true,
    configurable: true,
  });
}
