// Canvas storage and API state. Color glyphs use a bundled CPU raster module;
// no GPU or OS graphics backend is required.
const canvasCompatibilityState = (() => {
  // Preserve the selected generated WebIDL surface while replacing its storage
  // implementation. JS implementation parameters include internal slots and
  // optional arguments, neither of which determines public WebIDL arity.
  const interfaceNames = [
    'OffscreenCanvas',
    'CanvasRenderingContext2D',
    'OffscreenCanvasRenderingContext2D',
    'ImageData',
    'ImageBitmap',
    'CanvasGradient',
    'CanvasPattern',
    'TextMetrics',
    'HTMLCanvasElement',
  ];
  const reflectedInterfaces = new Map(
    interfaceNames.map((name) => {
      const ctor = globalThis[name];
      return [
        name,
        ctor
          ? { length: ctor.length, members: Object.getOwnPropertyDescriptors(ctor.prototype) }
          : null,
      ];
    }),
  );
  const decodedSourceImages = new WeakMap();
  const sourceImage = (source) => {
    const data = host.imagePixels(elementSlot(source).nodeId);
    if (!data) {
      decodedSourceImages.delete(source);
      return null;
    }
    if (data.vector) {
      host.semanticMissingAt(
        'canvas_state.js/sourceImage',
        'Canvas2D.vectorImageReadback',
        JSON.stringify({
          reason: 'SVG image pixels require a renderer; intrinsic dimensions only are decoded',
        }),
      );
      fail('NotSupportedError', 'SVG image raster readback is unsupported');
    }
    const cached = decodedSourceImages.get(source);
    if (cached && cached.encoded === data.pixels) return { ...data, pixels: cached.pixels };
    const raw = atob(data.pixels),
      pixels = new Uint8ClampedArray(raw.length);
    for (let i = 0; i < raw.length; i++) pixels[i] = raw.charCodeAt(i);
    decodedSourceImages.set(source, { encoded: data.pixels, pixels });
    return { ...data, pixels };
  };
  const imageBitmapArity = globalThis.createImageBitmap?.length;
  const selectedWorkerExposure = typeof __workerExposure === 'undefined' ? null : __workerExposure;
  const workerProperties = new Map(
    (selectedWorkerExposure?.properties || []).map((p) => [p.name, p]),
  );
  const canvases = new WeakMap(),
    contexts = new WeakMap(),
    images = new WeakMap(),
    bitmaps = new WeakMap(),
    gradients = new WeakMap(),
    patterns = new WeakMap(),
    factories = new Map(),
    token = {};
  const fail = (name, message) => {
    throw platformDOMException(message, name);
  };
  const uint = (value) => {
    value = Number(value);
    if (!Number.isFinite(value) || value < 0 || value > 4294967295)
      throw new TypeError('Canvas dimension is outside the accepted range');
    return Math.trunc(value);
  };
  const storage = (width, height) => {
    if (width * height > 16777216)
      fail('NotSupportedError', 'Canvas storage above 16 million pixels is not implemented');
    return new Uint8ClampedArray(width * height * 4);
  };
  const genericFontFamilies = new Set([
    'serif',
    'sans-serif',
    'monospace',
    'cursive',
    'fantasy',
    'system-ui',
    'ui-serif',
    'ui-sans-serif',
    'ui-monospace',
    'ui-rounded',
    'emoji',
    'math',
    'fangsong',
  ]);
  const serializeCanvasFont = (input) => {
    // Canvas exposes the parsed CSS shorthand, not the author's original text.
    // Keep this bounded to the pixel-size grammar that the text shaper supports.
    const match = /^\s*(.*?)\s*((?:\d+(?:\.\d+)?|\.\d+)px)(?:\s*\/\s*([^\s]+))?\s+(.+?)\s*$/.exec(
      input,
    );
    if (!match || Number.parseFloat(match[2]) <= 0) return null;
    const prefix = match[1].trim().split(/\s+/).filter(Boolean);
    if (
      prefix.some(
        (word) => !/^(?:normal|italic|oblique|small-caps|bold|bolder|lighter|[1-9]00)$/.test(word),
      )
    )
      return null;
    const families = [];
    let family = '',
      quote = '',
      escaped = false;
    for (const char of match[4]) {
      if (escaped) {
        family += '\\' + char;
        escaped = false;
      } else if (char === '\\') escaped = true;
      else if (quote) {
        family += char;
        if (char === quote) quote = '';
      } else if (char === '"' || char === "'") {
        quote = char;
        family += char;
      } else if (char === ',') {
        families.push(family.trim());
        family = '';
      } else family += char;
    }
    if (escaped || quote) return null;
    families.push(family.trim());
    if (families.some((name) => !name)) return null;
    const decode = (value) =>
      value.replace(/\\(?:([0-9a-fA-F]{1,6})\s?|([^\n\r\f]))/g, (_, hex, char) =>
        hex ? String.fromCodePoint(Math.min(Number.parseInt(hex, 16), 0x10ffff)) : char,
      );
    const normalized = families.map((name) => {
      const quoted = (name[0] === '"' || name[0] === "'") && name[name.length - 1] === name[0];
      const value = decode(quoted ? name.slice(1, -1) : name)
        .replace(/\s+/g, ' ')
        .trim();
      if (!value) return null;
      if (/^-?[_a-zA-Z][-_a-zA-Z0-9]*$/.test(value) && !(quoted && genericFontFamilies.has(value)))
        return value;
      return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
    });
    if (normalized.includes(null)) return null;
    const style = prefix.filter((word) => word === 'italic' || word === 'oblique');
    const rest = prefix.filter(
      (word) => word !== 'italic' && word !== 'oblique' && word !== 'normal',
    );
    const shorthand = [...style, ...rest, match[2] + (match[3] ? '/' + match[3] : '')];
    return shorthand.join(' ') + ' ' + normalized.join(', ');
  };
  const defaults = () => ({
    fillStyle: '#000000',
    strokeStyle: '#000000',
    globalAlpha: 1,
    globalCompositeOperation: 'source-over',
    lineWidth: 1,
    lineCap: 'butt',
    lineJoin: 'miter',
    miterLimit: 10,
    lineDashOffset: 0,
    shadowOffsetX: 0,
    shadowOffsetY: 0,
    shadowBlur: 0,
    shadowColor: 'rgba(0, 0, 0, 0)',
    font: '10px sans-serif',
    lang: 'inherit',
    textAlign: 'start',
    textBaseline: 'alphabetic',
    direction: 'inherit',
    imageSmoothingEnabled: true,
    imageSmoothingQuality: 'low',
    filter: 'none',
    fontKerning: 'auto',
    fontStretch: 'normal',
    fontVariantCaps: 'normal',
    letterSpacing: '0px',
    wordSpacing: '0px',
    textRendering: 'auto',
    transform: [1, 0, 0, 1, 0, 0],
    dash: [],
  });
  const fresh = (canvas, width, height) => {
    const state = {
      canvas,
      width,
      height,
      context: null,
      kind: null,
      pixels: null,
      originClean: true,
      unmodeled: false,
      operations: [],
      onResize: null,
    };
    canvases.set(canvas, state);
    return state;
  };
  const state = (canvas) => {
    let s = canvases.get(canvas);
    if (!s && typeof HTMLCanvasElement !== 'undefined' && canvas instanceof HTMLCanvasElement)
      s = fresh(canvas, htmlSize(canvas, 'width', 300), htmlSize(canvas, 'height', 150));
    if (!s) throw new TypeError('Illegal invocation');
    return s;
  };
  const ensurePixels = (s) => {
    if (!s.pixels) {
      s.pixels =
        s.colorType === 'float16'
          ? new Float32Array(
              s.width * s.height <= 16777216
                ? s.width * s.height * 4
                : fail(
                    'NotSupportedError',
                    'Canvas storage above 16 million pixels is not implemented',
                  ),
            )
          : storage(s.width, s.height);
      if (s.opaque) for (let i = 3; i < s.pixels.length; i += 4) s.pixels[i] = 255;
    }
    return s.pixels;
  };
  const buffer = (s) => {
    if (s.readPixels) return s.readPixels();
    ensurePixels(s);
    materializeText(s);
    return s.pixels;
  };
  const resize = (s, width, height) => {
    s.width = width;
    s.height = height;
    s.originClean = true;
    s.pixels = null;
    s.operations = [];
    s.pendingText = [];
    s.unmodeled = false;
    s.onResize?.();
  };
  const htmlSize = (canvas, key, fallback) => {
    const value = canvas.getAttribute(key);
    if (value === null || !/^\s*\d+\s*$/.test(value)) return fallback;
    const n = Number(value);
    return n <= 2147483647 ? n : fallback;
  };
  const sync = (s) => {
    if (typeof HTMLCanvasElement !== 'undefined' && s.canvas instanceof HTMLCanvasElement) {
      const w = htmlSize(s.canvas, 'width', 300),
        h = htmlSize(s.canvas, 'height', 150);
      if (w !== s.width || h !== s.height) resize(s, w, h);
    }
    return s;
  };
  const contextState = (context) => {
    const c = contexts.get(context);
    if (!c) throw new TypeError('Illegal invocation');
    sync(c.surface);
    return c;
  };
  const reportBoundary = (c, name) => {
    const reported = c.reported || (c.reported = new Set());
    if (!reported.has(name)) {
      reported.add(name);
      host.semanticMissingAt('canvas_state.js:23', 'Canvas2D.' + name);
    }
  };
  const record = (c, name, args) => {
    reportBoundary(c, name);
    c.surface.unmodeled = true;
    c.surface.operations.push({
      name,
      args: Array.from(args, (value) =>
        typeof value === 'number' ||
        typeof value === 'string' ||
        typeof value === 'boolean' ||
        value == null
          ? value
          : '[object]',
      ),
    });
    if (c.surface.operations.length > 4096) c.surface.operations.shift();
  };
  /* shared_canvas_colors */
  const color = (value, scheme = 'light') => {
    const wide = canvasWideColor(value);
    if (wide) return wide;
    const rgba = cssColorRGBA(value, scheme);
    return rgba ? rgba.slice(0, 3).concat(Math.round(rgba[3] * 255)) : null;
  };
  const canvasColorScheme = (surface) =>
    typeof HTMLCanvasElement !== 'undefined' && surface.canvas instanceof HTMLCanvasElement
      ? cssUsedColorScheme(surface.canvas)
      : 'light';
  const normalizedColor = (rgba) =>
    rgba[4]
      ? 'color(' +
        rgba[4] +
        ' ' +
        rgba
          .slice(0, 3)
          .map((v) => v / 255)
          .join(' ') +
        (rgba[3] === 255 ? '' : ' / ' + rgba[3] / 255) +
        ')'
      : rgba[3] === 255
        ? '#' +
          rgba
            .slice(0, 3)
            .map((v) => Math.round(v).toString(16).padStart(2, '0'))
            .join('')
        : `rgba(${rgba[0]}, ${rgba[1]}, ${rgba[2]}, ${rgba[3] / 255})`;
  class ImageData {
    constructor(data, width, height, settings = {}) {
      let pixels, w, h, format;
      if (
        data instanceof Uint8ClampedArray ||
        (typeof Float16Array === 'function' && data instanceof Float16Array)
      ) {
        pixels = data;
        format = data instanceof Uint8ClampedArray ? 'rgba-unorm8' : 'rgba-float16';
        w = uint(width);
        if (!pixels.length || pixels.length % 4)
          fail('InvalidStateError', 'The input data length is not a positive multiple of four');
        if (!w) fail('IndexSizeError', 'The source width is zero');
        h = height === undefined ? pixels.length / 4 / w : uint(height);
        if (!Number.isInteger(h) || !h || w * h * 4 !== pixels.length)
          fail('IndexSizeError', 'The input dimensions do not match the data');
        if (settings.pixelFormat !== undefined && settings.pixelFormat !== format)
          fail('InvalidStateError', 'Pixel format does not match the array type');
      } else {
        w = Number(data) >>> 0;
        h = Number(width) >>> 0;
        if (!w || !h || Number(data) < 0 || Number(width) < 0)
          fail('IndexSizeError', 'ImageData dimensions must be positive');
        settings = height || {};
        format = settings.pixelFormat || 'rgba-unorm8';
        pixels = canvasPixelArray(w, h, format);
      }
      const space = settings.colorSpace || 'srgb';
      if (!canvasColorSpaces.has(space)) throw new TypeError('Invalid color space');
      images.set(this, {
        data: pixels,
        width: w,
        height: h,
        colorSpace: space,
        pixelFormat: format,
      });
    }
    get data() {
      return images.get(this).data;
    }
    get width() {
      return images.get(this).width;
    }
    get height() {
      return images.get(this).height;
    }
    get colorSpace() {
      return images.get(this).colorSpace;
    }
    get pixelFormat() {
      if (!images.has(this)) throw new TypeError('Illegal invocation');
      return images.get(this).pixelFormat;
    }
  }
  class ImageBitmap {
    constructor(key, value) {
      if (key !== token) throw new TypeError('Illegal constructor');
      bitmaps.set(this, value);
    }
    get width() {
      return bitmaps.get(this).width;
    }
    get height() {
      return bitmaps.get(this).height;
    }
    close() {
      const b = bitmaps.get(this);
      b.width = 0;
      b.height = 0;
      b.pixels = null;
      b.closed = true;
    }
  }
  class CanvasGradient {
    constructor(key, kind, args) {
      if (key !== token) throw new TypeError('Illegal constructor');
      gradients.set(this, { kind, args, stops: [] });
    }
    addColorStop(offset, value) {
      offset = Number(offset);
      if (!Number.isFinite(offset) || offset < 0 || offset > 1)
        fail('IndexSizeError', 'Color stop offset is outside [0, 1]');
      const rgba = color(String(value));
      if (!rgba) fail('SyntaxError', 'Invalid color');
      gradients.get(this).stops.push({ offset, color: rgba });
    }
  }
  class CanvasPattern {
    constructor(key, image, repetition) {
      if (key !== token) throw new TypeError('Illegal constructor');
      patterns.set(this, { kind: 'pattern', image, repetition, matrix: [1, 0, 0, 1, 0, 0] });
    }
    setTransform(transform = {}) {
      const matrix = [
        transform.a ?? transform.m11 ?? 1,
        transform.b ?? transform.m12 ?? 0,
        transform.c ?? transform.m21 ?? 0,
        transform.d ?? transform.m22 ?? 1,
        transform.e ?? transform.m41 ?? 0,
        transform.f ?? transform.m42 ?? 0,
      ].map(Number);
      if (matrix.every(Number.isFinite)) patterns.get(this).matrix = matrix;
    }
  }
  const metricSlots = new WeakMap();
  class TextMetrics {
    constructor(key, data) {
      if (key !== token) throw new TypeError('Illegal constructor');
      metricSlots.set(this, data);
    }
  }
  const fontSize = (draw) => {
    const match = /(\d+(?:\.\d+)?)px(?:\s|\/|$)/.exec(draw.font);
    return match ? Number(match[1]) : 10;
  };
  const alignedX = (draw, width) =>
    draw.textAlign === 'center'
      ? -width / 2
      : draw.textAlign === 'right' ||
          (draw.textAlign === 'end' && draw.direction !== 'rtl') ||
          (draw.textAlign === 'start' && draw.direction === 'rtl')
        ? -width
        : 0;
  let hintBoundaryReported = false;
  // Rendering a color glyph must not consult author-overridable WebAssembly
  // globals or prototype methods during a later Canvas readback.
  let rasterModuleCtor, rasterInstanceCtor;
  const captureRasterIntrinsics = () => {
    rasterModuleCtor = globalThis.WebAssembly?.Module;
    rasterInstanceCtor = globalThis.WebAssembly?.Instance;
  };
  captureRasterIntrinsics();
  bootstrapRestoreHooks.push(captureRasterIntrinsics);
  const RasterUint8Array = Uint8Array,
    rasterSet = Function.prototype.call.bind(Uint8Array.prototype.set),
    rasterSlice = Function.prototype.call.bind(Uint8Array.prototype.slice),
    rasterMapGet = Function.prototype.call.bind(Map.prototype.get),
    rasterMapSet = Function.prototype.call.bind(Map.prototype.set),
    rasterMapClear = Function.prototype.call.bind(Map.prototype.clear);
  let colorRaster = null;
  const rasterFontIDs = new Map();
  const rasterTiles = new Map();
  let rasterTileBytes = 0;
  const rasterInstance = () => {
    if (colorRaster) return colorRaster;
    const bytes = host.canvasRasterModule();
    colorRaster = new rasterInstanceCtor(new rasterModuleCtor(bytes)).exports;
    return colorRaster;
  };
  const rasterFontID = (glyph) => {
    const raster = rasterInstance();
    let fontID = rasterMapGet(rasterFontIDs, glyph.rasterID);
    if (fontID === undefined) {
      const bytes = new RasterUint8Array(host.canvasRasterFont(glyph.rasterID)),
        pointer = raster.raster_alloc(bytes.length);
      if (!pointer) fail('NotSupportedError', 'Canvas color font exceeds the raster limit');
      rasterSet(new RasterUint8Array(raster.memory.buffer, pointer, bytes.length), bytes);
      fontID = raster.raster_register_font(pointer, bytes.length, glyph.rasterFace);
      if (fontID < 0) {
        raster.raster_free(pointer, bytes.length);
        fail('NotSupportedError', 'Canvas color font could not be loaded');
      }
      rasterMapSet(rasterFontIDs, glyph.rasterID, fontID);
    }
    return fontID;
  };
  const rasterGlyphTile = (run, glyph) => {
    const raster = rasterInstance();
    const fontID = rasterFontID(glyph);
    const strokeWidth = run.stroke && !glyph.color ? run.lineWidth : 0,
      m = run.matrix,
      transformScale = Math.max(Math.hypot(m[0], m[1]), Math.hypot(m[2], m[3])),
      margin = Math.ceil((strokeWidth * transformScale) / 2) + 2,
      localLeft = run.x + glyph.left * run.scale,
      localTop = run.y + glyph.top,
      localRight = run.x + glyph.right * run.scale,
      localBottom = run.y + glyph.bottom,
      corners = [
        [localLeft, localTop],
        [localRight, localTop],
        [localLeft, localBottom],
        [localRight, localBottom],
      ].map(([x, y]) => transformPoint(m, x, y)),
      left = Math.floor(Math.min(...corners.map((point) => point[0]))) - margin,
      top = Math.floor(Math.min(...corners.map((point) => point[1]))) - margin,
      right = Math.ceil(Math.max(...corners.map((point) => point[0]))) + margin,
      bottom = Math.ceil(Math.max(...corners.map((point) => point[1]))) + margin,
      width = right - left,
      height = bottom - top;
    if (width <= 0 || height <= 0 || width > 4096 || height > 4096)
      fail('NotSupportedError', 'Canvas color glyph exceeds the raster limit');
    const glyphX = run.x / run.scale + glyph.originX,
      glyphY = run.y + glyph.originY,
      affine = [m[0] * run.scale, m[1] * run.scale, m[2], m[3], m[4] - left, m[5] - top],
      key = `${fontID}:${glyph.glyphID}:${run.fontSize}:${glyphX}:${glyphY}:${affine.join(':')}:${strokeWidth}:${width}:${height}`;
    const cached = rasterMapGet(rasterTiles, key);
    if (cached) return { left, top, width, height, pixels: cached };
    const length = width * height * 4,
      pointer = raster.raster_alloc(length);
    if (!pointer) fail('NotSupportedError', 'Canvas color glyph exceeds the raster limit');
    try {
      const status = raster.raster_glyph(
        fontID,
        glyph.glyphID,
        run.fontSize,
        glyphX,
        glyphY,
        ...affine,
        strokeWidth,
        width,
        height,
        pointer,
      );
      if (status !== 0) fail('NotSupportedError', 'Canvas color glyph could not be rasterized');
      const pixels = rasterSlice(new RasterUint8Array(raster.memory.buffer, pointer, length));
      // Bound retained tiles independently of glyph dimensions. Large tiles
      // still render, but do not remain resident after the readback.
      if (length <= 1 << 20) {
        if (rasterTileBytes + length > 4 << 20) {
          rasterMapClear(rasterTiles);
          rasterTileBytes = 0;
        }
        rasterMapSet(rasterTiles, key, pixels);
        rasterTileBytes += length;
      }
      return {
        left,
        top,
        width,
        height,
        pixels,
      };
    } finally {
      raster.raster_free(pointer, length);
    }
  };
  const shapeCanvasText = (draw, text, drawing = false) => {
    text = text.replace(/[\t\n\r\f]/g, ' ');
    const match = /^(.*?)((?:\d+(?:\.\d+)?|\.\d+))px(?:\/[^\s]+)?\s+(.+)$/.exec(draw.font),
      size = fontSize(draw),
      prefix = match?.[1] || '',
      family = match?.[3] || 'sans-serif',
      weight = /\bbold\b/.test(prefix) ? 700 : Number(/\b([1-9]00)\b/.exec(prefix)?.[1] || 400),
      letter = parseFloat(draw.letterSpacing) || 0;
    // Blink shapes words independently: spaces must not introduce pair kerning
    // between the preceding/following words. Preserve source cluster offsets.
    let shaped = null,
      cluster = 0;
    for (const part of text.match(/ +|[^ ]+/g) || ['']) {
      const run = JSON.parse(
        host.shapeText(
          part,
          family,
          size,
          weight,
          Number(/\b(?:italic|oblique)\b/.test(prefix)),
          Number(draw.fontKerning === 'none'),
          Number(letter !== 0),
          1,
        ),
      );
      if (run.error) fail('NotSupportedError', run.error);
      if (run.inkApproximate && !hintBoundaryReported) {
        hintBoundaryReported = true;
        host.semanticMissingAt(
          'canvas_state.js/shapeCanvasText',
          'Canvas2D.fontHintingApproximation',
        );
      }
      if (!shaped) shaped = { ...run, glyphs: [] };
      for (const g of run.glyphs) shaped.glyphs.push({ ...g, cluster: g.cluster + cluster });
      cluster += Array.from(part).length;
    }
    const chars = Array.from(text),
      glyphs = [];
    let width = 0,
      left = Infinity,
      right = -Infinity,
      top = Infinity,
      bottom = -Infinity;
    for (const g of shaped.glyphs) {
      const x = width + g.xOffset,
        y = g.yOffset;
      if (g.ink) {
        left = Math.min(left, x + g.left);
        right = Math.max(right, x + g.right);
        top = Math.min(top, y + g.top);
        bottom = Math.max(bottom, y + g.bottom);
        glyphs.push({
          originX: x,
          originY: y,
          left: x + g.left,
          right: x + g.right,
          top: y + g.top,
          bottom: y + g.bottom,
          coverage: 96 + (chars[g.cluster].codePointAt(0) % 16) * 6,
          rasterID: g.rasterID,
          rasterFace: g.rasterFace,
          glyphID: g.glyphID,
          color: g.color,
        });
      }
      width +=
        g.advance +
        letter +
        (drawing && chars[g.cluster] === ' ' ? parseFloat(draw.wordSpacing) || 0 : 0);
    }
    const emTotal = shaped.emAscent + shaped.emDescent,
      emAscent = emTotal
        ? Math.floor(((size * shaped.emAscent) / emTotal) * 64) / 64
        : shaped.ascent,
      hanging = Math.fround(shaped.ascent * 0.8);
    const shift =
        draw.textBaseline === 'top'
          ? emAscent
          : draw.textBaseline === 'hanging'
            ? hanging
            : draw.textBaseline === 'middle'
              ? emAscent - size / 2
              : draw.textBaseline === 'bottom'
                ? emAscent - size
                : draw.textBaseline === 'ideographic'
                  ? -shaped.descent
                  : 0,
      x = alignedX(draw, width),
      ink = glyphs.length !== 0;
    return {
      glyphs,
      shift,
      metrics: {
        width,
        actualBoundingBoxLeft: ink ? -(x + left) : 0,
        actualBoundingBoxRight: ink ? x + right : 0,
        fontBoundingBoxAscent: shaped.ascent - shift,
        fontBoundingBoxDescent: shaped.descent + shift,
        actualBoundingBoxAscent: ink ? -top - shift : 0,
        actualBoundingBoxDescent: ink ? bottom + shift : 0,
        hangingBaseline: hanging - shift,
        alphabeticBaseline: -shift,
        ideographicBaseline: -shaped.descent - shift,
      },
    };
  };
  const textMetrics = (draw, text) => shapeCanvasText(draw, text).metrics;
  for (const name of [
    'width',
    'actualBoundingBoxLeft',
    'actualBoundingBoxRight',
    'fontBoundingBoxAscent',
    'fontBoundingBoxDescent',
    'actualBoundingBoxAscent',
    'actualBoundingBoxDescent',
    'hangingBaseline',
    'alphabeticBaseline',
    'ideographicBaseline',
  ])
    Object.defineProperty(TextMetrics.prototype, name, {
      get() {
        const data = metricSlots.get(this);
        if (!data) throw new TypeError('Illegal invocation');
        return data[name];
      },
      enumerable: true,
      configurable: true,
    });
  const queueText = (c, text, x, y, maxWidth, stroke) => {
    x = Number(x);
    y = Number(y);
    if (!Number.isFinite(x) || !Number.isFinite(y)) return;
    const d = c.draw,
      paint = paintSnapshot(
        stroke ? d.strokeStyle : d.fillStyle,
        c.surface.colorSpace || 'srgb',
        canvasColorScheme(c.surface),
        d.transform,
      );
    if (!paint) {
      record(c, stroke ? 'strokeText' : 'fillText', [text, x, y, maxWidth]);
      return;
    }
    const shaped = shapeCanvasText(d, text, true),
      width = shaped.metrics.width;
    let scale = 1;
    if (maxWidth !== undefined) {
      maxWidth = Number(maxWidth);
      if (!Number.isFinite(maxWidth) || maxWidth <= 0) return;
      if (maxWidth < width) scale = maxWidth / width;
    }
    x += alignedX(d, width * scale);
    y += shaped.shift;
    reportBoundary(c, 'approximateTextObservations');
    const pending = c.surface.pendingText || (c.surface.pendingText = []);
    pending.push({
      glyphs: shaped.glyphs,
      fontSize: fontSize(d),
      x,
      y,
      scale,
      matrix: d.transform.slice(),
      paint,
      alpha: d.globalAlpha,
      composite: d.globalCompositeOperation,
      clips: d.clipPaths || [],
      shadow: shadowSnapshot(d, c.surface, c.attributes.willReadFrequently),
      stroke,
      lineWidth: d.lineWidth,
    });
    if (pending.length >= 256) {
      ensurePixels(c.surface);
      materializeText(c.surface);
    }
    c.surface.unmodeled = true;
  };
  // Keep text in the same ordered surface queue as paths. Color glyphs use
  // their font paint graph; unsupported text modes retain the diagnosed model.
  const materializeText = (s) => {
    const runs = s.pendingText;
    if (!runs?.length) return;
    s.pendingText = [];
    const p = s.pixels;
    for (const run of runs) {
      if (run.kind === 'path') {
        materializePath(s, run);
        continue;
      }
      if (!supportedComposite.has(run.composite)) continue;
      const coverage = new Uint8Array(s.width * s.height),
        m = run.matrix,
        det = m[0] * m[3] - m[1] * m[2];
      if (!det) continue;
      const colorTiles = [];
      const maskTiles = [];
      let left = s.width,
        right = 0,
        top = s.height,
        bottom = 0;
      for (const glyph of run.glyphs) {
        if (glyph.rasterID && run.composite === 'source-over' && rasterModuleCtor !== undefined) {
          const tile = rasterGlyphTile(run, glyph);
          if (glyph.color) colorTiles.push(tile);
          else maskTiles.push(tile);
          continue;
        }
        const x = run.x + glyph.left * run.scale,
          y = run.y + glyph.top,
          w = (glyph.right - glyph.left) * run.scale,
          h = glyph.bottom - glyph.top;
        const points = [
            [x, y],
            [x + w, y],
            [x, y + h],
            [x + w, y + h],
          ].map(([a, b]) => transformPoint(m, a, b)),
          l = Math.max(0, Math.floor(Math.min(...points.map((p) => p[0])))),
          r = Math.min(s.width, Math.ceil(Math.max(...points.map((p) => p[0])))),
          t = Math.max(0, Math.floor(Math.min(...points.map((p) => p[1])))),
          b = Math.min(s.height, Math.ceil(Math.max(...points.map((p) => p[1]))));
        left = Math.min(left, l);
        right = Math.max(right, r);
        top = Math.min(top, t);
        bottom = Math.max(bottom, b);
        for (let yy = t; yy < b; yy++)
          for (let xx = l; xx < r; xx++) {
            const px = (m[3] * (xx + 0.5 - m[4]) - m[2] * (yy + 0.5 - m[5])) / det,
              py = (-m[1] * (xx + 0.5 - m[4]) + m[0] * (yy + 0.5 - m[5])) / det;
            if (px >= x && px < x + w && py >= y && py < y + h)
              coverage[yy * s.width + xx] = Math.max(coverage[yy * s.width + xx], glyph.coverage);
          }
      }
      for (const tile of maskTiles) {
        const l = Math.max(0, tile.left),
          r = Math.min(s.width, tile.left + tile.width),
          t = Math.max(0, tile.top),
          b = Math.min(s.height, tile.top + tile.height);
        left = Math.min(left, l);
        right = Math.max(right, r);
        top = Math.min(top, t);
        bottom = Math.max(bottom, b);
        for (let yy = t; yy < b; yy++)
          for (let xx = l; xx < r; xx++) {
            const source = ((yy - tile.top) * tile.width + xx - tile.left) * 4 + 3,
              target = yy * s.width + xx;
            coverage[target] = Math.max(coverage[target], tile.pixels[source]);
          }
      }
      if (run.shadow) {
        const source = new Float32Array(s.width * s.height);
        for (let yy = top; yy < bottom; yy++)
          for (let xx = left; xx < right; xx++) {
            const i = yy * s.width + xx;
            if (coverage[i])
              source[i] =
                (coverage[i] * paintColor(run.paint, xx + 0.5, yy + 0.5)[3]) / (255 * 255);
          }
        for (const tile of colorTiles)
          for (let yy = 0; yy < tile.height; yy++)
            for (let xx = 0; xx < tile.width; xx++) {
              const x = tile.left + xx,
                y = tile.top + yy;
              if (x < 0 || x >= s.width || y < 0 || y >= s.height) continue;
              const i = y * s.width + x,
                a = tile.pixels[(yy * tile.width + xx) * 4 + 3] / 255;
              source[i] = 1 - (1 - source[i]) * (1 - a);
            }
        materializeShadow(s, source, run.shadow, run.alpha, run.composite, run.clips);
      }
      for (const tile of colorTiles)
        for (let yy = 0; yy < tile.height; yy++)
          for (let xx = 0; xx < tile.width; xx++) {
            const source = (yy * tile.width + xx) * 4,
              a = tile.pixels[source + 3];
            // Skia's Windows color-glyph pipeline drops the final sub-8 alpha
            // coverage quantum. Vello retains those edge samples, which makes
            // the selected Chrome profile one pixel wider/taller on readback.
            if (a < 8) continue;
            const x = tile.left + xx,
              y = tile.top + yy;
            if (x < 0 || x >= s.width || y < 0 || y >= s.height) continue;
            if (!clipContains(run.clips, x + 0.5, y + 0.5)) continue;
            compositePixel(
              p,
              (y * s.width + x) * 4,
              [
                Math.min(255, Math.round((tile.pixels[source] * 255) / a)),
                Math.min(255, Math.round((tile.pixels[source + 1] * 255) / a)),
                Math.min(255, Math.round((tile.pixels[source + 2] * 255) / a)),
                a,
              ],
              run.alpha,
              run.composite,
              s.opaque,
            );
          }
      if (
        ['copy', 'source-in', 'source-out', 'destination-in', 'destination-atop'].includes(
          run.composite,
        )
      ) {
        left = 0;
        top = 0;
        right = s.width;
        bottom = s.height;
      }
      for (let y = top; y < bottom; y++)
        for (let x = left; x < right; x++) {
          if (!clipContains(run.clips, x + 0.5, y + 0.5)) continue;
          compositePixel(
            p,
            (y * s.width + x) * 4,
            paintColor(run.paint, x + 0.5, y + 0.5),
            (run.alpha * coverage[y * s.width + x]) / 255,
            run.composite,
            s.opaque,
          );
        }
    }
  };
  const multiply = (a, b) => [
    a[0] * b[0] + a[2] * b[1],
    a[1] * b[0] + a[3] * b[1],
    a[0] * b[2] + a[2] * b[3],
    a[1] * b[2] + a[3] * b[3],
    a[0] * b[4] + a[2] * b[5] + a[4],
    a[1] * b[4] + a[3] * b[5] + a[5],
  ];
  /* shared_canvas_paths */
  const fillRectangle = (c, x, y, w, h, clear) => {
    const a = [x, y, w, h].map(Number);
    if (!a.every(Number.isFinite) || !a[2] || !a[3]) return;
    const d = c.draw,
      left = Math.min(a[0], a[0] + a[2]),
      right = Math.max(a[0], a[0] + a[2]),
      top = Math.min(a[1], a[1] + a[3]),
      bottom = Math.max(a[1], a[1] + a[3]);
    // Animation loops conventionally clear the entire backing store before
    // drawing the next frame. The clear supersedes every deferred path; keeping
    // those unreachable prior-frame observations grows without bound.
    if (
      clear &&
      d.transform.every((value, index) => value === [1, 0, 0, 1, 0, 0][index]) &&
      !d.clipPaths?.length &&
      left <= 0 &&
      top <= 0 &&
      right >= c.surface.width &&
      bottom >= c.surface.height
    ) {
      c.surface.pendingText = [];
      c.surface.operations = [];
      c.surface.unmodeled = false;
      if (c.surface.pixels) {
        c.surface.pixels.fill(0);
        if (c.surface.opaque)
          for (let i = 3; i < c.surface.pixels.length; i += 4) c.surface.pixels[i] = 255;
      }
      return;
    }
    queuePath(c, rectanglePath(c, ...a), 'nonzero', false, clear);
  };
  class CanvasRenderingContext2D {
    constructor(key, s, attributes = {}) {
      if (key !== token) throw new TypeError('Illegal constructor');
      const colorSpace = attributes.colorSpace || 'srgb',
        colorType = attributes.colorType || 'unorm8';
      if (!canvasColorSpaces.has(colorSpace) || !['unorm8', 'float16'].includes(colorType))
        throw new TypeError('Invalid canvas color configuration');
      if (colorType === 'float16' && typeof Float16Array !== 'function')
        fail('NotSupportedError', 'Float16 typed arrays are unavailable in this engine');
      s.colorSpace = colorSpace;
      s.colorType = colorType;
      contexts.set(this, {
        surface: s,
        draw: defaults(),
        stack: [],
        attributes: {
          alpha: attributes.alpha !== false,
          colorSpace,
          colorType,
          toneMapping: { mode: attributes.toneMapping?.mode || 'standard' },
          desynchronized: !!attributes.desynchronized,
          willReadFrequently: !!attributes.willReadFrequently,
        },
      });
      s.opaque = attributes.alpha === false;
      s.onResize = () => {
        const c = contexts.get(this);
        c.draw = defaults();
        c.stack = [];
        c.path = [];
      };
    }
    get canvas() {
      return contextState(this).surface.canvas;
    }
    getContextAttributes() {
      const a = contextState(this).attributes;
      return { ...a, toneMapping: { ...a.toneMapping } };
    }
    isContextLost() {
      contextState(this);
      return false;
    }
    save() {
      const c = contextState(this);
      c.stack.push({ ...c.draw, transform: c.draw.transform.slice(), dash: c.draw.dash.slice() });
    }
    restore() {
      const c = contextState(this);
      if (c.stack.length) c.draw = c.stack.pop();
    }
    reset() {
      const c = contextState(this);
      resize(c.surface, c.surface.width, c.surface.height);
    }
    getTransform() {
      const a = contextState(this).draw.transform;
      return typeof DOMMatrix === 'function'
        ? new DOMMatrix(a)
        : { a: a[0], b: a[1], c: a[2], d: a[3], e: a[4], f: a[5] };
    }
    resetTransform() {
      contextState(this).draw.transform = [1, 0, 0, 1, 0, 0];
    }
    setTransform(...args) {
      const c = contextState(this);
      if (!args.length) {
        this.resetTransform();
        return;
      }
      let a = args;
      if (args.length === 1) {
        const o = args[0] || {};
        a = [
          o.a ?? o.m11 ?? 1,
          o.b ?? o.m12 ?? 0,
          o.c ?? o.m21 ?? 0,
          o.d ?? o.m22 ?? 1,
          o.e ?? o.m41 ?? 0,
          o.f ?? o.m42 ?? 0,
        ];
      }
      a = a.map(Number);
      if (a.length === 6 && a.every(Number.isFinite)) c.draw.transform = a;
    }
    transform(...a) {
      a = a.map(Number);
      if (a.length === 6 && a.every(Number.isFinite)) {
        const c = contextState(this);
        c.draw.transform = multiply(c.draw.transform, a);
      }
    }
    translate(x, y) {
      this.transform(1, 0, 0, 1, x, y);
    }
    scale(x, y) {
      this.transform(x, 0, 0, y, 0, 0);
    }
    rotate(angle) {
      angle = Number(angle);
      this.transform(Math.cos(angle), Math.sin(angle), -Math.sin(angle), Math.cos(angle), 0, 0);
    }
    setLineDash(value) {
      const c = contextState(this),
        a = Array.from(value, Number);
      if (a.some((v) => !Number.isFinite(v) || v < 0)) return;
      c.draw.dash = a.length % 2 ? a.concat(a) : a;
    }
    getLineDash() {
      return contextState(this).draw.dash.slice();
    }
    clearRect(x, y, w, h) {
      fillRectangle(contextState(this), x, y, w, h, true);
    }
    fillRect(x, y, w, h) {
      fillRectangle(contextState(this), x, y, w, h, false);
    }
    createImageData(width, height, settings = {}) {
      const c = contextState(this);
      if (images.has(width)) {
        const d = images.get(width);
        return new ImageData(d.width, d.height, {
          colorSpace: d.colorSpace,
          pixelFormat: d.pixelFormat,
        });
      }
      return new ImageData(Math.abs(Number(width)), Math.abs(Number(height)), {
        colorSpace: settings.colorSpace || c.surface.colorSpace,
        pixelFormat: settings.pixelFormat || 'rgba-unorm8',
      });
    }
    getImageData(x, y, width, height, settings = {}) {
      const c = contextState(this),
        s = c.surface;
      if (s.originClean === false) fail('SecurityError', 'Canvas is not origin-clean');
      [x, y, width, height] = [x, y, width, height].map((v) => Math.trunc(Number(v)));
      if (!width)
        fail(
          'IndexSizeError',
          "Failed to execute 'getImageData' on 'CanvasRenderingContext2D': The source width is 0.",
        );
      if (!height)
        fail(
          'IndexSizeError',
          "Failed to execute 'getImageData' on 'CanvasRenderingContext2D': The source height is 0.",
        );
      if (width < 0) {
        x += width;
        width = -width;
      }
      if (height < 0) {
        y += height;
        height = -height;
      }
      const space = settings.colorSpace || s.colorSpace || 'srgb',
        format = settings.pixelFormat || 'rgba-unorm8',
        out = new ImageData(width, height, { colorSpace: space, pixelFormat: format }),
        p = buffer(s),
        scale = format === 'rgba-float16' ? 1 / 255 : 1;
      for (let yy = 0; yy < height; yy++)
        for (let xx = 0; xx < width; xx++) {
          const px = x + xx,
            py = y + yy;
          if (px < 0 || py < 0 || px >= s.width || py >= s.height) continue;
          const i = (py * s.width + px) * 4,
            j = (yy * width + xx) * 4,
            a = p[i + 3],
            rgba = canvasConvertColor(
              [
                a ? (p[i] * 255) / a : 0,
                a ? (p[i + 1] * 255) / a : 0,
                a ? (p[i + 2] * 255) / a : 0,
                a,
                s.colorSpace || 'srgb',
              ],
              space,
            );
          for (let k = 0; k < 4; k++)
            out.data[j + k] = format === 'rgba-float16' ? rgba[k] * scale : Math.round(rgba[k]);
        }
      return out;
    }
    putImageData(image, x, y, ...dirty) {
      const c = contextState(this),
        s = c.surface,
        d = images.get(image);
      if (!d) throw new TypeError('Expected ImageData');
      x = Math.trunc(Number(x));
      y = Math.trunc(Number(y));
      if (!Number.isFinite(x) || !Number.isFinite(y)) throw new TypeError('Invalid coordinate');
      let [dx, dy, dw, dh] = dirty.length ? dirty.map(Number) : [0, 0, d.width, d.height];
      if (dw < 0) {
        dx += dw;
        dw = -dw;
      }
      if (dh < 0) {
        dy += dh;
        dh = -dh;
      }
      const p = buffer(s),
        scale = d.pixelFormat === 'rgba-float16' ? 255 : 1;
      for (let yy = Math.max(0, Math.trunc(dy)); yy < Math.min(d.height, dy + dh); yy++)
        for (let xx = Math.max(0, Math.trunc(dx)); xx < Math.min(d.width, dx + dw); xx++) {
          const tx = x + xx,
            ty = y + yy;
          if (tx < 0 || ty < 0 || tx >= s.width || ty >= s.height) continue;
          const i = (yy * d.width + xx) * 4,
            j = (ty * s.width + tx) * 4,
            rgba = canvasConvertColor(
              [
                d.data[i] * scale,
                d.data[i + 1] * scale,
                d.data[i + 2] * scale,
                d.data[i + 3] * scale,
                d.colorSpace,
              ],
              s.colorSpace || 'srgb',
            ),
            a = rgba[3];
          canvasStore(p, j + 3, s.opaque ? 255 : a);
          for (let k = 0; k < 3; k++) canvasStore(p, j + k, (rgba[k] * a) / 255);
        }
    }
    drawImage(source, ...args) {
      const c = contextState(this),
        destination = c.surface;
      let image;
      if (bitmaps.has(source)) {
        image = bitmaps.get(source);
        if (image.closed) fail('InvalidStateError', 'ImageBitmap is closed');
      } else if (
        canvases.has(source) ||
        (typeof HTMLCanvasElement !== 'undefined' && source instanceof HTMLCanvasElement)
      ) {
        const ss = sync(state(source));
        image = {
          width: ss.width,
          height: ss.height,
          pixels: buffer(ss).slice(),
          colorSpace: ss.colorSpace || 'srgb',
          unmodeled: ss.unmodeled,
          originClean: ss.originClean,
        };
      } else if (typeof HTMLVideoElement !== 'undefined' && source instanceof HTMLVideoElement) {
        image = cameraCaptureModel.frame(source);
        if (!image) return;
      } else if (typeof HTMLImageElement !== 'undefined' && source instanceof HTMLImageElement) {
        image = sourceImage(source);
        if (!image) {
          if (source.complete) fail('InvalidStateError', 'Image is broken');
          return;
        }
      } else {
        record(c, 'drawImage', args);
        return;
      }
      let sx = 0,
        sy = 0,
        sw = image.width,
        sh = image.height,
        dx,
        dy,
        dw = sw,
        dh = sh;
      if (args.length === 2) [dx, dy] = args;
      else if (args.length === 4) [dx, dy, dw, dh] = args;
      else if (args.length === 8) [sx, sy, sw, sh, dx, dy, dw, dh] = args;
      else throw new TypeError('Invalid drawImage arguments');
      [sx, sy, sw, sh, dx, dy, dw, dh] = [sx, sy, sw, sh, dx, dy, dw, dh].map(Number);
      if (![sx, sy, sw, sh, dx, dy, dw, dh].every(Number.isFinite) || !sw || !sh || !dw || !dh)
        return;
      if (image.unavailable) return;
      if (image.originClean === false) destination.originClean = false;
      // Negative dimensions grow in the opposite direction; they do not flip
      // the image. Clip the source rectangle before sampling and project the
      // clipped interval back into destination space, as Skia does.
      if (sw < 0) {
        sx += sw;
        sw = -sw;
      }
      if (sh < 0) {
        sy += sh;
        sh = -sh;
      }
      if (dw < 0) {
        dx += dw;
        dw = -dw;
      }
      if (dh < 0) {
        dy += dh;
        dh = -dh;
      }
      const sourceLeft = Math.max(0, sx),
        sourceTop = Math.max(0, sy),
        sourceRight = Math.min(image.width, sx + sw),
        sourceBottom = Math.min(image.height, sy + sh);
      if (sourceLeft >= sourceRight || sourceTop >= sourceBottom) return;
      const sourceScaleX = dw / sw,
        sourceScaleY = dh / sh;
      dx += (sourceLeft - sx) * sourceScaleX;
      dy += (sourceTop - sy) * sourceScaleY;
      dw = (sourceRight - sourceLeft) * sourceScaleX;
      dh = (sourceBottom - sourceTop) * sourceScaleY;
      sx = sourceLeft;
      sy = sourceTop;
      sw = sourceRight - sourceLeft;
      sh = sourceBottom - sourceTop;
      const m = c.draw.transform;
      if (
        m[1] ||
        m[2] ||
        m[0] < 0 ||
        m[3] < 0 ||
        !supportedComposite.has(c.draw.globalCompositeOperation)
      ) {
        record(c, 'drawImage', args);
        return;
      }
      dx = dx * m[0] + m[4];
      dy = dy * m[3] + m[5];
      dw *= m[0];
      dh *= m[3]; // Chrome's image sampling projection for a P3 float surface passes through
      // an RGBA8 snapshot; the source surface itself retains extended precision.
      const p = buffer(destination),
        input =
          image.colorSpace === 'display-p3' && image.pixels instanceof Float32Array
            ? new Uint8ClampedArray(image.pixels)
            : image.pixels.slice(),
        ga = c.draw.globalAlpha;
      const grayscale = /^grayscale\(\s*((?:\d+(?:\.\d*)?|\.\d+))(%)?\s*\)$/.exec(c.draw.filter);
      const grayscaleAmount = grayscale
        ? Math.min(1, Number(grayscale[1]) / (grayscale[2] ? 100 : 1))
        : 0;
      const directOpaque =
        p instanceof Uint8ClampedArray &&
        input instanceof Uint8ClampedArray &&
        ga === 1 &&
        !grayscaleAmount &&
        (image.colorSpace || 'srgb') === (destination.colorSpace || 'srgb') &&
        (c.draw.globalCompositeOperation === 'source-over' ||
          c.draw.globalCompositeOperation === 'copy');
      if (c.draw.globalCompositeOperation === 'copy')
        for (let clearY = 0; clearY < destination.height; clearY++)
          for (let clearX = 0; clearX < destination.width; clearX++) {
            if (!clipContains(c.draw.clipPaths, clearX + 0.5, clearY + 0.5)) continue;
            const clearOffset = (clearY * destination.width + clearX) * 4;
            p[clearOffset] = p[clearOffset + 1] = p[clearOffset + 2] = 0;
            p[clearOffset + 3] = destination.opaque ? 255 : 0;
          }
      const smooth = c.draw.imageSmoothingEnabled,
        smoothingQuality = c.draw.imageSmoothingQuality,
        // Filtering may read neighboring texels outside the selected source
        // rectangle. Only the image boundary clamps those taps.
        sourceMinX = 0,
        sourceMaxX = image.width - 1,
        sourceMinY = 0,
        sourceMaxY = image.height - 1,
        cubicWeight = (distance) => {
          const x = Math.abs(distance);
          if (x < 1) return (7 * x * x * x - 12 * x * x + 16 / 3) / 6;
          if (x < 2) return ((-7 / 3) * x * x * x + 12 * x * x - 20 * x + 32 / 3) / 6;
          return 0;
        },
        sample = (u, v) => {
          if (!smooth) {
            const ix = Math.max(sourceMinX, Math.min(sourceMaxX, Math.floor(u + 0.5))),
              iy = Math.max(sourceMinY, Math.min(sourceMaxY, Math.floor(v + 0.5))),
              i = (iy * image.width + ix) * 4;
            return [input[i], input[i + 1], input[i + 2], input[i + 3]];
          }
          if (smoothingQuality === 'high' && dw >= sw && dh >= sh) {
            const sourceX = Math.floor(u),
              sourceY = Math.floor(v),
              sampled = [0, 0, 0, 0];
            let totalWeight = 0;
            for (let row = sourceY - 1; row <= sourceY + 2; row++)
              for (let column = sourceX - 1; column <= sourceX + 2; column++) {
                const weight = cubicWeight(u - column) * cubicWeight(v - row),
                  ix = Math.max(sourceMinX, Math.min(sourceMaxX, column)),
                  iy = Math.max(sourceMinY, Math.min(sourceMaxY, row)),
                  offset = (iy * image.width + ix) * 4;
                totalWeight += weight;
                for (let channel = 0; channel < 4; channel++)
                  sampled[channel] += input[offset + channel] * weight;
              }
            for (let channel = 0; channel < 4; channel++)
              sampled[channel] = Math.max(0, Math.min(255, sampled[channel] / totalWeight));
            return sampled;
          }
          if (smoothingQuality !== 'low' && (dw < sw || dh < sh)) {
            const footprintX = Math.max(1, sw / dw),
              footprintY = Math.max(1, sh / dh),
              left = u - footprintX / 2,
              right = u + footprintX / 2,
              top = v - footprintY / 2,
              bottom = v + footprintY / 2,
              sampled = [0, 0, 0, 0];
            let totalWeight = 0;
            for (let row = Math.floor(top - 0.5); row <= Math.ceil(bottom + 0.5); row++)
              for (
                let column = Math.floor(left - 0.5);
                column <= Math.ceil(right + 0.5);
                column++
              ) {
                const weightX = Math.max(
                    0,
                    Math.min(right, column + 0.5) - Math.max(left, column - 0.5),
                  ),
                  weightY = Math.max(0, Math.min(bottom, row + 0.5) - Math.max(top, row - 0.5)),
                  weight = weightX * weightY;
                if (!weight) continue;
                const ix = Math.max(sourceMinX, Math.min(sourceMaxX, column)),
                  iy = Math.max(sourceMinY, Math.min(sourceMaxY, row)),
                  offset = (iy * image.width + ix) * 4;
                totalWeight += weight;
                for (let channel = 0; channel < 4; channel++)
                  sampled[channel] += input[offset + channel] * weight;
              }
            for (let channel = 0; channel < 4; channel++) sampled[channel] /= totalWeight;
            return sampled;
          }
          const sourceX0 = Math.floor(u),
            sourceY0 = Math.floor(v),
            x0 = Math.max(sourceMinX, Math.min(sourceMaxX, sourceX0)),
            x1 = Math.max(sourceMinX, Math.min(sourceMaxX, sourceX0 + 1)),
            y0 = Math.max(sourceMinY, Math.min(sourceMaxY, sourceY0)),
            y1 = Math.max(sourceMinY, Math.min(sourceMaxY, sourceY0 + 1)),
            fx = Math.max(0, Math.min(1, u - sourceX0)),
            fy = Math.max(0, Math.min(1, v - sourceY0)),
            topLeft = (y0 * image.width + x0) * 4,
            topRight = (y0 * image.width + x1) * 4,
            bottomLeft = (y1 * image.width + x0) * 4,
            bottomRight = (y1 * image.width + x1) * 4,
            sampled = [];
          for (let channel = 0; channel < 4; channel++)
            sampled[channel] =
              (input[topLeft + channel] * (1 - fx) + input[topRight + channel] * fx) * (1 - fy) +
              (input[bottomLeft + channel] * (1 - fx) + input[bottomRight + channel] * fx) * fy;
          return sampled;
        };
      for (
        let yy = Math.max(0, Math.floor(dy + 0.5));
        yy < Math.min(destination.height, Math.floor(dy + dh + 0.5));
        yy++
      )
        for (
          let xx = Math.max(0, Math.floor(dx + 0.5));
          xx < Math.min(destination.width, Math.floor(dx + dw + 0.5));
          xx++
        ) {
          if (!clipContains(c.draw.clipPaths, xx + 0.5, yy + 0.5)) continue;
          const u = sx + ((xx + 0.5 - dx) * sw) / dw - 0.5,
            v = sy + ((yy + 0.5 - dy) * sh) / dh - 0.5,
            sampled = sample(u, v),
            j = (yy * destination.width + xx) * 4,
            ia = sampled[3];
          if (directOpaque && ia === 255) {
            p[j] = sampled[0];
            p[j + 1] = sampled[1];
            p[j + 2] = sampled[2];
            p[j + 3] = 255;
            continue;
          }
          const rgba = canvasConvertColor(
            [
              ia ? (sampled[0] * 255) / ia : 0,
              ia ? (sampled[1] * 255) / ia : 0,
              ia ? (sampled[2] * 255) / ia : 0,
              ia,
              image.colorSpace || 'srgb',
            ],
            destination.colorSpace || 'srgb',
          );
          if (grayscaleAmount) {
            const gray = rgba[0] * 0.2126 + rgba[1] * 0.7152 + rgba[2] * 0.0722;
            for (let k = 0; k < 3; k++) rgba[k] += (gray - rgba[k]) * grayscaleAmount;
          }
          compositePixel(p, j, rgba, ga, c.draw.globalCompositeOperation, destination.opaque);
        }
      destination.unmodeled ||= image.unmodeled;
    }
    createLinearGradient(...args) {
      const c = contextState(this),
        gradient = new CanvasGradient(token, 'linear', args.map(Number));
      gradients.get(gradient).matrix = c.draw.transform.slice();
      return gradient;
    }
    createRadialGradient(...args) {
      const c = contextState(this);
      if (Number(args[2]) < 0 || Number(args[5]) < 0) fail('IndexSizeError', 'Negative radius');
      const gradient = new CanvasGradient(token, 'radial', args.map(Number));
      gradients.get(gradient).matrix = c.draw.transform.slice();
      return gradient;
    }
    createConicGradient(startAngle, x, y) {
      const c = contextState(this);
      if (arguments.length < 3)
        throw new TypeError(
          `Failed to execute 'createConicGradient' on 'CanvasRenderingContext2D': 3 arguments required, but only ${arguments.length} present.`,
        );
      const args = [startAngle, x, y].map(Number);
      if (args.some((value) => !Number.isFinite(value)))
        throw new TypeError(
          "Failed to execute 'createConicGradient' on 'CanvasRenderingContext2D': The provided double value is non-finite.",
        );
      const gradient = new CanvasGradient(token, 'conic', args);
      gradients.get(gradient).matrix = c.draw.transform.slice();
      return gradient;
    }
    createPattern(source, repetition) {
      contextState(this);
      repetition = repetition == null || repetition === '' ? 'repeat' : String(repetition);
      if (!['repeat', 'repeat-x', 'repeat-y', 'no-repeat'].includes(repetition))
        fail(
          'SyntaxError',
          `Failed to execute 'createPattern' on 'CanvasRenderingContext2D': The provided type ('${repetition}') is not one of 'repeat', 'no-repeat', 'repeat-x', or 'repeat-y'.`,
        );
      let image;
      if (bitmaps.has(source)) {
        image = bitmaps.get(source);
        if (image.closed) fail('InvalidStateError', 'ImageBitmap is closed');
      } else if (
        canvases.has(source) ||
        (typeof HTMLCanvasElement !== 'undefined' && source instanceof HTMLCanvasElement)
      ) {
        const surface = sync(state(source));
        if (!surface.width || !surface.height)
          fail(
            'InvalidStateError',
            "Failed to execute 'createPattern' on 'CanvasRenderingContext2D': The image argument is a canvas element with a width or height of 0.",
          );
        image = {
          width: surface.width,
          height: surface.height,
          pixels: buffer(surface),
          colorSpace: surface.colorSpace || 'srgb',
          originClean: surface.originClean,
        };
      } else if (typeof HTMLImageElement !== 'undefined' && source instanceof HTMLImageElement) {
        image = sourceImage(source);
        if (!image) return null;
      } else throw new TypeError('Invalid canvas pattern source');
      if (!image.pixels) return null;
      return new CanvasPattern(token, { ...image, pixels: image.pixels.slice() }, repetition);
    }
    drawFocusIfNeeded(element, maybeElement) {
      const c = contextState(this);
      if (!arguments.length)
        throw new TypeError(
          "Failed to execute 'drawFocusIfNeeded' on 'CanvasRenderingContext2D': 1 argument required, but only 0 present.",
        );
      if (!(element instanceof Element)) {
        if (arguments.length < 2)
          throw new TypeError(
            "Failed to execute 'drawFocusIfNeeded' on 'CanvasRenderingContext2D': parameter 1 is not of type 'Element'.",
          );
        if (!(element instanceof Path2D))
          throw new TypeError(
            "Failed to execute 'drawFocusIfNeeded' on 'CanvasRenderingContext2D': parameter 1 is not of type 'Path2D'.",
          );
        if (!(maybeElement instanceof Element))
          throw new TypeError(
            "Failed to execute 'drawFocusIfNeeded' on 'CanvasRenderingContext2D': parameter 2 is not of type 'Element'.",
          );
        element = maybeElement;
        if (document.activeElement === element && c.surface.canvas.contains(element))
          fail('NotSupportedError', 'Path2D focus-ring geometry is not implemented');
        return;
      }
      if (document.activeElement !== element || !c.surface.canvas.contains(element)) return;
      reportBoundary(c, 'approximateFocusRing');
      const original = c.draw;
      c.draw = {
        ...original,
        strokeStyle: '#101010',
        lineWidth: 6,
        globalAlpha: 1,
        globalCompositeOperation: 'source-over',
        transform: [1, 0, 0, 1, 0, 0],
      };
      try {
        queuePath(c, c.path || [], 'nonzero', true);
      } finally {
        c.draw = original;
      }
    }
    measureText(text) {
      const c = contextState(this);
      return new TextMetrics(token, textMetrics(c.draw, String(text)));
    }
    fillText(text, x, y, maxWidth) {
      queueText(contextState(this), String(text), x, y, maxWidth, false);
    }
    strokeText(text, x, y, maxWidth) {
      queueText(contextState(this), String(text), x, y, maxWidth, true);
    }
  }
  class OffscreenCanvasRenderingContext2D {
    constructor(key, s, attributes) {
      const context = new CanvasRenderingContext2D(key, s, attributes);
      Object.setPrototypeOf(context, OffscreenCanvasRenderingContext2D.prototype);
      return context;
    }
  }
  for (const key of Object.keys(defaults()).filter((k) => k !== 'transform' && k !== 'dash'))
    Object.defineProperty(CanvasRenderingContext2D.prototype, key, {
      get() {
        return contextState(this).draw[key];
      },
      set(value) {
        const c = contextState(this);
        if (key === 'font') {
          const parsed = serializeCanvasFont(String(value));
          if (parsed === null) return;
          c.draw[key] = parsed;
          return;
        }
        if (key === 'letterSpacing' || key === 'wordSpacing') {
          value = String(value);
          if (!/^-?(?:\d+(?:\.\d+)?|\.\d+)px$/.test(value)) return;
          c.draw[key] = value;
          return;
        }
        const allowed = {
          textAlign: ['start', 'end', 'left', 'right', 'center'],
          textBaseline: ['top', 'hanging', 'middle', 'alphabetic', 'ideographic', 'bottom'],
          direction: ['ltr', 'rtl', 'inherit'],
          lineCap: ['butt', 'round', 'square'],
          lineJoin: ['round', 'bevel', 'miter'],
        };
        if (allowed[key] && !allowed[key].includes(String(value))) return;
        if (key === 'fillStyle' || key === 'strokeStyle' || key === 'shadowColor') {
          if ((gradients.has(value) || patterns.has(value)) && key !== 'shadowColor') {
            c.draw[key] = value;
            return;
          }
          const rgba = color(String(value));
          if (rgba) {
            c.draw[key] = normalizedColor(rgba);
            if (rgba[4] || c.surface.colorSpace === 'display-p3')
              reportBoundary(c, 'colorConversionPrecision');
          }
          return;
        }
        if (typeof c.draw[key] === 'number') {
          value = Number(value);
          if (
            !Number.isFinite(value) ||
            (key === 'globalAlpha' && (value < 0 || value > 1)) ||
            (['lineWidth', 'miterLimit'].includes(key) && value <= 0) ||
            (key === 'shadowBlur' && value < 0)
          )
            return;
          if (key === 'shadowBlur') value = Math.fround(Math.min(value, 3.4028234663852886e38));
        } else if (typeof c.draw[key] === 'boolean') value = !!value;
        else value = String(value);
        c.draw[key] = value;
      },
      enumerable: true,
      configurable: true,
    });
  for (const name of [
    'beginPath',
    'closePath',
    'moveTo',
    'lineTo',
    'bezierCurveTo',
    'quadraticCurveTo',
    'arc',
    'arcTo',
    'ellipse',
    'rect',
    'roundRect',
    'clip',
    'fill',
    'stroke',
    'strokeRect',
  ])
    Object.defineProperty(CanvasRenderingContext2D.prototype, name, {
      value: {
        [name](...args) {
          record(contextState(this), name, args);
        },
      }[name],
      writable: true,
      enumerable: true,
      configurable: true,
    });
  installPathObservations(CanvasRenderingContext2D.prototype);
  for (const key of Reflect.ownKeys(CanvasRenderingContext2D.prototype))
    if (key !== 'constructor')
      Object.defineProperty(
        OffscreenCanvasRenderingContext2D.prototype,
        key,
        Object.getOwnPropertyDescriptor(CanvasRenderingContext2D.prototype, key),
      );
  const getContext = (canvas, kind, attributes) => {
    const s = sync(state(canvas));
    kind = String(kind);
    if (
      typeof ensureLazyDomain !== 'undefined' &&
      (kind === 'webgl' || kind === 'webgl2' || kind === 'experimental-webgl')
    )
      ensureLazyDomain('webgl');
    const entry = factories.get(kind);
    if (!entry) return null;
    if (s.kind) return s.kind === entry.canonical ? s.context : null;
    const context = entry.create(canvas, attributes || {}, s);
    if (context) {
      s.kind = entry.canonical;
      s.context = context;
    }
    return context;
  };
  class OffscreenCanvas extends EventTarget {
    constructor(width, height) {
      super();
      if (arguments.length < 2) throw new TypeError('Two dimensions are required');
      fresh(this, uint(width), uint(height));
    }
    get width() {
      return state(this).width;
    }
    set width(value) {
      const s = state(this);
      resize(s, uint(value), s.height);
    }
    get height() {
      return state(this).height;
    }
    set height(value) {
      const s = state(this);
      resize(s, s.width, uint(value));
    }
    getContext(kind, attributes) {
      return getContext(this, kind, attributes);
    }
    transferToImageBitmap() {
      const s = state(this);
      if (!s.kind) fail('InvalidStateError', 'Canvas has no context');
      if (!s.width || !s.height) fail('InvalidStateError', 'Canvas has zero dimensions');
      const bitmap = new ImageBitmap(token, {
        width: s.width,
        height: s.height,
        pixels: buffer(s).slice(),
        colorSpace: s.colorSpace || 'srgb',
        closed: false,
        unmodeled: s.unmodeled,
        originClean: s.originClean,
      });
      s.pixels = null;
      s.operations = [];
      s.pendingText = [];
      s.unmodeled = false;
      s.onTransfer?.();
      return bitmap;
    }
    async convertToBlob(options = {}) {
      const requested =
        options.type === undefined || String(options.type).trim() === ''
          ? 'image/png'
          : String(options.type).trim().toLowerCase();
      if (requested !== 'image/png')
        fail('NotSupportedError', requested + ' canvas serialization is not implemented');
      const bytes = pngBytes(this);
      if (bytes === null)
        fail(
          'IndexSizeError',
          "Failed to execute 'convertToBlob' on 'OffscreenCanvas': The size of the OffscreenCanvas is zero.",
        );
      return new Blob([bytes], { type: 'image/png' });
    }
  }
  factories.set('2d', {
    canonical: '2d',
    create: (canvas, attributes, s) =>
      new (canvas instanceof OffscreenCanvas
        ? OffscreenCanvasRenderingContext2D
        : CanvasRenderingContext2D)(token, s, attributes),
  });
  const createImageBitmap = async (source, ...args) => {
    let b;
    if (bitmaps.has(source)) {
      const a = bitmaps.get(source);
      if (a.closed) fail('InvalidStateError', 'ImageBitmap is closed');
      b = { ...a, pixels: a.pixels.slice() };
    } else if (
      canvases.has(source) ||
      (typeof HTMLCanvasElement !== 'undefined' && source instanceof HTMLCanvasElement)
    ) {
      const s = sync(state(source));
      b = {
        width: s.width,
        height: s.height,
        pixels: buffer(s).slice(),
        colorSpace: s.colorSpace || 'srgb',
        closed: false,
        unmodeled: s.unmodeled,
        originClean: s.originClean,
      };
    } else if (typeof HTMLVideoElement !== 'undefined' && source instanceof HTMLVideoElement) {
      b = cameraCaptureModel.frame(source);
      if (!b) fail('InvalidStateError', 'Video has no current frame');
      b.closed = false;
    } else if (typeof HTMLImageElement !== 'undefined' && source instanceof HTMLImageElement) {
      b = sourceImage(source);
      if (!b || b.unavailable) fail('InvalidStateError', 'Image is not decoded');
      b.closed = false;
    } else if (images.has(source)) {
      const d = images.get(source),
        pixels =
          d.pixelFormat === 'rgba-float16'
            ? new Float32Array(d.data.length)
            : new Uint8ClampedArray(d.data.length),
        scale = d.pixelFormat === 'rgba-float16' ? 255 : 1;
      for (let i = 0; i < pixels.length; i += 4) {
        const a = d.data[i + 3] * scale;
        canvasStore(pixels, i + 3, a);
        for (let k = 0; k < 3; k++) canvasStore(pixels, i + k, (d.data[i + k] * scale * a) / 255);
      }
      b = { width: d.width, height: d.height, pixels, colorSpace: d.colorSpace, closed: false };
    } else fail('NotSupportedError', 'Decoding this ImageBitmap source is not implemented');
    if (!b.width || !b.height) fail('InvalidStateError', 'ImageBitmap source has zero dimensions');
    let options = args[0] || {};
    if (args.length >= 4) {
      let [x, y, w, h] = args.slice(0, 4).map((value) => Math.trunc(Number(value)));
      if (![x, y, w, h].every(Number.isFinite) || !w || !h)
        fail('RangeError', 'Invalid ImageBitmap crop');
      if (w < 0) {
        x += w;
        w = -w;
      }
      if (h < 0) {
        y += h;
        h = -h;
      }
      const pixels = b.pixels instanceof Float32Array ? new Float32Array(w * h * 4) : storage(w, h);
      for (let yy = 0; yy < h; yy++)
        for (let xx = 0; xx < w; xx++) {
          const sx = x + xx,
            sy = y + yy;
          if (sx < 0 || sy < 0 || sx >= b.width || sy >= b.height) continue;
          const i = (sy * b.width + sx) * 4,
            j = (yy * w + xx) * 4;
          for (let k = 0; k < 4; k++) pixels[j + k] = b.pixels[i + k];
        }
      b = { ...b, width: w, height: h, pixels };
      options = args[4] || {};
    } else if (args.length > 1) throw new TypeError('Invalid ImageBitmap arguments');
    if (
      options.resizeWidth !== undefined ||
      options.resizeHeight !== undefined ||
      options.imageOrientation === 'flipY' ||
      options.premultiplyAlpha === 'none'
    )
      fail('NotSupportedError', 'ImageBitmap conversion options are not implemented');
    return new ImageBitmap(token, b);
  };
  // Canvas serialization consumes the same readback state as getImageData and
  // bitmap copies. Compress the scanlines without a second raster model.
  const pngCRC = (() => {
    const table = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      table[n] = c >>> 0;
    }
    return (bytes) => {
      let c = 0xffffffff;
      for (const value of bytes) c = table[(c ^ value) & 255] ^ (c >>> 8);
      return (c ^ 0xffffffff) >>> 0;
    };
  })();
  const pngU32 = (value) => [
    (value >>> 24) & 255,
    (value >>> 16) & 255,
    (value >>> 8) & 255,
    value & 255,
  ];
  const pngChunk = (name, data) => {
    const type = Array.from(name, (c) => c.charCodeAt(0)),
      body = type.concat(Array.from(data)),
      crc = pngCRC(body);
    return pngU32(data.length).concat(body, pngU32(crc));
  };
  const pngBytes = (canvas) => {
    const s = sync(state(canvas));
    if (s.originClean === false) fail('SecurityError', 'Canvas is not origin-clean');
    if (!s.width || !s.height) return null;
    const source = buffer(s),
      scan = new Uint8Array((s.width * 4 + 1) * s.height);
    const srgb = !s.colorSpace || s.colorSpace === 'srgb';
    for (let y = 0; y < s.height; y++) {
      const row = y * (s.width * 4 + 1);
      for (let x = 0; x < s.width; x++) {
        const i = (y * s.width + x) * 4,
          j = row + 1 + x * 4,
          a = source[i + 3];
        if (srgb) {
          // Integer sRGB storage needs only alpha unpremultiplication. Avoid
          // allocating and converting an RGBA vector for every PNG pixel.
          if (a === 255) {
            scan[j] = source[i];
            scan[j + 1] = source[i + 1];
            scan[j + 2] = source[i + 2];
          } else if (a) {
            const scale = 255 / a;
            scan[j] = Math.round(source[i] * scale);
            scan[j + 1] = Math.round(source[i + 1] * scale);
            scan[j + 2] = Math.round(source[i + 2] * scale);
          }
          scan[j + 3] = a;
        } else {
          const rgba = canvasConvertColor(
            [
              a ? (source[i] * 255) / a : 0,
              a ? (source[i + 1] * 255) / a : 0,
              a ? (source[i + 2] * 255) / a : 0,
              a,
              s.colorSpace,
            ],
            'srgb',
          );
          for (let k = 0; k < 4; k++) scan[j + k] = Math.round(rgba[k]);
        }
      }
    }
    // Frozen headful Chrome 152 serializes RGBA canvas rows with PNG Up
    // filtering, including the first row. Walk backward so the previous row
    // still contains its unfiltered bytes when computing each delta.
    const stride = s.width * 4 + 1;
    for (let y = s.height - 1; y >= 0; y--) {
      const row = y * stride;
      scan[row] = 2;
      if (y > 0) for (let x = 1; x < stride; x++) scan[row + x] -= scan[row - stride + x];
    }
    const compressed = atob(host.compressCanvasScanline(pngBase64(scan)));
    const z = new Uint8Array(compressed.length);
    for (let i = 0; i < compressed.length; i++) z[i] = compressed.charCodeAt(i);
    const ihdr = pngU32(s.width).concat(pngU32(s.height), [8, 6, 0, 0, 0]);
    // Chrome's streaming PNG writer emits 4096-byte IDAT chunks and writes
    // the six-byte stream trailer as its own final IDAT chunk.
    const chunks = [pngChunk('IHDR', ihdr)];
    for (let i = 0; i < z.length - 6; i += 4096)
      chunks.push(pngChunk('IDAT', z.subarray(i, Math.min(i + 4096, z.length - 6))));
    chunks.push(pngChunk('IDAT', z.subarray(z.length - 6)), pngChunk('IEND', []));
    const png = new Uint8Array(8 + chunks.reduce((size, chunk) => size + chunk.length, 0));
    png.set([137, 80, 78, 71, 13, 10, 26, 10]);
    let offset = 8;
    for (const chunk of chunks) {
      png.set(chunk, offset);
      offset += chunk.length;
    }
    return png;
  };
  const pngBase64 = (bytes) => {
    let binary = '';
    for (let offset = 0; offset < bytes.length; offset += 32768)
      binary += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
    return btoa(binary);
  };
  const canvasToDataURL = function (type) {
    const bytes = pngBytes(this);
    if (bytes === null) return 'data:,';
    if (type !== undefined) {
      const requested = String(type).trim().toLowerCase();
      if (
        requested &&
        requested !== 'image/png' &&
        requested !== 'image/jpeg' &&
        requested !== 'image/webp'
      )
        return 'data:image/png;base64,' + pngBase64(bytes);
      if (requested && requested !== 'image/png')
        fail('NotSupportedError', requested + ' canvas serialization is not implemented');
    }
    return 'data:image/png;base64,' + pngBase64(bytes);
  };
  const canvasToBlob = function (callback, type) {
    if (typeof callback !== 'function')
      throw new TypeError('The callback provided as parameter 1 is not a function');
    const bytes = pngBytes(this);
    if (type !== undefined && String(type).trim() && !/^image\/png$/i.test(String(type).trim()))
      fail('NotSupportedError', 'Only PNG canvas serialization is implemented');
    setTimeout(() => callback(bytes === null ? null : new Blob([bytes], { type: 'image/png' })), 0);
  };
  for (const [name, ctor] of Object.entries({
    OffscreenCanvas,
    CanvasRenderingContext2D,
    OffscreenCanvasRenderingContext2D,
    ImageData,
    ImageBitmap,
    CanvasGradient,
    CanvasPattern,
    TextMetrics,
  })) {
    if (name === 'CanvasRenderingContext2D' && typeof document === 'undefined') continue;
    Object.defineProperty(ctor.prototype, Symbol.toStringTag, { value: name, configurable: true });
    Object.defineProperty(globalThis, name, { value: ctor, writable: true, configurable: true });
  }
  Object.defineProperty(globalThis, 'createImageBitmap', {
    value: createImageBitmap,
    writable: true,
    configurable: true,
  });
  if (typeof HTMLCanvasElement === 'function') {
    const p = HTMLCanvasElement.prototype;
    for (const [key, fallback] of [
      ['width', 300],
      ['height', 150],
    ])
      Object.defineProperty(p, key, {
        get() {
          return htmlSize(this, key, fallback);
        },
        set(value) {
          value = Number(value) >>> 0;
          this.setAttribute(key, String(value));
          const s = state(this);
          resize(s, htmlSize(this, 'width', 300), htmlSize(this, 'height', 150));
        },
        enumerable: true,
        configurable: true,
      });
    const setAttribute = p.setAttribute;
    Object.defineProperty(p, 'setAttribute', {
      value: function (name, value) {
        setAttribute.call(this, name, value);
        if (/^(width|height)$/i.test(String(name))) {
          const s = state(this);
          resize(s, htmlSize(this, 'width', 300), htmlSize(this, 'height', 150));
        }
      },
      writable: true,
      configurable: true,
    });
    Object.defineProperty(p, 'getContext', {
      value: function (kind, attributes) {
        return getContext(this, kind, attributes);
      },
      writable: true,
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(p, 'toDataURL', {
      value: canvasToDataURL,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    Object.defineProperty(p, 'toBlob', {
      value: canvasToBlob,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  }
  if (typeof markNative === 'function') {
    for (const ctor of [
      OffscreenCanvas,
      CanvasRenderingContext2D,
      OffscreenCanvasRenderingContext2D,
      ImageData,
      ImageBitmap,
      CanvasGradient,
      CanvasPattern,
      TextMetrics,
    ]) {
      markNative(ctor, ctor.name);
      for (const key of Reflect.ownKeys(ctor.prototype)) {
        if (key === 'constructor' || typeof key !== 'string') continue;
        const d = Object.getOwnPropertyDescriptor(ctor.prototype, key);
        if (typeof d.value === 'function') markNative(d.value, key);
        if (d.get) markNative(d.get, key, 'get ');
        if (d.set) markNative(d.set, key, 'set ');
        if (!d.enumerable) Object.defineProperty(ctor.prototype, key, { ...d, enumerable: true });
      }
    }
    markNative(createImageBitmap, 'createImageBitmap');
    if (typeof HTMLCanvasElement === 'function') {
      for (const name of ['getContext', 'setAttribute', 'toDataURL', 'toBlob', 'width', 'height']) {
        const d = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, name);
        if (d?.value) markNative(d.value, name);
        if (d?.get) markNative(d.get, name, 'get ');
        if (d?.set) markNative(d.set, name, 'set ');
      }
    }
  }
  const preserveFunction = (fn, reference) => {
    if (typeof fn !== 'function' || typeof reference !== 'function') return;
    for (const key of ['length', 'name'])
      Object.defineProperty(fn, key, { value: reference[key], configurable: true });
  };
  for (const name of interfaceNames) {
    const ctor = globalThis[name],
      reference = reflectedInterfaces.get(name);
    if (!ctor || !reference) continue;
    Object.defineProperty(ctor, 'length', {
      value: workerProperties.get(name)?.functionLength ?? reference.length,
      configurable: true,
    });
    for (const key of Object.getOwnPropertyNames(ctor.prototype)) {
      if (key === 'constructor') continue;
      const current = Object.getOwnPropertyDescriptor(ctor.prototype, key),
        original = reference.members[key];
      if (!original) {
        if (name !== 'HTMLCanvasElement') delete ctor.prototype[key];
        continue;
      }
      preserveFunction(current.value, original.value);
      const member = (selectedWorkerExposure?.prototypes?.[name] || []).find((p) => p.name === key);
      if (current.value && member?.functionName)
        Object.defineProperty(current.value, 'name', {
          value: member.functionName,
          configurable: true,
        });
      if (current.value && member?.functionLength !== undefined && member.functionLength !== null)
        Object.defineProperty(current.value, 'length', {
          value: member.functionLength,
          configurable: true,
        });
      preserveFunction(current.get, original.get);
      preserveFunction(current.set, original.set);
      if (current.get) {
        Object.defineProperty(current.get, 'length', { value: 0, configurable: true });
        Object.defineProperty(current.get, 'name', { value: 'get ' + key, configurable: true });
      }
      if (current.set) {
        Object.defineProperty(current.set, 'length', { value: 1, configurable: true });
        Object.defineProperty(current.set, 'name', { value: 'set ' + key, configurable: true });
      }
      Object.defineProperty(ctor.prototype, key, {
        ...current,
        enumerable: original.enumerable,
        configurable: original.configurable,
        ...('writable' in current ? { writable: original.writable } : {}),
      });
    }
  }
  if (imageBitmapArity !== undefined)
    Object.defineProperty(createImageBitmap, 'length', {
      value:
        workerProperties.get('createImageBitmap')?.functionLength ??
        selectedWorkerExposure?.prototypes?.WorkerGlobalScope?.find(
          (p) => p.name === 'createImageBitmap',
        )?.functionLength ??
        imageBitmapArity,
      configurable: true,
    });
  return {
    registerContext(kind, create, canonical = kind) {
      factories.set(kind, { create, canonical });
    },
    state,
    buffer,
    bitmapState: (bitmap) => bitmaps.get(bitmap),
  };
})();
