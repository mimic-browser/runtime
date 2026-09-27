// One scroll state per document owner. Layout remains in document coordinates;
// viewport observations subtract scroll offsets without rebuilding box sizes.
compatibilityScrolling = (() => {
  let positions = new WeakMap(),
    positionRanges = new WeakMap(),
    revision = 0,
    hasOffsets = false;
  const pending = new Set(),
    ending = new Set();
  let eventTimer = null,
    animations = new WeakMap();
  bootstrapRestoreHooks.push(() => {
    positions = new WeakMap();
    positionRanges = new WeakMap();
  });
  const geometryVersion = () => host.observationVersion() + ':' + constructedStyleSheets.revision();
  const documentRoot = () =>
    cssObservationChildren(document).find((node) => elementSlot(node)?.type === 'element') || null;
  const root = () => {
    if (styleReadCache?.scrollRoot) return styleReadCache.scrollRoot;
    const nodes = cssObservationChildren(document),
      html = documentRoot(),
      quirks =
        !nodes.some((node) => node.nodeType === 10) &&
        html?.namespaceURI === 'http://www.w3.org/1999/xhtml';
    const value =
      quirks && html
        ? cssObservationChildren(html).find((node) =>
            ['BODY', 'FRAMESET'].includes(elementSlot(node)?.tagName),
          ) || null
        : html;
    if (styleReadCache) styleReadCache.scrollRoot = value;
    return value;
  };
  const owner = (element) => (element === root() ? null : element);
  const remote = (element, action, args = {}) =>
    JSON.parse(
      host.mainWorldInput(
        element ? elementSlot(element).nodeId : 0,
        'scroll',
        JSON.stringify({ action, ...args }),
      ),
    );
  const remoteMutation = (element, action, args = {}) => {
    const result = remote(element, action, args);
    // The main world owns canonical scroll state, but the observation being
    // consumed belongs to this isolated world. Invalidate both sides of that
    // boundary before the inspector callback performs another geometry read.
    invalidateRetainedViewportGeometry();
    styleReadCache = null;
    return result;
  };
  const isolated = () => host.isIsolatedInputWorld();
  const style = (element, name) => cssBoxModel.state(element).get(name) || '';
  const overflow = (element, axis) => {
    const shorthand = style(element, 'overflow').split(/\s+/),
      x = style(element, 'overflow-x') || shorthand[0] || 'visible',
      y = style(element, 'overflow-y') || shorthand[1] || shorthand[0] || 'visible';
    const value = axis === 'x' ? x : y,
      other = axis === 'x' ? y : x;
    return !['visible', 'clip'].includes(other)
      ? value === 'visible'
        ? 'auto'
        : value === 'clip'
          ? 'hidden'
          : value
      : value;
  };
  // In standards mode, when the root has visible overflow, BODY overflow is
  // propagated to the viewport. BODY is not a second scroll container and must
  // not clip, scroll, or offset its descendants independently.
  const bodyOverflowPropagates = (element) =>
    elementSlot(element)?.tagName === 'BODY' &&
    element === document.body &&
    document.compatMode !== 'BackCompat' &&
    root() === document.documentElement &&
    overflow(document.documentElement, 'x') === 'visible' &&
    overflow(document.documentElement, 'y') === 'visible';
  const scrollingOverflow = (element, axis) => {
    const cache =
      styleReadCache?.scrollingOverflow ||
      (styleReadCache && (styleReadCache.scrollingOverflow = new WeakMap()));
    let values = cache?.get(element);
    if (!values) {
      values = bodyOverflowPropagates(element)
        ? { x: 'visible', y: 'visible' }
        : { x: overflow(element, 'x'), y: overflow(element, 'y') };
      cache?.set(element, values);
    }
    return values[axis];
  };
  const children = (element) =>
    cssObservationChildren(elementShadows.get(element) || element).filter(
      (node) => elementSlot(node)?.type === 'element',
    );
  const metrics = (element) =>
    withStyleReadCache(() => {
      element = owner(element);
      const cache = styleReadCache.scrollMetrics || (styleReadCache.scrollMetrics = new WeakMap()),
        cacheKey = element || document;
      if (cache.has(cacheKey)) return cache.get(cacheKey);
      const viewport = host.viewport(),
        base = element
          ? layoutRectInObservation(element)
          : { x: 0, y: 0, clientWidth: viewport.width, clientHeight: viewport.height },
        node = element || documentRoot();
      if (!node || !cssBoxModel.hasBox(node))
        return { width: 0, height: 0, clientWidth: 0, clientHeight: 0, minX: 0, maxX: 0, maxY: 0 };
      const edges = element
        ? cssBoxModel.size(element).edges
        : { bleft: 0, btop: 0, pright: 0, pbottom: 0 };
      const left = base.x + edges.bleft,
        top = base.y + edges.btop,
        cw = element ? base.clientWidth : viewport.width,
        ch = element ? base.clientHeight : viewport.height;
      let right = left + cw,
        bottom = top + ch,
        west = left;
      const visit = (
        current,
        first = false,
        clipLeft = -Infinity,
        clipRight = Infinity,
        clipBottom = Infinity,
      ) => {
        if (!cssBoxModel.hasBox(current)) return;
        const state = cssBoxModel.state(current);
        if (state.position === 'fixed' && !cssBoxModel.fixedContainer(current)) return;
        const r = layoutRectInObservation(current);
        right = Math.max(right, Math.min(r.right, clipRight));
        bottom = Math.max(bottom, Math.min(r.bottom, clipBottom));
        west = Math.min(west, Math.max(r.left, clipLeft));
        // Descendant overflow does not escape a clipping/scrolling box.
        const clipX = !first && scrollingOverflow(current, 'x') !== 'visible',
          clipY = !first && scrollingOverflow(current, 'y') !== 'visible';
        if (clipX && clipY) return;
        for (const child of children(current))
          visit(
            child,
            false,
            clipX ? Math.max(clipLeft, r.left) : clipLeft,
            clipX ? Math.min(clipRight, r.right) : clipRight,
            clipY ? Math.min(clipBottom, r.bottom) : clipBottom,
          );
      };
      if (element) {
        for (const child of children(node)) visit(child);
        const box = cssBoxModel.size(node);
        bottom = Math.max(bottom, top + box.contentHeight + box.edges.ptop + box.edges.pbottom);
        right = Math.max(
          right,
          left + (box.overflowWidth || 0) + box.edges.pleft + box.edges.pright,
        );
      } else visit(node, true);
      const rtl = cssBoxModel.state(node).inherited('direction') === 'rtl',
        width = Math.round(Math.max(cw, rtl ? left + cw - west : right - left)),
        height = Math.round(Math.max(ch, bottom - top));
      const allowX = !element || !['visible', 'clip'].includes(scrollingOverflow(element, 'x')),
        allowY = !element || !['visible', 'clip'].includes(scrollingOverflow(element, 'y'));
      const result = {
        width,
        height,
        clientWidth: cw,
        clientHeight: ch,
        minX: rtl && allowX ? Math.min(0, cw - width) : 0,
        maxX: !rtl && allowX ? Math.max(0, width - cw) : 0,
        maxY: allowY ? Math.max(0, height - ch) : 0,
      };
      cache.set(cacheKey, result);
      return result;
    });
  const enqueue = (element, endOnly = false) => {
    if (!endOnly) pending.add(element || document);
    ending.add(element || document);
    if (eventTimer !== null) return;
    eventTimer = host.setTimer(
      () => {
        eventTimer = null;
        const targets = Array.from(pending),
          ends = Array.from(ending);
        pending.clear();
        ending.clear();
        for (const target of targets)
          dispatchTrusted(target, new Event('scroll', { bubbles: target === document }));
        for (const target of ends)
          if (!animations.has(target))
            dispatchTrusted(target, new Event('scrollend', { bubbles: target === document }));
      },
      0,
      false,
    );
    scheduleIntersectionUpdate();
  };
  const localPosition = (element) => {
    element = owner(element);
    const saved = element
      ? positions.get(element) || { x: 0, y: 0 }
      : { x: windowScrollX, y: windowScrollY };
    if (!saved.x && !saved.y) return saved;
    const known = positionRanges.get(element || document);
    if (
      known &&
      known.version === geometryVersion() &&
      saved.x >= known.minX &&
      saved.x <= known.maxX &&
      saved.y >= 0 &&
      saved.y <= known.maxY
    )
      return saved;
    const m = styleReadCache?.scrollMetrics?.get(element || document) || metrics(element),
      x = Math.max(m.minX, Math.min(m.maxX, saved.x)),
      y = Math.max(0, Math.min(m.maxY, saved.y));
    if (x !== saved.x || y !== saved.y) {
      if (element) positions.set(element, { x, y });
      else {
        windowScrollX = x;
        windowScrollY = y;
      }
      revision++;
      // Clamping may happen inside a geometry read after content shrinks.
      // Keep that read's style/box scope alive; only viewport projections
      // depend on the updated offset. Clearing the active scope here leaves
      // its remaining ancestor reads without their required observation.
      invalidateRetainedViewportGeometry();
      enqueue(element);
    }
    return { x, y };
  };
  const position = (element) => (isolated() ? remote(element, 'position') : localPosition(element));
  const number = (value) => {
    const n = +value;
    return Number.isFinite(n) ? n : 0;
  };
  const set = (element, x, y, animated = false, knownRange = null) => {
    if (isolated()) return remoteMutation(element, 'set', { x, y });
    element = owner(element);
    const old = localPosition(element);
    if (!animated) animations.delete(element || document);
    x = Math.round(x);
    y = Math.round(y);
    // Avoid an O(document size) extent calculation for an exact no-op. This
    // is a common navigation path: sites reset an already-unscrolled window
    // to (0, 0) during DOMContentLoaded.
    if (x === old.x && y === old.y) return;
    const withinKnownRange =
      knownRange && x >= knownRange.minX && x <= knownRange.maxX && y >= 0 && y <= knownRange.maxY;
    const m = withinKnownRange ? knownRange : metrics(element);
    x = Math.max(m.minX, Math.min(m.maxX, x));
    y = Math.max(0, Math.min(m.maxY, y));
    if (x === old.x && y === old.y) return;
    if (element) positions.set(element, { x, y });
    else {
      windowScrollX = x;
      windowScrollY = y;
    }
    if (!element) host.recordScrollPosition(x, y);
    if (withinKnownRange)
      positionRanges.set(element || document, { ...knownRange, version: geometryVersion() });
    if (x || y) hasOffsets = true;
    revision++;
    // A scroll is an observation boundary even though the DOM epoch is
    // unchanged: getBoundingClientRect() and hit testing use viewport space.
    styleReadCache = null;
    enqueue(element);
  };
  const options = (args, relative = false) => {
    if (args.length > 1)
      return { x: number(args[0]), y: number(args[1]), behavior: 'auto', relative };
    const value = args[0];
    if (value != null && typeof value !== 'object' && typeof value !== 'function')
      throw new TypeError('Scroll options must be a dictionary');
    const behavior = String(value?.behavior ?? 'auto'),
      left = value?.left,
      top = value?.top;
    if (!['auto', 'instant', 'smooth'].includes(behavior))
      throw new TypeError('Invalid scroll behavior');
    return {
      x: left === undefined ? null : number(left),
      y: top === undefined ? null : number(top),
      behavior,
      relative,
    };
  };
  const scroll = (element, opts, knownRange = null) => {
    if (isolated()) return remoteMutation(element, 'scroll', { opts });
    const old = position(element),
      x = opts.x === null ? old.x : opts.x + (opts.relative ? old.x : 0),
      y = opts.y === null ? old.y : opts.y + (opts.relative ? old.y : 0);
    if (
      opts.behavior === 'smooth' ||
      (opts.behavior === 'auto' &&
        withStyleReadCache(() => style(element || document.documentElement, 'scroll-behavior')) ===
          'smooth')
    ) {
      element = owner(element);
      const key = element || document,
        m = metrics(element),
        to = { x: Math.max(m.minX, Math.min(m.maxX, x)), y: Math.max(0, Math.min(m.maxY, y)) };
      animations.delete(key);
      if (old.x === to.x && old.y === to.y) return;
      // Desktop Chromium's delta-based programmatic curve; frame sampling is
      // driven by this Page's scheduler, never a separate compositor thread.
      const duration = Math.min(
          1500,
          (Math.sqrt(Math.max(Math.abs(to.x - old.x), Math.abs(to.y - old.y))) * 1000) / 60,
        ),
        animation = { start: null };
      animations.set(key, animation);
      const tick = (time) => {
        if (animations.get(key) !== animation) return;
        if (animation.start === null) animation.start = time;
        const progress = Math.min(1, (time - animation.start) / duration);
        let low = 0,
          high = 1;
        for (let i = 0; i < 24; i++) {
          const t = (low + high) / 2,
            value = 1.2 * t * (1 - t) * (1 - t) + t * t * t;
          if (value < progress) low = t;
          else high = t;
        }
        const t = (low + high) / 2,
          eased = 3 * t * t - 2 * t * t * t;
        if (progress === 1) animations.delete(key);
        set(
          element,
          progress === 1 ? to.x : old.x + (to.x - old.x) * eased,
          progress === 1 ? to.y : old.y + (to.y - old.y) * eased,
          true,
        );
        if (progress === 1) enqueue(element, true);
        if (progress < 1) requestRenderingFrame(tick);
      };
      requestRenderingFrame(tick);
      return;
    }
    set(element, x, y, false, knownRange);
  };
  const baseOffset = (element) => {
    if (!hasOffsets) return { x: 0, y: 0 };
    let x = 0,
      y = 0,
      fixed = false;
    for (let node = element; node; node = geometryParent(node)) {
      if (node !== element && node !== root()) {
        const p = localPosition(node);
        x += p.x;
        y += p.y;
      }
      if (cssBoxModel.state(node).position === 'fixed' && !cssBoxModel.fixedContainer(node)) {
        fixed = true;
        break;
      }
    }
    if (!fixed) {
      const p = localPosition(null);
      x += p.x;
      y += p.y;
    }
    return { x, y };
  };
  const offset = (element) => {
    const result = baseOffset(element);
    if (!hasOffsets) return result;
    for (let node = element; node; node = geometryParent(node)) {
      const state = cssBoxModel.state(node);
      if (state.position !== 'sticky') continue;
      const r = layoutRectInObservation(node),
        own = baseOffset(node);
      let container = geometryParent(node);
      while (
        container &&
        container !== root() &&
        scrollingOverflow(container, 'x') === 'visible' &&
        scrollingOverflow(container, 'y') === 'visible'
      )
        container = geometryParent(container);
      const viewport = host.viewport(),
        bounds =
          container && container !== root()
            ? layoutRectInObservation(container)
            : { x: 0, y: 0, width: viewport.width, height: viewport.height },
        shift = container && container !== root() ? baseOffset(container) : { x: 0, y: 0 };
      const parent = geometryParent(node),
        pr = parent ? layoutRectInObservation(parent) : null,
        po = parent ? baseOffset(parent) : { x: 0, y: 0 };
      for (const [axis, start, end, size] of [
        ['x', 'left', 'right', 'width'],
        ['y', 'top', 'bottom', 'height'],
      ]) {
        const near = state.length(state.get(start), bounds[size]),
          far = state.length(state.get(end), bounds[size]);
        let at = r[axis] - own[axis];
        if (near !== null) at = Math.max(at, bounds[axis] - shift[axis] + near);
        else if (far !== null)
          at = Math.min(at, bounds[axis] - shift[axis] + bounds[size] - far - r[size]);
        if (pr)
          at = Math.max(
            pr[axis] - po[axis],
            Math.min(at, pr[axis] - po[axis] + pr[size] - r[size]),
          );
        result[axis] -= at - (r[axis] - own[axis]);
      }
    }
    return result;
  };
  const delta = (start, end, near, far, alignment) =>
    alignment === 'start'
      ? start - near
      : alignment === 'end'
        ? end - far
        : alignment === 'center'
          ? (start + end - near - far) / 2
          : (start < near && end > far) || (start >= near && end <= far)
            ? 0
            : start < near
              ? end - start > far - near
                ? end - far
                : start - near
              : end - start > far - near
                ? start - near
                : end - far;
  const extendRootRange = (element, range, requiredBottom, oldY) => {
    // A later non-fixed border box is direct evidence that the document can
    // scroll far enough to expose that box. Prefer nearby following siblings
    // over constructing the complete scroll-overflow projection merely to
    // validate one scrollIntoView offset.
    let cursor = element,
      inspected = 0;
    while (cursor && inspected < 64) {
      const parent = geometryParent(cursor);
      if (!parent) break;
      const siblings = children(parent).filter((node) => elementSlot(node)?.type === 'element'),
        index = siblings.indexOf(cursor);
      if (index >= 0)
        for (let i = index + 1; i < siblings.length && inspected < 64; i++) {
          const candidate = siblings[i];
          inspected++;
          if (!cssBoxModel.hasBox(candidate) || cssBoxModel.state(candidate).position === 'fixed')
            continue;
          const box = clientRectInObservation(candidate),
            bottom = box.bottom + oldY;
          if (Number.isFinite(bottom))
            range.maxY = Math.max(range.maxY, bottom - range.clientHeight);
          if (range.maxY >= requiredBottom - range.clientHeight) return range;
        }
      cursor = parent;
    }
    return range;
  };
  const into = (element, opts) => {
    if (isolated()) return remoteMutation(element, 'into', { opts });
    if (!element?.isConnected) return;
    // The common actionability case asks to scroll an already visible element.
    // If no intermediate ancestor establishes a scrolling/clipping context, the
    // viewport containment test is sufficient and avoids materializing document
    // scroll extents or ancestor layout.
    if (
      opts.ifNeeded &&
      withStyleReadCache(() => {
        for (let p = geometryParent(element); p && p !== root(); p = geometryParent(p)) {
          if (bodyOverflowPropagates(p)) continue;
          if (scrollingOverflow(p, 'x') !== 'visible' || scrollingOverflow(p, 'y') !== 'visible')
            return false;
        }
        const r = clientRectInObservation(element),
          viewport = host.viewport(),
          node = document.documentElement,
          px = (key) => parseFloat(style(node, key)) || 0;
        return (
          r.width > 0 &&
          r.height > 0 &&
          r.left >= px('scroll-padding-left') &&
          r.right <= viewport.width - px('scroll-padding-right') &&
          r.top >= px('scroll-padding-top') &&
          r.bottom <= viewport.height - px('scroll-padding-bottom')
        );
      })
    )
      return;
    if (!cssBoxModel.hasBox(element)) return;
    const ancestors = [];
    for (let p = geometryParent(element); p; p = geometryParent(p))
      if (p !== root() && !withStyleReadCache(() => bodyOverflowPropagates(p))) ancestors.push(p);
    ancestors.push(null);
    const targetRect = () => {
      const r = clientRectInObservation(element);
      if (!opts.rect) return r;
      const e = cssBoxModel.size(element).edges,
        x = r.x + (opts.frameRect ? e.bleft : 0) + opts.rect.x,
        y = r.y + (opts.frameRect ? e.btop : 0) + opts.rect.y;
      return {
        x,
        y,
        left: x,
        top: y,
        right: x + opts.rect.width,
        bottom: y + opts.rect.height,
        width: opts.rect.width,
        height: opts.rect.height,
      };
    };
    for (const ancestor of ancestors) {
      let scrollable = false;
      withStyleReadCache(() => {
        // The root scroll extent requires walking every laid-out descendant. For
        // scroll-if-needed (notably the implicit scroll performed by focus()),
        // first use the viewport we already know. A visible target cannot cause
        // a root scroll, regardless of the document's total extent.
        if (opts.ifNeeded && !ancestor) {
          const r = targetRect(),
            viewport = host.viewport(),
            node = document.documentElement,
            px = (key) => parseFloat(style(node, key)) || 0;
          if (
            r.left >= px('scroll-padding-left') &&
            r.right <= viewport.width - px('scroll-padding-right') &&
            r.top >= px('scroll-padding-top') &&
            r.bottom <= viewport.height - px('scroll-padding-bottom')
          )
            return;
        }
        let knownRange = null;
        if (!ancestor) {
          const viewport = host.viewport(),
            rootBox = layoutRectInObservation(documentRoot());
          // A rendered root proves that at least this much content exists. It
          // can undercount positioned overflow, but any offset inside the lower
          // bound is valid without enumerating every descendant.
          knownRange = {
            width: Math.max(viewport.width, rootBox.width),
            height: Math.max(viewport.height, rootBox.height),
            clientWidth: viewport.width,
            clientHeight: viewport.height,
            minX: 0,
            // Keep horizontal scrolling on the regular metrics path. The
            // root's width alone does not prove the signed scroll range in
            // RTL documents.
            maxX: 0,
            maxY: Math.max(0, rootBox.height - viewport.height),
          };
        }
        let m = knownRange || metrics(ancestor);
        const r = targetRect(),
          a = ancestor ? clientRectInObservation(ancestor) : { x: 0, y: 0 },
          edges = ancestor ? cssBoxModel.size(ancestor).edges : { bleft: 0, btop: 0 },
          old = position(ancestor);
        const px = (node, key) => parseFloat(style(node, key)) || 0,
          node = ancestor || document.documentElement;
        const x = a.x + edges.bleft + px(node, 'scroll-padding-left'),
          y = a.y + edges.btop + px(node, 'scroll-padding-top');
        if (
          opts.ifNeeded &&
          r.left >= x &&
          r.right <= a.x + edges.bleft + m.clientWidth &&
          r.top >= y &&
          r.bottom <= a.y + edges.btop + m.clientHeight
        )
          return;
        const dx = delta(
          r.left - px(element, 'scroll-margin-left'),
          r.right + px(element, 'scroll-margin-right'),
          x,
          a.x + edges.bleft + m.clientWidth - px(node, 'scroll-padding-right'),
          opts.inline,
        );
        const dy = delta(
          r.top - px(element, 'scroll-margin-top'),
          r.bottom + px(element, 'scroll-margin-bottom'),
          y,
          a.y + edges.btop + m.clientHeight - px(node, 'scroll-padding-bottom'),
          opts.block,
        );
        if (knownRange) {
          const requestedX = old.x + dx,
            requestedY = old.y + dy;
          if (requestedY > knownRange.maxY)
            extendRootRange(element, knownRange, requestedY + knownRange.clientHeight, old.y);
          if (
            requestedX < knownRange.minX ||
            requestedX > knownRange.maxX ||
            requestedY > knownRange.maxY
          ) {
            knownRange = null;
            m = metrics(ancestor);
          }
        }
        if (!m.maxX && !m.minX && !m.maxY) return;
        scrollable = true;
        scroll(
          ancestor,
          { x: old.x + dx, y: old.y + dy, behavior: opts.behavior || 'auto' },
          knownRange,
        );
      });
      if (opts.container === 'nearest' && scrollable) break;
    }
    if (opts.container !== 'nearest' && windowRelations.self !== windowRelations.top) {
      const rect = withStyleReadCache(targetRect);
      host.scrollParentFrame(
        JSON.stringify({
          action: 'into',
          opts: {
            ...opts,
            frameRect: true,
            rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
          },
        }),
      );
    }
  };
  const clipped = (element, x, y) => {
    for (let p = geometryParent(element); p; p = geometryParent(p)) {
      if (cssBoxModel.state(element).position === 'fixed' && !cssBoxModel.fixedContainer(element))
        break;
      const r = clientRectInObservation(p);
      if (
        (scrollingOverflow(p, 'x') !== 'visible' && (x < r.left || x >= r.right)) ||
        (scrollingOverflow(p, 'y') !== 'visible' && (y < r.top || y >= r.bottom))
      )
        return true;
    }
    return false;
  };
  const wheel = (element, dx, dy) => {
    for (let p = element; ; p = geometryParent(p)) {
      if (p && bodyOverflowPropagates(p)) continue;
      const target = p === root() ? null : p,
        old = position(target),
        m = metrics(target);
      const canX = !p || scrollingOverflow(p, 'x') !== 'hidden',
        canY = !p || scrollingOverflow(p, 'y') !== 'hidden';
      set(target, old.x + (canX ? dx : 0), old.y + (canY ? dy : 0));
      const next = position(target);
      if (next.x !== old.x || next.y !== old.y) return true;
      if (
        p &&
        withStyleReadCache(() =>
          /contain|none/.test(
            style(p, 'overscroll-behavior') + ' ' + style(p, 'overscroll-behavior-y'),
          ),
        )
      )
        return false;
      if (!p) return false;
    }
  };
  const receiver = (value) => {
    requireRealmBinding(value, 'ElementGeometry');
    return value;
  };
  const operation = (element, params) =>
    callRealmBinding(element, requireRealmBinding(element, 'ElementGeometry'), 'scroll', [params]);
  const method = (object, name, value) =>
    Object.defineProperty(object, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  for (const [name, axis] of [
    ['scrollLeft', 'x'],
    ['scrollTop', 'y'],
  ])
    Object.defineProperty(Element.prototype, name, {
      get() {
        return operation(this, { action: 'position' })[axis];
      },
      set(value) {
        receiver(this);
        value = number(value);
        const old = operation(this, { action: 'position' });
        operation(this, {
          action: 'set',
          x: axis === 'x' ? value : old.x,
          y: axis === 'y' ? value : old.y,
        });
      },
      enumerable: true,
      configurable: true,
    });
  for (const [name, key] of [
    ['scrollWidth', 'width'],
    ['scrollHeight', 'height'],
  ]) {
    // The generated surface used to put these on HTMLElement. Element owns them.
    delete HTMLElement.prototype[name];
    Object.defineProperty(Element.prototype, name, {
      get() {
        return operation(this, { action: 'metrics' })[key];
      },
      enumerable: true,
      configurable: true,
    });
  }
  const originalScrollingElement = Object.getOwnPropertyDescriptor(
    Document.prototype,
    'scrollingElement',
  );
  Object.defineProperty(Document.prototype, 'scrollingElement', {
    get() {
      if (this !== document) return originalScrollingElement.get.call(this);
      return root();
    },
    enumerable: true,
    configurable: true,
  });
  for (const [name, relative] of [
    ['scroll', false],
    ['scrollTo', false],
    ['scrollBy', true],
  ]) {
    method(Element.prototype, name, function () {
      receiver(this);
      operation(this, { action: 'scroll', opts: options(arguments, relative) });
    });
    method(window, name, function () {
      scroll(null, options(arguments, relative));
    });
  }
  method(Element.prototype, 'scrollIntoView', function (value) {
    receiver(this);
    const opts = { behavior: 'auto', block: 'start', inline: 'nearest', container: 'all' };
    if (typeof value === 'boolean') opts.block = value ? 'start' : 'end';
    else if (value != null) {
      if (typeof value !== 'object' && typeof value !== 'function')
        throw new TypeError('Scroll options must be a dictionary');
      for (const key of ['behavior', 'block', 'container', 'inline'])
        if (value[key] !== undefined) opts[key] = String(value[key]);
    }
    if (
      !['auto', 'instant', 'smooth'].includes(opts.behavior) ||
      !['start', 'center', 'end', 'nearest'].includes(opts.block) ||
      !['start', 'center', 'end', 'nearest'].includes(opts.inline) ||
      !['all', 'nearest'].includes(opts.container)
    )
      throw new TypeError('Invalid scrollIntoView options');
    operation(this, { action: 'into', opts });
  });
  method(Element.prototype, 'scrollIntoViewIfNeeded', function (center = true) {
    receiver(this);
    operation(this, {
      action: 'into',
      opts: {
        block: center ? 'center' : 'nearest',
        inline: center ? 'center' : 'nearest',
        container: 'all',
        behavior: 'instant',
        ifNeeded: true,
      },
    });
  });
  bootstrapRestoreHooks.push(() => {
    positions = new WeakMap();
    animations = new WeakMap();
    revision = 0;
    hasOffsets = false;
    pending.clear();
    ending.clear();
    eventTimer = null;
    windowScrollX = windowScrollY = 0;
  });
  const dispatch = (element, p) => {
    if (element) {
      const foreign = foreignCSSObservation(element, 'scroll', JSON.stringify(p));
      if (foreign !== null) return foreign;
    }
    if (isolated()) return remoteMutation(element, p.action, p);
    if (p.action === 'protocolInto') {
      if (!element?.isConnected) throw Error('Node is detached from document');
      into(element, p.opts);
      return {};
    }
    if (p.action === 'restore') {
      if (historySlots.get(hist).scrollRestoration !== 'manual') set(null, p.x, p.y);
      return {};
    }
    if (p.action === 'fragment') {
      into(element, { block: 'start', inline: 'nearest', behavior: 'auto' });
      return {};
    }
    return (
      (p.action === 'position'
        ? position(element)
        : p.action === 'metrics'
          ? metrics(element)
          : p.action === 'set'
            ? set(element, p.x, p.y)
            : p.action === 'scroll'
              ? scroll(element, p.opts)
              : into(element, p.opts)) ?? {}
    );
  };
  const snapshot = () => {
    const values = {};
    const visit = (tree) => {
      for (const node of compatibilitySelectors.query(tree, '*')) {
        const p = positions.get(node);
        if (p && (p.x || p.y)) values[elementSlot(node).nodeId] = [p.x, p.y];
        const shadow = elementShadows.get(node);
        if (shadow) visit(shadow);
      }
    };
    if (hasOffsets) visit(document);
    return values;
  };
  return {
    position,
    metrics,
    set,
    scroll,
    into,
    offset,
    clipped,
    wheel,
    snapshot,
    revision: () => revision,
    dispatch,
  };
})();
