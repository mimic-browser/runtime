// SVG geometry observations use canonical DOM attributes and query-time math.
// No raster surface or graphics backend is created. Unknown geometry is an
// explicit boundary, never an invented zero-sized successful measurement.
{
  // Retain canonical prototypes before author code can replace window bindings.
  for (const [name, interfaceName] of Object.entries(svgElementInterfaces)) {
    const prototype = globalThis[interfaceName]?.prototype;
    if (prototype) svgElementPrototypes.set(name, prototype);
  }
  const ns = 'http://www.w3.org/2000/svg',
    graphics = globalThis.SVGGraphicsElement;
  if (graphics) {
    Object.setPrototypeOf(SVGSVGElement.prototype, graphics.prototype);
    Object.setPrototypeOf(SVGSVGElement, graphics);
    const graphicsTags = new Set([
      'svg',
      'g',
      'a',
      'defs',
      'symbol',
      'switch',
      'rect',
      'circle',
      'ellipse',
      'line',
      'polyline',
      'polygon',
      'path',
      'text',
      'tspan',
      'textPath',
      'image',
      'use',
      'foreignObject',
    ]);
    const tag = (n) => {
      const d = elementSlot(n);
      return d ? (d.qualifiedName || String(d.tagName || '').toLowerCase()).split(':').at(-1) : '';
    };
    const attr = (n, name) => host.getAttribute(elementSlot(n).nodeId, name);
    const parent = (n) =>
      syntheticParents.get(n) ||
      shadowSlots.get(n)?.host ||
      (elementSlot(n) ? wrap(host.parentNode(elementSlot(n).nodeId)) : null);
    const childElements = (n) => host.elementChildren(elementSlot(n).nodeId).map(wrap);
    const check = (n) => {
      const d = elementSlot(n);
      if (!d || d.namespaceURI !== ns || !graphicsTags.has(tag(n)))
        throw new TypeError('Illegal invocation');
      return n;
    };
    const unsupported = (name) => {
      host.semanticMissingAt('svg_geometry.js:17', 'SVG.getBBox.' + name);
      throw platformDOMException(
        'Unsupported SVG bounding-box observation: ' + name,
        'NotSupportedError',
      );
    };
    const numberPattern = '[+-]?(?:\\d*\\.\\d+|\\d+\\.?\\d*)(?:[eE][+-]?\\d+)?';
    const numbers = (input) => {
      const s = String(input || ''),
        token = new RegExp(numberPattern, 'y'),
        space = /[\s,]*/y,
        out = [];
      let i = 0;
      while (i < s.length) {
        space.lastIndex = i;
        i += space.exec(s)[0].length;
        if (i === s.length) break;
        token.lastIndex = i;
        const m = token.exec(s);
        if (!m || !Number.isFinite(Number(m[0]))) return [];
        out.push(Number(m[0]));
        i = token.lastIndex;
        if (out.length > 65536) unsupported('numberListComplexity');
      }
      return out;
    };
    const ident = [1, 0, 0, 1, 0, 0],
      multiply = (a, b) => [
        a[0] * b[0] + a[2] * b[1],
        a[1] * b[0] + a[3] * b[1],
        a[0] * b[2] + a[2] * b[3],
        a[1] * b[2] + a[3] * b[3],
        a[0] * b[4] + a[2] * b[5] + a[4],
        a[1] * b[4] + a[3] * b[5] + a[5],
      ];
    const point = (m, x, y) => [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
    const union = (a, b) =>
      !a
        ? b
        : !b
          ? a
          : [
              Math.min(a[0], b[0]),
              Math.min(a[1], b[1]),
              Math.max(a[2], b[2]),
              Math.max(a[3], b[3]),
            ];
    const pointBox = (x, y) => [x, y, x, y],
      rect = (x, y, w, h) => [x, y, x + w, y + h];
    const transformBox = (b, m) => {
      if (!b) return null;
      const F = Math.fround,
        x = F(b[0]),
        y = F(b[1]),
        r = F(x + F(b[2] - b[0])),
        bottom = F(y + F(b[3] - b[1]));
      m = m.map(F);
      let out = null;
      for (const [px, py] of [
        [x, y],
        [r, y],
        [r, bottom],
        [x, bottom],
      ]) {
        const p = point(m, px, py).map(F);
        out = union(out, pointBox(...p));
      }
      return out;
    };
    /* shared_svg_css_transform */
    const transform = (n, box) => {
      const css = computedCSSDeclarations(n).find((e) => e.name === 'transform');
      if (css) return cssTransform(n, box, css.parsedValue ?? css.value);
      const raw = attr(n, 'transform') || '';
      let m = ident,
        rest = raw;
      const re = /^\s*(matrix|translate|scale|rotate|skewX|skewY)\s*\(([^)]*)\)\s*,?/;
      while (rest.trim()) {
        const match = re.exec(rest);
        if (!match) return ident;
        rest = rest.slice(match[0].length);
        const a = numbers(match[2]);
        if (!a.every(Number.isFinite)) return ident;
        let v;
        switch (match[1]) {
          case 'matrix':
            if (a.length !== 6) return ident;
            v = a;
            break;
          case 'translate':
            if (a.length < 1 || a.length > 2) return ident;
            v = [1, 0, 0, 1, a[0], a[1] || 0];
            break;
          case 'scale':
            if (a.length < 1 || a.length > 2) return ident;
            v = [a[0], 0, 0, a.length === 2 ? a[1] : a[0], 0, 0];
            break;
          case 'rotate': {
            if (a.length !== 1 && a.length !== 3) return ident;
            const rad = (a[0] * Math.PI) / 180,
              c = Math.cos(rad),
              s = Math.sin(rad),
              x = a[1] || 0,
              y = a[2] || 0;
            v = [c, s, -s, c, x - c * x + s * y, y - s * x - c * y];
            break;
          }
          default:
            if (a.length !== 1) return ident;
            v =
              match[1] === 'skewX'
                ? [1, 0, Math.tan((a[0] * Math.PI) / 180), 1, 0, 0]
                : [1, Math.tan((a[0] * Math.PI) / 180), 0, 1, 0, 0];
        }
        m = multiply(m, v);
      }
      return specified(n, 'transform-origin') != null || specified(n, 'transform-box') != null
        ? cssTransform(n, box, m)
        : m;
    };
    const specified = (n, name) => {
      const entry = computedCSSDeclarations(n).find((e) => e.name === name);
      return entry ? entry.value : attr(n, name);
    };
    const viewport = (n) => {
      for (let p = parent(n); isDOMNode(p); p = parent(p))
        if (tag(p) === 'svg') {
          const v = numbers(attr(p, 'viewBox'));
          if (v.length === 4 && v[2] > 0 && v[3] > 0) return [v[2], v[3]];
          return [length(p, 'width', 'x', 300), length(p, 'height', 'y', 150)];
        }
      return [300, 150];
    };
    const length = (n, name, axis = 'x', fallback = 0) => {
      const raw = specified(n, name);
      if (raw == null || raw === '') return fallback;
      const m = new RegExp(
        '^\\s*(' + numberPattern + ')(%|px|in|cm|mm|q|pt|pc|em|ex)?\\s*$',
        'i',
      ).exec(String(raw));
      if (!m) return fallback;
      let value = Number(m[1]),
        unit = (m[2] || '').toLowerCase();
      if (!Number.isFinite(value)) return fallback;
      if (unit === '%') {
        const [w, h] = viewport(n);
        value *= (axis === 'x' ? w : axis === 'y' ? h : Math.hypot(w, h) / Math.SQRT2) / 100;
      } else if (unit === 'em' || unit === 'ex') unsupported('fontRelativeLength');
      else
        value *=
          { in: 96, cm: 96 / 2.54, mm: 96 / 25.4, q: 96 / 101.6, pt: 96 / 72, pc: 16 }[unit] || 1;
      return Math.fround(value);
    };
    const displayNone = (n) => specified(n, 'display') === 'none';
    const viewportTransform = (n) => {
      const x = length(n, 'x', 'x'),
        y = length(n, 'y', 'y'),
        v = numbers(attr(n, 'viewBox') || '');
      if (v.length !== 4 || v[2] <= 0 || v[3] <= 0) return [1, 0, 0, 1, x, y];
      const w = length(n, 'width', 'x', 300),
        h = length(n, 'height', 'y', 150),
        p = (attr(n, 'preserveAspectRatio') || 'xMidYMid meet').trim();
      let sx = w / v[2],
        sy = h / v[3],
        ox = 0,
        oy = 0;
      if (p !== 'none') {
        const scale = p.endsWith('slice') ? Math.max(sx, sy) : Math.min(sx, sy);
        sx = sy = scale;
        ox = (w - v[2] * scale) * (p.includes('xMax') ? 1 : p.includes('xMin') ? 0 : 0.5);
        oy = (h - v[3] * scale) * (p.includes('YMax') ? 1 : p.includes('YMin') ? 0 : 0.5);
      }
      return [sx, 0, 0, sy, x + ox - v[0] * sx, y + oy - v[1] * sy];
    };
    // Cubic and quadratic extrema are analytic, not samples of the curve.
    const curveBox = (ps) => {
      let b = union(pointBox(...ps[0]), pointBox(...ps[ps.length - 1]));
      const at = (t) =>
        [0, 1].map((k) =>
          ps.length === 3
            ? (1 - t) ** 2 * ps[0][k] + 2 * (1 - t) * t * ps[1][k] + t * t * ps[2][k]
            : (1 - t) ** 3 * ps[0][k] +
              3 * (1 - t) ** 2 * t * ps[1][k] +
              3 * (1 - t) * t * t * ps[2][k] +
              t ** 3 * ps[3][k],
        );
      for (let k = 0; k < 2; k++) {
        let roots = [];
        if (ps.length === 3) {
          const d = ps[0][k] - 2 * ps[1][k] + ps[2][k];
          if (d) roots = [(ps[0][k] - ps[1][k]) / d];
        } else {
          const a = -ps[0][k] + 3 * ps[1][k] - 3 * ps[2][k] + ps[3][k],
            c = ps[1][k] - ps[0][k],
            d = 2 * (ps[0][k] - 2 * ps[1][k] + ps[2][k]);
          if (Math.abs(a) < 1e-14) {
            if (d) roots = [-c / d];
          } else {
            const disc = d * d - 4 * a * c;
            if (disc >= 0)
              roots = [(-d + Math.sqrt(disc)) / (2 * a), (-d - Math.sqrt(disc)) / (2 * a)];
          }
        }
        for (const t of roots) if (t > 0 && t < 1) b = union(b, pointBox(...at(t)));
      }
      return b;
    };
    const arcBox = (start, end, rx, ry, angle, large, sweep) => {
      rx = Math.abs(rx);
      ry = Math.abs(ry);
      if (!rx || !ry) return union(pointBox(...start), pointBox(...end));
      if (start[0] === end[0] && start[1] === end[1]) return null;
      const phi = (angle * Math.PI) / 180,
        c = Math.cos(phi),
        s = Math.sin(phi),
        dx = (start[0] - end[0]) / 2,
        dy = (start[1] - end[1]) / 2,
        x = c * dx + s * dy,
        y = -s * dx + c * dy,
        scale = (x * x) / (rx * rx) + (y * y) / (ry * ry);
      if (scale > 1) {
        rx *= Math.sqrt(scale);
        ry *= Math.sqrt(scale);
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
        ];
      const a = Math.atan2((y - cy) / ry, (x - cx) / rx),
        z = Math.atan2((-y - cy) / ry, (-x - cx) / rx),
        tau = 2 * Math.PI;
      let delta = (z - a) % tau;
      if (!sweep && delta > 0) delta -= tau;
      if (sweep && delta < 0) delta += tau;
      const at = (t) => [
        center[0] + rx * c * Math.cos(t) - ry * s * Math.sin(t),
        center[1] + rx * s * Math.cos(t) + ry * c * Math.sin(t),
      ];
      let b = union(pointBox(...start), pointBox(...end));
      for (const v of [Math.atan2(-ry * s, rx * c), Math.atan2(ry * c, rx * s)])
        for (const t of [v, v + Math.PI]) {
          const distance =
            delta >= 0 ? (((t - a) % tau) + tau) % tau : (((a - t) % tau) + tau) % tau;
          if (distance <= Math.abs(delta) + 1e-12) b = union(b, pointBox(...at(t)));
        }
      return b;
    };
    const pathBox = (source, emit) => {
      source = String(source || '');
      const token = new RegExp(numberPattern, 'y'),
        space = /[\s,]*/y;
      let i = 0,
        countSegments = 0,
        cmd = '',
        last = '',
        p = [0, 0],
        start = p,
        cubic = null,
        quad = null,
        b = null;
      const counts = { M: 2, L: 2, H: 1, V: 1, C: 6, S: 4, Q: 4, T: 2, A: 7 },
        skip = () => {
          space.lastIndex = i;
          const m = space.exec(source);
          i += m[0].length;
        };
      while (i < source.length) {
        skip();
        if (i === source.length) break;
        if (/[a-zA-Z]/.test(source[i])) cmd = source[i++];
        const op = cmd.toUpperCase(),
          relative = cmd !== op;
        if (!last && op !== 'M') break;
        if (++countSegments > 16384) unsupported('pathComplexity');
        if (op === 'Z') {
          if (emit) emit('L', [p, start]);
          if (last && last !== 'M') b = union(b, union(pointBox(...p), pointBox(...start)));
          p = start.slice();
          cubic = quad = null;
          last = op;
          cmd = '';
          continue;
        }
        const count = counts[op];
        if (!count) break;
        const v = [];
        for (let j = 0; j < count; j++) {
          skip();
          if (op === 'A' && (j === 3 || j === 4)) {
            if (source[i] !== '0' && source[i] !== '1') break;
            v.push(Number(source[i++]));
            continue;
          }
          token.lastIndex = i;
          const m = token.exec(source);
          if (!m) break;
          v.push(Number(m[0]));
          i = token.lastIndex;
        }
        if (v.length !== count || !v.every(Number.isFinite)) break;
        const xy = (x, y) => [x + (relative ? p[0] : 0), y + (relative ? p[1] : 0)];
        let next = p,
          cb = null,
          qb = null;
        if (op === 'M') {
          next = xy(v[0], v[1]);
          start = next.slice();
          if (emit) emit('M', [next]);
          cmd = relative ? 'l' : 'L';
        } else if (op === 'L' || op === 'H' || op === 'V') {
          next =
            op === 'L'
              ? xy(v[0], v[1])
              : op === 'H'
                ? [v[0] + (relative ? p[0] : 0), p[1]]
                : [p[0], v[0] + (relative ? p[1] : 0)];
          b = union(b, union(pointBox(...p), pointBox(...next)));
          if (emit) emit('L', [p, next]);
        } else if (op === 'C' || op === 'S') {
          const c1 =
              op === 'C'
                ? xy(v[0], v[1])
                : cubic && ['C', 'S'].includes(last)
                  ? [2 * p[0] - cubic[0], 2 * p[1] - cubic[1]]
                  : p,
            offset = op === 'C' ? 2 : 0;
          cb = xy(v[offset], v[offset + 1]);
          next = xy(v[offset + 2], v[offset + 3]);
          b = union(b, curveBox([p, c1, cb, next]));
          if (emit) emit('C', [p, c1, cb, next]);
        } else if (op === 'Q' || op === 'T') {
          qb =
            op === 'Q'
              ? xy(v[0], v[1])
              : quad && ['Q', 'T'].includes(last)
                ? [2 * p[0] - quad[0], 2 * p[1] - quad[1]]
                : p;
          next = op === 'Q' ? xy(v[2], v[3]) : xy(v[0], v[1]);
          b = union(b, curveBox([p, qb, next]));
          if (emit) emit('Q', [p, qb, next]);
        } else if (op === 'A') {
          if (![0, 1].includes(v[3]) || ![0, 1].includes(v[4])) break;
          next = xy(v[5], v[6]);
          b = union(b, arcBox(p, next, v[0], v[1], v[2], v[3], v[4]));
          if (emit) emit('A', [p, next], v.slice(0, 5));
        }
        p = next;
        cubic = cb;
        quad = qb;
        last = op;
      }
      return b || (last ? pointBox(...p) : null);
    };
    /* shared_svg_types */
    /* shared_svg_reflections */
    /* shared_svg_path_metrics */
    /* shared_svg_text */
    const bounds = (n, depth = 0) => {
      if (depth > 256) unsupported('treeDepth');
      const kind = tag(n),
        L = (name, axis, fallback) => length(n, name, axis, fallback);
      if (['rect', 'image', 'foreignObject'].includes(kind)) {
        const x = L('x', 'x'),
          y = L('y', 'y'),
          w = Math.max(0, L('width', 'x')),
          h = Math.max(0, L('height', 'y'));
        return rect(x, y, w, h);
      }
      if (kind === 'circle' || kind === 'ellipse') {
        const x = L('cx', 'x'),
          y = L('cy', 'y'),
          rx = Math.max(0, kind === 'circle' ? L('r', 'diagonal') : L('rx', 'x')),
          ry = kind === 'circle' ? rx : Math.max(0, L('ry', 'y'));
        return rect(x - rx, y - ry, 2 * rx, 2 * ry);
      }
      if (kind === 'line')
        return union(pointBox(L('x1', 'x'), L('y1', 'y')), pointBox(L('x2', 'x'), L('y2', 'y')));
      if (kind === 'polyline' || kind === 'polygon') {
        const ps = numbers(attr(n, 'points') || '');
        let b = null;
        for (let i = 0; i + 1 < ps.length; i += 2) b = union(b, pointBox(ps[i], ps[i + 1]));
        return b;
      }
      if (kind === 'path') {
        const css = computedCSSDeclarations(n).find((e) => e.name === 'd');
        if (css) unsupported('cssPath');
        return pathBox(attr(n, 'd'));
      }
      if (kind === 'text' || kind === 'tspan') return textLayout(n).box;
      if (kind === 'use') return svgUseBox(n, depth);
      if (['textPath', 'switch', 'symbol'].includes(kind)) return unsupported(kind);
      let b = null;
      for (const child of childElements(n)) {
        const kind = tag(child);
        if (!graphicsTags.has(kind) || ['defs', 'symbol'].includes(kind) || displayNone(child))
          continue;
        const childBox = bounds(child, depth + 1);
        if (
          childBox &&
          ['rect', 'circle', 'ellipse', 'image', 'foreignObject'].includes(kind) &&
          (childBox[2] === childBox[0] || childBox[3] === childBox[1])
        )
          continue;
        const m =
          kind === 'svg'
            ? multiply(transform(child, childBox), viewportTransform(child))
            : transform(child, childBox);
        b = union(b, transformBox(childBox, m));
      }
      return b;
    };
    const measurable = (n) => {
      if (!host.documentHasLayout()) return false;
      let connected = false;
      for (let p = n; isDOMNode(p); p = parent(p)) {
        const d = elementSlot(p);
        if (p === document || d?.type === 'document') connected = true;
        if (d?.type === 'element' && (d.namespaceURI !== ns || tag(p) === 'svg') && displayNone(p))
          return false;
      }
      return (
        connected && (!displayNone(n) || ['g', 'a', 'defs', 'symbol', 'switch'].includes(tag(n)))
      );
    };
    const getBBox = {
      getBBox() {
        const n = check(this),
          result = new SVGRect(hostToken);
        const b = measurable(n) ? bounds(n) : null;
        if (b) {
          result.x = Math.fround(b[0]);
          result.y = Math.fround(b[1]);
          result.width = Math.fround(b[2] - b[0]);
          result.height = Math.fround(b[3] - b[1]);
        }
        return result;
      },
    }.getBBox;
    markNative(getBBox, 'getBBox');
    Object.defineProperty(graphics.prototype, 'getBBox', {
      value: getBBox,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    /* shared_svg_boundaries */
  }
}
