// Typed OM read projections use the same live computed style resolver as
// CSSStyleDeclaration. Values are snapshots; the map remains live.
(() => {
  if (!globalThis.StylePropertyMapReadOnly || !globalThis.CSSStyleValue) return;
  const values = new WeakMap(),
    maps = new WeakMap();
  const valueState = (receiver) => {
    const state = values.get(receiver);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const mapState = (receiver) => {
    const state = maps.get(receiver);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const unitGroups = {
    number: ['number', 1],
    percent: ['percent', 1],
    px: ['length', 1],
    in: ['length', 96],
    cm: ['length', 96 / 2.54],
    mm: ['length', 96 / 25.4],
    q: ['length', 96 / 101.6],
    pt: ['length', 96 / 72],
    pc: ['length', 16],
    deg: ['angle', 1],
    grad: ['angle', 0.9],
    rad: ['angle', 180 / Math.PI],
    turn: ['angle', 360],
    s: ['time', 1],
    ms: ['time', 0.001],
    Hz: ['frequency', 1],
    kHz: ['frequency', 1000],
    dpi: ['resolution', 1],
    dpcm: ['resolution', 2.54],
    dppx: ['resolution', 96],
  };
  const relativeUnits = new Set([
    'em',
    'ex',
    'ch',
    'rem',
    'lh',
    'rlh',
    'cap',
    'ic',
    'vw',
    'vh',
    'vi',
    'vb',
    'vmin',
    'vmax',
    'svw',
    'svh',
    'lvw',
    'lvh',
    'dvw',
    'dvh',
    'fr',
  ]);
  const unitName = (input) => {
    input = String(input).toLowerCase();
    if (input === 'hz') return 'Hz';
    if (input === 'khz') return 'kHz';
    if (!unitGroups[input] && !relativeUnits.has(input)) throw new TypeError('Invalid CSS unit');
    return input;
  };
  const makeUnit = (value, unit) => {
    value = Number(value);
    unit = unitName(unit);
    if (!Number.isFinite(value)) throw new TypeError('CSSUnitValue requires a finite number');
    const result = Object.create(CSSUnitValue.prototype);
    values.set(result, { kind: 'unit', value, unit });
    return result;
  };
  const NumericBase = globalThis.CSSNumericValue;
  const CSSUnitValue = class CSSUnitValue extends NumericBase {
    constructor(value, unit) {
      if (arguments.length < 2) throw new TypeError('Two arguments are required');
      return makeUnit(value, unit);
    }
    get value() {
      return valueState(this).value;
    }
    set value(value) {
      value = Number(value);
      if (!Number.isFinite(value)) throw new TypeError('CSSUnitValue requires a finite number');
      valueState(this).value = value;
    }
    get unit() {
      return valueState(this).unit;
    }
    to(unit) {
      const state = valueState(this);
      unit = unitName(unit);
      if (unit === state.unit) return makeUnit(state.value, unit);
      const from = unitGroups[state.unit],
        to = unitGroups[unit];
      if (!from || !to || from[0] !== to[0]) throw new TypeError('Incompatible CSS units');
      return makeUnit((state.value * from[1]) / to[1], unit);
    }
    toString() {
      const state = valueState(this);
      return (
        cssSerializeNumber(state.value) +
        (state.unit === 'number' ? '' : state.unit === 'percent' ? '%' : state.unit)
      );
    }
  };
  Object.defineProperty(CSSUnitValue.prototype, Symbol.toStringTag, {
    value: 'CSSUnitValue',
    configurable: true,
  });
  Object.defineProperty(globalThis, 'CSSUnitValue', {
    value: CSSUnitValue,
    writable: true,
    configurable: true,
  });
  const CSSKeywordValue = class CSSKeywordValue extends globalThis.CSSStyleValue {
    constructor(value) {
      if (!arguments.length) throw new TypeError('One argument is required');
      value = String(value);
      if (!value) throw new TypeError('A keyword cannot be empty');
      const result = Object.create(CSSKeywordValue.prototype);
      values.set(result, { kind: 'keyword', value });
      return result;
    }
    get value() {
      return valueState(this).value;
    }
    set value(value) {
      value = String(value);
      if (!value) throw new TypeError('A keyword cannot be empty');
      valueState(this).value = value;
    }
    toString() {
      return valueState(this).value;
    }
  };
  Object.defineProperty(CSSKeywordValue.prototype, Symbol.toStringTag, {
    value: 'CSSKeywordValue',
    configurable: true,
  });
  Object.defineProperty(globalThis, 'CSSKeywordValue', {
    value: CSSKeywordValue,
    writable: true,
    configurable: true,
  });
  const unparsed = (text) => {
    const object = Object.create(CSSUnparsedValue.prototype);
    values.set(object, { kind: 'unparsed', text });
    Object.defineProperties(object, {
      0: { value: text, writable: true, enumerable: true, configurable: true },
      length: { value: 1, configurable: true },
      toString: { value: () => String(object[0]), configurable: true },
      [Symbol.iterator]: {
        value: function* () {
          yield object[0];
        },
        configurable: true,
      },
    });
    return object;
  };
  const typed = (name, text) => {
    if (name.startsWith('--')) return unparsed(text);
    const scalar = /^([+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?)([a-z]+|%)?$/i.exec(text);
    if (scalar)
      return makeUnit(Number(scalar[1]), scalar[2] === '%' ? 'percent' : scalar[2] || 'number');
    if (/^[-_a-zA-Z][-_a-zA-Z0-9]*$/.test(text)) return new CSSKeywordValue(text);
    const object = Object.create(CSSStyleValue.prototype);
    values.set(object, { kind: 'serialized', text });
    Object.defineProperty(object, 'toString', { value: () => text, configurable: true });
    return object;
  };
  const propertyName = (name) => {
    name = String(name);
    if (!name.startsWith('--')) name = name.toLowerCase();
    if (!name.startsWith('--') && !cssInitialValues.has(name) && !cssComputedShorthands[name])
      throw new TypeError('Invalid CSS property name');
    return name;
  };
  const properties = (element) => {
    const names = new Set(cssComputedNames);
    for (const name of cssRegisteredProperties.names()) names.add(name);
    for (let node = element; elementSlot(node)?.type === 'element'; node = cssFontParent(node))
      for (const entry of computedCSSDeclarations(node))
        if (entry.name.startsWith('--')) names.add(entry.name);
    return Array.from(names).sort();
  };
  const getAll = (receiver, name) => {
    const element = mapState(receiver);
    name = propertyName(name);
    const text = cssComputedValue(element, name);
    return text === '' ? [] : [typed(name, text)];
  };
  const proto = StylePropertyMapReadOnly.prototype;
  Object.defineProperties(proto, {
    get: {
      value: function get(name) {
        return getAll(this, name)[0];
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    getAll: {
      value: function getAllValues(name) {
        return getAll(this, name);
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    has: {
      value: function has(name) {
        return getAll(this, name).length > 0;
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    size: {
      get: function () {
        return withStyleReadCache(
          () =>
            properties(mapState(this)).filter(
              (name) => cssComputedValue(mapState(this), name) !== '',
            ).length,
        );
      },
      configurable: true,
      enumerable: true,
    },
    entries: {
      value: function* entries() {
        for (const name of withStyleReadCache(() => properties(mapState(this)))) {
          const value = getAll(this, name);
          if (value.length) yield [name, value];
        }
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    keys: {
      value: function* keys() {
        for (const [name] of this.entries()) yield name;
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    values: {
      value: function* mapValues() {
        for (const [, value] of this.entries()) yield value;
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    forEach: {
      value: function forEach(callback, thisArg) {
        if (typeof callback !== 'function') throw new TypeError('Callback must be callable');
        for (const [name, value] of this.entries()) callback.call(thisArg, value, name, this);
      },
      writable: true,
      configurable: true,
      enumerable: true,
    },
    [Symbol.iterator]: {
      value: function entriesIterator() {
        return this.entries();
      },
      writable: true,
      configurable: true,
    },
  });
  Object.defineProperty(Element.prototype, 'computedStyleMap', {
    value: function computedStyleMap() {
      if (elementSlot(this)?.type !== 'element') throw new TypeError('Illegal invocation');
      const map = Object.create(proto);
      maps.set(map, this);
      return map;
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  if (globalThis.CSS)
    for (const unit of [...Object.keys(unitGroups), ...relativeUnits])
      Object.defineProperty(CSS, unit, {
        value: (value) => makeUnit(value, unit),
        writable: true,
        configurable: true,
        enumerable: true,
      });
})();
