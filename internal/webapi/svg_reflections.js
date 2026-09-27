const svgReflections = new WeakMap();
const svgCached = (n, key, create) => {
  let cache = svgReflections.get(n);
  if (!cache) {
    cache = new Map();
    svgReflections.set(n, cache);
  }
  if (!cache.has(key)) cache.set(key, create());
  return cache.get(key);
};
const svgListTypes = {
  SVGLengthList: 'SVGLength',
  SVGNumberList: 'SVGNumber',
  SVGPointList: 'SVGPoint',
  SVGTransformList: 'SVGTransform',
  SVGStringList: 'string',
};
const svgList = (type, node, key, readonly = false) => {
  const list = svgMake(type, { node, key, readonly, entries: [], last: null }),
    s = svgSlots.get(list),
    itemType = svgListTypes[type];
  const encode = (item) => {
    if (itemType === 'string') return item;
    const v = svgSlots.get(item);
    if (itemType === 'SVGPoint') return cssSerializeNumber(v.x) + ' ' + cssSerializeNumber(v.y);
    if (itemType === 'SVGTransform') {
      const m = svgMatrixValues(v.matrix),
        num = (values) => values.map(cssSerializeNumber).join(' ');
      return v.kind === 2
        ? 'translate(' + num([m[4], m[5]]) + ')'
        : v.kind === 3
          ? 'scale(' + num([m[0], m[3]]) + ')'
          : v.kind === 4
            ? 'rotate(' + num([v.angle, ...(v.center || [0, 0])]) + ')'
            : 'matrix(' + num(m) + ')';
    }
    return itemType === 'SVGLength' ? item.valueAsString : cssSerializeNumber(item.value);
  };
  s.refresh = () => {
    const raw = attr(node, key) || '';
    if (raw === s.last) return;
    for (const old of s.entries) {
      const state = svgSlots.get(old);
      if (state) state.changed = null;
    }
    s.last = raw;
    s.entries = [];
    if (itemType === 'SVGTransform') {
      const re = /(matrix|translate|scale|rotate|skewX|skewY)\s*\(([^)]*)\)/g;
      let m,
        at = 0;
      while ((m = re.exec(raw))) {
        if (raw.slice(at, m.index).trim().replace(/,/g, '')) break;
        at = re.lastIndex;
        const a = numbers(m[2]),
          t = svgTransform();
        try {
          if (m[1] === 'matrix' && a.length === 6) t.setMatrix(svgMatrix(a));
          else if (m[1] === 'translate' && a.length >= 1) t.setTranslate(a[0], a[1] || 0);
          else if (m[1] === 'scale' && a.length >= 1) t.setScale(a[0], a[1] ?? a[0]);
          else if (m[1] === 'rotate' && (a.length === 1 || a.length === 3))
            t.setRotate(a[0], a[1] || 0, a[2] || 0);
          else if (m[1] === 'skewX' && a.length === 1) t.setSkewX(a[0]);
          else if (m[1] === 'skewY' && a.length === 1) t.setSkewY(a[0]);
          else break;
          s.entries.push(t);
        } catch {
          break;
        }
      }
    } else if (itemType === 'SVGPoint') {
      const values = numbers(raw);
      for (let i = 0; i + 1 < values.length; i += 2)
        s.entries.push(svgPoint(Math.fround(values[i]), Math.fround(values[i + 1])));
    } else if (itemType === 'string') s.entries = raw.trim() ? raw.trim().split(/\s+/) : [];
    else
      for (const token of raw
        .trim()
        .split(/[\s,]+/)
        .filter(Boolean)) {
        const value = svgScalar(itemType, { node, axis: ['y', 'dy'].includes(key) ? 'y' : 'x' });
        try {
          if (itemType === 'SVGLength') value.valueAsString = token;
          else value.value = Number(token);
          s.entries.push(value);
        } catch {
          break;
        }
      }
    for (const item of s.entries) {
      const v = svgSlots.get(item);
      if (v) {
        v.readonly = readonly;
        v.changed = () => {
          s.refresh();
          if (s.entries.includes(item)) s.commit();
        };
        if (v.type === 'SVGTransform') svgSlots.get(v.matrix).readonly = readonly;
      }
    }
  };
  s.commit = () => {
    const raw = s.entries.map(encode).join(' ');
    svgAttr(node, key, raw);
    s.last = raw;
  };
  s.item = (index) => {
    s.refresh();
    index = +index >>> 0;
    if (index >= s.entries.length)
      throw platformDOMException('Index out of range', 'IndexSizeError');
    const value = s.entries[index];
    if (itemType === 'string') return value;
    const copy = Object.create(globalThis[itemType].prototype);
    svgSlots.set(copy, svgSlots.get(value));
    return copy;
  };
  s.clone = (value) => {
    if (itemType === 'string') return String(value);
    svgSlot(value, itemType);
    let copy;
    if (itemType === 'SVGPoint') copy = svgPoint(value.x, value.y);
    else if (itemType === 'SVGTransform') {
      copy = svgTransform();
      copy.setMatrix(value.matrix);
    } else {
      copy = svgScalar(itemType, { node });
      if (itemType === 'SVGLength') copy.valueAsString = value.valueAsString;
      else copy.value = value.value;
    }
    return copy;
  };
  const proxy = new Proxy(list, {
    get(target, key, receiver) {
      if (typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key)) {
        s.refresh();
        return Number(key) < s.entries.length ? s.item(Number(key)) : undefined;
      }
      return Reflect.get(target, key, receiver);
    },
    ownKeys(target) {
      s.refresh();
      return s.entries.map((_, i) => String(i)).concat(Reflect.ownKeys(target));
    },
    getOwnPropertyDescriptor(target, key) {
      s.refresh();
      if (typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key) && Number(key) < s.entries.length)
        return { value: s.item(Number(key)), writable: true, enumerable: true, configurable: true };
      return Reflect.getOwnPropertyDescriptor(target, key);
    },
    set(target, key, value, receiver) {
      if (typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key)) {
        svgWrite(s);
        s.refresh();
        const index = Number(key);
        if (index >= s.entries.length)
          throw platformDOMException('Index out of range', 'IndexSizeError');
        const copy = s.clone(value);
        s.entries[index] = copy;
        const v = svgSlots.get(copy);
        if (v) v.changed = () => s.commit();
        s.commit();
        return true;
      }
      return Reflect.set(target, key, value, receiver);
    },
    deleteProperty(target, key) {
      s.refresh();
      if (typeof key === 'string' && /^(0|[1-9]\d*)$/.test(key) && Number(key) < s.entries.length)
        return false;
      return Reflect.deleteProperty(target, key);
    },
  });
  svgSlots.set(proxy, s);
  return proxy;
};
// Proxies share the same private brand as their target.
const svgListCreate = svgList;
const svgBoundList = (...args) => {
  const value = svgListCreate(...args);
  return value;
};
for (const type of Object.keys(svgListTypes)) {
  for (const key of ['length', 'numberOfItems'])
    svgProp(type, key, function () {
      const s = svgSlot(this, type);
      s.refresh();
      return s.entries.length;
    });
  svgMethod(type, 'getItem', function (index) {
    return svgSlot(this, type).item(index);
  });
  svgMethod(type, 'clear', function () {
    const s = svgSlot(this, type);
    svgWrite(s);
    s.refresh();
    for (const item of s.entries) {
      const v = svgSlots.get(item);
      if (v) v.changed = null;
    }
    s.entries = [];
    s.commit();
  });
  for (const name of ['initialize', 'appendItem', 'insertItemBefore', 'replaceItem'])
    svgMethod(type, name, function (value, index) {
      const s = svgSlot(this, type);
      svgWrite(s);
      s.refresh();
      const copy = s.clone(value);
      index = +index >>> 0;
      if (name === 'initialize') {
        s.entries = [];
        index = 0;
      } else if (name === 'appendItem') index = s.entries.length;
      else if (name === 'replaceItem' && index >= s.entries.length)
        throw platformDOMException('Index out of range', 'IndexSizeError');
      index = Math.min(index, s.entries.length);
      s.entries.splice(index, name === 'replaceItem' ? 1 : 0, copy);
      const v = svgSlots.get(copy);
      if (v)
        v.changed = () => {
          s.refresh();
          if (s.entries.includes(copy)) s.commit();
        };
      s.commit();
      return s.item(index);
    });
  svgMethod(type, 'removeItem', function (index) {
    const s = svgSlot(this, type);
    svgWrite(s);
    const value = s.item(index);
    s.entries.splice(+index >>> 0, 1);
    const v = svgSlots.get(value);
    if (v) v.changed = null;
    s.commit();
    return value;
  });
}
svgMethod('SVGTransformList', 'createSVGTransformFromMatrix', function (m) {
  svgSlot(this, 'SVGTransformList');
  const t = svgTransform();
  t.setMatrix(m);
  return t;
});
svgMethod('SVGTransformList', 'consolidate', function () {
  const s = svgSlot(this, 'SVGTransformList');
  svgWrite(s);
  s.refresh();
  if (!s.entries.length) return null;
  let m = ident;
  for (const item of s.entries) m = multiply(m, svgMatrixValues(item.matrix));
  const t = svgTransform();
  t.setMatrix(svgMatrix(m));
  return this.initialize(t);
});
const svgAnimated = (n, key, type, fallback = '0', axis = 'x') =>
  svgCached(n, type + ':' + key, () => {
    const read = () => attr(n, key) ?? fallback,
      write = (value) => svgAttr(n, key, value),
      object = svgMake(type, { node: n, key, read, write }),
      state = svgSlots.get(object);
    const make = (readonly) => {
      const options = {
        node: n,
        axis,
        readonly,
        read: () => state.read(),
        write: (v) => state.write(v),
      };
      if (key === 'textLength')
        options.automatic = () => (measurable(n) ? textLayout(n).length : 0);
      if (type === 'SVGAnimatedLength') return svgScalar('SVGLength', options);
      if (type === 'SVGAnimatedAngle') return svgScalar('SVGAngle', options);
      if (type.endsWith('List'))
        return svgBoundList(type.replace('Animated', ''), n, key, readonly);
      if (type === 'SVGAnimatedRect')
        return svgRect([0, 0, 0, 0], {
          readonly,
          read: () => {
            const v = numbers(read());
            return { x: v[0] || 0, y: v[1] || 0, width: v[2] || 0, height: v[3] || 0 };
          },
          changed: (name, value) => {
            const v = numbers(read());
            while (v.length < 4) v.push(0);
            v[['x', 'y', 'width', 'height'].indexOf(name)] = value;
            write(v.map(cssSerializeNumber).join(' '));
          },
        });
      if (type === 'SVGAnimatedPreserveAspectRatio') {
        const value = svgMake('SVGPreserveAspectRatio', options);
        return value;
      }
      return null;
    };
    state.base = make(false);
    state.anim = make(true);
    return object;
  });
for (const type of [
  'SVGAnimatedLength',
  'SVGAnimatedAngle',
  'SVGAnimatedLengthList',
  'SVGAnimatedNumberList',
  'SVGAnimatedTransformList',
  'SVGAnimatedRect',
  'SVGAnimatedPreserveAspectRatio',
  'SVGAnimatedString',
  'SVGAnimatedNumber',
  'SVGAnimatedInteger',
  'SVGAnimatedBoolean',
  'SVGAnimatedEnumeration',
])
  for (const key of ['baseVal', 'animVal']) {
    const primitive = [
      'SVGAnimatedString',
      'SVGAnimatedNumber',
      'SVGAnimatedInteger',
      'SVGAnimatedBoolean',
      'SVGAnimatedEnumeration',
    ].includes(type);
    svgProp(
      type,
      key,
      function () {
        const s = svgSlot(this, type);
        if (!primitive) return key === 'baseVal' ? s.base : s.anim;
        const raw = s.read();
        if (type === 'SVGAnimatedString') return String(raw);
        if (type === 'SVGAnimatedBoolean') return raw === 'true';
        return Math.fround(Number(raw) || 0);
      },
      key === 'baseVal' && primitive
        ? function (value) {
            const s = svgSlot(this, type);
            if (type === 'SVGAnimatedString' && s.key === 'href' && tag(s.node) === 'script') {
              const text = trustedConvert(
                value,
                'TrustedScriptURL',
                'SVGScriptElement href',
                "Failed to set the 'baseVal' property on 'SVGAnimatedString': ",
                s.node,
              );
              s.write(trustedValue(TrustedScriptURL, text));
              return;
            }
            const v =
              type === 'SVGAnimatedString'
                ? String(value)
                : type === 'SVGAnimatedBoolean'
                  ? String(Boolean(value))
                  : svgFloat(value);
            s.write(v);
          }
        : undefined,
    );
  }
const alignNames = [
  '',
  'none',
  'xMinYMin',
  'xMidYMin',
  'xMaxYMin',
  'xMinYMid',
  'xMidYMid',
  'xMaxYMid',
  'xMinYMax',
  'xMidYMax',
  'xMaxYMax',
];
for (const key of ['align', 'meetOrSlice'])
  svgProp(
    'SVGPreserveAspectRatio',
    key,
    function () {
      const s = svgSlot(this, 'SVGPreserveAspectRatio'),
        raw = s.read().trim().split(/\s+/);
      return key === 'align' ? Math.max(1, alignNames.indexOf(raw[0])) : raw[1] === 'slice' ? 2 : 1;
    },
    function (value) {
      const s = svgSlot(this, 'SVGPreserveAspectRatio');
      svgWrite(s);
      value = Number(value) >>> 0;
      if (value < 1 || value > (key === 'align' ? 10 : 2))
        throw platformDOMException('Invalid enumeration', 'NotSupportedError');
      const a = this.align,
        m = this.meetOrSlice;
      s.write(
        (key === 'align' ? alignNames[value] : alignNames[a]) +
          ' ' +
          ((key === 'meetOrSlice' ? value : m) === 2 ? 'slice' : 'meet'),
      );
    },
  );
const svgReflect = (type, key, valueType, fallback = '0', axis = 'x', attribute = key) =>
  svgProp(type, key, function () {
    const n = svgElementCheck(this);
    return svgAnimated(n, attribute, valueType, fallback, axis);
  });
svgReflect('SVGElement', 'className', 'SVGAnimatedString', '', 'x', 'class');
svgReflect('SVGGraphicsElement', 'transform', 'SVGAnimatedTransformList', '');
svgReflect('SVGGeometryElement', 'pathLength', 'SVGAnimatedNumber');
for (const [type, names] of Object.entries({
  SVGRectElement: 'x y width height rx ry',
  SVGCircleElement: 'cx cy r',
  SVGEllipseElement: 'cx cy rx ry',
  SVGLineElement: 'x1 y1 x2 y2',
  SVGImageElement: 'x y width height',
  SVGUseElement: 'x y width height',
  SVGForeignObjectElement: 'x y width height',
  SVGSVGElement: 'x y width height',
}))
  for (const name of names.split(' '))
    svgReflect(
      type,
      name,
      'SVGAnimatedLength',
      type === 'SVGSVGElement' && ['width', 'height'].includes(name) ? '100%' : '0',
      ['y', 'y1', 'y2', 'cy', 'ry', 'height'].includes(name)
        ? 'y'
        : name === 'r'
          ? 'diagonal'
          : 'x',
    );
for (const type of ['SVGTextPositioningElement'])
  for (const name of ['x', 'y', 'dx', 'dy', 'rotate'])
    svgReflect(
      type,
      name,
      name === 'rotate' ? 'SVGAnimatedNumberList' : 'SVGAnimatedLengthList',
      '',
      ['y', 'dy'].includes(name) ? 'y' : 'x',
    );
for (const type of [
  'SVGSVGElement',
  'SVGSymbolElement',
  'SVGViewElement',
  'SVGMarkerElement',
  'SVGPatternElement',
]) {
  svgReflect(type, 'viewBox', 'SVGAnimatedRect', '');
  svgReflect(type, 'preserveAspectRatio', 'SVGAnimatedPreserveAspectRatio', 'xMidYMid meet');
}
for (const type of [
  'SVGUseElement',
  'SVGImageElement',
  'SVGAElement',
  'SVGTextPathElement',
  'SVGPatternElement',
  'SVGGradientElement',
  'SVGScriptElement',
  'SVGMPathElement',
])
  svgReflect(type, 'href', 'SVGAnimatedString', '');
svgReflect('SVGAElement', 'target', 'SVGAnimatedString', '');
svgReflect('SVGTextContentElement', 'textLength', 'SVGAnimatedLength');
svgReflect('SVGTextContentElement', 'lengthAdjust', 'SVGAnimatedEnumeration', '1');
for (const type of ['SVGPolygonElement', 'SVGPolylineElement'])
  for (const key of ['points', 'animatedPoints'])
    svgProp(type, key, function () {
      const n = check(this);
      return svgCached(n, key, () =>
        svgBoundList('SVGPointList', n, 'points', key === 'animatedPoints'),
      );
    });
