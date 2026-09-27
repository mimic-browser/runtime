const svgEnumTables = {
  lengthAdjust: ['', 'spacing', 'spacingAndGlyphs'],
  spreadMethod: ['', 'pad', 'reflect', 'repeat'],
  markerUnits: ['', 'userSpaceOnUse', 'strokeWidth'],
  edgeMode: ['', 'duplicate', 'wrap', 'none'],
  xChannelSelector: ['', 'R', 'G', 'B', 'A'],
  yChannelSelector: ['', 'R', 'G', 'B', 'A'],
  stitchTiles: ['', 'stitch', 'noStitch'],
  operator: ['', 'over', 'in', 'out', 'atop', 'xor', 'arithmetic', 'lighter'],
  mode: [
    '',
    'normal',
    'multiply',
    'screen',
    'darken',
    'lighten',
    'overlay',
    'color-dodge',
    'color-burn',
    'hard-light',
    'soft-light',
    'difference',
    'exclusion',
    'hue',
    'saturation',
    'color',
    'luminosity',
  ],
};
const svgEnumNames = (n, key) => {
  if (/Units$/.test(key) && key !== 'markerUnits')
    return ['', 'userSpaceOnUse', 'objectBoundingBox'];
  if (key === 'type') {
    if (tag(n) === 'feTurbulence') return ['', 'fractalNoise', 'turbulence'];
    if (tag(n) === 'feColorMatrix')
      return ['', 'matrix', 'saturate', 'hueRotate', 'luminanceToAlpha'];
    if (tag(n).startsWith('feFunc'))
      return ['', 'identity', 'table', 'discrete', 'linear', 'gamma'];
  }
  if (key === 'operator' && tag(n) === 'feMorphology') return ['', 'erode', 'dilate'];
  return svgEnumTables[key];
};
for (const [type, key, valueType, fallback] of svgAttributeDefaults) {
  const prototype = globalThis[type]?.prototype;
  if (!prototype) continue;
  svgProp(type, key, function () {
    const n = svgElementCheck(this);
    if (!prototype.isPrototypeOf(n)) throw new TypeError('Illegal invocation');
    const axis = /^(y|y1|y2|cy|ry|dy|fy|height|refY|markerHeight)$/.test(key)
        ? 'y'
        : key === 'r'
          ? 'diagonal'
          : 'x',
      value = svgAnimated(n, key, valueType, fallback, axis),
      state = svgSlots.get(value);
    if (key === 'in1')
      ((state.read = () => attr(n, 'in') ?? fallback), (state.write = (v) => svgAttr(n, 'in', v)));
    if (key === 'href') state.read = () => attr(n, 'href') ?? attr(n, 'xlink:href') ?? fallback;
    const compound = /^(stdDeviation|baseFrequency|kernelUnitLength|radius|order)(X|Y)$/.exec(key);
    if (compound) {
      const attribute = compound[1],
        index = compound[2] === 'Y' ? 1 : 0;
      state.read = () => {
        const a = numbers(attr(n, attribute));
        return String(a[index] ?? a[0] ?? fallback);
      };
      state.write = (v) => {
        const a = numbers(attr(n, attribute));
        const x = a[0] ?? Number(fallback),
          y = a[1] ?? x;
        const pair = [index === 0 ? v : x, index === 1 ? v : y];
        svgAttr(n, attribute, +pair[0] === +pair[1] ? String(pair[0]) : pair.join(' '));
      };
    }
    if (valueType === 'SVGAnimatedEnumeration') {
      const names = svgEnumNames(n, key);
      if (names) {
        state.read = () => {
          const raw = attr(n, key);
          return raw == null ? fallback : String(Math.max(0, names.indexOf(raw)));
        };
        state.write = (v) => {
          v = Number(v) >>> 0;
          if (v < 1 || v >= names.length) throw new TypeError('Invalid enumeration value');
          svgAttr(n, key, names[v]);
        };
      }
    }
    return value;
  });
}
for (const type of Object.keys(svgElementInterfaces)
  .map((k) => svgElementInterfaces[k])
  .concat(['SVGGraphicsElement', 'SVGAnimationElement'])) {
  const prototype = globalThis[type]?.prototype;
  if (!prototype) continue;
  for (const key of ['nearestViewportElement', 'farthestViewportElement'])
    if (Object.hasOwn(prototype, key))
      svgProp(type, key, function () {
        let n = svgElementCheck(this),
          owner = svgOwner(n);
        if (key === 'farthestViewportElement')
          while (owner && svgOwner(owner)) owner = svgOwner(owner);
        return owner;
      });
  for (const key of ['requiredExtensions', 'systemLanguage'])
    if (Object.hasOwn(prototype, key))
      svgProp(type, key, function () {
        const n = svgElementCheck(this);
        return svgCached(n, key, () => svgBoundList('SVGStringList', n, key));
      });
}
for (const [type, keys] of [
  ['SVGAElement', ['download', 'hreflang', 'ping', 'referrerPolicy', 'rel', 'type']],
  ['SVGStyleElement', ['type', 'media', 'title']],
  ['SVGScriptElement', ['type']],
])
  for (const key of keys)
    svgProp(
      type,
      key,
      function () {
        return attr(svgElementCheck(this), key === 'referrerPolicy' ? 'referrerpolicy' : key) || '';
      },
      function (value) {
        svgAttr(
          svgElementCheck(this),
          key === 'referrerPolicy' ? 'referrerpolicy' : key,
          String(value),
        );
      },
    );
svgProp(
  'SVGAElement',
  'relList',
  function () {
    const n = svgElementCheck(this);
    return svgCached(n, 'relList', () => new DOMTokenList(n, 'rel'));
  },
  function (value) {
    this.relList.value = String(value);
  },
);
svgProp(
  'SVGAElement',
  'interestForElement',
  function () {
    const n = svgElementCheck(this),
      id = attr(n, 'interestfor');
    return id ? document.getElementById(id) : null;
  },
  function (value) {
    const n = svgElementCheck(this);
    if (value != null)
      svgFail('interestForElement', 'element-reference association is unsupported');
    svgAttr(n, 'interestfor', '');
  },
);
for (const [type, key] of [['SVGScriptElement', 'async']])
  svgProp(
    type,
    key,
    function () {
      return attr(svgElementCheck(this), key) != null;
    },
    function (value) {
      const n = svgElementCheck(this);
      if (value) svgAttr(n, key, '');
      else host.removeAttribute(elementSlot(n).nodeId, key);
    },
  );
const svgViewportState = (n) =>
  svgCached(n, 'viewport-state', () => ({
    scale: 1,
    translate: svgPoint(),
    paused: false,
    time: 0,
  }));
svgProp(
  'SVGSVGElement',
  'currentScale',
  function () {
    const n = svgElementCheck(this);
    let connected = false;
    for (let p = n; isDOMNode(p); p = parent(p))
      if (p === document || elementSlot(p)?.type === 'document') connected = true;
    return connected && !svgOwner(n) ? svgViewportState(n).scale : 1;
  },
  function (value) {
    const state = svgViewportState(svgElementCheck(this)),
      v = svgFloat(value);
    state.scale = v;
  },
);
svgProp('SVGSVGElement', 'currentTranslate', function () {
  return svgViewportState(svgElementCheck(this)).translate;
});
for (const type of ['SVGSVGElement', 'SVGViewElement'])
  svgProp(
    type,
    'zoomAndPan',
    function () {
      const raw = attr(svgElementCheck(this), 'zoomAndPan');
      return raw === 'disable' ? 1 : 2;
    },
    function (value) {
      value = Number(value) >>> 0;
      if (value !== 1 && value !== 2)
        throw platformDOMException('Invalid zoomAndPan', 'NoModificationAllowedError');
      svgAttr(svgElementCheck(this), 'zoomAndPan', value === 1 ? 'disable' : 'magnify');
    },
  );
svgProp('SVGAnimationElement', 'targetElement', function () {
  const n = svgElementCheck(this),
    href = attr(n, 'href') ?? attr(n, 'xlink:href');
  return href?.startsWith('#') ? document.getElementById(href.slice(1)) : parent(n);
});
for (const key of ['onbegin', 'onend', 'onrepeat'])
  svgProp(
    'SVGAnimationElement',
    key,
    function () {
      return svgCached(svgElementCheck(this), 'animation-handlers', () => ({}))[key] || null;
    },
    function (value) {
      const n = svgElementCheck(this),
        state = svgCached(n, 'animation-handlers', () => ({}));
      if (!state.installed) {
        for (const k of ['onbegin', 'onend', 'onrepeat'])
          n.addEventListener(k.slice(2), function (event) {
            if (state[k]) state[k].call(this, event);
          });
        state.installed = true;
      }
      state[key] = typeof value === 'function' ? value : null;
    },
  );

svgProp(
  'SVGImageElement',
  'crossOrigin',
  function () {
    const raw = attr(svgElementCheck(this), 'crossorigin');
    return raw == null
      ? null
      : raw.toLowerCase() === 'use-credentials'
        ? 'use-credentials'
        : 'anonymous';
  },
  function (value) {
    const n = svgElementCheck(this);
    if (value == null) host.removeAttribute(elementSlot(n).nodeId, 'crossorigin');
    else svgAttr(n, 'crossorigin', String(value));
  },
);
svgProp(
  'SVGImageElement',
  'decoding',
  function () {
    const raw = (attr(svgElementCheck(this), 'decoding') || '').toLowerCase();
    return ['sync', 'async'].includes(raw) ? raw : 'auto';
  },
  function (value) {
    svgAttr(svgElementCheck(this), 'decoding', String(value));
  },
);
svgProp('SVGStyleElement', 'sheet', function () {
  return constructedStyleSheets.ownerSheet(svgElementCheck(this));
});
svgBindings.add('SVGStyleElement.disabled');
