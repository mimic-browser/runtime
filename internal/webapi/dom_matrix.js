// Geometry Interfaces use column-major matrices. This is CPU algebra only.
const compatibilityMatrix = {};
const DOMMatrix = (() => {
  const slots = new WeakMap(),
    identity = () => [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1],
    aliases = { a: 0, b: 1, c: 4, d: 5, e: 12, f: 13 };
  const slot = (o) => {
    const s = slots.get(o);
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const planar = (m) =>
    m[2] === 0 &&
    m[3] === 0 &&
    m[6] === 0 &&
    m[7] === 0 &&
    m[8] === 0 &&
    m[9] === 0 &&
    m[10] === 1 &&
    m[11] === 0 &&
    m[14] === 0 &&
    m[15] === 1;
  const product = (a, b) =>
    Array.from({ length: 16 }, (_, i) => {
      let v = 0;
      for (let k = 0; k < 4; k++) v += a[k * 4 + (i % 4)] * b[Math.floor(i / 4) * 4 + k];
      return v;
    });
  const expand = (a) => [a[0], a[1], 0, 0, a[2], a[3], 0, 0, 0, 0, 1, 0, a[4], a[5], 0, 1];
  const syntax = () => {
    throw platformDOMException('Failed to parse matrix', 'SyntaxError');
  };
  const parse = (init) => {
    if (init === undefined) return { m: identity(), two: true };
    if (typeof init === 'string') {
      if (init === '' || init === 'none') return { m: identity(), two: true };
      const match = /^\s*(matrix|matrix3d)\(([^()]*)\)\s*$/.exec(init);
      if (!match) return parseTransforms(init);
      const a = match[2].split(',').map((v) => {
        if (!v.trim()) syntax();
        return +v;
      });
      if (a.some((v) => !Number.isFinite(v)) || a.length !== (match[1] === 'matrix' ? 6 : 16))
        syntax();
      return { m: a.length === 6 ? expand(a) : a, two: a.length === 6 };
    }
    const a = Array.from(init, (v) => +v);
    if (a.length !== 6 && a.length !== 16) throw new TypeError('Expected 6 or 16 elements');
    return { m: a.length === 6 ? expand(a) : a, two: a.length === 6 };
  };
  const axisRotation = (x, y, z, angle) => {
    const m = identity(),
      norm = Math.hypot(x, y, z);
    if (!norm) return m;
    x /= norm;
    y /= norm;
    z /= norm;
    const a = (angle * Math.PI) / 180,
      c = Math.cos(a),
      s = Math.sin(a),
      t = 1 - c;
    m[0] = t * x * x + c;
    m[1] = t * x * y + s * z;
    m[2] = t * x * z - s * y;
    m[4] = t * x * y - s * z;
    m[5] = t * y * y + c;
    m[6] = t * y * z + s * x;
    m[8] = t * x * z + s * y;
    m[9] = t * y * z - s * x;
    m[10] = t * z * z + c;
    return m;
  };
  // Internal CSS geometry and DOMMatrix construction share this parser and pure
  // algebra. No method/accessor on an author-visible matrix is invoked here.
  const parseTransforms = (source, resolveLength) => {
    const re = /([a-zA-Z0-9]+)\(([^()]*)\)/g;
    let match,
      at = 0,
      m = identity(),
      two = true;
    const number = (v) => {
        if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(v)) syntax();
        return +v;
      },
      length = (v, axis) => {
        if (resolveLength) {
          const resolved = resolveLength(v, axis);
          if (resolved !== null && resolved !== undefined) return resolved;
        }
        const match = /^(.+?)(px|cm|mm|q|in|pt|pc)$/.exec(v);
        if (!match) {
          if (number(v) !== 0) syntax();
          return 0;
        }
        return (
          number(match[1]) *
          { px: 1, cm: 96 / 2.54, mm: 96 / 25.4, q: 96 / 101.6, in: 96, pt: 96 / 72, pc: 16 }[
            match[2]
          ]
        );
      },
      angle = (v) => {
        const match = /^(.+?)(deg|rad|grad|turn)$/.exec(v);
        if (!match) {
          if (number(v) !== 0) syntax();
          return 0;
        }
        return number(match[1]) * { deg: 1, rad: 180 / Math.PI, grad: 0.9, turn: 360 }[match[2]];
      };
    while ((match = re.exec(source))) {
      if (source.slice(at, match.index).trim()) syntax();
      at = re.lastIndex;
      const name = match[1],
        a = match[2].split(',').map((v) => v.trim()),
        arity = (min, max = min) => {
          if (a.length < min || a.length > max) syntax();
        };
      let n = identity();
      if (name === 'matrix' || name === 'matrix3d') {
        arity(name === 'matrix' ? 6 : 16);
        const values = a.map(number);
        n = values.length === 6 ? expand(values) : values;
        if (values.length === 16) two = false;
      } else if (name === 'translate') {
        arity(1, 2);
        n[12] = length(a[0], 0);
        n[13] = a[1] === undefined ? 0 : length(a[1], 1);
      } else if (name === 'translate3d') {
        arity(3);
        for (let i = 0; i < 3; i++) n[12 + i] = length(a[i], i);
        two = false;
      } else if (/^translate[XYZ]$/.test(name)) {
        arity(1);
        const axis = 'XYZ'.indexOf(name.at(-1));
        n[12 + axis] = length(a[0], axis);
        if (axis === 2) two = false;
      } else if (name === 'scale') {
        arity(1, 2);
        n[0] = number(a[0]);
        n[5] = a[1] === undefined ? n[0] : number(a[1]);
      } else if (name === 'scale3d') {
        arity(3);
        n[0] = number(a[0]);
        n[5] = number(a[1]);
        n[10] = number(a[2]);
        two = false;
      } else if (/^scale[XYZ]$/.test(name)) {
        arity(1);
        const axis = 'XYZ'.indexOf(name.at(-1));
        n[axis * 5] = number(a[0]);
        if (axis === 2) two = false;
      } else if (name === 'rotate' || name === 'rotateZ') {
        arity(1);
        n = axisRotation(0, 0, 1, angle(a[0]));
        if (name === 'rotateZ') two = false;
      } else if (name === 'rotateX' || name === 'rotateY') {
        arity(1);
        n = axisRotation(name === 'rotateX' ? 1 : 0, name === 'rotateY' ? 1 : 0, 0, angle(a[0]));
        two = false;
      } else if (name === 'rotate3d') {
        arity(4);
        n = axisRotation(number(a[0]), number(a[1]), number(a[2]), angle(a[3]));
        two = false;
      } else if (name === 'skewX' || name === 'skewY') {
        arity(1);
        n[name === 'skewX' ? 4 : 1] = Math.tan((angle(a[0]) * Math.PI) / 180);
      } else if (name === 'skew') {
        arity(1, 2);
        n[4] = Math.tan((angle(a[0]) * Math.PI) / 180);
        n[1] = a[1] === undefined ? 0 : Math.tan((angle(a[1]) * Math.PI) / 180);
      } else if (name === 'perspective') {
        arity(1);
        const value = length(a[0], 2);
        if (value < 0) syntax();
        n[11] = -1 / Math.max(1, value);
        two = false;
      } else syntax();
      m = product(m, n);
    }
    if (!at || source.slice(at).trim()) syntax();
    return { m, two };
  };
  const dict = (value) => {
    value = value ?? {};
    const m = identity();
    for (let i = 0; i < 16; i++) {
      const key = 'm' + (Math.floor(i / 4) + 1) + ((i % 4) + 1);
      if (value[key] !== undefined) m[i] = +value[key];
    }
    for (const [key, i] of Object.entries(aliases)) {
      if (value[key] !== undefined) {
        const v = +value[key],
          full = 'm' + (Math.floor(i / 4) + 1) + ((i % 4) + 1);
        if (value[full] !== undefined && !Object.is(v, m[i]) && !(v === m[i]))
          throw new TypeError('Conflicting matrix aliases');
        m[i] = v;
      }
    }
    const two = value.is2D === undefined ? planar(m) : !!value.is2D;
    if (two && !planar(m)) throw new TypeError('Inconsistent is2D');
    return { m, two };
  };
  const make = (state, ctor = DOMMatrix) => {
    const result = Object.create(ctor.prototype);
    slots.set(result, { m: state.m.slice(), two: state.two });
    return result;
  };
  const inverse = (m) => {
    const rows = Array.from({ length: 4 }, (_, r) =>
      Array.from({ length: 8 }, (_, c) => (c < 4 ? m[c * 4 + r] : +(c - 4 === r))),
    );
    for (let c = 0; c < 4; c++) {
      let pivot = c;
      for (let r = c + 1; r < 4; r++)
        if (Math.abs(rows[r][c]) > Math.abs(rows[pivot][c])) pivot = r;
      if (!rows[pivot][c]) return null;
      [rows[c], rows[pivot]] = [rows[pivot], rows[c]];
      const scale = rows[c][c];
      rows[c] = rows[c].map((v) => v / scale);
      for (let r = 0; r < 4; r++)
        if (r !== c) {
          const factor = rows[r][c];
          rows[r] = rows[r].map((v, i) => v - factor * rows[c][i]);
        }
    }
    return Array.from({ length: 16 }, (_, i) => rows[i % 4][4 + Math.floor(i / 4)]);
  };
  class DOMMatrixReadOnly {
    constructor(init) {
      slots.set(this, parse(init));
    }
    static fromMatrix(other = {}) {
      return make(dict(other), this);
    }
    static fromFloat32Array(array) {
      if (!(array instanceof Float32Array)) throw new TypeError('Expected Float32Array');
      return make(parse(array), this);
    }
    static fromFloat64Array(array) {
      if (!(array instanceof Float64Array)) throw new TypeError('Expected Float64Array');
      return make(parse(array), this);
    }
    get is2D() {
      return slot(this).two;
    }
    get isIdentity() {
      return slot(this).m.every((v, i) => v === identity()[i]);
    }
    toFloat32Array() {
      return new Float32Array(slot(this).m);
    }
    toFloat64Array() {
      return new Float64Array(slot(this).m);
    }
    toString() {
      const s = slot(this);
      if (s.m.some((v) => !Number.isFinite(v)))
        throw platformDOMException('Matrix is not finite', 'InvalidStateError');
      return s.two
        ? 'matrix(' + [s.m[0], s.m[1], s.m[4], s.m[5], s.m[12], s.m[13]].join(', ') + ')'
        : 'matrix3d(' + s.m.join(', ') + ')';
    }
    toJSON() {
      const s = slot(this),
        out = {};
      for (const [k, i] of Object.entries(aliases)) out[k] = s.m[i];
      for (let i = 0; i < 16; i++) out['m' + (Math.floor(i / 4) + 1) + ((i % 4) + 1)] = s.m[i];
      out.is2D = s.two;
      out.isIdentity = this.isIdentity;
      return out;
    }
    multiply(other = {}) {
      return make(slot(this)).multiplySelf(other);
    }
    translate(x = 0, y = 0, z = 0) {
      return make(slot(this)).translateSelf(x, y, z);
    }
    scale(x = 1, y = x, z = 1, ox = 0, oy = 0, oz = 0) {
      return make(slot(this)).scaleSelf(x, y, z, ox, oy, oz);
    }
    scaleNonUniform(x = 1, y = 1) {
      return this.scale(x, y);
    }
    scale3d(scale = 1, ox = 0, oy = 0, oz = 0) {
      return this.scale(scale, scale, scale, ox, oy, oz);
    }
    rotate(x = 0, y, z) {
      return make(slot(this)).rotateSelf(x, y, z);
    }
    rotateFromVector(x = 0, y = 0) {
      return make(slot(this)).rotateFromVectorSelf(x, y);
    }
    rotateAxisAngle(x = 0, y = 0, z = 0, angle = 0) {
      return make(slot(this)).rotateAxisAngleSelf(x, y, z, angle);
    }
    skewX(angle = 0) {
      return make(slot(this)).skewXSelf(angle);
    }
    skewY(angle = 0) {
      return make(slot(this)).skewYSelf(angle);
    }
    flipX() {
      return this.scale(-1, 1);
    }
    flipY() {
      return this.scale(1, -1);
    }
    inverse() {
      return make(slot(this)).invertSelf();
    }
    transformPoint(point = {}) {
      point = point ?? {};
      const a = [+(point.x ?? 0), +(point.y ?? 0), +(point.z ?? 0), +(point.w ?? 1)],
        m = slot(this).m,
        r = Array.from(
          { length: 4 },
          (_, i) => m[i] * a[0] + m[4 + i] * a[1] + m[8 + i] * a[2] + m[12 + i] * a[3],
        );
      return new globalThis.DOMPoint(...r);
    }
  }
  class DOMMatrix extends DOMMatrixReadOnly {
    constructor(init) {
      super(init);
    }
    multiplySelf(other = {}) {
      const s = slot(this),
        b = dict(other);
      s.m = product(s.m, b.m);
      s.two = s.two && b.two;
      return this;
    }
    preMultiplySelf(other = {}) {
      const s = slot(this),
        b = dict(other);
      s.m = product(b.m, s.m);
      s.two = s.two && b.two;
      return this;
    }
    translateSelf(x = 0, y = 0, z = 0) {
      x = +x;
      y = +y;
      z = +z;
      return this.multiplySelf({ m41: x, m42: y, m43: z, is2D: z === 0 });
    }
    scaleSelf(x = 1, y = x, z = 1, ox = 0, oy = 0, oz = 0) {
      x = +x;
      y = +y;
      z = +z;
      ox = +ox;
      oy = +oy;
      oz = +oz;
      this.translateSelf(ox, oy, oz);
      this.multiplySelf({ m11: x, m22: y, m33: z, is2D: z === 1 });
      return this.translateSelf(-ox, -oy, -oz);
    }
    scale3dSelf(scale = 1, ox = 0, oy = 0, oz = 0) {
      return this.scaleSelf(scale, scale, scale, ox, oy, oz);
    }
    rotateSelf(x = 0, y, z) {
      x = +x;
      if (y === undefined && z === undefined) {
        z = x;
        x = 0;
        y = 0;
      } else {
        y = +(y ?? 0);
        z = +(z ?? 0);
      }
      return this.rotateAxisAngleSelf(0, 0, 1, z)
        .rotateAxisAngleSelf(0, 1, 0, y)
        .rotateAxisAngleSelf(1, 0, 0, x);
    }
    rotateFromVectorSelf(x = 0, y = 0) {
      return this.rotateSelf((Math.atan2(+y, +x) * 180) / Math.PI);
    }
    rotateAxisAngleSelf(x = 0, y = 0, z = 0, angle = 0) {
      x = +x;
      y = +y;
      z = +z;
      angle = +angle;
      const state = slot(this);
      state.m = product(state.m, axisRotation(x, y, z, angle));
      if (angle !== 0 && (x !== 0 || y !== 0) && Math.hypot(x, y, z) !== 0) state.two = false;
      return this;
    }
    skewXSelf(angle = 0) {
      return this.multiplySelf({ c: Math.tan((+angle * Math.PI) / 180) });
    }
    skewYSelf(angle = 0) {
      return this.multiplySelf({ b: Math.tan((+angle * Math.PI) / 180) });
    }
    invertSelf() {
      const s = slot(this),
        m = inverse(s.m);
      s.m = m || Array(16).fill(NaN);
      if (!m) s.two = false;
      return this;
    }
    setMatrixValue(value) {
      const state = parse(String(value));
      slot(this);
      slots.set(this, state);
      return this;
    }
  }
  for (const [name, ctor] of [
    ['DOMMatrixReadOnly', DOMMatrixReadOnly],
    ['DOMMatrix', DOMMatrix],
  ]) {
    for (const [key, index] of [
      ...Object.entries(aliases),
      ...Array.from({ length: 16 }, (_, i) => ['m' + (Math.floor(i / 4) + 1) + ((i % 4) + 1), i]),
    ]) {
      const d = {
        get() {
          return slot(this).m[index];
        },
        enumerable: true,
        configurable: true,
      };
      if (name === 'DOMMatrix')
        d.set = function (value) {
          const s = slot(this);
          s.m[index] = +value;
          if (!planar(s.m)) s.two = false;
        };
      Object.defineProperty(ctor.prototype, key, d);
    }
    for (const key of Object.getOwnPropertyNames(ctor.prototype)) {
      if (key === 'constructor') continue;
      const d = Object.getOwnPropertyDescriptor(ctor.prototype, key);
      Object.defineProperty(ctor.prototype, key, { ...d, enumerable: true });
    }
    Object.defineProperty(ctor.prototype, Symbol.toStringTag, { value: name, configurable: true });
    Object.defineProperty(globalThis, name, { value: ctor, writable: true, configurable: true });
  }
  const points = new WeakMap(),
    pointSlot = (o) => {
      const p = points.get(o);
      if (!p) throw new TypeError('Illegal invocation');
      return p;
    };
  class DOMPointReadOnly {
    constructor(x = 0, y = 0, z = 0, w = 1) {
      points.set(this, [+x, +y, +z, +w]);
    }
    static fromPoint(value = {}) {
      value = value ?? {};
      return new this(value.x ?? 0, value.y ?? 0, value.z ?? 0, value.w ?? 1);
    }
    matrixTransform(matrix = {}) {
      return DOMMatrix.fromMatrix(matrix).transformPoint(this);
    }
    toJSON() {
      const p = pointSlot(this);
      return { x: p[0], y: p[1], z: p[2], w: p[3] };
    }
  }
  class DOMPoint extends DOMPointReadOnly {
    constructor(x = 0, y = 0, z = 0, w = 1) {
      super(x, y, z, w);
    }
  }
  for (const [name, ctor] of [
    ['DOMPointReadOnly', DOMPointReadOnly],
    ['DOMPoint', DOMPoint],
  ]) {
    for (const [i, key] of ['x', 'y', 'z', 'w'].entries()) {
      const d = {
        get() {
          return pointSlot(this)[i];
        },
        enumerable: true,
        configurable: true,
      };
      if (name === 'DOMPoint')
        d.set = function (value) {
          pointSlot(this)[i] = +value;
        };
      Object.defineProperty(ctor.prototype, key, d);
    }
    for (const key of Object.getOwnPropertyNames(ctor.prototype)) {
      if (key === 'constructor') continue;
      const d = Object.getOwnPropertyDescriptor(ctor.prototype, key);
      Object.defineProperty(ctor.prototype, key, { ...d, enumerable: true });
    }
    Object.defineProperty(ctor.prototype, Symbol.toStringTag, { value: name, configurable: true });
    Object.defineProperty(globalThis, name, { value: ctor, writable: true, configurable: true });
  }
  compatibilityMatrix.parse = (source, resolveLength) =>
    source === 'none' ? identity() : parseTransforms(source, resolveLength).m;
  // gfx::ClampFloatGeometry reserves exponent headroom for later operations;
  // this belongs to client-coordinate projection, not DOMMatrix double algebra.
  const geometryLimit = Math.fround(((2 - Math.pow(2, -23)) * Math.pow(2, 127)) / 1e6);
  compatibilityMatrix.geometryCoordinate = (value) =>
    Number.isNaN(value) ? 0 : Math.fround(Math.max(-geometryLimit, Math.min(geometryLimit, value)));
  compatibilityMatrix.point = (m, x, y, z = 0, w = 1) =>
    Array.from({ length: 4 }, (_, i) => m[i] * x + m[4 + i] * y + m[8 + i] * z + m[12 + i] * w);
  return DOMMatrix;
})();
