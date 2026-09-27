// SVG value objects have private slots. Attached values read the canonical
// attribute on each access; they do not maintain a second copy of DOM state.
const svgBindings = new Set();
const svgSlots = new WeakMap(),
  svgSetAttribute = Element.prototype.setAttribute;
const svgFail = (operation, reason) => {
  host.semanticMissingAt('svg_types.js:4', 'SVG.' + operation, JSON.stringify({ reason }));
  throw platformDOMException(reason, 'NotSupportedError');
};
const svgSlot = (value, type) => {
  const s = svgSlots.get(value);
  if (!s || s.type !== type) throw new TypeError('Illegal invocation');
  return s;
};
const svgFloat = (value) => {
  const n = +value;
  if (!Number.isFinite(n) || !Number.isFinite(Math.fround(n)))
    throw new TypeError('The provided float value is non-finite.');
  return Math.fround(n);
};
const svgDouble = (value) => {
  const n = +value;
  if (!Number.isFinite(n)) throw new TypeError('The provided double value is non-finite.');
  return n;
};
const svgWrite = (s) => {
  if (s.readonly)
    throw platformDOMException('The object is read-only.', 'NoModificationAllowedError');
};
const svgAttr = (n, key, value) => Reflect.apply(svgSetAttribute, n, [key, value]);
const svgMethod = (type, name, fn) => {
  svgBindings.add(type + '.' + name);
  const p = globalThis[type]?.prototype;
  if (!p) return;
  const arity = Object.getOwnPropertyDescriptor(p, name)?.value?.length ?? fn.length;
  const f = {
    [name](...args) {
      if (args.length < arity) throw new TypeError('Not enough arguments');
      return fn.call(this, ...args);
    },
  }[name];
  Object.defineProperty(f, 'length', { value: arity, configurable: true });
  markNative(f, name);
  Object.defineProperty(p, name, {
    value: f,
    writable: true,
    configurable: true,
    enumerable: true,
  });
};
const svgProp = (type, name, get, set) => {
  svgBindings.add(type + '.' + name);
  const p = globalThis[type]?.prototype;
  if (!p) return;
  const getter = {
    get [name]() {
      return get.call(this);
    },
  };
  const g = Object.getOwnPropertyDescriptor(getter, name).get;
  markNative(g, 'get ' + name);
  const d = { get: g, enumerable: true, configurable: true };
  if (set) {
    d.set = function (value) {
      return set.call(this, value);
    };
    markNative(d.set, 'set ' + name);
  }
  Object.defineProperty(p, name, d);
};
const svgMake = (type, state = {}) => {
  const value = Object.create(globalThis[type].prototype);
  svgSlots.set(value, { type, ...state });
  return value;
};
const svgScalar = (type, state = {}) => svgMake(type, { value: 0, unit: 1, ...state });
const svgUnits = {
  SVGLength: ['', '', '%', 'em', 'ex', 'px', 'cm', 'mm', 'in', 'pt', 'pc'],
  SVGAngle: ['', '', 'deg', 'rad', 'grad'],
};
const svgReadScalar = (s) => {
  if (s.automatic && attr(s.node, 'textLength') == null) return { value: s.automatic(), unit: 0 };
  if (s.read) {
    const raw = String(s.read());
    const m = new RegExp('^\\s*(' + numberPattern + ')([a-z%]*)\\s*$', 'i').exec(raw),
      units = svgUnits[s.type];
    if (m) {
      const u = units ? units.indexOf(m[2].toLowerCase(), 1) : 1;
      return { value: Math.fround(Number(m[1])), unit: u < 0 ? 1 : u };
    }
    return { value: 0, unit: 1 };
  }
  return s;
};
const svgScale = (s, unit) => {
  if (unit === 0 && s.automatic) return 1;
  if (s.type === 'SVGAngle') return [0, 1, 1, 180 / Math.PI, 0.9][unit];
  if ([2, 3, 4].includes(unit)) {
    if (!s.node || !measurable(s.node))
      throw platformDOMException('Cannot resolve relative length', 'NotSupportedError');
    if (unit === 2) {
      if (tag(s.node).startsWith('fe')) {
        let owner = parent(s.node);
        if (tag(owner) !== 'filter') return 0;
        svgFail('filterLength', 'filter primitive percentage region requires filter evaluation');
      }
      const v = viewport(s.node);
      return (
        (s.axis === 'y' ? v[1] : s.axis === 'diagonal' ? Math.hypot(...v) / Math.SQRT2 : v[0]) / 100
      );
    }
    const size = cssComputedFontSize(s.node);
    if (size === null || unit === 4) svgFail('lengthUnits', 'font-dependent length is unsupported');
    return size;
  }
  return [0, 1, 0, 0, 0, 1, 96 / 2.54, 96 / 25.4, 96, 96 / 72, 16][unit];
};
const svgSetScalar = (s, value, unit) => {
  svgWrite(s);
  s.value = value;
  s.unit = unit;
  if (s.write) s.write(cssSerializeNumber(value) + (svgUnits[s.type]?.[unit] || ''));
  if (s.changed) s.changed();
};
for (const type of ['SVGNumber', 'SVGLength', 'SVGAngle']) {
  svgProp(
    type,
    'value',
    function () {
      const s = svgSlot(this, type),
        v = svgReadScalar(s);
      return type === 'SVGAngle' && v.unit === 3
        ? Math.fround(Math.fround(v.value * 180) / Math.fround(Math.PI))
        : Math.fround(v.value * (type === 'SVGNumber' ? 1 : svgScale(s, v.unit)));
    },
    function (value) {
      const s = svgSlot(this, type);
      svgWrite(s);
      const n = svgFloat(value),
        v = svgReadScalar(s);
      svgSetScalar(s, Math.fround(n / (type === 'SVGNumber' ? 1 : svgScale(s, v.unit))), v.unit);
    },
  );
  if (type === 'SVGNumber') continue;
  svgProp(type, 'unitType', function () {
    return svgReadScalar(svgSlot(this, type)).unit;
  });
  svgProp(
    type,
    'valueInSpecifiedUnits',
    function () {
      return svgReadScalar(svgSlot(this, type)).value;
    },
    function (v) {
      const s = svgSlot(this, type);
      svgWrite(s);
      svgSetScalar(s, svgFloat(v), svgReadScalar(s).unit);
    },
  );
  svgProp(
    type,
    'valueAsString',
    function () {
      const v = svgReadScalar(svgSlot(this, type));
      return cssSerializeNumber(v.value) + svgUnits[type][v.unit];
    },
    function (value) {
      const s = svgSlot(this, type);
      svgWrite(s);
      const raw = String(value),
        m = new RegExp('^\\s*(' + numberPattern + ')([a-z%]*)\\s*$', 'i').exec(raw);
      if (!m) throw platformDOMException('Invalid value', 'SyntaxError');
      const unit = svgUnits[type].indexOf(m[2].toLowerCase(), 1);
      if (unit < 1) throw platformDOMException('Invalid unit', 'SyntaxError');
      svgSetScalar(s, svgFloat(m[1]), unit);
    },
  );
  svgMethod(type, 'newValueSpecifiedUnits', function (unit, value) {
    const s = svgSlot(this, type);
    svgWrite(s);
    unit = +unit & 65535;
    const n = svgFloat(value);
    if (unit < 1 || unit >= svgUnits[type].length)
      throw platformDOMException('Invalid unit', 'NotSupportedError');
    svgSetScalar(s, n, unit);
  });
  svgMethod(type, 'convertToSpecifiedUnits', function (unit) {
    const s = svgSlot(this, type);
    svgWrite(s);
    unit = +unit & 65535;
    if (unit < 1 || unit >= svgUnits[type].length)
      throw platformDOMException('Invalid unit', 'NotSupportedError');
    const v = svgReadScalar(s),
      value = v.value * svgScale(s, v.unit);
    svgSetScalar(s, Math.fround(value / svgScale(s, unit)), unit);
  });
}
const svgMatrix = (values = ident, state = {}) =>
  svgMake('SVGMatrix', { values: values.slice(), ...state });
const svgMatrixValues = (value) => {
  const s = svgSlot(value, 'SVGMatrix');
  return s.read ? s.read() : s.values;
};
for (const [index, key] of ['a', 'b', 'c', 'd', 'e', 'f'].entries())
  svgProp(
    'SVGMatrix',
    key,
    function () {
      return svgMatrixValues(this)[index];
    },
    function (value) {
      const s = svgSlot(this, 'SVGMatrix');
      svgWrite(s);
      const m = svgMatrixValues(this).slice();
      m[index] = svgDouble(value);
      s.values = m;
      if (s.write) s.write(m);
      if (s.changed) s.changed();
    },
  );
const svgRotation = (a) => {
  a = (svgDouble(a) * Math.PI) / 180;
  return [Math.cos(a), Math.sin(a), -Math.sin(a), Math.cos(a), 0, 0];
};
svgMethod('SVGMatrix', 'multiply', function (other) {
  return svgMatrix(multiply(svgMatrixValues(this), svgMatrixValues(other)));
});
svgMethod('SVGMatrix', 'inverse', function () {
  const m = svgMatrixValues(this),
    d = m[0] * m[3] - m[1] * m[2];
  if (!d) throw platformDOMException('Matrix is not invertible', 'InvalidStateError');
  return svgMatrix([
    m[3] / d,
    -m[1] / d,
    -m[2] / d,
    m[0] / d,
    (m[2] * m[5] - m[3] * m[4]) / d,
    (m[1] * m[4] - m[0] * m[5]) / d,
  ]);
});
for (const [name, build] of Object.entries({
  translate: (x, y) => [1, 0, 0, 1, svgDouble(x), svgDouble(y)],
  scale: (x) => [svgDouble(x), 0, 0, svgDouble(x), 0, 0],
  scaleNonUniform: (x, y) => [svgDouble(x), 0, 0, svgDouble(y), 0, 0],
  rotate: svgRotation,
  rotateFromVector: (x, y) => {
    x = svgDouble(x);
    y = svgDouble(y);
    if (!x || !y) throw platformDOMException('Invalid vector', 'InvalidAccessError');
    return svgRotation((Math.atan2(y, x) * 180) / Math.PI);
  },
  flipX: () => [-1, 0, 0, 1, 0, 0],
  flipY: () => [1, 0, 0, -1, 0, 0],
  skewX: (a) => [1, 0, Math.tan((svgDouble(a) * Math.PI) / 180), 1, 0, 0],
  skewY: (a) => [1, Math.tan((svgDouble(a) * Math.PI) / 180), 0, 1, 0, 0],
}))
  svgMethod('SVGMatrix', name, function (...a) {
    const m = svgMatrixValues(this);
    return svgMatrix(multiply(m, build(...a)));
  });
const svgPoint = (x = 0, y = 0, state = {}) => svgMake('SVGPoint', { x, y, ...state });
for (const key of ['x', 'y'])
  svgProp(
    'SVGPoint',
    key,
    function () {
      const s = svgSlot(this, 'SVGPoint');
      return s.read ? s.read()[key] : s[key];
    },
    function (value) {
      const s = svgSlot(this, 'SVGPoint');
      svgWrite(s);
      s[key] = svgFloat(value);
      if (s.changed) s.changed();
    },
  );
svgMethod('SVGPoint', 'matrixTransform', function (m) {
  const p = svgSlot(this, 'SVGPoint'),
    v = point(svgMatrixValues(m), this.x, this.y);
  return svgPoint(Math.fround(v[0]), Math.fround(v[1]));
});
const svgRect = (box = [0, 0, 0, 0], state = {}) => {
  const r = new SVGRect(hostToken);
  Object.assign(
    svgRectSlots.get(r),
    {
      x: Math.fround(box[0]),
      y: Math.fround(box[1]),
      width: Math.fround(box[2] - box[0]),
      height: Math.fround(box[3] - box[1]),
    },
    state,
  );
  return r;
};
for (const key of ['x', 'y', 'width', 'height'])
  svgProp(
    'SVGRect',
    key,
    function () {
      const s = svgRectSlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      return s.read ? s.read()[key] : s[key];
    },
    function (value) {
      const s = svgRectSlots.get(this);
      if (!s) throw new TypeError('Illegal invocation');
      svgWrite(s);
      const v = svgFloat(value);
      s[key] = v;
      if (s.changed) s.changed(key, v);
    },
  );
const svgTransform = (state = {}) => {
  const t = svgMake('SVGTransform', { kind: 1, angle: 0, ...state }),
    s = svgSlots.get(t);
  s.matrix = svgMatrix(ident, {
    readonly: s.readonly,
    changed: () => {
      s.kind = 1;
      s.angle = 0;
      if (s.changed) s.changed();
    },
  });
  return t;
};
for (const [key, slot] of [
  ['type', 'kind'],
  ['angle', 'angle'],
  ['matrix', 'matrix'],
])
  svgProp('SVGTransform', key, function () {
    return svgSlot(this, 'SVGTransform')[slot];
  });
for (const [name, kind, build] of [
  ['setTranslate', 2, (x, y) => [1, 0, 0, 1, svgFloat(x), svgFloat(y)]],
  ['setScale', 3, (x, y) => [svgFloat(x), 0, 0, svgFloat(y), 0, 0]],
  [
    'setRotate',
    4,
    (a, x, y) => {
      const m = svgRotation(svgFloat(a));
      x = svgFloat(x);
      y = svgFloat(y);
      return multiply(multiply([1, 0, 0, 1, x, y], m), [1, 0, 0, 1, -x, -y]);
    },
  ],
  ['setSkewX', 5, (a) => [1, 0, Math.tan((svgFloat(a) * Math.PI) / 180), 1, 0, 0]],
  ['setSkewY', 6, (a) => [1, Math.tan((svgFloat(a) * Math.PI) / 180), 0, 1, 0, 0]],
  ['setMatrix', 1, (m) => svgMatrixValues(m).slice()],
])
  svgMethod('SVGTransform', name, function (...args) {
    const s = svgSlot(this, 'SVGTransform');
    svgWrite(s);
    const m = build(...args);
    svgSlots.get(s.matrix).values = m;
    s.kind = kind;
    s.center = kind === 4 ? [svgFloat(args[1]), svgFloat(args[2])] : null;
    s.angle = kind >= 4 ? svgFloat(args[0]) : 0;
    if (s.changed) s.changed();
  });
for (const [name, make] of Object.entries({
  createSVGNumber: () => svgScalar('SVGNumber'),
  createSVGLength: () => svgScalar('SVGLength'),
  createSVGAngle: () => svgScalar('SVGAngle'),
  createSVGPoint: () => svgPoint(),
  createSVGRect: () => svgRect(),
  createSVGMatrix: () => svgMatrix(),
  createSVGTransform: () => svgTransform(),
  createSVGTransformFromMatrix: (m) => {
    const t = svgTransform();
    t.setMatrix(m);
    return t;
  },
}))
  svgMethod('SVGSVGElement', name, function (...args) {
    if (tag(check(this)) !== 'svg') throw new TypeError('Illegal invocation');
    return make(...args);
  });
for (const [type, name, target] of [
  ['SVGElement', 'style', 'cssText'],
  ['SVGElement', 'focusGroup', 'value'],
  ['SVGAElement', 'relList', 'value'],
]) {
  const p = globalThis[type].prototype,
    d = Object.getOwnPropertyDescriptor(p, name);
  if (d?.get && !d.set) {
    const set = function (value) {
      d.get.call(this)[target] = value;
    };
    markNative(set, 'set ' + name);
    Object.defineProperty(p, name, { ...d, set });
  }
}
