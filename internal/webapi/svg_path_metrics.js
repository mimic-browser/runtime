// One parser feeds both analytic bounds and path observations. Curves are
// adaptively subdivided with a bounded error; no paint surface is allocated.
const svgPathSegments = (n) => {
  const d = elementSlot(n);
  if (
    !d ||
    d.namespaceURI !== ns ||
    !['path', 'rect', 'circle', 'ellipse', 'line', 'polyline', 'polygon'].includes(tag(n))
  )
    throw new TypeError('Illegal invocation');
  const contours = [];
  let current = null,
    segments = 0;
  const move = (p) => {
      current = [p];
      contours.push(current);
    },
    line = (p) => {
      if (++segments > 65536) svgFail('pathMetrics', 'segment limit');
      if (!current) move(p);
      else current.push(p);
    };
  const curve = (ps) => {
    const subdivide = (p, depth) => {
      const start = p[0],
        end = p.at(-1),
        chord = Math.hypot(end[0] - start[0], end[1] - start[1]);
      let polygon = 0;
      for (let i = 1; i < p.length; i++)
        polygon += Math.hypot(p[i][0] - p[i - 1][0], p[i][1] - p[i - 1][1]);
      if (depth >= 20 || polygon - chord < 0.00005) {
        line(end);
        return;
      }
      const left = [start],
        right = [end];
      while (p.length > 1) {
        p = p.slice(1).map((v, i) => [(v[0] + p[i][0]) / 2, (v[1] + p[i][1]) / 2]);
        left.push(p[0]);
        right.unshift(p.at(-1));
      }
      subdivide(left, depth + 1);
      subdivide(right, depth + 1);
    };
    subdivide(ps, 0);
  };
  const arc = (ps, a) => {
    let [rx, ry, angle, large, sweep] = a;
    rx = Math.abs(rx);
    ry = Math.abs(ry);
    const [start, end] = ps;
    if (!rx || !ry) {
      line(end);
      return;
    }
    if (start[0] === end[0] && start[1] === end[1]) return;
    const phi = (angle * Math.PI) / 180,
      c = Math.cos(phi),
      s = Math.sin(phi),
      dx = (start[0] - end[0]) / 2,
      dy = (start[1] - end[1]) / 2,
      x = c * dx + s * dy,
      y = -s * dx + c * dy,
      k = (x * x) / (rx * rx) + (y * y) / (ry * ry);
    if (k > 1) {
      rx *= Math.sqrt(k);
      ry *= Math.sqrt(k);
    }
    const f =
        (large === sweep ? -1 : 1) *
        Math.sqrt(
          Math.max(
            0,
            (rx * rx * ry * ry - rx * rx * y * y - ry * ry * x * x) /
              (rx * rx * y * y + ry * ry * x * x),
          ),
        ),
      cx = (f * rx * y) / ry,
      cy = (-f * ry * x) / rx,
      center = [
        c * cx - s * cy + (start[0] + end[0]) / 2,
        s * cx + c * cy + (start[1] + end[1]) / 2,
      ],
      first = Math.atan2((y - cy) / ry, (x - cx) / rx),
      last = Math.atan2((-y - cy) / ry, (-x - cx) / rx);
    let delta = last - first;
    if (sweep && delta < 0) delta += 2 * Math.PI;
    if (!sweep && delta > 0) delta -= 2 * Math.PI;
    const count = Math.min(
      16384,
      Math.max(16, Math.ceil(Math.abs(delta) * Math.sqrt(Math.max(rx, ry) / 0.0002))),
    );
    for (let i = 1; i <= count; i++) {
      const t = first + (delta * i) / count;
      line([
        center[0] + rx * c * Math.cos(t) - ry * s * Math.sin(t),
        center[1] + rx * s * Math.cos(t) + ry * c * Math.sin(t),
      ]);
    }
  };
  const emit = (type, ps, args) => {
    if (type === 'M') move(ps[0]);
    else if (type === 'L') line(ps[1]);
    else if (type === 'A') arc(ps, args);
    else curve(ps);
  };
  const kind = tag(n),
    L = (name, axis = 'x') => length(n, name, axis);
  if (kind === 'path') {
    if (specified(n, 'd') !== attr(n, 'd')) svgFail('pathMetrics', 'CSS path data is unsupported');
    pathBox(attr(n, 'd'), emit);
  } else if (kind === 'line') {
    move([L('x1'), L('y1', 'y')]);
    line([L('x2'), L('y2', 'y')]);
  } else if (kind === 'rect') {
    const x = L('x'),
      y = L('y', 'y'),
      w = L('width'),
      h = L('height', 'y');
    if (w > 0 && h > 0) {
      if (L('rx') || L('ry', 'y'))
        svgFail('pathMetrics', 'rounded rectangle metrics are unsupported');
      move([x, y]);
      for (const p of [
        [x + w, y],
        [x + w, y + h],
        [x, y + h],
        [x, y],
      ])
        line(p);
    }
  } else if (kind === 'circle' || kind === 'ellipse') {
    const x = L('cx'),
      y = L('cy', 'y'),
      rx = kind === 'circle' ? L('r', 'diagonal') : L('rx'),
      ry = kind === 'circle' ? rx : L('ry', 'y');
    if (rx > 0 && ry > 0) {
      move([x + rx, y]);
      const count = 2048;
      for (let i = 1; i <= count; i++) {
        const t = (2 * Math.PI * i) / count;
        line([x + rx * Math.cos(t), y + ry * Math.sin(t)]);
      }
    }
  } else {
    const ps = numbers(attr(n, 'points'));
    if (ps.length >= 2) {
      move(ps.slice(0, 2));
      for (let i = 2; i + 1 < ps.length; i += 2) line(ps.slice(i, i + 2));
      if (kind === 'polygon') line(current[0]);
    }
  }
  return contours;
};
const svgPathMeasure = (n) => {
  const contours = svgPathSegments(n);
  let length = 0;
  const edges = [];
  for (const points of contours)
    for (let i = 1; i < points.length; i++) {
      const a = points[i - 1],
        b = points[i],
        len = Math.hypot(b[0] - a[0], b[1] - a[1]);
      edges.push({ a, b, start: length, length: len });
      length += len;
    }
  return { contours, edges, length };
};
svgMethod('SVGGeometryElement', 'getTotalLength', function () {
  const n = check(this),
    result = svgPathMeasure(n);
  if (['circle', 'ellipse', 'path'].includes(tag(n)))
    host.semanticMissingAt(
      'svg_path_metrics.js:23',
      'SVG.approximatePathMetrics',
      JSON.stringify({ reason: 'CPU curve integration differs from Chrome subdivision tolerance' }),
    );
  return Math.fround(result.length);
});
svgMethod('SVGGeometryElement', 'getPointAtLength', function (distance) {
  const n = check(this);
  distance = svgFloat(distance);
  const m = svgPathMeasure(n);
  if (!m.contours.length) throw platformDOMException('No path', 'InvalidStateError');
  distance = Math.max(0, Math.min(m.length, distance));
  const e = m.edges.find((e) => e.length && e.start + e.length >= distance);
  if (!e) return svgPoint(...m.contours.at(-1).at(-1));
  const t = (distance - e.start) / e.length;
  return svgPoint(
    Math.fround(e.a[0] + t * (e.b[0] - e.a[0])),
    Math.fround(e.a[1] + t * (e.b[1] - e.a[1])),
  );
});
const svgDistanceToSegment = (x, y, a, b) => {
  const dx = b[0] - a[0],
    dy = b[1] - a[1],
    den = dx * dx + dy * dy,
    t = den ? Math.max(0, Math.min(1, ((x - a[0]) * dx + (y - a[1]) * dy) / den)) : 0;
  return Math.hypot(x - a[0] - t * dx, y - a[1] - t * dy);
};
svgMethod('SVGGeometryElement', 'isPointInFill', function (value = {}) {
  const n = check(this),
    m = svgPathMeasure(n);
  value = value ?? {};
  const x = Number(value.x ?? 0),
    y = Number(value.y ?? 0);
  if (!Number.isFinite(x) || !Number.isFinite(y) || tag(n) === 'line') return false;
  let winding = 0;
  for (const ps of m.contours) {
    if (ps.length < 3) continue;
    for (let i = 0; i < ps.length; i++) {
      const a = ps[i],
        b = ps[(i + 1) % ps.length];
      if (svgDistanceToSegment(x, y, a, b) < 1e-7) return true;
      if (a[1] <= y && b[1] > y && (b[0] - a[0]) * (y - a[1]) - (x - a[0]) * (b[1] - a[1]) > 0)
        winding++;
      if (a[1] > y && b[1] <= y && (b[0] - a[0]) * (y - a[1]) - (x - a[0]) * (b[1] - a[1]) < 0)
        winding--;
    }
  }
  return specified(n, 'fill-rule') === 'evenodd' ? Math.abs(winding) % 2 === 1 : winding !== 0;
});
svgMethod('SVGGeometryElement', 'isPointInStroke', function (value = {}) {
  const n = check(this),
    m = svgPathMeasure(n);
  value = value ?? {};
  const x = +(value.x ?? 0),
    y = +(value.y ?? 0);
  if (!Number.isFinite(x) || !Number.isFinite(y)) return false;
  if (specified(n, 'stroke-dasharray') && specified(n, 'stroke-dasharray') !== 'none')
    svgFail('strokeHitTest', 'dash geometry is unsupported');
  if (specified(n, 'vector-effect') && specified(n, 'vector-effect') !== 'none')
    svgFail('strokeHitTest', 'non-scaling stroke geometry is unsupported');
  const radius = length(n, 'stroke-width', 'diagonal', 1) / 2;
  if (radius <= 0) return false;
  const cap = specified(n, 'stroke-linecap') || 'butt',
    join = specified(n, 'stroke-linejoin') || 'miter',
    limit = +(specified(n, 'stroke-miterlimit') || 4);
  if (!['butt', 'round', 'square'].includes(cap) || !['miter', 'round', 'bevel'].includes(join))
    svgFail('strokeHitTest', 'unsupported cap or join');
  const triangle = (a, b, c) => {
    const cross = (p, q) => (q[0] - p[0]) * (y - p[1]) - (q[1] - p[1]) * (x - p[0]),
      v = [cross(a, b), cross(b, c), cross(c, a)];
    return v.every((v) => v >= -1e-9) || v.every((v) => v <= 1e-9);
  };
  for (const ps of m.contours) {
    const closed = ps.length > 2 && ps[0][0] === ps.at(-1)[0] && ps[0][1] === ps.at(-1)[1];
    for (let i = 1; i < ps.length; i++) {
      const a = ps[i - 1],
        b = ps[i],
        dx = b[0] - a[0],
        dy = b[1] - a[1],
        len = Math.hypot(dx, dy);
      if (!len) continue;
      const along = ((x - a[0]) * dx + (y - a[1]) * dy) / len,
        across = Math.abs((x - a[0]) * dy - (y - a[1]) * dx) / len,
        first = !closed && i === 1,
        last = !closed && i === ps.length - 1;
      if (
        across <= radius &&
        along >= (first && cap === 'square' ? -radius : 0) &&
        along <= len + (last && cap === 'square' ? radius : 0)
      )
        return true;
      if (
        cap === 'round' &&
        ((first && Math.hypot(x - a[0], y - a[1]) <= radius) ||
          (last && Math.hypot(x - b[0], y - b[1]) <= radius))
      )
        return true;
    }
    const end = closed ? ps.length - 1 : ps.length - 2;
    for (let i = closed ? 0 : 1; i <= end; i++) {
      const b = ps[i],
        a = ps[(i - 1 + ps.length - 1) % (ps.length - 1)],
        c = ps[i + 1] || ps[1],
        u = [b[0] - a[0], b[1] - a[1]],
        v = [c[0] - b[0], c[1] - b[1]],
        ul = Math.hypot(...u),
        vl = Math.hypot(...v);
      if (!ul || !vl) continue;
      u[0] /= ul;
      u[1] /= ul;
      v[0] /= vl;
      v[1] /= vl;
      const turn = u[0] * v[1] - u[1] * v[0];
      if (Math.abs(turn) < 1e-12) continue;
      if (join === 'round') {
        if (Math.hypot(x - b[0], y - b[1]) <= radius) return true;
        continue;
      }
      const side = turn > 0 ? -1 : 1,
        p = [b[0] - u[1] * radius * side, b[1] + u[0] * radius * side],
        q = [b[0] - v[1] * radius * side, b[1] + v[0] * radius * side];
      if (triangle(b, p, q)) return true;
      if (join === 'miter') {
        const t = ((q[0] - p[0]) * v[1] - (q[1] - p[1]) * v[0]) / turn,
          tip = [p[0] + t * u[0], p[1] + t * u[1]];
        if (Math.hypot(tip[0] - b[0], tip[1] - b[1]) <= limit * radius && triangle(p, tip, q))
          return true;
      }
    }
  }
  return false;
});
