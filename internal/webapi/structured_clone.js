// One engine serializer is used by the public operation and message transports.
// Do not export live JS graphs through host.Value: that loses brands and aliases.
const clonePlatformCodecs = [];
function createStructuredCloneCodec(rejectHost, foreign) {
  const apply = Reflect.apply,
    Uint8 = Uint8Array;
  const platformStringify = JSON.stringify,
    platformParse = JSON.parse,
    platformSetPrototype = Object.setPrototypeOf;
  const platforms = clonePlatformCodecs;
  const platformEncode = (value) => {
    for (const codec of platforms) {
      const data = codec.encode(value);
      if (data !== undefined)
        return platformStringify(platformSetPrototype([codec.tag, data], null));
    }
  };
  const classify = (value) => platformEncode(value) ?? rejectHost(value);
  const readPlatform = (raw) => {
    const [tag, data] = platformParse(raw);
    for (const codec of platforms) if (codec.tag === tag) return codec.decode(data);
    fail('The platform object is unavailable in this realm.');
  };
  const native = typeof host.serializeClone === 'function';
  const fail = (message) => {
    throw platformDOMException(message || 'The value could not be cloned.', 'DataCloneError');
  };
  const encode = (value) => {
    if (foreign) value = foreign(value);
    if (!native) return 'js:' + encodeGraph(value);
    const result = host.serializeClone(value, classify);
    if (!result[0]) fail(result[1]);
    return 'v8:' + result[1];
  };
  const decode = (value) =>
    value.startsWith('v8:')
      ? host.deserializeClone(value.slice(3), readPlatform)
      : decodeGraph(value.slice(3));
  // Engines without a native codec use the same bounded wire graph for all
  // consumers. This fallback is not a Chrome equivalence claim: brand discovery
  // uses script-visible tags/getters. Native V8 owns the authoritative path.
  const tag = Function.call.bind(Object.prototype.toString);
  const encodeGraph = (root) => {
    const seen = new Map(),
      nodes = [];
    const visit = (value) => {
      if (value === undefined) return ['u'];
      if (typeof value === 'bigint') return ['b', String(value)];
      if (typeof value === 'number') return ['n', Object.is(value, -0) ? '-0' : String(value)];
      if (value === null || typeof value === 'string' || typeof value === 'boolean')
        return ['p', value];
      if (typeof value !== 'object' || host.cloneIsProxy?.(value)) fail();
      if (seen.has(value)) return ['r', seen.get(value)];
      const id = nodes.length;
      seen.set(value, id);
      nodes.push(null);
      let row,
        brand = tag(value),
        platform = classify(value);
      if (platform === true) fail();
      if (typeof platform === 'string') row = ['Platform', platform];
      else if (Array.isArray(value))
        row = ['Array', value.length, Object.keys(value).map((k) => [k, visit(value[k])])];
      else if (
        ['[object Number]', '[object String]', '[object Boolean]', '[object BigInt]'].includes(
          brand,
        )
      ) {
        const C = {
          '[object Number]': Number,
          '[object String]': String,
          '[object Boolean]': Boolean,
          '[object BigInt]': BigInt,
        }[brand];
        row = ['Box', visit(C.prototype.valueOf.call(value))];
      } else if (brand === '[object Date]')
        row = ['Date', visit(Date.prototype.getTime.call(value))];
      else if (brand === '[object RegExp]')
        row = [
          'RegExp',
          Object.getOwnPropertyDescriptor(RegExp.prototype, 'source').get.call(value),
          Object.getOwnPropertyDescriptor(RegExp.prototype, 'flags').get.call(value),
        ];
      else if (brand === '[object Map]')
        row = [
          'Map',
          Array.from(Map.prototype.entries.call(value), ([k, v]) => [visit(k), visit(v)]),
        ];
      else if (brand === '[object Set]')
        row = ['Set', Array.from(Set.prototype.values.call(value), visit)];
      else if (brand === '[object ArrayBuffer]') {
        try {
          row = ['ArrayBuffer', Array.from(new Uint8Array(value))];
        } catch {
          fail();
        }
      } else if (ArrayBuffer.isView(value))
        row = [
          'View',
          brand.slice(8, -1),
          visit(value.buffer),
          value.byteOffset,
          brand === '[object DataView]' ? value.byteLength : value.length,
        ];
      else if (value instanceof Error)
        row = [
          'Error',
          value.name,
          String(value.message),
          value.stack,
          Object.hasOwn(value, 'cause') ? visit(value.cause) : null,
        ];
      else if (brand === '[object Object]')
        row = ['Object', Object.keys(value).map((k) => [k, visit(value[k])])];
      else fail();
      nodes[id] = row;
      return ['r', id];
    };
    return JSON.stringify([visit(root), nodes]);
  };
  const decodeGraph = (raw) => {
    const [root, nodes] = JSON.parse(raw),
      seen = new Map();
    const read = (item) => {
      if (item[0] === 'u') return undefined;
      if (item[0] === 'p') return item[1];
      if (item[0] === 'b') return BigInt(item[1]);
      if (item[0] === 'n') return Number(item[1]);
      const id = item[1];
      if (seen.has(id)) return seen.get(id);
      const row = nodes[id];
      let value;
      switch (row[0]) {
        case 'Platform':
          value = readPlatform(row[1]);
          break;
        case 'Box':
          value = Object(read(row[1]));
          break;
        case 'Array':
          value = new Array(row[1]);
          break;
        case 'Object':
          value = {};
          break;
        case 'Date':
          value = new Date(read(row[1]));
          break;
        case 'RegExp':
          value = new RegExp(row[1], row[2]);
          break;
        case 'Map':
          value = new Map();
          break;
        case 'Set':
          value = new Set();
          break;
        case 'ArrayBuffer':
          value = new Uint8Array(row[1]).buffer;
          break;
        case 'View':
          value = new globalThis[row[1]](read(row[2]), row[3], row[4]);
          break;
        case 'Error': {
          const C =
            { Error, EvalError, RangeError, ReferenceError, SyntaxError, TypeError, URIError }[
              row[1]
            ] || Error;
          value = new C(row[2]);
          if (row[3] !== undefined) value.stack = row[3];
          break;
        }
        default:
          fail();
      }
      seen.set(id, value);
      if (row[0] === 'Array' || row[0] === 'Object')
        for (const [k, v] of row[row[0] === 'Array' ? 2 : 1])
          Object.defineProperty(value, k, {
            value: read(v),
            writable: true,
            enumerable: true,
            configurable: true,
          });
      if (row[0] === 'Map') for (const [k, v] of row[1]) value.set(read(k), read(v));
      if (row[0] === 'Set') for (const v of row[1]) value.add(read(v));
      if (row[0] === 'Error' && row[4])
        Object.defineProperty(value, 'cause', {
          value: read(row[4]),
          writable: true,
          configurable: true,
        });
      return value;
    };
    return read(root);
  };
  const transferBuffer = ArrayBuffer.prototype.transfer;
  const byteLength = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, 'byteLength').get;
  const clone = function structuredClone(value, options = undefined) {
    if (!arguments.length)
      throw new TypeError(
        "Failed to execute 'structuredClone': 1 argument required, but only 0 present.",
      );
    if (options != null && typeof options !== 'object' && typeof options !== 'function')
      throw new TypeError('The provided value is not an object.');
    const list = options == null ? undefined : options.transfer;
    const transfers = [],
      seen = new Set();
    if (list !== undefined) {
      if (list === null || (typeof list !== 'object' && typeof list !== 'function'))
        throw new TypeError('The provided value cannot be converted to a sequence.');
      const iterator = list[Symbol.iterator];
      if (typeof iterator !== 'function')
        throw new TypeError('The provided value cannot be converted to a sequence.');
      // WebIDL completes sequence<object> conversion before transfer validation.
      // Read @@iterator exactly once; a second read is observable to author code.
      for (const item of { [Symbol.iterator]: () => apply(iterator, list, []) }) {
        if (item === null || (typeof item !== 'object' && typeof item !== 'function'))
          throw new TypeError('Transfer list item is not an object.');
        transfers.push(item);
      }
      for (const item of transfers) {
        if (seen.has(item)) fail('Transfer list contains duplicate ArrayBuffer.');
        try {
          apply(byteLength, item, []);
          new Uint8(item);
        } catch {
          fail('Transfer list contains an invalid or detached ArrayBuffer.');
        }
        if (typeof transferBuffer !== 'function')
          fail('ArrayBuffer transfer is unavailable in this engine.');
        seen.add(item);
      }
    }

    const serialized = encode(value);
    // Serialization may run getters. Validate again before detaching any buffer.
    for (const item of transfers) {
      try {
        new Uint8(item);
      } catch {
        fail('An ArrayBuffer is detached.');
      }
    }
    const result = decode(serialized);
    for (const item of transfers) apply(transferBuffer, item, []);
    return result;
  };
  const stringify = JSON.stringify,
    ownKeys = Object.keys,
    descriptor = Object.getOwnPropertyDescriptor;
  const create = Object.create,
    setPrototype = Object.setPrototypeOf,
    define = Object.defineProperty,
    isArray = Array.isArray;
  const SetCtor = Set,
    setHas = Set.prototype.has,
    setAdd = Set.prototype.add,
    setDelete = Set.prototype.delete;
  const trace = (wire) => {
    // Inspect only the already-cloned value. Never rerun author getters/toJSON
    // merely to make a diagnostic, and never use this projection for delivery.
    try {
      const seen = new SetCtor(),
        copy = (value) => {
          if (value === null || typeof value === 'string' || typeof value === 'boolean')
            return value;
          if (typeof value === 'number' && Number.isFinite(value)) return value;
          if (typeof value !== 'object' || apply(setHas, seen, [value])) throw 0;
          apply(setAdd, seen, [value]);
          const out = isArray(value) ? [] : create(null);
          setPrototype(out, null);
          for (const key of ownKeys(value)) {
            const d = descriptor(value, key);
            if (!d || !('value' in d)) throw 0;
            define(out, key, {
              value: copy(d.value),
              enumerable: true,
              configurable: true,
              writable: true,
            });
          }
          apply(setDelete, seen, [value]);
          return out;
        };
      return stringify(copy(decode(wire)));
    } catch {
      return '';
    }
  };
  return { encode, decode, clone, native, trace, platformEncode, platformDecode: readPlatform };
}
