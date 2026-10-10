// Query-time coverage model. No native drawing backend, glyph rasterizer or
// presentation surface. Curves use bounded geometric approximation; stable
// operation snapshots preserve local changes, overlapping reads and ordering.
const pathCopy = (p) =>
  p.map((s) => ({ closed: s.closed, points: s.points.map((p) => p.slice()) }));
const transformPoint = (m, x, y) => [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
const currentPath = (c) => c.path || (c.path = []);
const appendPoint = (c, x, y, move = false) => {
  const p = currentPath(c),
    point = transformPoint(c.draw.transform, x, y);
  let sub = p[p.length - 1];
  if (move || !sub) {
    sub = { points: [], closed: false };
    p.push(sub);
  }
  if (sub.points.length >= 4096) fail('NotSupportedError', 'Path observation complexity limit');
  sub.points.push(point);
};
const containsPath = (path, x, y, rule) => {
  let winding = 0;
  for (const sub of path) {
    const ps = sub.points;
    for (let i = 0; i < ps.length; i++) {
      const a = ps[i],
        b = ps[(i + 1) % ps.length];
      if ((a[1] <= y && b[1] > y) || (a[1] > y && b[1] <= y)) {
        const cross = (b[0] - a[0]) * (y - a[1]) - (x - a[0]) * (b[1] - a[1]);
        if (b[1] > a[1] && cross > 0) winding++;
        else if (b[1] < a[1] && cross < 0) winding--;
      }
    }
  }
  return rule === 'evenodd' ? !!(Math.abs(winding) % 2) : winding !== 0;
};
// Only immutable operation/clip snapshots use this accelerator. A simple
// convex polygon has one interval on each scanline. Keep ambiguous boundary
// points on the original winding predicate, including its floating rounding.
const convexScanlines = (path) => {
  const subpaths = path.filter((sub) => sub.points.length > 1);
  if (subpaths.length !== 1) return null;
  let points = subpaths[0].points.filter(
    (p, i, ps) => !i || p[0] !== ps[i - 1][0] || p[1] !== ps[i - 1][1],
  );
  if (points.length > 3 && points[0][0] === points.at(-1)[0] && points[0][1] === points.at(-1)[1])
    points = points.slice(0, -1);
  if (points.length < 3 || points.length > 256) return null;
  const seen = new Set();
  let sign = 0,
    scale = 1;
  for (let i = 0; i < points.length; i++) {
    const a = points[i],
      b = points[(i + 1) % points.length],
      c = points[(i + 2) % points.length];
    if (!a.every(Number.isFinite)) return null;
    const key = a[0] + ':' + a[1];
    if (seen.has(key)) return null;
    seen.add(key);
    scale = Math.max(scale, Math.abs(a[0]), Math.abs(a[1]));
    if (scale > 1e9) return null;
    const turn = (b[0] - a[0]) * (c[1] - b[1]) - (b[1] - a[1]) * (c[0] - b[0]);
    if (turn) {
      if (sign && sign !== Math.sign(turn)) return null;
      sign = Math.sign(turn);
    }
  }
  if (!sign) return null;
  // Equal local turns alone also admit self-intersecting stars. Verify every
  // edge's half-plane before using convexity to skip coverage work.
  for (let i = 0; i < points.length; i++) {
    const a = points[i],
      b = points[(i + 1) % points.length];
    for (const p of points)
      if (sign * ((b[0] - a[0]) * (p[1] - a[1]) - (b[1] - a[1]) * (p[0] - a[0])) < 0) return null;
  }
  return { points, epsilon: 1e-7 + Number.EPSILON * 64 * scale, rows: new Map() };
};
const scanlineInterval = (scan, y) => {
  if (scan.rows.has(y)) return scan.rows.get(y);
  let left = Infinity,
    right = -Infinity;
  const points = scan.points;
  for (let i = 0; i < points.length; i++) {
    const a = points[i],
      b = points[(i + 1) % points.length];
    if ((a[1] <= y && b[1] > y) || (a[1] > y && b[1] <= y)) {
      const x = a[0] + ((y - a[1]) * (b[0] - a[0])) / (b[1] - a[1]);
      left = Math.min(left, x);
      right = Math.max(right, x);
    }
  }
  const row = left < right ? { left, right } : null;
  if (scan.rows.size < 4096) scan.rows.set(y, row);
  return row;
};
const clipContains = (clips, x, y) =>
  !clips ||
  clips.every((clip) => {
    if (clip.scan === undefined) clip.scan = convexScanlines(clip.path);
    if (clip.scan) {
      const row = scanlineInterval(clip.scan, y),
        epsilon = clip.scan.epsilon;
      if (!row) return false;
      if (x > row.left + epsilon && x < row.right - epsilon) return true;
      if (x < row.left - epsilon || x > row.right + epsilon) return false;
    }
    return containsPath(clip.path, x, y, clip.rule);
  });
const strokeContains = (path, x, y, width) => {
  const radius = width / 2;
  for (const sub of path) {
    const ps = sub.points,
      n = ps.length;
    for (let i = 1; i < n + (sub.closed ? 1 : 0); i++) {
      const a = ps[i - 1],
        b = ps[i % n],
        dx = b[0] - a[0],
        dy = b[1] - a[1],
        length = dx * dx + dy * dy;
      if (!length) continue;
      const t = ((x - a[0]) * dx + (y - a[1]) * dy) / length;
      if (t < 0 || t > 1) continue;
      if ((x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2 <= radius * radius) return true;
    }
  }
  return false;
};
// Immutable stroke snapshots can select the same segments once per pixel,
// rather than walking the entire path for each of its 64 coverage samples.
const strokePixelEdges = (op, x, y) => {
  const radius = op.width / 2;
  if (!Number.isFinite(radius) || radius > 1e9) return null;
  if (op.strokeEdges === null) return null;
  if (!op.strokeEdges) {
    if (
      op.path.some((sub) =>
        sub.points.some((p) => p.some((v) => !Number.isFinite(v) || Math.abs(v) > 1e9)),
      )
    ) {
      op.strokeEdges = null;
      return null;
    }
    op.strokeEdges = [];
    for (const sub of op.path) {
      const ps = sub.points;
      for (let i = 1; i < ps.length + (sub.closed ? 1 : 0); i++) {
        const a = ps[i - 1],
          b = ps[i % ps.length];
        const dx = b[0] - a[0],
          dy = b[1] - a[1],
          length = dx * dx + dy * dy;
        if (length)
          op.strokeEdges.push({
            a,
            dx,
            dy,
            length,
            left: Math.min(a[0], b[0]) - radius - 1e-5,
            right: Math.max(a[0], b[0]) + radius + 1e-5,
            top: Math.min(a[1], b[1]) - radius - 1e-5,
            bottom: Math.max(a[1], b[1]) + radius + 1e-5,
          });
      }
    }
  }
  return op.strokeEdges.filter(
    (e) => x + 1 >= e.left && x <= e.right && y + 1 >= e.top && y <= e.bottom,
  );
};
const strokeEdgesContain = (edges, x, y, width) => {
  const radius = width / 2;
  for (const { a, dx, dy, length } of edges) {
    const t = ((x - a[0]) * dx + (y - a[1]) * dy) / length;
    if (t < 0 || t > 1) continue;
    if ((x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2 <= radius * radius) return true;
  }
  return false;
};
const paintSnapshot = (style, space = 'srgb', scheme = 'light', transform = [1, 0, 0, 1, 0, 0]) => {
  const pattern = patterns.get(style);
  if (pattern) {
    const [a, b, c, d, e, f] = transform,
      [u, v, w, z, q, r] = pattern.matrix;
    return {
      ...pattern,
      matrix: [
        a * u + c * v,
        b * u + d * v,
        a * w + c * z,
        b * w + d * z,
        a * q + c * r + e,
        b * q + d * r + f,
      ],
      image: { ...pattern.image, pixels: pattern.image.pixels.slice() },
      targetSpace: space,
    };
  }
  const g = gradients.get(style);
  return g
    ? {
        ...g,
        args: g.args.slice(),
        matrix: (g.matrix || [1, 0, 0, 1, 0, 0]).slice(),
        stops: g.stops
          .map((s) => ({ offset: s.offset, color: canvasPaintColor(s.color, space) }))
          .sort((a, b) => a.offset - b.offset),
      }
    : color(style, scheme)
      ? canvasPaintColor(color(style, scheme), space)
      : null;
};
const paintColor = (paint, x, y) => {
  if (Array.isArray(paint)) return paint;
  if (paint?.kind === 'pattern') {
    const m = paint.matrix,
      det = m[0] * m[3] - m[1] * m[2];
    if (!det) return [0, 0, 0, 0];
    let px = Math.floor((m[3] * (x - m[4]) - m[2] * (y - m[5])) / det),
      py = Math.floor((-m[1] * (x - m[4]) + m[0] * (y - m[5])) / det);
    const image = paint.image;
    if (
      (px < 0 || px >= image.width) &&
      (paint.repetition === 'repeat-y' || paint.repetition === 'no-repeat')
    )
      return [0, 0, 0, 0];
    if (
      (py < 0 || py >= image.height) &&
      (paint.repetition === 'repeat-x' || paint.repetition === 'no-repeat')
    )
      return [0, 0, 0, 0];
    px = ((px % image.width) + image.width) % image.width;
    py = ((py % image.height) + image.height) % image.height;
    const index = (py * image.width + px) * 4,
      alpha = image.pixels[index + 3];
    return canvasPaintColor(
      [
        alpha ? (image.pixels[index] * 255) / alpha : 0,
        alpha ? (image.pixels[index + 1] * 255) / alpha : 0,
        alpha ? (image.pixels[index + 2] * 255) / alpha : 0,
        alpha,
        image.colorSpace || 'srgb',
      ],
      paint.targetSpace,
    );
  }
  if (!paint || !paint.stops.length) return [0, 0, 0, 0];
  const m = paint.matrix,
    det = m[0] * m[3] - m[1] * m[2];
  if (!det) return [0, 0, 0, 0];
  const px = (m[3] * (x - m[4]) - m[2] * (y - m[5])) / det,
    py = (-m[1] * (x - m[4]) + m[0] * (y - m[5])) / det;
  let t;
  if (paint.kind === 'linear') {
    const [x0, y0, x1, y1] = paint.args,
      dx = x1 - x0,
      dy = y1 - y0;
    t = ((px - x0) * dx + (py - y0) * dy) / (dx * dx + dy * dy);
  } else if (paint.kind === 'conic') {
    const [startAngle, centerX, centerY] = paint.args;
    const turn = (Math.atan2(py - centerY, px - centerX) - startAngle) / (2 * Math.PI);
    t = ((turn % 1) + 1) % 1;
  } else {
    const [x0, y0, r0, x1, y1, r1] = paint.args,
      dx = x1 - x0,
      dy = y1 - y0,
      dr = r1 - r0,
      qx = px - x0,
      qy = py - y0,
      A = dx * dx + dy * dy - dr * dr,
      B = -2 * (qx * dx + qy * dy + r0 * dr),
      C = qx * qx + qy * qy - r0 * r0,
      D = B * B - 4 * A * C;
    const roots =
      Math.abs(A) < 1e-12
        ? [-C / B]
        : D < 0
          ? []
          : [(-B - Math.sqrt(D)) / (2 * A), (-B + Math.sqrt(D)) / (2 * A)];
    t = Math.max(...roots.filter((t) => r0 + t * dr >= 0));
  }
  if (!Number.isFinite(t)) return [0, 0, 0, 0];
  const stops = paint.stops;
  if (t < stops[0].offset) return stops[0].color;
  if (t >= stops[stops.length - 1].offset) return stops[stops.length - 1].color;
  let i = 1;
  while (i < stops.length && stops[i].offset <= t) i++;
  const a = stops[i - 1],
    b = stops[i],
    f = (t - a.offset) / (b.offset - a.offset),
    alpha = a.color[3] * (1 - f) + b.color[3] * f;
  return [0, 1, 2]
    .map((k) =>
      alpha ? (a.color[k] * a.color[3] * (1 - f) + b.color[k] * b.color[3] * f) / alpha : 0,
    )
    .concat(alpha);
};
const canvasBlendModes = new Set(
  'multiply screen overlay darken lighten color-dodge color-burn hard-light soft-light difference exclusion hue saturation color luminosity'.split(
    ' ',
  ),
);
const blendRGB = (source, destination, mode) => {
  // The selected Chrome graphics profile uses half precision for the
  // nonseparable blend shader. Preserve its rounding before unorm storage.
  const half = (value) => {
    if (!value) return value;
    const step = 2 ** (Math.max(-14, Math.floor(Math.log2(Math.abs(value)))) - 10);
    const scaled = Math.abs(value) / step,
      lower = Math.floor(scaled),
      fraction = scaled - lower;
    return (
      Math.sign(value) *
      (lower + (fraction > 0.5 || (fraction === 0.5 && lower % 2) ? 1 : 0)) *
      step
    );
  };
  const nonseparable = ['hue', 'saturation', 'color', 'luminosity'].includes(mode);
  if (nonseparable) source = source.map(half);
  const luminosity = (c) => half(half(0.3) * c[0] + half(0.59) * c[1] + half(0.11) * c[2]);
  const saturation = (c) => Math.max(...c) - Math.min(...c);
  const setLuminosity = (c, value) => {
    const delta = half(value - luminosity(c));
    let result = c.map((v) => half(v + delta));
    const l = luminosity(result),
      low = Math.min(...result),
      high = Math.max(...result);
    if (low < 0) result = result.map((v) => l + ((v - l) * l) / (l - low));
    if (high > 1) result = result.map((v) => l + ((v - l) * (1 - l)) / (high - l));
    return result;
  };
  const setSaturation = (c, value) => {
    const order = [0, 1, 2].sort((a, b) => c[a] - c[b]),
      [low, mid, high] = order;
    const result = [0, 0, 0];
    if (c[high] > c[low]) {
      result[mid] = ((c[mid] - c[low]) * value) / (c[high] - c[low]);
      result[high] = value;
    }
    return result;
  };
  if (mode === 'hue')
    return setLuminosity(setSaturation(source, saturation(destination)), luminosity(destination));
  if (mode === 'saturation')
    return setLuminosity(setSaturation(destination, saturation(source)), luminosity(destination));
  if (mode === 'color') return setLuminosity(source, luminosity(destination));
  if (mode === 'luminosity') return setLuminosity(destination, luminosity(source));
  return source.map((s, k) => {
    const d = destination[k];
    switch (mode) {
      case 'multiply':
        return s * d;
      case 'screen':
        return s + d - s * d;
      case 'overlay':
        return d <= 0.5 ? 2 * s * d : 1 - 2 * (1 - s) * (1 - d);
      case 'darken':
        return Math.min(s, d);
      case 'lighten':
        return Math.max(s, d);
      case 'color-dodge':
        return d === 0 ? 0 : s === 1 ? 1 : Math.min(1, d / (1 - s));
      case 'color-burn':
        return d === 1 ? 1 : s === 0 ? 0 : 1 - Math.min(1, (1 - d) / s);
      case 'hard-light':
        return s <= 0.5 ? 2 * s * d : 1 - 2 * (1 - s) * (1 - d);
      case 'soft-light':
        return s <= 0.5
          ? d - (1 - 2 * s) * d * (1 - d)
          : d + (2 * s - 1) * ((d <= 0.25 ? ((16 * d - 12) * d + 4) * d : Math.sqrt(d)) - d);
      case 'difference':
        return Math.abs(d - s);
      case 'exclusion':
        return d + s - 2 * d * s;
      default:
        return s;
    }
  });
};
const compositePixel = (p, i, rgba, opacity, mode, opaque, coverage = 1) => {
  if (
    p instanceof Uint8ClampedArray &&
    coverage === 1 &&
    opacity === 1 &&
    rgba[3] === 255 &&
    (mode === 'source-over' || mode === 'copy')
  ) {
    for (let k = 0; k < 3; k++) canvasStore(p, i + k, rgba[k]);
    p[i + 3] = 255;
    return;
  }
  const sa = (rgba[3] / 255) * opacity,
    da = p[i + 3] / 255;
  let fa = 1,
    fb = 1 - sa;
  if (mode === 'copy') fb = 0;
  else if (mode === 'source-in') {
    fa = da;
    fb = 0;
  } else if (mode === 'source-out') {
    fa = 1 - da;
    fb = 0;
  } else if (mode === 'source-atop') {
    fa = da;
    fb = 1 - sa;
  } else if (mode === 'destination-over') {
    fa = 1 - da;
    fb = 1;
  } else if (mode === 'destination-in') {
    fa = 0;
    fb = sa;
  } else if (mode === 'destination-out') {
    fa = 0;
    fb = 1 - sa;
  } else if (mode === 'destination-atop') {
    fa = 1 - da;
    fb = sa;
  } else if (mode === 'xor') {
    fa = 1 - da;
    fb = 1 - sa;
  } else if (mode === 'lighter') {
    fa = 1;
    fb = 1;
  }
  const blended = canvasBlendModes.has(mode)
    ? blendRGB(
        rgba.slice(0, 3).map((v) => v / 255),
        [0, 1, 2].map((k) => (da ? p[i + k] / 255 / da : 0)),
        mode,
      )
    : null;
  for (let k = 0; k < 3; k++) {
    let value = rgba[k] * sa * fa + p[i + k] * fb;
    if (blended) {
      const sc = rgba[k] / 255,
        dc = da ? p[i + k] / 255 / da : 0,
        blend = blended[k];
      value = (sc * sa * (1 - da) + dc * da * (1 - sa) + blend * sa * da) * 255;
    }
    canvasStore(p, i + k, value * coverage + p[i + k] * (1 - coverage));
  }
  canvasStore(
    p,
    i + 3,
    opaque ? 255 : (Math.min(1, sa * fa + da * fb) * coverage + da * (1 - coverage)) * 255,
  );
};
const supportedComposite = new Set([
  'source-over',
  'copy',
  'source-in',
  'source-out',
  'source-atop',
  'destination-over',
  'destination-in',
  'destination-out',
  'destination-atop',
  'xor',
  'lighter',
  'multiply',
  'screen',
  ...canvasBlendModes,
]);
const shadowSnapshot = (d, s) => {
  const rgba = paintSnapshot(d.shadowColor, s.colorSpace || 'srgb', canvasColorScheme(s));
  if (!Array.isArray(rgba) || !rgba[3]) return null;
  return { rgba, blur: d.shadowBlur, x: d.shadowOffsetX, y: d.shadowOffsetY };
};
// Blur the source alpha before painting the source. Keep this in the canonical
// pixel buffer so crops, copies and later overlapping draws see the same shadow.
const materializeShadow = (s, source, shadow, alpha, composite, clips) => {
  if (!shadow || !source.some((v) => v > 0)) return;
  const w = s.width,
    h = s.height,
    sigma = shadow.blur / 2,
    radius = Math.min(Math.ceil(3 * sigma), Math.max(w, h));
  let mask = source;
  if (radius) {
    const weights = new Float64Array(radius + 1);
    let total = 1;
    weights[0] = 1;
    for (let k = 1; k <= radius; k++) {
      weights[k] = Math.exp((-k * k) / (2 * sigma * sigma));
      total += 2 * weights[k];
    }
    for (let k = 0; k <= radius; k++) weights[k] /= total;
    for (const horizontal of [true, false]) {
      const next = new Float32Array(w * h);
      for (let y = 0; y < h; y++)
        for (let x = 0; x < w; x++) {
          let value = mask[y * w + x] * weights[0];
          for (let k = 1; k <= radius; k++) {
            const a = horizontal ? x - k : y - k,
              b = horizontal ? x + k : y + k;
            if (a >= 0) value += mask[(horizontal ? y : a) * w + (horizontal ? a : x)] * weights[k];
            if (b < (horizontal ? w : h))
              value += mask[(horizontal ? y : b) * w + (horizontal ? b : x)] * weights[k];
          }
          next[y * w + x] = value;
        }
      mask = next;
    }
  }
  for (let y = 0; y < h; y++)
    for (let x = 0; x < w; x++) {
      const sx = x - shadow.x,
        sy = y - shadow.y;
      if (sx < 0 || sy < 0 || sx >= w || sy >= h) continue;
      const ix = Math.floor(sx),
        iy = Math.floor(sy),
        nx = Math.min(ix + 1, w - 1),
        ny = Math.min(iy + 1, h - 1),
        fx = sx - ix,
        fy = sy - iy,
        coverage =
          mask[iy * w + ix] * (1 - fx) * (1 - fy) +
          mask[iy * w + nx] * fx * (1 - fy) +
          mask[ny * w + ix] * (1 - fx) * fy +
          mask[ny * w + nx] * fx * fy;
      if (coverage > 0 && clipContains(clips, x + 0.5, y + 0.5))
        compositePixel(
          s.pixels,
          (y * w + x) * 4,
          shadow.rgba,
          alpha,
          composite,
          s.opaque,
          coverage,
        );
    }
};
const queuePath = (c, path, rule, stroke = false, clear = false) => {
  const d = c.draw;
  if (!supportedComposite.has(d.globalCompositeOperation) && !clear) {
    record(c, 'composite', [d.globalCompositeOperation]);
    return;
  }
  const paint = clear
    ? [0, 0, 0, 0]
    : paintSnapshot(
        stroke ? d.strokeStyle : d.fillStyle,
        c.surface.colorSpace || 'srgb',
        canvasColorScheme(c.surface),
        d.transform,
      );
  if (!paint) return;
  if (paint.kind === 'pattern' && paint.image.originClean === false) c.surface.originClean = false;
  const scale = Math.max(
    Math.hypot(d.transform[0], d.transform[1]),
    Math.hypot(d.transform[2], d.transform[3]),
  );
  const pending = c.surface.pendingText || (c.surface.pendingText = []);
  pending.push({
    kind: 'path',
    path: pathCopy(path),
    rule,
    stroke,
    width: d.lineWidth * scale,
    paint,
    alpha: clear ? 1 : d.globalAlpha,
    composite: clear ? 'copy' : d.globalCompositeOperation,
    clear,
    clips: d.clipPaths || [],
    shadow: clear ? null : shadowSnapshot(d, c.surface),
  });
  // Long-lived animations need bounded deferred state even when they never
  // clear the full canvas. Folding a batch into the canonical pixel model
  // preserves subsequent overlapping draws and readbacks.
  if (pending.length >= 256) {
    ensurePixels(c.surface);
    materializeText(c.surface);
  }
  reportBoundary(c, 'approximatePathObservations');
  c.surface.unmodeled = true;
};
// Integrate polygon coverage in the pixel square. This keeps fractional edges,
// overlapping readbacks and copies coherent without a native rasterizer.
const polygonPixelArea = (points, x, y) => {
  let p = points;
  for (const [axis, bound, greater] of [
    [0, x, true],
    [0, x + 1, false],
    [1, y, true],
    [1, y + 1, false],
  ]) {
    if (!p.length) return 0;
    const out = [];
    let a = p[p.length - 1],
      insideA = greater ? a[axis] >= bound : a[axis] <= bound;
    for (const b of p) {
      const insideB = greater ? b[axis] >= bound : b[axis] <= bound;
      if (insideA !== insideB) {
        const t = (bound - a[axis]) / (b[axis] - a[axis]);
        out.push([a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1])]);
      }
      if (insideB) out.push(b);
      a = b;
      insideA = insideB;
    }
    p = out;
  }
  let area = 0;
  for (let i = 0; i < p.length; i++) {
    const a = p[i],
      b = p[(i + 1) % p.length];
    area += (a[0] - x) * (b[1] - y) - (b[0] - x) * (a[1] - y);
  }
  return area / 2;
};
const pathPixelCoverage = (op, x, y) => {
  if (op.coveragePolygon === undefined) {
    const paths = op.path.filter((s) => s.points.length > 2);
    let convex = !op.stroke && !op.clips.length && paths.length === 1,
      sign = 0;
    if (convex) {
      const p = paths[0].points;
      for (let i = 0; i < p.length; i++) {
        const a = p[i],
          b = p[(i + 1) % p.length],
          c = p[(i + 2) % p.length],
          cross = (b[0] - a[0]) * (c[1] - b[1]) - (b[1] - a[1]) * (c[0] - b[0]);
        if (Math.abs(cross) > 1e-12) {
          const next = Math.sign(cross);
          if (sign && sign !== next) {
            convex = false;
            break;
          }
          sign = next;
        }
      }
    }
    op.coveragePolygon = convex ? paths[0].points : null;
  }
  if (op.scan === undefined) op.scan = op.stroke ? null : convexScanlines(op.path);
  if (op.coveragePolygon) {
    if (op.scan) {
      const top = scanlineInterval(op.scan, y),
        bottom = scanlineInterval(op.scan, y + 1);
      if (
        top &&
        bottom &&
        x > Math.max(top.left, bottom.left) + op.scan.epsilon &&
        x + 1 < Math.min(top.right, bottom.right) - op.scan.epsilon
      )
        return 1;
    }
    return Math.min(1, Math.abs(polygonPixelArea(op.coveragePolygon, x, y)));
  }
  if (
    op.scan &&
    op.clips.every((clip) => {
      if (clip.scan === undefined) clip.scan = convexScanlines(clip.path);
      return !!clip.scan;
    })
  ) {
    if (!op.sampleRows) op.sampleRows = new Map();
    let row = op.sampleRows.get(y);
    if (!row) {
      let innerLeft = -Infinity,
        innerRight = Infinity;
      let outerLeft = Infinity,
        outerRight = -Infinity;
      let epsilon = op.scan.epsilon;
      const scans = [op.scan, ...op.clips.map((clip) => clip.scan)];
      for (const scan of scans) epsilon = Math.max(epsilon, scan.epsilon);
      for (let i = 0; i < 8; i++) {
        let left = -Infinity,
          right = Infinity;
        for (const scan of scans) {
          const interval = scanlineInterval(scan, y + (i + 0.5) / 8);
          if (!interval) {
            left = Infinity;
            right = -Infinity;
            break;
          }
          left = Math.max(left, interval.left);
          right = Math.min(right, interval.right);
        }
        innerLeft = Math.max(innerLeft, left);
        innerRight = Math.min(innerRight, right);
        if (left < right) {
          outerLeft = Math.min(outerLeft, left);
          outerRight = Math.max(outerRight, right);
        }
      }
      row = { innerLeft, innerRight, outerLeft, outerRight, epsilon };
      if (op.sampleRows.size < 4096) op.sampleRows.set(y, row);
    }
    if (x + 0.0625 > row.innerLeft + row.epsilon && x + 0.9375 < row.innerRight - row.epsilon)
      return 1;
    if (x + 0.9375 < row.outerLeft - row.epsilon || x + 0.0625 > row.outerRight + row.epsilon)
      return 0;
  }
  // Intersect clips and compound winding/strokes at a bounded common subpixel
  // grid, rather than multiplying independent coverage fractions.
  const edges = op.stroke ? strokePixelEdges(op, x, y) : null;
  if (edges && !edges.length) return 0;
  let count = 0;
  for (let yy = 0; yy < 8; yy++)
    for (let xx = 0; xx < 8; xx++) {
      const px = x + (xx + 0.5) / 8,
        py = y + (yy + 0.5) / 8;
      if (
        clipContains(op.clips, px, py) &&
        (op.stroke
          ? edges
            ? strokeEdgesContain(edges, px, py, op.width)
            : strokeContains(op.path, px, py, op.width)
          : containsPath(op.path, px, py, op.rule))
      )
        count++;
    }
  return count / 64;
};
const materializePath = (s, op) => {
  const ps = op.path.flatMap((p) => p.points);
  if (!ps.length) return;
  const pad = op.stroke ? op.width / 2 : 0,
    unbounded =
      !op.clear &&
      ['copy', 'source-in', 'source-out', 'destination-in', 'destination-atop'].includes(
        op.composite,
      );
  const left = unbounded ? 0 : Math.max(0, Math.floor(Math.min(...ps.map((p) => p[0])) - pad)),
    right = unbounded
      ? s.width
      : Math.min(s.width, Math.ceil(Math.max(...ps.map((p) => p[0])) + pad)),
    top = unbounded ? 0 : Math.max(0, Math.floor(Math.min(...ps.map((p) => p[1])) - pad)),
    bottom = unbounded
      ? s.height
      : Math.min(s.height, Math.ceil(Math.max(...ps.map((p) => p[1])) + pad));
  if (op.shadow) {
    const source = new Float32Array(s.width * s.height);
    for (let y = top; y < bottom; y++)
      for (let x = left; x < right; x++) {
        const coverage = pathPixelCoverage(op, x, y);
        if (coverage)
          source[y * s.width + x] = (coverage * paintColor(op.paint, x + 0.5, y + 0.5)[3]) / 255;
      }
    materializeShadow(s, source, op.shadow, op.alpha, op.composite, op.clips);
  }
  for (let y = top; y < bottom; y++)
    for (let x = left; x < right; x++) {
      const coverage = pathPixelCoverage(op, x, y);
      if (!coverage && !unbounded) continue;
      if (unbounded) {
        if (!clipContains(op.clips, x + 0.5, y + 0.5)) continue;
        compositePixel(
          s.pixels,
          (y * s.width + x) * 4,
          coverage ? paintColor(op.paint, x + 0.5, y + 0.5) : [0, 0, 0, 0],
          op.alpha * coverage,
          op.composite,
          s.opaque,
        );
      } else
        compositePixel(
          s.pixels,
          (y * s.width + x) * 4,
          paintColor(op.paint, x + 0.5, y + 0.5),
          op.alpha,
          op.composite,
          s.opaque,
          coverage,
        );
    }
};
const rectanglePath = (c, x, y, w, h) => [
  {
    closed: true,
    points: [
      [x, y],
      [x + w, y],
      [x + w, y + h],
      [x, y + h],
    ].map((p) => transformPoint(c.draw.transform, ...p)),
  },
];
const installPathObservations = (proto) => {
  const method = (name, fn) =>
    Object.defineProperty(proto, name, {
      value: fn,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  method('beginPath', function () {
    contextState(this).path = [];
  });
  method('closePath', function () {
    const p = currentPath(contextState(this)),
      s = p[p.length - 1];
    if (s?.points.length) {
      s.closed = true;
      p.push({ closed: false, points: [s.points[0].slice()] });
    }
  });
  method('moveTo', function (x, y) {
    const c = contextState(this);
    x = Number(x);
    y = Number(y);
    if (Number.isFinite(x) && Number.isFinite(y)) appendPoint(c, x, y, true);
  });
  method('lineTo', function (x, y) {
    const c = contextState(this);
    x = Number(x);
    y = Number(y);
    if (Number.isFinite(x) && Number.isFinite(y)) appendPoint(c, x, y);
  });
  method('rect', function (x, y, w, h) {
    const c = contextState(this),
      a = [x, y, w, h].map(Number);
    if (!a.every(Number.isFinite)) return;
    currentPath(c).push(...rectanglePath(c, ...a));
    const sub = c.path[c.path.length - 1];
    c.path.push({ closed: false, points: [sub.points[0].slice()] });
  });
  for (const [name, n] of [
    ['quadraticCurveTo', 4],
    ['bezierCurveTo', 6],
  ])
    method(name, function (...args) {
      const c = contextState(this);
      args = args.slice(0, n).map(Number);
      if (args.length < n || !args.every(Number.isFinite)) return;
      let path = currentPath(c);
      if (!path.length) appendPoint(c, args[0], args[1], true);
      const sub = path[path.length - 1],
        start = sub.points[sub.points.length - 1],
        control = [];
      for (let i = 0; i < n; i += 2)
        control.push(transformPoint(c.draw.transform, args[i], args[i + 1]));
      for (let step = 1; step <= 32; step++) {
        const t = step / 32,
          u = 1 - t;
        sub.points.push(
          [0, 1].map((k) =>
            n === 4
              ? u * u * start[k] + 2 * u * t * control[0][k] + t * t * control[1][k]
              : u * u * u * start[k] +
                3 * u * u * t * control[0][k] +
                3 * u * t * t * control[1][k] +
                t * t * t * control[2][k],
          ),
        );
      }
    });
  method('ellipse', function (x, y, rx, ry, rotation, start, end, ccw = false) {
    const c = contextState(this),
      a = [x, y, rx, ry, rotation, start, end].map(Number);
    if (!a.every(Number.isFinite)) return;
    [x, y, rx, ry, rotation, start, end] = a;
    if (rx < 0 || ry < 0) fail('IndexSizeError', 'Negative radius');
    let delta = end - start,
      tau = Math.PI * 2;
    if (Math.abs(delta) >= tau) delta = ccw ? -tau : tau;
    else if (!ccw) delta = ((delta % tau) + tau) % tau;
    else delta = -(((-delta % tau) + tau) % tau);
    const steps = Math.max(1, Math.ceil((Math.abs(delta) / tau) * 128)),
      co = Math.cos(rotation),
      si = Math.sin(rotation);
    for (let i = 0; i <= steps; i++) {
      const t = start + (delta * i) / steps,
        px = rx * Math.cos(t),
        py = ry * Math.sin(t);
      appendPoint(c, x + px * co - py * si, y + px * si + py * co);
    }
  });
  method('arc', function (x, y, r, start, end, ccw = false) {
    return this.ellipse(x, y, r, r, 0, start, end, ccw);
  });
  const fillRule = (value) => {
    value = value === undefined ? 'nonzero' : String(value);
    if (!['nonzero', 'evenodd'].includes(value)) throw new TypeError('Invalid fill rule');
    return value;
  };
  method('fill', function (rule) {
    const c = contextState(this);
    if (rule !== undefined && typeof rule !== 'string') {
      record(c, 'fill Path2D', []);
      return;
    }
    queuePath(c, currentPath(c), fillRule(rule));
  });
  method('stroke', function (path) {
    const c = contextState(this);
    if (path !== undefined) {
      record(c, 'stroke Path2D', []);
      return;
    }
    queuePath(c, currentPath(c), 'nonzero', true);
  });
  method('clip', function (rule) {
    const c = contextState(this);
    if (rule !== undefined && typeof rule !== 'string') {
      record(c, 'clip Path2D', []);
      return;
    }
    c.draw.clipPaths = [
      ...(c.draw.clipPaths || []),
      { path: pathCopy(currentPath(c)), rule: fillRule(rule) },
    ];
  });
  method('strokeRect', function (x, y, w, h) {
    const c = contextState(this),
      a = [x, y, w, h].map(Number);
    if (a.every(Number.isFinite)) queuePath(c, rectanglePath(c, ...a), 'nonzero', true);
  });
  method('isPointInPath', function (x, y, rule) {
    const c = contextState(this);
    x = Number(x);
    y = Number(y);
    return (
      Number.isFinite(x) && Number.isFinite(y) && containsPath(currentPath(c), x, y, fillRule(rule))
    );
  });
  method('isPointInStroke', function (x, y) {
    const c = contextState(this);
    x = Number(x);
    y = Number(y);
    return (
      Number.isFinite(x) &&
      Number.isFinite(y) &&
      strokeContains(currentPath(c), x, y, c.draw.lineWidth)
    );
  });
};
