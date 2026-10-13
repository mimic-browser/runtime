// Query-time coverage model. No native drawing backend, glyph rasterizer or
// presentation surface. Curves use bounded geometric approximation; stable
// operation snapshots preserve local changes, overlapping reads and ordering.
const pathCopy = (p) =>
  p.map((s) => ({
    closed: s.closed,
    curved: !!s.curved,
    points: s.points.map((p) => p.slice()),
  }));
const dashedPath = (path, pattern, offset) => {
  if (!pattern.length) return pathCopy(path);
  const total = pattern.reduce((sum, value) => sum + value, 0);
  // Frozen Chrome treats an all-zero dash list as a solid stroke.
  if (!total) return pathCopy(path);
  let phase = ((offset % total) + total) % total,
    initialIndex = 0;
  while (phase >= pattern[initialIndex]) {
    phase -= pattern[initialIndex];
    initialIndex = (initialIndex + 1) % pattern.length;
  }
  const result = [];
  for (const sub of path) {
    const points = sub.points,
      edgeCount = Math.max(0, points.length - 1 + (sub.closed ? 1 : 0));
    let index = initialIndex,
      remaining = pattern[index] - phase,
      active = null;
    const advance = () => {
      do {
        index = (index + 1) % pattern.length;
        remaining = pattern[index];
      } while (!remaining);
      active = null;
    };
    if (!remaining) advance();
    for (let edge = 0; edge < edgeCount; edge++) {
      const a = points[edge],
        b = points[(edge + 1) % points.length],
        dx = b[0] - a[0],
        dy = b[1] - a[1],
        length = Math.hypot(dx, dy);
      if (!length) continue;
      let consumed = 0;
      while (consumed < length) {
        const amount = Math.min(remaining, length - consumed),
          start = [a[0] + (dx * consumed) / length, a[1] + (dy * consumed) / length],
          end = [
            a[0] + (dx * (consumed + amount)) / length,
            a[1] + (dy * (consumed + amount)) / length,
          ];
        if (index % 2 === 0) {
          if (!active) {
            active = { closed: false, curved: !!sub.curved, points: [start] };
            result.push(active);
          }
          active.points.push(end);
        }
        consumed += amount;
        remaining -= amount;
        if (remaining <= 1e-12) advance();
      }
    }
  }
  return result;
};
const transformPoint = (m, x, y) => [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
const inverseTransformPoint = (m, x, y) => {
  const determinant = m[0] * m[3] - m[1] * m[2];
  if (!determinant) return null;
  x -= m[4];
  y -= m[5];
  return [(m[3] * x - m[2] * y) / determinant, (-m[1] * x + m[0] * y) / determinant];
};
const parseCanvasFilter = (input) => {
  input = String(input).trim();
  if (input === 'none') return [];
  const filters = [];
  let offset = 0;
  const token = /\s*([a-z-]+)\(\s*([^)]*?)\s*\)/gy;
  while (offset < input.length) {
    token.lastIndex = offset;
    const match = token.exec(input);
    if (!match) return null;
    offset = token.lastIndex;
    const name = match[1],
      argument = match[2],
      amount = /^([+]?(?:\d+(?:\.\d*)?|\.\d+))(%)?$/.exec(argument);
    if (name === 'hue-rotate') {
      const angle = /^([+-]?(?:\d+(?:\.\d*)?|\.\d+))(deg|grad|rad|turn)?$/.exec(argument);
      if (!angle || (!angle[2] && Number(angle[1]) !== 0)) return null;
      const value = Number(angle[1]),
        radians =
          angle[2] === 'rad'
            ? value
            : angle[2] === 'turn'
              ? value * Math.PI * 2
              : angle[2] === 'grad'
                ? (value * Math.PI) / 200
                : (value * Math.PI) / 180;
      filters.push({ name, amount: radians });
      continue;
    }
    if (
      !['brightness', 'contrast', 'grayscale', 'opacity', 'invert', 'sepia', 'saturate'].includes(
        name,
      ) ||
      !amount
    )
      return null;
    const value = Number(amount[1]) / (amount[2] ? 100 : 1);
    filters.push({ name, amount: value });
  }
  return filters.length ? filters : null;
};
const canvasFilterSnapshot = (draw) =>
  (draw.filterFunctions || []).map((filter) => ({ ...filter }));
const filterCanvasColor = (filters, input) => {
  if (!filters?.length) return input;
  let rgba = input.slice();
  const matrix = (values) => {
    const [r, g, b] = rgba;
    rgba[0] = r * values[0] + g * values[1] + b * values[2];
    rgba[1] = r * values[3] + g * values[4] + b * values[5];
    rgba[2] = r * values[6] + g * values[7] + b * values[8];
  };
  for (const filter of filters) {
    const amount = filter.amount;
    if (filter.name === 'brightness') for (let i = 0; i < 3; i++) rgba[i] *= amount;
    else if (filter.name === 'contrast')
      for (let i = 0; i < 3; i++) rgba[i] = (rgba[i] - 127.5) * amount + 127.5;
    else if (filter.name === 'opacity') rgba[3] = Math.floor(rgba[3] * Math.min(1, amount));
    else if (filter.name === 'invert')
      for (let i = 0; i < 3; i++) rgba[i] = rgba[i] * (1 - amount) + (255 - rgba[i]) * amount;
    else if (filter.name === 'grayscale') {
      const value = 1 - Math.min(1, amount);
      matrix([
        0.2126 + 0.7874 * value,
        0.7152 - 0.7152 * value,
        0.0722 - 0.0722 * value,
        0.2126 - 0.2126 * value,
        0.7152 + 0.2848 * value,
        0.0722 - 0.0722 * value,
        0.2126 - 0.2126 * value,
        0.7152 - 0.7152 * value,
        0.0722 + 0.9278 * value,
      ]);
    } else if (filter.name === 'sepia') {
      const value = Math.min(1, amount),
        inverse = 1 - value;
      matrix([
        inverse + 0.393 * value,
        0.769 * value,
        0.189 * value,
        0.349 * value,
        inverse + 0.686 * value,
        0.168 * value,
        0.272 * value,
        0.534 * value,
        inverse + 0.131 * value,
      ]);
    } else if (filter.name === 'saturate') {
      const value = amount;
      matrix([
        0.213 + 0.787 * value,
        0.715 - 0.715 * value,
        0.072 - 0.072 * value,
        0.213 - 0.213 * value,
        0.715 + 0.285 * value,
        0.072 - 0.072 * value,
        0.213 - 0.213 * value,
        0.715 - 0.715 * value,
        0.072 + 0.928 * value,
      ]);
    } else if (filter.name === 'hue-rotate') {
      const cosine = Math.cos(amount),
        sine = Math.sin(amount);
      matrix([
        0.213 + 0.787 * cosine - 0.213 * sine,
        0.715 - 0.715 * cosine - 0.715 * sine,
        0.072 - 0.072 * cosine + 0.928 * sine,
        0.213 - 0.213 * cosine + 0.143 * sine,
        0.715 + 0.285 * cosine + 0.14 * sine,
        0.072 - 0.072 * cosine - 0.283 * sine,
        0.213 - 0.213 * cosine - 0.787 * sine,
        0.715 - 0.715 * cosine + 0.715 * sine,
        0.072 + 0.928 * cosine + 0.072 * sine,
      ]);
    }
    for (let i = 0; i < 4; i++) rgba[i] = Math.max(0, Math.min(255, rgba[i]));
  }
  return rgba;
};
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
// Immutable fill/clip snapshots reuse the edges crossing a sampled row. Keep
// the scalar cross-product arithmetic and strict signs: interpolated crossing
// positions can round differently at a path boundary. Mutable hit testing
// continues to walk the current path above.
const windingScanlines = (path) => {
  const edges = [];
  for (const sub of path) {
    const points = sub.points;
    for (let i = 0; i < points.length; i++) {
      const a = points[i],
        b = points[(i + 1) % points.length];
      if (a.some((v) => !Number.isFinite(v) || Math.abs(v) > 1e9)) return null;
      if (a[1] !== b[1]) edges.push({ a, b });
    }
  }
  return { edges, rows: new Map(), cachedEdges: 0 };
};
const containsWindingSnapshot = (snapshot, x, y) => {
  if (snapshot.winding === undefined) snapshot.winding = windingScanlines(snapshot.path);
  const scan = snapshot.winding;
  if (!scan || !Number.isFinite(y)) return containsPath(snapshot.path, x, y, snapshot.rule);
  let row = scan.rows.get(y);
  if (!row) {
    row = [];
    for (const { a, b } of scan.edges)
      if ((a[1] <= y && b[1] > y) || (a[1] > y && b[1] <= y))
        row.push({
          x: a[0],
          dy: b[1] - a[1],
          crossY: (b[0] - a[0]) * (y - a[1]),
        });
    // Bound both empty rows and crossings retained by a compound path. Once
    // full, uncached rows still use the same predicate, without retained state.
    if (scan.rows.size < 4096 && scan.cachedEdges + row.length <= 16384) {
      scan.rows.set(y, row);
      scan.cachedEdges += row.length;
    }
  }
  let winding = 0;
  for (const edge of row) {
    const cross = edge.crossY - (x - edge.x) * edge.dy;
    if (edge.dy > 0 && cross > 0) winding++;
    else if (edge.dy < 0 && cross < 0) winding--;
  }
  return snapshot.rule === 'evenodd' ? !!(Math.abs(winding) % 2) : winding !== 0;
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
    return containsWindingSnapshot(clip, x, y);
  });
const strokeSegmentContains = (x, y, width, lineCap, edge) => {
  const radius = width / 2;
  const { a, dx, dy, length, capStart, capEnd } = edge;
  let t = ((x - a[0]) * dx + (y - a[1]) * dy) / length;
  if (lineCap === 'square') {
    const extension = radius / Math.sqrt(length);
    if ((capStart ? t < -extension : t < 0) || (capEnd ? t > 1 + extension : t > 1)) return false;
  } else if (t < 0 || t > 1) {
    if (lineCap !== 'round' || (t < 0 ? !capStart : !capEnd)) return false;
    t = t < 0 ? 0 : 1;
  }
  return (x - a[0] - t * dx) ** 2 + (y - a[1] - t * dy) ** 2 <= radius * radius;
};
const strokeSegmentsContain = (path, x, y, width, lineCap) => {
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
      if (
        strokeSegmentContains(x, y, width, lineCap, {
          a,
          dx,
          dy,
          length,
          capStart: !sub.closed && i === 1,
          capEnd: !sub.closed && i === n - 1,
        })
      )
        return true;
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
            capStart: !sub.closed && i === 1,
            capEnd: !sub.closed && i === ps.length - 1,
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
const strokeEdgesContain = (edges, x, y, width, lineCap) => {
  for (const edge of edges) if (strokeSegmentContains(x, y, width, lineCap, edge)) return true;
  return false;
};
const pointInConvexJoin = (points, x, y) => {
  let sign = 0;
  for (let i = 0; i < points.length; i++) {
    const a = points[i],
      b = points[(i + 1) % points.length],
      cross = (b[0] - a[0]) * (y - a[1]) - (b[1] - a[1]) * (x - a[0]);
    if (!cross) continue;
    if (sign && Math.sign(cross) !== sign) return false;
    sign = Math.sign(cross);
  }
  return true;
};
const strokeJoins = (op) => {
  if (op.strokeJoins) return op.strokeJoins;
  const radius = op.width / 2,
    joins = [];
  for (const sub of op.path) {
    const points = sub.points,
      start = sub.closed ? 0 : 1,
      end = sub.closed ? points.length : points.length - 1;
    for (let i = start; i < end; i++) {
      const previous = points[(i - 1 + points.length) % points.length],
        point = points[i],
        next = points[(i + 1) % points.length],
        firstLength = Math.hypot(point[0] - previous[0], point[1] - previous[1]),
        secondLength = Math.hypot(next[0] - point[0], next[1] - point[1]);
      if (!firstLength || !secondLength) continue;
      const first = [
          (point[0] - previous[0]) / firstLength,
          (point[1] - previous[1]) / firstLength,
        ],
        second = [(next[0] - point[0]) / secondLength, (next[1] - point[1]) / secondLength],
        turn = first[0] * second[1] - first[1] * second[0];
      if (Math.abs(turn) < 1e-12) continue;
      if (op.lineJoin === 'round') {
        joins.push({ kind: 'round', point, radius });
        continue;
      }
      const side = turn > 0 ? 1 : -1,
        firstCorner = [point[0] + side * first[1] * radius, point[1] - side * first[0] * radius],
        secondCorner = [point[0] + side * second[1] * radius, point[1] - side * second[0] * radius];
      let polygon = [point, firstCorner, secondCorner];
      if (op.lineJoin === 'miter') {
        const delta = [secondCorner[0] - firstCorner[0], secondCorner[1] - firstCorner[1]],
          distance = (delta[0] * second[1] - delta[1] * second[0]) / turn,
          miter = [firstCorner[0] + first[0] * distance, firstCorner[1] + first[1] * distance];
        if (Math.hypot(miter[0] - point[0], miter[1] - point[1]) <= op.miterLimit * radius)
          polygon = [point, firstCorner, miter, secondCorner];
      }
      joins.push({ kind: 'polygon', points: polygon });
    }
  }
  op.strokeJoins = joins;
  return joins;
};
const strokeJoinsContain = (op, x, y) => {
  for (const join of strokeJoins(op)) {
    if (join.kind === 'round') {
      if ((x - join.point[0]) ** 2 + (y - join.point[1]) ** 2 <= join.radius ** 2) return true;
    } else if (pointInConvexJoin(join.points, x, y)) return true;
  }
  return false;
};
// Keep hit testing, the unaccelerated predicate, and the per-pixel edge
// accelerator on one complete stroke geometry. Canvas defaults are part of
// the predicate because callers such as isPointInStroke do not pass a draw
// snapshot.
const strokeContains = (path, x, y, width, lineCap = 'butt', lineJoin = 'miter', miterLimit = 10) =>
  strokeSegmentsContain(path, x, y, width, lineCap) ||
  strokeJoinsContain({ path, width, lineJoin, miterLimit }, x, y);
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
const shadowSnapshot = (d, s, frequent) => {
  const rgba = paintSnapshot(d.shadowColor, s.colorSpace || 'srgb', canvasColorScheme(s));
  if (!Array.isArray(rgba) || !rgba[3]) return null;
  return { rgba, blur: d.shadowBlur, x: d.shadowOffsetX, y: d.shadowOffsetY, frequent };
};
// Blur the source mask before painting the source. Paths shade the blurred
// geometry afterward; text supplies its painted alpha directly. Keep the result
// in the canonical pixel buffer for later crops, copies and overlapping draws.
const materializeShadow = (
  s,
  source,
  shadow,
  alpha,
  composite,
  clips,
  sourceBounds,
  sourcePaint,
) => {
  if (!shadow || !source.some((v) => v > 0)) return;
  const w = s.width,
    h = s.height,
    sourceLeft = sourceBounds?.left ?? 0,
    sourceTop = sourceBounds?.top ?? 0,
    sourceWidth = sourceBounds?.width ?? w,
    sourceHeight = sourceBounds?.height ?? h,
    sigma = shadow.blur / 2,
    radius = Math.ceil(3 * sigma);
  // Keep unsupported extreme blurs explicit instead of silently truncating
  // their kernel or allocating an unbounded CPU mask.
  if (
    !Number.isSafeInteger(radius) ||
    radius > 4096 ||
    sourceWidth * sourceHeight * (radius + 1) > 20_000_000
  )
    fail('NotSupportedError', 'Canvas shadow blur exceeds the CPU observation limit');
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
      const next = new Float32Array(sourceWidth * sourceHeight);
      for (let y = 0; y < sourceHeight; y++)
        for (let x = 0; x < sourceWidth; x++) {
          let value = mask[y * sourceWidth + x] * weights[0];
          for (let k = 1; k <= radius; k++) {
            const a = horizontal ? x - k : y - k,
              b = horizontal ? x + k : y + k;
            if (a >= 0)
              value += mask[(horizontal ? y : a) * sourceWidth + (horizontal ? a : x)] * weights[k];
            if (b < (horizontal ? sourceWidth : sourceHeight))
              value += mask[(horizontal ? y : b) * sourceWidth + (horizontal ? b : x)] * weights[k];
          }
          next[y * sourceWidth + x] = value;
        }
      mask = next;
    }
  }
  for (let y = 0; y < h; y++)
    for (let x = 0; x < w; x++) {
      const sx = x - shadow.x - sourceLeft,
        sy = y - shadow.y - sourceTop;
      if (sx < 0 || sy < 0 || sx >= sourceWidth || sy >= sourceHeight) continue;
      // Chrome's frequent-read CPU canvas rounds the blurred mask to A8
      // before tinting. The ordinary canvas uses a different presentation path.
      const ix = Math.floor(sx),
        iy = Math.floor(sy),
        nx = Math.min(ix + 1, sourceWidth - 1),
        ny = Math.min(iy + 1, sourceHeight - 1),
        fx = sx - ix,
        fy = sy - iy,
        coverage =
          mask[iy * sourceWidth + ix] * (1 - fx) * (1 - fy) +
          mask[iy * sourceWidth + nx] * fx * (1 - fy) +
          mask[ny * sourceWidth + ix] * (1 - fx) * fy +
          mask[ny * sourceWidth + nx] * fx * fy,
        maskAlpha =
          (shadow.frequent ? Math.round(coverage * 255) / 255 : coverage) *
          (sourcePaint
            ? paintColor(sourcePaint, x - shadow.x + 0.5, y - shadow.y + 0.5)[3] / 255
            : 1);
      if (maskAlpha > 0 && clipContains(clips, x + 0.5, y + 0.5))
        compositePixel(
          s.pixels,
          (y * w + x) * 4,
          shadow.rgba,
          alpha,
          composite,
          s.opaque,
          maskAlpha,
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
  const strokePath =
    stroke && d.dash.length
      ? dashedPath(
          path,
          d.dash.map((value) => value * scale),
          d.lineDashOffset * scale,
        )
      : pathCopy(path);
  const pending = c.surface.pendingText || (c.surface.pendingText = []);
  pending.push({
    kind: 'path',
    path: strokePath,
    rule,
    stroke,
    width: d.lineWidth * scale,
    lineCap: d.lineCap,
    lineJoin: d.lineJoin,
    miterLimit: d.miterLimit,
    frequentRead: c.attributes.willReadFrequently,
    paint,
    alpha: clear ? 1 : d.globalAlpha,
    composite: clear ? 'copy' : d.globalCompositeOperation,
    clear,
    clips: d.clipPaths || [],
    shadow: clear ? null : shadowSnapshot(d, c.surface, c.attributes.willReadFrequently),
    filters: clear ? [] : canvasFilterSnapshot(d),
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
    if (op.frequentRead && op.path.some((sub) => sub.curved) && op.scan) {
      // Skia's raster canvas integrates curved edges on four vertically
      // quantized scan rows. It is observably different from the geometric
      // polygon area used by accelerated canvases, most clearly for arcs only
      // a few device pixels wide. Preserve that CPU readback boundary without
      // increasing the work for ordinary polygon paths.
      let area = 0;
      for (let row = 0; row < 4; row++) {
        const interval = scanlineInterval(op.scan, y + (row + 0.2946) / 4);
        if (interval)
          area += Math.max(0, Math.min(x + 1, interval.right) - Math.max(x, interval.left));
      }
      return Math.min(1, area / 4);
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
  let count = 0;
  for (let yy = 0; yy < 8; yy++)
    for (let xx = 0; xx < 8; xx++) {
      const px = x + (xx + 0.5) / 8,
        py = y + (yy + 0.5) / 8;
      if (
        clipContains(op.clips, px, py) &&
        (op.stroke
          ? (edges
              ? strokeEdgesContain(edges, px, py, op.width, op.lineCap)
              : strokeSegmentsContain(op.path, px, py, op.width, op.lineCap)) ||
            strokeJoinsContain(op, px, py)
          : containsWindingSnapshot(op, px, py))
      )
        count++;
    }
  return count / 64;
};
// The frequent-read CPU canvas draws unit-width paths as antialiased hairlines.
// Rasterize a straight segment in 26.6/16.16 fixed point so its endpoint caps
// and two neighboring pixels share one mask used by both paint and shadow.
const frequentReadHairlineMask = (s, op, left, right, top, bottom) => {
  if (!op.frequentRead || !op.stroke || op.width !== 1 || op.lineCap !== 'butt' || op.clips.length)
    return null;
  const paths = op.path.filter((sub) => sub.points.length);
  if (paths.length !== 1 || paths[0].closed || paths[0].points.length !== 2) return null;
  const [a, b] = paths[0].points;
  if (
    ![...a, ...b].every(Number.isFinite) ||
    [...a, ...b].some((v) => Math.abs(v) > 32767) ||
    a[0] < 0 ||
    a[0] > s.width ||
    b[0] < 0 ||
    b[0] > s.width ||
    a[1] < 0 ||
    a[1] > s.height ||
    b[1] < 0 ||
    b[1] > s.height
  )
    return null;
  let [x0, y0, x1, y1] = [...a, ...b].map((v) => Math.trunc(v * 64));
  const horizontal = Math.abs(x1 - x0) > Math.abs(y1 - y0);
  if ((horizontal && x0 > x1) || (!horizontal && y0 > y1)) {
    [x0, x1] = [x1, x0];
    [y0, y1] = [y1, y0];
  }
  const [u0, v0, u1, v1] = horizontal ? [x0, y0, x1, y1] : [y0, x0, y1, x1];
  const stride = right - left;
  if (u0 === u1) return new Uint8Array(stride * (bottom - top));
  const start = Math.floor(u0 / 64),
    stop = Math.ceil(u1 / 64),
    slope = Math.trunc(((v1 - v0) * 65536) / (u1 - u0)),
    first = v0 * 1024 + Math.floor((slope * (32 - (u0 & 63)) + 32) / 64),
    mask = new Uint8Array(stride * (bottom - top));
  for (let u = start; u < stop; u++) {
    const scale =
        stop - start === 1
          ? u1 - u0
          : u === start
            ? 64 - (u0 & 63)
            : u === stop - 1 && u1 & 63
              ? u1 & 63
              : 64,
      fixed = first + (u - start) * slope + 32768,
      v = Math.floor(fixed / 65536),
      fraction = Math.floor(fixed / 256) & 255;
    for (const [minor, alpha] of [
      [v - 1, 255 - fraction],
      [v, fraction],
    ]) {
      const [x, y] = horizontal ? [u, minor] : [minor, u];
      if (x < left || x >= right || y < top || y >= bottom) continue;
      const coverage = Math.floor((alpha * scale) / 64),
        index = (y - top) * stride + x - left;
      mask[index] = 255 - Math.floor(((255 - mask[index]) * (255 - coverage) + 127) / 255);
    }
  }
  return mask;
};
const materializePath = (s, op) => {
  const ps = op.path.flatMap((p) => p.points);
  if (!ps.length) return;
  const pad = op.stroke
      ? (op.lineJoin === 'miter' ? Math.max(1, op.miterLimit) : 1) * (op.width / 2)
      : 0,
    unbounded =
      !op.clear &&
      ['copy', 'source-in', 'source-out', 'destination-in', 'destination-atop'].includes(
        op.composite,
      );
  const pathLeft = Math.floor(Math.min(...ps.map((p) => p[0])) - pad),
    pathRight = Math.ceil(Math.max(...ps.map((p) => p[0])) + pad),
    pathTop = Math.floor(Math.min(...ps.map((p) => p[1])) - pad),
    pathBottom = Math.ceil(Math.max(...ps.map((p) => p[1])) + pad),
    left = unbounded ? 0 : Math.max(0, pathLeft),
    right = unbounded ? s.width : Math.min(s.width, pathRight),
    top = unbounded ? 0 : Math.max(0, pathTop),
    bottom = unbounded ? s.height : Math.min(s.height, pathBottom);
  const hairline = unbounded ? null : frequentReadHairlineMask(s, op, left, right, top, bottom),
    hairlineStride = right - left;
  if (op.shadow) {
    // Shadow blur sees geometry outside the canvas before the result is cropped
    // to the canvas. Restrict the extended mask to the pixels that can influence
    // the visible result, including blur support and the requested offset.
    const radius = Math.ceil((3 * op.shadow.blur) / 2),
      visibleLeft = Math.floor(-op.shadow.x - 1),
      visibleRight = Math.ceil(s.width - op.shadow.x + 1),
      visibleTop = Math.floor(-op.shadow.y - 1),
      visibleBottom = Math.ceil(s.height - op.shadow.y + 1),
      shadowLeft = Math.min(visibleLeft, Math.max(pathLeft, visibleLeft - radius)),
      shadowRight = Math.max(visibleRight, Math.min(pathRight, visibleRight + radius)),
      shadowTop = Math.min(visibleTop, Math.max(pathTop, visibleTop - radius)),
      shadowBottom = Math.max(visibleBottom, Math.min(pathBottom, visibleBottom + radius)),
      shadowWidth = shadowRight - shadowLeft,
      shadowHeight = shadowBottom - shadowTop;
    if (shadowWidth > 0 && shadowHeight > 0) {
      if (
        !Number.isSafeInteger(shadowWidth * shadowHeight) ||
        shadowWidth * shadowHeight > 4_000_000
      )
        fail('NotSupportedError', 'Canvas shadow mask exceeds the CPU observation limit');
      const source = new Float32Array(shadowWidth * shadowHeight);
      for (let y = shadowTop; y < shadowBottom; y++)
        for (let x = shadowLeft; x < shadowRight; x++) {
          const coverage =
            hairline && x >= left && x < right && y >= top && y < bottom
              ? hairline[(y - top) * hairlineStride + x - left] / 255
              : pathPixelCoverage(op, x, y);
          if (coverage) source[(y - shadowTop) * shadowWidth + x - shadowLeft] = coverage;
        }
      materializeShadow(
        s,
        source,
        op.shadow,
        op.alpha,
        op.composite,
        op.clips,
        {
          left: shadowLeft,
          top: shadowTop,
          width: shadowWidth,
          height: shadowHeight,
        },
        op.paint,
      );
    }
  }
  // RGBA8 constant source-over interiors have exactly unit coverage. Select their
  // scanline span once and store complete pixels in bulk; fractional edges keep
  // the same coverage and compositing path below.
  let solidPixels = null,
    solidWord = 0,
    solidScan = null,
    solidTables = null;
  if (
    !unbounded &&
    !op.stroke &&
    !op.clear &&
    !op.shadow &&
    !op.filters.length &&
    !op.clips.length &&
    op.composite === 'source-over' &&
    Array.isArray(op.paint) &&
    op.paint.every(Number.isFinite) &&
    s.pixels instanceof Uint8ClampedArray &&
    s.pixels.byteOffset % 4 === 0
  ) {
    solidScan = convexScanlines(op.path);
    if (solidScan) {
      if (op.alpha === 1 && op.paint[3] === 255) {
        const rgba = new Uint8ClampedArray(4);
        for (let k = 0; k < 4; k++) rgba[k] = Math.round(op.paint[k]);
        solidWord = new Uint32Array(rgba.buffer)[0];
        solidPixels = new Uint32Array(s.pixels.buffer, s.pixels.byteOffset, s.pixels.length / 4);
      } else {
        // Destination channels are bytes. Precompute precisely the existing
        // scalar arithmetic/rounding for each possible byte, once per paint.
        const sa = (op.paint[3] / 255) * op.alpha,
          fb = 1 - sa;
        solidTables = Array.from({ length: 4 }, () => new Uint8ClampedArray(256));
        for (let value = 0; value < 256; value++) {
          for (let k = 0; k < 3; k++)
            solidTables[k][value] = Math.round(op.paint[k] * sa + value * fb);
          solidTables[3][value] = Math.round(
            s.opaque ? 255 : Math.min(1, sa + (value / 255) * fb) * 255,
          );
        }
        solidPixels = s.pixels;
      }
    }
  }
  for (let y = top; y < bottom; y++) {
    let spanStart = right,
      spanEnd = right;
    if (solidPixels) {
      const a = scanlineInterval(solidScan, y),
        b = scanlineInterval(solidScan, y + 1);
      if (a && b) {
        spanStart = Math.max(left, Math.floor(Math.max(a.left, b.left) + solidScan.epsilon) + 1);
        spanEnd = Math.min(right, Math.ceil(Math.min(a.right, b.right) - solidScan.epsilon) - 1);
      }
    }
    for (let x = left; x < right; x++) {
      if (solidPixels && x === spanStart && spanEnd > spanStart) {
        if (solidTables) {
          const end = (y * s.width + spanEnd) * 4;
          for (let i = (y * s.width + spanStart) * 4; i < end; i += 4) {
            solidPixels[i] = solidTables[0][solidPixels[i]];
            solidPixels[i + 1] = solidTables[1][solidPixels[i + 1]];
            solidPixels[i + 2] = solidTables[2][solidPixels[i + 2]];
            solidPixels[i + 3] = solidTables[3][solidPixels[i + 3]];
          }
        } else solidPixels.fill(solidWord, y * s.width + spanStart, y * s.width + spanEnd);
        x = spanEnd - 1;
        continue;
      }
      const coverage = hairline
        ? hairline[(y - top) * hairlineStride + x - left] / 255
        : pathPixelCoverage(op, x, y);
      if (!coverage && !unbounded) continue;
      if (unbounded) {
        if (!clipContains(op.clips, x + 0.5, y + 0.5)) continue;
        compositePixel(
          s.pixels,
          (y * s.width + x) * 4,
          coverage
            ? filterCanvasColor(op.filters, paintColor(op.paint, x + 0.5, y + 0.5))
            : [0, 0, 0, 0],
          op.alpha * coverage,
          op.composite,
          s.opaque,
        );
      } else
        compositePixel(
          s.pixels,
          (y * s.width + x) * 4,
          filterCanvasColor(op.filters, paintColor(op.paint, x + 0.5, y + 0.5)),
          op.alpha,
          op.composite,
          s.opaque,
          coverage,
        );
    }
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
const roundRectRadii = (value) => {
  let values;
  if (value !== null && typeof value !== 'string' && typeof value?.[Symbol.iterator] === 'function')
    values = Array.from(value);
  else values = [value];
  if (!values.length || values.length > 4) throw new RangeError('Invalid radii length');
  const radii = values.map((radius) => {
    let x, y;
    if (radius !== null && typeof radius === 'object') {
      x = Number(radius.x ?? 0);
      y = Number(radius.y ?? 0);
    } else x = y = Number(radius);
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    if (x < 0 || y < 0) throw new RangeError('Negative radius');
    return { x, y };
  });
  if (radii.some((radius) => radius === null)) return null;
  if (radii.length === 1) return [radii[0], radii[0], radii[0], radii[0]];
  if (radii.length === 2) return [radii[0], radii[1], radii[0], radii[1]];
  if (radii.length === 3) return [radii[0], radii[1], radii[2], radii[1]];
  return radii;
};
const roundedRectanglePath = (c, x, y, width, height, radiusValue) => {
  let radii = roundRectRadii(radiusValue);
  if (!radii) return null;
  if (width < 0) radii = [radii[1], radii[0], radii[3], radii[2]];
  if (height < 0) radii = [radii[3], radii[2], radii[1], radii[0]];
  const left = Math.min(x, x + width),
    right = Math.max(x, x + width),
    top = Math.min(y, y + height),
    bottom = Math.max(y, y + height),
    horizontal = Math.abs(width),
    vertical = Math.abs(height),
    ratios = [
      horizontal / (radii[0].x + radii[1].x),
      vertical / (radii[1].y + radii[2].y),
      horizontal / (radii[2].x + radii[3].x),
      vertical / (radii[3].y + radii[0].y),
    ],
    scale = Math.min(1, ...ratios.filter(Number.isFinite));
  radii = radii.map((radius) => ({ x: radius.x * scale, y: radius.y * scale }));
  const points = [[left + radii[0].x, top]],
    corners = [
      [right - radii[1].x, top + radii[1].y, radii[1], -Math.PI / 2],
      [right - radii[2].x, bottom - radii[2].y, radii[2], 0],
      [left + radii[3].x, bottom - radii[3].y, radii[3], Math.PI / 2],
      [left + radii[0].x, top + radii[0].y, radii[0], Math.PI],
    ];
  for (const [cx, cy, radius, start] of corners)
    for (let step = 0; step <= 32; step++) {
      const angle = start + (step * Math.PI) / 64;
      points.push([cx + radius.x * Math.cos(angle), cy + radius.y * Math.sin(angle)]);
    }
  return {
    closed: true,
    curved: true,
    points: points.map((point) => transformPoint(c.draw.transform, ...point)),
  };
};
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
  method('roundRect', function (x, y, width, height, radii = 0) {
    const c = contextState(this),
      values = [x, y, width, height].map(Number);
    if (!values.every(Number.isFinite)) return;
    const sub = roundedRectanglePath(c, ...values, radii);
    if (!sub) return;
    currentPath(c).push(sub, { closed: false, points: [sub.points[0].slice()] });
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
      sub.curved = true;
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
    const before = currentPath(c).length;
    for (let i = 0; i <= steps; i++) {
      const t = start + (delta * i) / steps,
        px = rx * Math.cos(t),
        py = ry * Math.sin(t);
      appendPoint(c, x + px * co - py * si, y + px * si + py * co);
    }
    const path = currentPath(c);
    if (path.length) path[Math.max(0, before - 1)].curved = true;
  });
  method('arc', function (x, y, r, start, end, ccw = false) {
    return this.ellipse(x, y, r, r, 0, start, end, ccw);
  });
  method('arcTo', function (x1, y1, x2, y2, radius) {
    const c = contextState(this),
      values = [x1, y1, x2, y2, radius].map(Number);
    if (!values.every(Number.isFinite)) return;
    [x1, y1, x2, y2, radius] = values;
    if (radius < 0) fail('IndexSizeError', 'Negative radius');
    const path = currentPath(c),
      sub = path[path.length - 1];
    if (!sub?.points.length) {
      appendPoint(c, x1, y1, true);
      return;
    }
    const current = inverseTransformPoint(c.draw.transform, ...sub.points[sub.points.length - 1]);
    // A singular CTM cannot contribute finite observable geometry. Chrome
    // keeps the existing current point and otherwise leaves the path alone.
    if (!current) return;
    const first = [current[0] - x1, current[1] - y1],
      second = [x2 - x1, y2 - y1],
      firstLength = Math.hypot(...first),
      secondLength = Math.hypot(...second),
      cross = first[0] * second[1] - first[1] * second[0];
    if (!radius || !firstLength || !secondLength || Math.abs(cross) < 1e-12) {
      appendPoint(c, x1, y1);
      return;
    }
    const firstUnit = [first[0] / firstLength, first[1] / firstLength],
      secondUnit = [second[0] / secondLength, second[1] / secondLength],
      cosine = Math.max(
        -1,
        Math.min(1, firstUnit[0] * secondUnit[0] + firstUnit[1] * secondUnit[1]),
      ),
      angle = Math.acos(cosine),
      distance = radius / Math.tan(angle / 2),
      tangent1 = [x1 + firstUnit[0] * distance, y1 + firstUnit[1] * distance],
      tangent2 = [x1 + secondUnit[0] * distance, y1 + secondUnit[1] * distance],
      side = cross > 0 ? 1 : -1,
      center = [
        tangent1[0] + side * -firstUnit[1] * radius,
        tangent1[1] + side * firstUnit[0] * radius,
      ];
    appendPoint(c, ...tangent1);
    let start = Math.atan2(tangent1[1] - center[1], tangent1[0] - center[0]),
      end = Math.atan2(tangent2[1] - center[1], tangent2[0] - center[0]),
      delta = end - start;
    if (cross > 0 && delta < 0) delta += Math.PI * 2;
    else if (cross < 0 && delta > 0) delta -= Math.PI * 2;
    const steps = Math.max(1, Math.ceil((Math.abs(delta) / (Math.PI * 2)) * 128));
    sub.curved = true;
    for (let step = 1; step <= steps; step++) {
      const angle = start + (delta * step) / steps;
      appendPoint(c, center[0] + radius * Math.cos(angle), center[1] + radius * Math.sin(angle));
    }
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
