(() => {
  const apply = Reflect.apply,
    keys = Reflect.ownKeys,
    nativeDescriptor = Object.getOwnPropertyDescriptor;
  const proto = Object.getPrototypeOf,
    create = Object.create,
    define = Object.defineProperty,
    setPrototype = Object.setPrototypeOf;
  // Native descriptor records are ordinary objects. Normalize their prototype
  // before examining optional fields, which authors can shadow on Object.prototype.
  const nativeDescriptorLookup = (value, key) => {
    const result = nativeDescriptor(value, key);
    return result && setPrototype(result, null);
  };
  const isArray = Array.isArray,
    stringify = JSON.stringify,
    parse = JSON.parse;
  const numberString = Number.prototype.toString,
    functionString = Function.prototype.toString;
  const string = String,
    bigint = BigInt,
    is = Object.is;
  const mapSize = nativeDescriptorLookup(Map.prototype, 'size').get,
    setSize = nativeDescriptorLookup(Set.prototype, 'size').get;
  const dateString = Date.prototype.toString;
  const regexpSource = nativeDescriptorLookup(RegExp.prototype, 'source').get;
  const regexpFlags = [
    'hasIndices',
    'global',
    'ignoreCase',
    'multiline',
    'dotAll',
    'unicode',
    'unicodeSets',
    'sticky',
  ].map((name, index) => ({
    get: nativeDescriptorLookup(RegExp.prototype, name)?.get,
    flag: 'dgimsuvy'[index],
  }));
  const arrayBufferLength = nativeDescriptorLookup(ArrayBuffer.prototype, 'byteLength').get;
  const typedArrayLength = nativeDescriptorLookup(proto(Uint8Array.prototype), 'length').get;
  const typedArrayName = nativeDescriptorLookup(
    proto(Uint8Array.prototype),
    Symbol.toStringTag,
  ).get;
  const symbolString = Symbol.prototype.toString;
  const stringSlice = String.prototype.slice;
  const SetClass = Set,
    ErrorClass = Error,
    finite = Number.isFinite;
  const setHas = Set.prototype.has,
    setAdd = Set.prototype.add,
    indexOf = Array.prototype.indexOf,
    pop = Array.prototype.pop;
  const regexTest = RegExp.prototype.test;
  return (node, prefix, inspect) => {
    const descriptor = inspect(null, 'descriptorRuntime')
      ? (value, key) => {
          const result = inspect(value, 'descriptor', key);
          return result && setPrototype(result, null);
        }
      : nativeDescriptorLookup;
    const opaque = (value) => inspect(value, 'opaque');
    // Keep handles in one private table. Native iterator-result construction
    // can itself consult author Object.prototype setters in some engines.
    const objects = create(null);
    let sequence = 0;
    const get = (id) => {
      const entry = objects[id];
      if (!entry) throw new ErrorClass('Could not find object with given id');
      return entry;
    };
    const plain = () => create(null);
    const put = (o, k, v) => {
      const d = plain();
      d.value = v;
      d.enumerable = true;
      d.writable = true;
      d.configurable = true;
      return define(o, k, d);
    };
    const array = () => setPrototype([], null),
      append = (a, v) => put(a, string(a.length), v);
    const envelope = (key, value) => {
      const result = plain();
      result[key] = value;
      return stringify(result);
    };
    const scalar = (value) => {
      const type = typeof value,
        result = plain();
      result.type = type;
      if (value === null) {
        result.type = 'object';
        result.subtype = 'null';
        result.value = null;
        return result;
      }
      if (type === 'undefined') return result;
      if (type === 'string' || type === 'boolean') {
        result.value = value;
        return result;
      }
      if (type === 'number') {
        result.description = apply(numberString, value, []);
        if (is(value, -0)) result.description = '-0';
        if (!finite(value) || is(value, -0)) result.unserializableValue = result.description;
        else result.value = value;
        return result;
      }
      if (type === 'bigint') {
        result.unserializableValue = string(value) + 'n';
        result.description = result.unserializableValue;
        return result;
      }
      return null;
    };
    const clone = (value, chain, depth) => {
      if (value === null) return null;
      const type = typeof value;
      if (type === 'bigint' || type === 'symbol')
        throw new ErrorClass("Object couldn't be returned by value");
      if (type === 'undefined') return undefined;
      if (type === 'number') return finite(value) ? (is(value, -0) ? 0 : value) : null;
      if (type !== 'object' && type !== 'function') return value;
      if (opaque(value) || inspect(value, 'kind') === 'error-opaque')
        throw new ErrorClass(
          'Opaque objects cannot be returned by value without executing author code',
        );
      if (depth > 1000 || apply(indexOf, chain, [value]) !== -1)
        throw new ErrorClass('Object reference chain is too long');
      append(chain, value);
      const result = isArray(value) ? array() : plain();
      if (isArray(value)) {
        const length = descriptor(value, 'length').value;
        for (let i = 0; i < length; i++) {
          const d = descriptor(value, string(i));
          if (d && !('value' in d))
            throw new ErrorClass(
              'Accessor values cannot be returned by value without executing author code',
            );
          const next = clone(d ? d.value : undefined, chain, depth + 1);
          put(result, string(i), next === undefined ? null : next);
        }
      } else {
        const names = keys(value);
        for (let index = 0; index < names.length; index++) {
          const key = names[index];
          if (typeof key !== 'string') continue;
          const desc = descriptor(value, key);
          if (!desc || !desc.enumerable) continue;
          if (!('value' in desc))
            throw new ErrorClass(
              'Accessor values cannot be returned by value without executing author code',
            );
          const next = clone(desc.value, chain, depth + 1);
          if (next !== undefined) put(result, key, next);
        }
      }
      apply(pop, chain, []);
      return result;
    };
    const describe = (value, group, byValue) => {
      let result = scalar(value);
      if (result) return result;
      result = plain();
      result.type = typeof value;
      if (byValue) {
        result.value = clone(value, array(), 0);
        return result;
      }
      let className = 'Object',
        description = 'Object',
        subtype,
        kind;
      if (typeof value === 'symbol') {
        description = apply(symbolString, value, []);
      } else if (node(value)) {
        className = 'Node';
        subtype = 'node';
        description = inspect(value, 'node');
      } else if ((kind = inspect(value, 'kind')) === 'proxy' || kind === 'opaque') {
        subtype = 'proxy';
        description = 'Proxy';
      } else if (kind === 'function') {
        className = 'Function';
        description = apply(functionString, value, []);
      } else {
        let cursor = proto(value);
        for (
          let count = 0;
          cursor && !opaque(cursor) && count < 100;
          count++, cursor = proto(cursor)
        ) {
          const c = descriptor(cursor, 'constructor');
          if (c && typeof c.value === 'function' && !opaque(c.value)) {
            const n = descriptor(c.value, 'name');
            if (n && typeof n.value === 'string' && n.value) {
              className = n.value;
              break;
            }
          }
        }
        description = className;
        switch (kind) {
          case 'array':
            subtype = 'array';
            className = 'Array';
            description = 'Array(' + descriptor(value, 'length').value + ')';
            break;
          case 'promise':
          case 'weakmap':
          case 'weakset':
            subtype = kind;
            break;
          case 'map':
          case 'set':
            subtype = kind;
            className = kind === 'map' ? 'Map' : 'Set';
            description =
              className + '(' + apply(kind === 'map' ? mapSize : setSize, value, []) + ')';
            break;
          case 'date':
            subtype = 'date';
            className = 'Date';
            description = apply(dateString, value, []);
            break;
          case 'regexp': {
            const source = apply(regexpSource, value, []);
            let flags = '';
            for (let index = 0; index < regexpFlags.length; index++) {
              const flag = regexpFlags[index];
              if (flag.get && apply(flag.get, value, [])) flags += flag.flag;
            }
            subtype = 'regexp';
            className = 'RegExp';
            description = '/' + source + '/' + flags;
            break;
          }
          case 'arraybuffer':
            subtype = 'arraybuffer';
            className = 'ArrayBuffer';
            description = 'ArrayBuffer(' + apply(arrayBufferLength, value, []) + ')';
            break;
          case 'typedarray':
            subtype = 'typedarray';
            className = apply(typedArrayName, value, []);
            description = className + '(' + apply(typedArrayLength, value, []) + ')';
            break;
          case 'error':
          case 'error-opaque': {
            subtype = 'error';
            const message = descriptor(value, 'message');
            // Some engines materialize Error.stack even during descriptor lookup.
            const stack = kind === 'error' ? descriptor(value, 'stack') : null;
            description =
              stack && typeof stack.value === 'string'
                ? stack.value
                : className +
                  (message && typeof message.value === 'string' ? ': ' + message.value : '');
            break;
          }
        }
      }
      if (typeof value !== 'symbol') result.className = className;
      if (subtype) result.subtype = subtype;
      result.description = description;
      const id = prefix + '.' + ++sequence;
      put(objects, id, { value, group });
      result.objectId = id;
      return result;
    };
    const argument = (a) => {
      if (a.objectId !== undefined) return get(a.objectId).value;
      if (a.unserializableValue !== undefined) {
        const value = a.unserializableValue;
        if (value === 'NaN') return NaN;
        if (value === 'Infinity') return Infinity;
        if (value === '-Infinity') return -Infinity;
        if (value === '-0') return -0;
        if (apply(regexTest, /^-?\d+n$/, [value]))
          return bigint(apply(stringSlice, value, [0, -1]));
        throw new ErrorClass('Unserializable value: ' + value);
      }
      // Protocol metadata stays prototype-free, but an explicitly supplied JSON
      // value enters author code as ordinary objects/arrays of this realm.
      // The sanitized tree has no accessors or inherited toJSON hook; parsing
      // its captured-intrinsic serialization invokes no author code.
      return a.value === undefined ? undefined : parse(stringify(a.value));
    };
    return function (operation, encoded, value) {
      const p = parse(encoded, (_, value) =>
        value !== null && typeof value === 'object' ? setPrototype(value, null) : value,
      );
      if (operation === 'lookup') return get(p.objectId).value;
      if (operation === 'prepareCall') {
        const values = array(),
          supplied = p.arguments || [];
        append(values, p.objectId ? get(p.objectId).value : globalThis);
        for (let index = 0; index < supplied.length; index++)
          append(values, argument(supplied[index]));
        return values;
      }
      if (operation === 'release') {
        delete objects[p.objectId];
        return '{}';
      }
      if (operation === 'clear') {
        const names = keys(objects);
        for (let index = 0; index < names.length; index++) delete objects[names[index]];
        return '{}';
      }
      if (operation === 'releaseGroup') {
        const names = keys(objects);
        for (let index = 0; index < names.length; index++) {
          const id = names[index];
          if (get(id).group === p.objectGroup) delete objects[id];
        }
        return '{}';
      }
      if (operation === 'group') return envelope('group', get(p.objectId).group);
      if (operation === 'node') return envelope('nodeId', node(get(p.objectId).value));
      if (operation === 'resolve')
        return envelope('result', describe(node(p.nodeId, true), p.objectGroup || '', false));
      if (operation === 'resolveValue') return node(p.nodeId, true);
      if (operation === 'identity') return value;
      if (operation === 'hold')
        return envelope('result', describe(value, p.objectGroup || '', !!p.returnByValue));
      if (operation === 'console') {
        const args = array();
        for (let i = 0; i < value.length; i++) {
          const item = describe(value[i], 'console', false);
          // Remote descriptions must not materialize lazy properties or execute author hooks.
          append(args, item);
        }
        return envelope('args', args);
      }
      if (operation === 'properties') {
        const entry = get(p.objectId),
          value = entry.value,
          result = array(),
          seen = new SetClass();
        if (opaque(value)) throw new ErrorClass('Opaque object property inspection is unsupported');
        let cursor = value;
        while (cursor !== null) {
          if (opaque(cursor) || inspect(cursor, 'kind') === 'error-opaque')
            throw new ErrorClass('Opaque prototype property inspection is unsupported');
          const names = keys(cursor);
          for (let index = 0; index < names.length; index++) {
            const key = names[index];
            if (apply(setHas, seen, [key])) continue;
            apply(setAdd, seen, [key]);
            if (
              p.nonIndexedPropertiesOnly &&
              typeof key === 'string' &&
              apply(regexTest, /^(0|[1-9]\d*)$/, [key])
            )
              continue;
            const desc = descriptor(cursor, key);
            if (!desc) continue;
            if (p.accessorPropertiesOnly && 'value' in desc) continue;
            const out = plain();
            out.name = typeof key === 'symbol' ? apply(symbolString, key, []) : key;
            if (typeof key === 'symbol') out.symbol = describe(key, entry.group, false);
            out.configurable = !!desc.configurable;
            out.enumerable = !!desc.enumerable;
            out.isOwn = cursor === value;
            if ('value' in desc) {
              out.value = describe(desc.value, entry.group, false);
              out.writable = !!desc.writable;
            } else {
              out.get = describe(desc.get, entry.group, false);
              out.set = describe(desc.set, entry.group, false);
            }
            append(result, out);
          }
          if (p.ownProperties) break;
          cursor = proto(cursor);
        }
        const response = plain();
        response.result = result;
        const parent = proto(value);
        if (parent !== null) {
          response.internalProperties = array();
          const property = plain();
          property.name = '[[Prototype]]';
          property.value = describe(parent, entry.group, false);
          append(response.internalProperties, property);
        }
        return stringify(response);
      }
      throw new ErrorClass('Unknown debugger operation');
    };
  };
})();
