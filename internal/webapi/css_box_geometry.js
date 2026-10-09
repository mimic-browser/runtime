// A box graph projects a canonical CSS/DOM epoch. Style-derived data can be
// reused after exact input validation; mutated geometry gets a fresh graph.
// CSS lengths leave room for float conversion within signed 26.6 LayoutUnit.
const cssGeometryLength = (value) =>
  Math.fround(Math.max(-(2 ** 31) / 64 + 2, Math.min(Math.trunc((2 ** 31 - 1) / 64) - 2, value)));
const cssBoxModel = (() => {
  // Compact shaping results contain no element state. Retain a bounded JS
  // projection across DOM epochs to avoid re-transferring and parsing the same
  // native metrics. Font selection has its own authoritative realm revision.
  let textMetrics = new Map(),
    nodeTextMetrics = new WeakMap(),
    nodeTextLines = new WeakMap(),
    textMetricBytes = 0,
    textMetricVersion;
  bootstrapRestoreHooks.push(() => {
    textMetrics = new Map();
    nodeTextMetrics = new WeakMap();
    nodeTextLines = new WeakMap();
    textMetricBytes = 0;
    textMetricVersion = undefined;
  });
  const measureText = (key, text, family, size, weight, italic) => {
    const version =
      styleReadCache.fontCollectionVersion ??
      (styleReadCache.fontCollectionVersion = host.fontCollectionVersion());
    if (version !== textMetricVersion) {
      textMetrics.clear();
      textMetricBytes = 0;
      textMetricVersion = version;
    }
    const known = textMetrics.get(key);
    if (known) return known.value;
    const encoded = host.shapeTextMetrics(text, family, size, weight, Number(italic), 0, 0),
      value = JSON.parse(encoded);
    const bytes = (key.length + encoded.length) * 2 + 256;
    if (!value.error && bytes <= 1024 * 1024) {
      while (textMetricBytes + bytes > 1024 * 1024) {
        const oldest = textMetrics.keys().next().value;
        textMetricBytes -= textMetrics.get(oldest).bytes;
        textMetrics.delete(oldest);
      }
      textMetrics.set(key, { value, bytes });
      textMetricBytes += bytes;
    }
    return value;
  };
  const tag = (element) => String(elementSlot(element)?.tagName || '').toUpperCase(),
    textContent = (element) => {
      const cache = styleReadCache.textContents || (styleReadCache.textContents = new WeakMap());
      if (cache.has(element)) return cache.get(element);
      const value = canonicalTextContent(elementSlot(element).nodeId);
      cache.set(element, value);
      return value;
    };
  const unit = (value) => Math.trunc(cssGeometryLength(value) * 64) / 64,
    collapse = (a, b) => Math.max(a, b, 0) + Math.min(a, b, 0);
  const invisible = new Set([
    'STYLE',
    'SCRIPT',
    'HEAD',
    'TITLE',
    'META',
    'LINK',
    'TEMPLATE',
    'OPTION',
    'NOSCRIPT',
  ]);
  const tableDisplays = {
    TABLE: 'table',
    CAPTION: 'table-caption',
    TBODY: 'table-row-group',
    THEAD: 'table-header-group',
    TFOOT: 'table-footer-group',
    TR: 'table-row',
    TD: 'table-cell',
    TH: 'table-cell',
  };
  const blocks = new Set([
    'HTML',
    'BODY',
    'DIV',
    'P',
    'SECTION',
    'MAIN',
    'ARTICLE',
    'ASIDE',
    'HEADER',
    'FOOTER',
    'NAV',
    'FORM',
    'FIELDSET',
    'DETAILS',
    'SUMMARY',
    'H1',
    'H2',
    'H3',
    'H4',
    'H5',
    'H6',
    'UL',
    'OL',
    'LI',
    'TABLE',
    'CAPTION',
    'TBODY',
    'THEAD',
    'TFOOT',
    'TR',
    'TD',
    'TH',
  ]);
  let retainedStyles = new WeakMap();
  bootstrapRestoreHooks.push(() => {
    retainedStyles = new WeakMap();
  });
  const styleContext = (element) => {
    if (!styleReadCache.retainable) return null;
    const contexts =
        styleReadCache.boxStyleContexts || (styleReadCache.boxStyleContexts = new WeakMap()),
      pending = [];
    let node = element,
      context = null;
    while (node) {
      if (contexts.has(node)) {
        context = contexts.get(node);
        break;
      }
      pending.push(node);
      node = geometryParent(node);
    }
    // A detached tree may still resolve rem against this document's root.
    const rootFont = cssComputedRootFontSize();
    for (let i = pending.length - 1; i >= 0; i--) {
      const current = pending[i],
        entries = computedCSSDeclarations(current),
        attrs = cssObservationNodeState(current).attributes;
      const extra = JSON.stringify([attrs.hidden, attrs.type, attrs['font-size'], attrs.dir]),
        environment = styleReadCache.environmentVersion;
      const prior = retainedStyles.get(current);
      const next =
        prior &&
        prior.entries === entries &&
        prior.parent === context &&
        prior.extra === extra &&
        prior.environment === environment &&
        prior.rootFont === rootFont
          ? prior
          : { entries, parent: context, extra, environment, rootFont };
      retainedStyles.set(current, next);
      contexts.set(current, next);
      context = next;
    }
    return context;
  };
  const state = (element) => {
    const cache = styleReadCache.boxStyles || (styleReadCache.boxStyles = new WeakMap());
    if (cache.has(element)) return cache.get(element);
    const nativeDisplay = blitzStyleValue(element, 'display');
    const context = nativeDisplay !== null ? null : styleContext(element);
    if (context?.value) {
      cache.set(element, context.value);
      return context.value;
    }
    const entries = nativeDisplay !== null ? [] : computedCSSDeclarations(element),
      properties = new Map(entries.map((entry) => [entry.name, entry.value])),
      resolved = new Map();
    const get = (name) => {
      if (resolved.has(name)) return resolved.get(name);
      if (nativeDisplay !== null) {
        const value = blitzStyleValue(element, name);
        resolved.set(name, value);
        return value;
      }
      const value = geometryValue(
        element,
        properties.get(name) ??
          (tag(element) === 'BODY' && /^margin-(top|right|bottom|left)$/.test(name)
            ? '8px'
            : undefined),
      );
      resolved.set(name, value);
      return value;
    };
    const hiddenInput =
      tag(element) === 'INPUT' &&
      String(host.getAttribute(elementSlot(element).nodeId, 'type') || '').toLowerCase() ===
        'hidden';
    const display = hiddenInput
        ? 'none'
        : get('display') ||
          (invisible.has(tag(element)) ||
          host.getAttribute(elementSlot(element).nodeId, 'hidden') !== null
            ? 'none'
            : tableDisplays[tag(element)] ||
              (tag(element) === 'SUMMARY'
                ? 'list-item'
                : blocks.has(tag(element))
                  ? 'block'
                  : 'inline')),
      position = get('position') || 'static';
    const result = { element, entries, get, display, position };
    cache.set(element, result);
    const ownInherited = new Map();
    result.inherited = (name) => {
      if (nativeDisplay !== null) return get(name);
      if (ownInherited.has(name)) return ownInherited.get(name);
      const inherited =
        styleReadCache.inheritedValues || (styleReadCache.inheritedValues = new WeakMap());
      const visited = [];
      let value = null;
      for (let p = element; p; p = geometryParent(p)) {
        const known = inherited.get(p);
        if (known?.has(name)) {
          value = known.get(name);
          break;
        }
        visited.push(p);
        let v = computedCSSDeclarations(p).find((e) => e.name === name)?.value;
        if (!v && name === 'direction') {
          const dir = cssObservationAttribute(p, 'dir');
          if (dir === 'rtl' || dir === 'ltr') v = dir;
        }
        if (v && !['inherit', 'unset'].includes(v)) {
          value = v;
          break;
        }
      }
      for (const p of visited) {
        let values = inherited.get(p);
        if (!values) inherited.set(p, (values = new Map()));
        values.set(name, value);
      }
      ownInherited.set(name, value);
      return value;
    };
    const resolveLength = (text, basis = 0) => {
      if (text == null || ['auto', 'none', 'normal', 'initial', 'unset'].includes(text))
        return null;
      const value = cssResolveLength(
        text,
        cssGeometryLengthContext(element, basis, result.fontSize ?? 16),
      );
      if (value !== null) return unit(value);
      const viewport = /^([+-]?[\d.]+)(vw|vh|vmin|vmax)$/.exec(text);
      if (viewport) {
        const size = host.viewport();
        return unit(
          (Number(viewport[1]) *
            {
              vw: size.width,
              vh: size.height,
              vmin: Math.min(size.width, size.height),
              vmax: Math.max(size.width, size.height),
            }[viewport[2]]) /
            100,
        );
      }
      return null;
    };
    const lengths = new Map();
    let lengthFont;
    result.length = (text, basis = 0) => {
      if (text == null) return null;
      const font = result.fontSize ?? 16;
      if (lengthFont !== font) {
        lengths.clear();
        lengthFont = font;
      }
      let values = lengths.get(text);
      if (!values) {
        if (lengths.size >= 64) lengths.delete(lengths.keys().next().value);
        lengths.set(text, (values = new Map()));
      }
      if (values.has(basis)) return values.get(basis);
      const value = resolveLength(text, basis);
      if (values.size >= 4) values.delete(values.keys().next().value);
      values.set(basis, value);
      return value;
    };
    const edgesByBasis = new Map();
    let edgesFont;
    result.edges = (basis) => {
      const font = result.fontSize ?? 16;
      if (edgesFont !== font) {
        edgesByBasis.clear();
        edgesFont = font;
      }
      if (edgesByBasis.has(basis)) return { ...edgesByBasis.get(basis) };
      const out = {};
      for (const side of ['top', 'right', 'bottom', 'left']) {
        const borderStyle = get('border-' + side + '-style'),
          borderWidth = get('border-' + side + '-width');
        out['b' + side] =
          borderStyle && ['none', 'hidden'].includes(borderStyle)
            ? 0
            : borderWidth
              ? Math.max(
                  0,
                  Math.floor(
                    result.length(borderWidth) ??
                      ({ thin: 1, medium: 3, thick: 5 }[borderWidth] || 0),
                  ),
                )
              : 0;
        out['p' + side] = Math.max(
          0,
          result.length(get('padding-' + side), basis) ??
            (['TD', 'TH'].includes(tag(element)) ? 1 : 0),
        );
        out['m' + side] = result.length(get('margin-' + side), basis) || 0;
      }
      // Layout may adjust auto margins on its own edge record. Never share that
      // mutable record with another size/width calculation.
      if (edgesByBasis.size >= 4) edgesByBasis.delete(edgesByBasis.keys().next().value);
      edgesByBasis.set(basis, { ...out });
      return out;
    };
    // Style-only observations need display/visibility without font metrics.
    // Publish the complete recursive state before resolving the font on demand;
    // reentrant geometry retains the same undefined/default basis as before.
    let fontSize,
      resolvingFont = false;
    Object.defineProperty(result, 'fontSize', {
      get() {
        if (fontSize !== undefined || resolvingFont) return fontSize;
        resolvingFont = true;
        try {
          fontSize =
            nativeDisplay !== null
              ? parseFloat(get('font-size'))
              : (cssComputedFontSize(element) ?? 16);
        } finally {
          resolvingFont = false;
        }
        return fontSize;
      },
    });
    // Only style-derived data is retained. Sizes, positions, children, text flow
    // and availability are rebuilt in the current geometry graph after mutation.
    if (context && styleReadCache.retainable) context.value = result;
    return result;
  };
  const children = (element) => cssObservationChildren(elementShadows.get(element) || element);
  const rendered = (element) => {
    const cache = styleReadCache.rendered || (styleReadCache.rendered = new WeakMap()),
      visited = [];
    let visible = true;
    for (let parent = element; parent; parent = geometryParent(parent)) {
      if (cache.has(parent)) {
        visible = cache.get(parent);
        break;
      }
      visited.push(parent);
      if (state(parent).display === 'none') {
        visible = false;
        break;
      }
    }
    for (const node of visited) cache.set(node, visible);
    return visible;
  };
  const textInfo = (element, text) => {
    const s = state(element),
      family = s.inherited('font-family') || '"Times New Roman"',
      weight = Number(s.inherited('font-weight')) || (tag(element) === 'TH' ? 700 : 400),
      italic = s.inherited('font-style') === 'italic';
    if (s.fontSize === 0) {
      const raw = s.inherited('line-height'),
        height = raw && raw !== 'normal' ? (cssNumberRegex.test(raw) ? 0 : s.length(raw, 0)) : 0;
      return { width: 0, height: height ?? 0, ascent: 0, descent: 0 };
    }
    // A layout epoch may change because a sibling moved or acquired a class.
    // That does not change this node's text shaping inputs. Keep its last
    // compact result with a weak owner instead of cycling the entire document
    // through the bounded cross-node string cache on each such mutation.
    const version =
        styleReadCache.fontCollectionVersion ??
        (styleReadCache.fontCollectionVersion = host.fontCollectionVersion()),
      prior = nodeTextMetrics.get(element);
    let shaped;
    if (
      prior &&
      prior.version === version &&
      prior.text === text &&
      prior.family === family &&
      prior.size === s.fontSize &&
      prior.weight === weight &&
      prior.italic === italic
    )
      shaped = prior.shaped;
    else {
      const key = JSON.stringify([text, family, s.fontSize, weight, italic]);
      shaped = measureText(key, text, family, s.fontSize, weight, italic);
      if (!shaped.error && text.length <= 8192)
        nodeTextMetrics.set(element, {
          version,
          text,
          family,
          size: s.fontSize,
          weight,
          italic,
          shaped,
        });
      else nodeTextMetrics.delete(element);
    }
    if (shaped.error) {
      host.semanticMissingAt('css_box_geometry.js/textInfo', 'CSS.textBoxMetrics');
      return { width: 0, height: 0, ascent: 0, descent: 0 };
    }
    const raw = s.inherited('line-height'),
      height =
        raw && raw !== 'normal'
          ? cssNumberRegex.test(raw)
            ? unit(Number(raw) * s.fontSize)
            : s.length(raw, s.fontSize)
          : shaped.ascent + shaped.descent + (shaped.lineGap || 0);
    return {
      width: Math.ceil(shaped.advance * 64) / 64,
      height: height ?? shaped.ascent + shaped.descent + (shaped.lineGap || 0),
      ascent: shaped.ascent,
      descent: shaped.descent,
    };
  };
  const textOf = (element) =>
    children(element)
      .filter((n) => elementSlot(n)?.type === 'text')
      .map((n) => textContent(n))
      .join('')
      .replace(/[\t\n\r\f ]+/g, ' ')
      .trim();
  const controlSize = (element) => {
    const cache = styleReadCache.controlSizes || (styleReadCache.controlSizes = new WeakMap());
    if (cache.has(element)) return cache.get(element);
    const result = uncachedControlSize(element);
    cache.set(element, result);
    return result;
  };
  const uncachedControlSize = (element) => {
    const s = state(element);
    if (tag(element) === 'IFRAME') {
      const edges = s.edges(containingWidth(element));
      return {
        width: 300 + edges.pleft + edges.pright + edges.bleft + edges.bright,
        height: 150 + edges.ptop + edges.pbottom + edges.btop + edges.bbottom,
      };
    }
    if (tag(element) === 'BUTTON' && !textContent(element)) return { width: 16, height: 6 };
    if (tag(element) === 'PROGRESS') return { width: s.fontSize * 10, height: s.fontSize };
    return compatibilityElementState.controlGeometry?.(element, s.entries) || null;
  };
  const intrinsic = (element) => {
    const cache =
      styleReadCache.intrinsicWidths || (styleReadCache.intrinsicWidths = new WeakMap());
    if (cache.has(element)) return cache.get(element);
    const plans =
        styleReadCache.intrinsicWidthPlans || (styleReadCache.intrinsicWidthPlans = new WeakMap()),
      plan = plans.get(element);
    // Cyclic min-content contributions use the legacy zero fallback, but it is
    // deliberately not published as a completed measurement.
    if (plan?.state === 'computing') return 0;
    plans.set(element, { state: 'computing' });
    const value = uncachedIntrinsic(element);
    cache.set(element, value);
    plans.set(element, { state: 'ready' });
    return value;
  };
  const uncachedIntrinsic = (element) => {
    const s = state(element);
    if (s.display === 'none') return 0;
    const own = controlSize(element);
    if (own) return own.width;
    if (s.display === 'table') return tableColumns(element).width;
    const listType =
        s.get('list-style-type') || (tag(element) === 'SUMMARY' ? 'disclosure-closed' : ''),
      inside = s.get('list-style-position') === 'inside' || tag(element) === 'SUMMARY';
    // Disclosure markers are geometric symbols: Blink uses .66em plus .4em
    // inline margin, each quantized independently to a layout unit.
    const marker =
      s.display === 'list-item' &&
      inside &&
      ['disclosure-open', 'disclosure-closed'].includes(listType)
        ? unit(s.fontSize * 0.66) + unit(s.fontSize * 0.4)
        : 0;
    let width = textInfo(element, textOf(element)).width + marker,
      line = width;
    const rowFlex =
      /^(?:inline-)?flex$/.test(s.display) &&
      !(s.get('flex-direction') || 'row').startsWith('column');
    for (const child of children(element)) {
      if (elementSlot(child)?.type !== 'element') continue;
      const c = state(child);
      if (c.display === 'none' || ['absolute', 'fixed'].includes(c.position)) continue;
      const edges = c.edges(0),
        value =
          (c.length(c.get('width')) ?? intrinsic(child)) +
          edges.pleft +
          edges.pright +
          edges.bleft +
          edges.bright +
          edges.mleft +
          edges.mright;
      if (
        rowFlex ||
        ['inline', 'inline-block', 'inline-flex'].includes(c.display) ||
        replacedGeometryTags.has(tag(child))
      ) {
        line += value;
        width = Math.max(width, line);
      } else {
        width = Math.max(width, line, value);
        line = 0;
      }
    }
    return Math.max(width, line);
  };
  const tableColumns = (table) => {
    const cache = styleReadCache.tableColumns || (styleReadCache.tableColumns = new WeakMap());
    if (cache.has(table)) return cache.get(table);
    const rows = compatibilitySelectors.query(table, 'tr').filter((row) => {
        for (let p = geometryParent(row); p; p = geometryParent(p)) {
          if (tag(p) === 'TABLE') return p === table;
        }
        return false;
      }),
      columns = [],
      cells = [];
    const s = state(table),
      spacing = s.length(s.get('border-spacing')?.split(/\s+/)[0]) ?? 2;
    for (const row of rows) {
      const list = children(row).filter((cell) => ['TD', 'TH'].includes(tag(cell)));
      cells.push(list);
      for (let i = 0; i < list.length; i++) {
        const cell = list[i],
          c = state(cell),
          e = c.edges(0),
          w =
            (c.length(c.get('width')) ?? intrinsic(cell)) + e.pleft + e.pright + e.bleft + e.bright;
        columns[i] = Math.max(columns[i] || 0, w);
      }
    }
    const e = s.edges(0),
      width =
        columns.reduce((a, b) => a + b, 0) + spacing * (columns.length + 1) + e.bleft + e.bright;
    const result = { rows, cells, columns, spacing, width };
    cache.set(table, result);
    return result;
  };
  const tableSize = (element, value) => {
    const info = tableColumns(element),
      s = state(element),
      e = value.edges,
      cache = styleReadCache.boxSizes;
    const captions = children(element).filter((c) => c.tagName === 'CAPTION'),
      contentWidth = value.width - e.bleft - e.bright;
    let captionHeight = 0;
    for (const caption of captions) {
      const c = state(caption),
        ce = c.edges(value.width),
        font = textInfo(caption, textOf(caption)),
        available = Math.max(
          0,
          value.width - ce.mleft - ce.mright - ce.pleft - ce.pright - ce.bleft - ce.bright,
        );
      let lines = 1,
        line = 0;
      for (const word of textOf(caption).split(' ')) {
        const wordWidth = textInfo(caption, word).width,
          space = line ? textInfo(caption, ' ').width : 0;
        if (line && line + space + wordWidth > available) {
          lines++;
          line = wordWidth;
        } else line += space + wordWidth;
      }
      const h = lines * font.height + ce.ptop + ce.pbottom + ce.btop + ce.bbottom;
      cache.set(caption, {
        width: value.width - ce.mleft - ce.mright,
        height: h,
        edges: ce,
        positions: new Map(),
        contentHeight: lines * font.height,
      });
      value.positions.set(caption, { x: ce.mleft - e.bleft, y: captionHeight - e.btop });
      captionHeight += h + ce.mtop + ce.mbottom;
    }
    let cursor = captionHeight + info.spacing;
    const groupStarts = new Map();
    for (let r = 0; r < info.rows.length; r++) {
      const row = info.rows[r],
        group = geometryParent(row),
        rowCells = info.cells[r],
        height = Math.max(
          0,
          ...rowCells.map((cell) => {
            const c = state(cell),
              ce = c.edges(contentWidth);
            return (
              (c.length(c.get('height')) ?? textInfo(cell, textOf(cell)).height) +
              ce.ptop +
              ce.pbottom +
              ce.btop +
              ce.bbottom
            );
          }),
        );
      let x = 0;
      const rowBox = {
        width:
          info.columns.reduce((a, b) => a + b, 0) +
          Math.max(0, info.columns.length - 1) * info.spacing,
        height,
        edges: state(row).edges(contentWidth),
        positions: new Map(),
        contentHeight: height,
      };
      cache.set(row, rowBox);
      for (let i = 0; i < rowCells.length; i++) {
        const cell = rowCells[i],
          ce = state(cell).edges(contentWidth);
        cache.set(cell, {
          width: info.columns[i],
          height,
          edges: ce,
          positions: new Map(),
          contentHeight: height - ce.ptop - ce.pbottom - ce.btop - ce.bbottom,
        });
        rowBox.positions.set(cell, { x, y: 0 });
        x += info.columns[i] + info.spacing;
      }
      if (group === element) value.positions.set(row, { x: info.spacing, y: cursor });
      else {
        if (!groupStarts.has(group)) {
          groupStarts.set(group, cursor);
          cache.set(group, {
            width: rowBox.width,
            height: 0,
            edges: state(group).edges(contentWidth),
            positions: new Map(),
            contentHeight: 0,
          });
          value.positions.set(group, { x: info.spacing, y: cursor });
        }
        const groupBox = cache.get(group);
        groupBox.positions.set(row, { x: 0, y: cursor - groupStarts.get(group) });
        groupBox.height = cursor - groupStarts.get(group) + height;
        groupBox.contentHeight = groupBox.height;
      }
      cursor += height + info.spacing;
    }
    value.height = cursor + e.btop + e.bbottom;
    value.contentHeight = cursor;
    return value;
  };
  const containingWidth = (element) => {
    if (['absolute', 'fixed'].includes(state(element).position)) {
      const container = positionedContainer(element);
      if (!container) return host.viewport().width;
      const width = layoutWidthFor(container),
        edges = state(container).edges(width);
      // An absolutely positioned box uses its containing block's padding box.
      return Math.max(0, width - edges.bleft - edges.bright);
    }
    const parent = geometryParent(element);
    if (!parent) return host.viewport().width;
    const width = layoutWidthFor(parent),
      edges = state(parent).edges(width);
    return Math.max(0, width - edges.pleft - edges.pright - edges.bleft - edges.bright);
  };
  const width = (element) => {
    const known = styleReadCache.boxSizes?.get(element);
    if (known) return known.width;
    const cache = styleReadCache.widths;
    if (cache.has(element)) return cache.get(element);
    const plans = styleReadCache.widthPlans || (styleReadCache.widthPlans = new WeakMap());
    // containingWidth() can re-enter through an ancestor formatting context.
    // Keep the old cycle break explicit without making zero look cache-ready.
    if (plans.get(element)?.state === 'computing') return 0;
    plans.set(element, { state: 'computing' });
    const s = state(element);
    if (s.display === 'none') {
      cache.set(element, 0);
      plans.set(element, { state: 'ready' });
      return 0;
    }
    const basis = containingWidth(element),
      e = s.edges(basis),
      extra = e.pleft + e.pright + e.bleft + e.bright;
    let content = s.length(s.get('width'), basis),
      borderBox = s.get('box-sizing') === 'border-box';
    if (content === null) {
      const attribute = host.getAttribute(elementSlot(element).nodeId, 'width');
      if (attribute && /^\d+(?:\.\d+)?$/.test(attribute)) content = Number(attribute);
      else if (controlSize(element)) {
        const value = controlSize(element).width;
        cache.set(element, value);
        plans.set(element, { state: 'ready' });
        return value;
      } else if (s.display === 'table') {
        content = tableColumns(element).width - extra;
      } else {
        if (['absolute', 'fixed'].includes(s.position) && !replacedGeometryTags.has(tag(element))) {
          const left = s.length(s.get('left'), basis),
            right = s.length(s.get('right'), basis);
          if (left !== null && right !== null)
            content = Math.max(
              0,
              basis - left - right - e.mleft - e.mright - (borderBox ? 0 : extra),
            );
        }
        const parent = geometryParent(element),
          p = parent && state(parent);
        if (
          content === null &&
          parent &&
          /^(?:inline-)?flex$/.test(p.display) &&
          !(p.get('flex-direction') || 'row').startsWith('column')
        ) {
          const items = children(parent).filter(
            (child) =>
              elementSlot(child)?.type === 'element' &&
              rendered(child) &&
              !['absolute', 'fixed'].includes(state(child).position),
          );
          let base = 0,
            totalGrow = 0,
            totalShrink = 0,
            own = 0,
            ownGrow = 0,
            ownShrink = 0;
          for (const item of items) {
            const itemState = state(item),
              itemEdges = itemState.edges(basis),
              flexBasis = itemState.length(itemState.get('flex-basis'), basis),
              specified = itemState.length(itemState.get('width'), basis),
              itemBase =
                (flexBasis ?? specified ?? intrinsic(item)) +
                ((flexBasis !== null || specified !== null) &&
                itemState.get('box-sizing') === 'border-box'
                  ? 0
                  : itemEdges.pleft + itemEdges.pright + itemEdges.bleft + itemEdges.bright),
              outer = itemBase + itemEdges.mleft + itemEdges.mright,
              grow = Number(itemState.get('flex-grow')) || 0,
              shrink = Number(itemState.get('flex-shrink'));
            base += outer;
            totalGrow += grow;
            totalShrink += (Number.isFinite(shrink) ? shrink : 1) * itemBase;
            if (item === element) {
              own = itemBase;
              ownGrow = grow;
              ownShrink = (Number.isFinite(shrink) ? shrink : 1) * itemBase;
            }
          }
          const free = basis - base,
            allocated =
              free >= 0
                ? own + (free * ownGrow) / Math.max(1, totalGrow)
                : own + (free * ownShrink) / Math.max(1, totalShrink);
          content = Math.max(0, allocated - (borderBox ? 0 : extra));
        }
        if (content === null)
          content =
            ['absolute', 'fixed'].includes(s.position) ||
            ['inline', 'inline-block', 'inline-flex'].includes(s.display)
              ? Math.min(Math.max(0, basis - e.mleft - e.mright - extra), intrinsic(element))
              : Math.max(0, basis - e.mleft - e.mright - extra);
      }
    }
    let value = Math.max(0, content + (borderBox ? 0 : extra));
    const minimum = s.length(s.get('min-width'), basis),
      maximum = s.length(s.get('max-width'), basis);
    if (minimum !== null) value = Math.max(value, minimum + (borderBox ? 0 : extra));
    if (maximum !== null) value = Math.min(value, maximum + (borderBox ? 0 : extra));
    cache.set(element, value);
    plans.set(element, { state: 'ready' });
    return value;
  };
  const fixedContainer = (element) => {
    for (let p = geometryParent(element); p; p = geometryParent(p))
      if (state(p).get('transform') && state(p).get('transform') !== 'none') return p;
    return null;
  };
  const positionedContainer = (element) => {
    if (state(element).position === 'fixed') return fixedContainer(element);
    for (let p = geometryParent(element); p; p = geometryParent(p))
      if (state(p).position !== 'static') return p;
    return null;
  };
  const percentageHeightBasis = (element, own) => {
    const parent = geometryParent(element);
    if (
      !(styleReadCache.quirksMode ??= documentIsQuirks(document)) ||
      !['static', 'relative', 'sticky'].includes(own.position) ||
      own.display.startsWith('table-')
    )
      return definiteGeometryHeight(parent) ? size(parent).height : null;

    // Quirks percentage heights skip auto-height block containers. Flex/grid
    // and positioned auto-height containing blocks remain boundaries. Root
    // and body edges reduce the available initial containing-block height.
    // https://quirks.spec.whatwg.org/#the-percentage-height-calculation-quirk
    let inset = 0;
    for (let node = parent; node; node = geometryParent(node)) {
      const s = state(node);
      if (s.display === 'inline' || s.display === 'contents') continue;
      const height = s.get('height');
      if (height && height !== 'auto') {
        const box = size(node),
          e = box.edges;
        return Math.max(0, box.height - e.ptop - e.pbottom - e.btop - e.bbottom);
      }
      if (
        ['absolute', 'fixed'].includes(s.position) ||
        !['block', 'inline-block', 'flow-root', 'list-item'].includes(s.display)
      )
        return null;
      if (tag(node) === 'HTML' || tag(node) === 'BODY') {
        const e = s.edges(containingWidth(node));
        inset += e.mtop + e.mbottom + e.ptop + e.pbottom + e.btop + e.bbottom;
      }
    }
    return Math.max(0, host.viewport().height - inset);
  };
  const size = (element) => {
    const cache = styleReadCache.boxSizes || (styleReadCache.boxSizes = new WeakMap()),
      plans = styleReadCache.sizePlans || (styleReadCache.sizePlans = new WeakMap()),
      active = plans.get(element);
    if (active?.state === 'computing') return active.value;
    // A width dependency may request this box before its flex allocation is
    // complete. Return a cycle value without publishing it as a finished box.
    if (styleReadCache.widthPlans?.get(element)?.state === 'computing')
      return {
        width: 0,
        height: 0,
        edges: state(element).edges(0),
        positions: new Map(),
        contentHeight: 0,
      };
    for (let parent = geometryParent(element); parent; parent = geometryParent(parent))
      if (state(parent).display === 'table') {
        if (!cache.has(parent)) size(parent);
        break;
      }
    if (cache.has(element)) return cache.get(element);
    const s = state(element),
      basis = containingWidth(element),
      e = s.edges(basis),
      value = {
        width: layoutWidthFor(element),
        height: 0,
        edges: e,
        positions: new Map(),
        contentHeight: 0,
      };
    // The provisional object is the defined fallback for cyclic percentage and
    // containing-block dependencies. Only this recursive edge can observe it.
    plans.set(element, { state: 'computing', value });
    cache.set(element, value);
    const parent = geometryParent(element),
      parentDisplay = parent ? state(parent).display : '';
    if (
      !/^(?:inline-)?flex$/.test(parentDisplay) &&
      !['absolute', 'fixed'].includes(s.position) &&
      !['inline', 'inline-block'].includes(s.display)
    ) {
      const left = s.get('margin-left') === 'auto',
        right = s.get('margin-right') === 'auto',
        free = Math.max(0, basis - value.width - e.mleft - e.mright);
      if (left) e.mleft = free / (right ? 2 : 1);
      if (right) e.mright = free / (left ? 2 : 1);
    }
    if (
      !rendered(element) ||
      !computedStyleDocumentAvailable(element) ||
      !computedStyleAvailable(element)
    ) {
      value.width = 0;
      plans.set(element, { state: 'ready', value });
      return value;
    }
    if (s.display === 'table') {
      tableSize(element, value);
      plans.set(element, { state: 'ready', value });
      return value;
    }
    const rawHeight = s.get('height'),
      heightBasis = rawHeight?.endsWith('%') ? percentageHeightBasis(element, s) : 0;
    let height = heightBasis === null ? null : s.length(rawHeight, heightBasis);
    // Opposing insets stretch an auto-sized non-replaced absolute box in its
    // containing padding box. Querying it first must also complete parent flow.
    if (
      height === null &&
      ['absolute', 'fixed'].includes(s.position) &&
      !replacedGeometryTags.has(tag(element))
    ) {
      const container = positionedContainer(element),
        cb = container ? size(container) : null,
        basis = cb ? cb.height - cb.edges.btop - cb.edges.bbottom : host.viewport().height,
        top = s.length(s.get('top'), basis),
        bottom = s.length(s.get('bottom'), basis);
      if (top !== null && bottom !== null)
        height = Math.max(
          0,
          basis -
            top -
            bottom -
            e.mtop -
            e.mbottom -
            (s.get('box-sizing') === 'border-box' ? 0 : e.ptop + e.pbottom + e.btop + e.bbottom),
        );
    }
    value.height =
      height === null
        ? 0
        : height +
          (s.get('box-sizing') === 'border-box' ? 0 : e.ptop + e.pbottom + e.btop + e.bbottom);
    const own = controlSize(element);
    if (own) {
      value.height = height === null ? own.height : value.height;
      return value;
    }
    if (
      /^(?:inline-)?flex$/.test(s.display) &&
      !(s.get('flex-direction') || 'row').startsWith('column')
    ) {
      const items = children(element).filter(
          (child) =>
            elementSlot(child)?.type === 'element' &&
            rendered(child) &&
            !['absolute', 'fixed'].includes(state(child).position),
        ),
        contentWidth = Math.max(0, value.width - e.pleft - e.pright - e.bleft - e.bright);
      const measured = items.map((child) => {
          const box = size(child),
            edges = box.edges,
            childState = state(child);
          return {
            child,
            box,
            edges,
            autoLeft: childState.get('margin-left') === 'auto',
            autoRight: childState.get('margin-right') === 'auto',
            outerWidth: box.width + edges.mleft + edges.mright,
            outerHeight: box.height + edges.mtop + edges.mbottom,
          };
        }),
        used = measured.reduce((sum, item) => sum + item.outerWidth, 0),
        lineHeight = measured.reduce((maximum, item) => Math.max(maximum, item.outerHeight), 0),
        crossSize =
          height === null
            ? lineHeight
            : Math.max(0, value.height - e.ptop - e.pbottom - e.btop - e.bbottom),
        autoMargins = measured.reduce(
          (count, item) => count + Number(item.autoLeft) + Number(item.autoRight),
          0,
        );
      const justify = s.get('justify-content'),
        free = Math.max(0, contentWidth - used),
        autoSpace = autoMargins ? free / autoMargins : 0,
        justifyFree = autoMargins ? 0 : free;
      let cursor =
          justify === 'flex-end' || justify === 'end'
            ? justifyFree
            : justify === 'center'
              ? justifyFree / 2
              : 0,
        gap =
          justify === 'space-between' && measured.length > 1
            ? justifyFree / (measured.length - 1)
            : justify === 'space-around' && measured.length
              ? justifyFree / measured.length
              : justify === 'space-evenly' && measured.length
                ? justifyFree / (measured.length + 1)
                : 0;
      if (justify === 'space-around') cursor = gap / 2;
      else if (justify === 'space-evenly') cursor = gap;
      for (const item of measured) {
        const align = state(item.child).get('align-self') || s.get('align-items'),
          remaining = Math.max(0, crossSize - item.outerHeight),
          y =
            align === 'flex-end' || align === 'end'
              ? remaining
              : align === 'center'
                ? remaining / 2
                : 0;
        cursor += item.autoLeft ? autoSpace : 0;
        value.positions.set(item.child, { x: cursor + item.edges.mleft, y: y + item.edges.mtop });
        cursor += item.outerWidth + (item.autoRight ? autoSpace : 0) + gap;
      }
      value.contentHeight = lineHeight;
      value.height =
        (height === null ? lineHeight : height) +
        (s.get('box-sizing') === 'border-box' && height !== null
          ? 0
          : e.ptop + e.pbottom + e.btop + e.bbottom);
      const min = s.length(s.get('min-height')),
        max = s.length(s.get('max-height'));
      if (min !== null) value.height = Math.max(value.height, min);
      if (max !== null) value.height = Math.min(value.height, max);
      return value;
    }
    const text = textOf(element),
      font = textInfo(element, text),
      contentWidth = Math.max(0, value.width - e.pleft - e.pright - e.bleft - e.bright);
    const generated = (pseudo) => {
      const entries = uncachedCSSDeclarations(element, pseudo),
        get = (name) => entries.find((e) => e.name === name)?.value;
      if (
        get('display') === 'none' ||
        !['\"\"', "''"].includes(get('content')) ||
        ['absolute', 'fixed'].includes(get('position'))
      )
        return 0;
      return (
        (s.length(get('height')) || 0) +
        (s.length(get('padding-top'), value.width) || 0) +
        (s.length(get('padding-bottom'), value.width) || 0)
      );
    };
    // Line breaking depends on its actual text/font/width inputs, not sibling
    // mutations. Retain only the last numeric result with the live node owner.
    const lineKey = JSON.stringify([
        text,
        contentWidth,
        s.fontSize,
        s.inherited('font-family'),
        s.inherited('font-weight'),
        s.inherited('font-style'),
        s.inherited('white-space'),
        String(styleReadCache.fontCollectionVersion),
      ]),
      priorLines = nodeTextLines.get(element);
    let textLines = text ? 1 : 0,
      lastTextWidth = 0,
      lineText = '',
      textOverflowWidth = 0;
    const wrapping = !['nowrap', 'pre'].includes(s.inherited('white-space'));
    if (priorLines?.key === lineKey) {
      textLines = priorLines.lines;
      lastTextWidth = priorLines.lastWidth;
      textOverflowWidth = priorLines.overflowWidth;
    } else if (text)
      for (const word of text.split(' ')) {
        const candidate = lineText ? lineText + ' ' + word : word,
          advance = textInfo(element, candidate).width;
        if (wrapping && lineText && advance > contentWidth) {
          textLines++;
          lineText = word;
          lastTextWidth = textInfo(element, word).width;
        } else {
          lineText = candidate;
          lastTextWidth = advance;
        }
        textOverflowWidth = Math.max(textOverflowWidth, lastTextWidth);
      }
    if (text) value.overflowWidth = Math.max(value.overflowWidth || 0, textOverflowWidth);
    if (lineKey.length <= 16384)
      nodeTextLines.set(element, {
        key: lineKey,
        lines: textLines,
        lastWidth: lastTextWidth,
        overflowWidth: textOverflowWidth,
      });
    else nodeTextLines.delete(element);
    let cursor = generated('before'),
      margin = 0,
      lineWidth = lastTextWidth,
      lineHeight = textLines * font.height,
      line = [],
      adjoiningMargins = [];
    const collapsedMargin = (extra) => {
      const values = [margin, ...adjoiningMargins, ...extra];
      return Math.max(0, ...values) + Math.min(0, ...values);
    };
    const flush = () => {
      if (!lineHeight) return;
      for (const child of line) {
        const box = size(child),
          c = state(child);
        value.positions.set(child, {
          x: lineWidth === 0 ? 0 : value.positions.get(child)?.x || 0,
          y:
            cursor +
            (c.display === 'inline' || replacedGeometryTags.has(tag(child))
              ? tag(child) === 'BUTTON' && !textContent(child)
                ? font.ascent - box.height / 2
                : tag(child) === 'PROGRESS'
                  ? font.ascent + unit(state(child).fontSize * 0.2) - box.height
                  : Math.max(0, lineHeight - box.height)
              : 0),
        });
      }
      cursor += lineHeight;
      line = [];
      lineWidth = 0;
      lineHeight = 0;
    };
    for (const child of children(element)) {
      if (elementSlot(child)?.type !== 'element') continue;
      const c = state(child);
      if (!rendered(child)) continue;
      if (['absolute', 'fixed'].includes(c.position)) {
        value.positions.set(child, { x: 0, y: cursor + lineHeight });
        continue;
      }
      const box = size(child),
        ce = box.edges,
        isInline =
          c.display === 'inline' ||
          c.display === 'inline-block' ||
          replacedGeometryTags.has(tag(child)) ||
          tag(child) === 'PROGRESS';
      if (isInline) {
        const w = box.width + ce.mleft + ce.mright;
        if (lineWidth && lineWidth + w > contentWidth) flush();
        lineHeight = Math.max(lineHeight, font.height, box.height + ce.mtop + ce.mbottom);
        value.positions.set(child, { x: lineWidth + ce.mleft, y: cursor + ce.mtop });
        line.push(child);
        lineWidth += w;
        continue;
      }
      flush();
      if (
        box.height === 0 &&
        ce.btop === 0 &&
        ce.bbottom === 0 &&
        ce.ptop === 0 &&
        ce.pbottom === 0
      ) {
        adjoiningMargins.push(ce.mtop, ce.mbottom);
        value.positions.set(child, { x: ce.mleft, y: cursor + collapsedMargin([]) });
        continue;
      }
      cursor += collapsedMargin([ce.mtop]);
      adjoiningMargins = [];
      value.positions.set(child, { x: ce.mleft, y: cursor });
      cursor += box.height;
      margin = ce.mbottom;
    }
    flush();
    cursor += collapsedMargin([]) + generated('after');
    // Closed disclosure content still has queryable boxes. Only the summary
    // contributes to the disclosure's visible normal-flow height.
    if (
      tag(element) === 'DETAILS' &&
      host.getAttribute(elementSlot(element).nodeId, 'open') === null
    ) {
      const summary = children(element).find((node) => tag(node) === 'SUMMARY');
      if (summary) {
        const box = size(summary);
        cursor = (value.positions.get(summary)?.y || 0) + box.height + box.edges.mbottom;
      }
    }
    value.contentHeight = cursor;
    value.height =
      (height === null ? cursor : height) +
      (s.get('box-sizing') === 'border-box' && height !== null
        ? 0
        : e.ptop + e.pbottom + e.btop + e.bbottom);
    const min = s.length(s.get('min-height')),
      max = s.length(s.get('max-height'));
    if (min !== null) value.height = Math.max(value.height, min);
    if (max !== null) value.height = Math.min(value.height, max);
    plans.set(element, { state: 'ready', value });
    return value;
  };
  const rect = (element) => {
    const cache = styleReadCache.rects,
      plans = styleReadCache.placementPlans || (styleReadCache.placementPlans = new WeakMap()),
      active = plans.get(element);
    if (active?.state === 'computing') return active.value;
    if (cache.has(element)) return cache.get(element);
    const value = { x: 0, y: 0, left: 0, top: 0, width: 0, height: 0 };
    // Placement recursion consumes this provisional origin; other consumers
    // only see the same object after it transitions to ready below.
    plans.set(element, { state: 'computing', value });
    cache.set(element, value);
    if (
      !rendered(element) ||
      !computedStyleDocumentAvailable(element) ||
      !computedStyleAvailable(element)
    ) {
      value.width = value.height = 0;
      value.right = value.bottom = 0;
      plans.set(element, { state: 'ready', value });
      return value;
    }
    const s = state(element),
      box = size(element);
    value.width = box.width;
    value.height = box.height;
    const parent = geometryParent(element),
      positioned = ['absolute', 'fixed'].includes(s.position),
      hasInset = (value) => value != null && value !== '' && value !== 'auto',
      hasHorizontalInset = hasInset(s.get('left')) || hasInset(s.get('right')),
      hasVerticalInset = hasInset(s.get('top')) || hasInset(s.get('bottom')),
      independentPosition = positioned && hasHorizontalInset && hasVerticalInset,
      parentRect = independentPosition
        ? { x: 0, y: 0, width: host.viewport().width, height: host.viewport().height }
        : parent
          ? rect(parent)
          : { x: 0, y: 0, width: host.viewport().width, height: host.viewport().height },
      parentBox = independentPosition ? null : parent ? size(parent) : null;
    const local = parentBox?.positions.get(element) || { x: 0, y: 0 };
    value.x =
      parentRect.x + (parentBox ? parentBox.edges.bleft + parentBox.edges.pleft : 0) + local.x;
    value.y =
      parentRect.y + (parentBox ? parentBox.edges.btop + parentBox.edges.ptop : 0) + local.y;
    if (
      parentBox &&
      !['absolute', 'fixed'].includes(s.position) &&
      state(parent).inherited('direction') === 'rtl'
    )
      value.x =
        parentRect.x +
        parentBox.width -
        parentBox.edges.bright -
        parentBox.edges.pright -
        local.x -
        value.width;
    if (['absolute', 'fixed'].includes(s.position)) {
      const ancestor = positionedContainer(element);
      const r = ancestor ? rect(ancestor) : { x: 0, y: 0, ...host.viewport() },
        a = ancestor ? size(ancestor).edges : { bleft: 0, btop: 0 };
      const left = s.length(s.get('left'), r.width),
        right = s.length(s.get('right'), r.width),
        top = s.length(s.get('top'), r.height),
        bottom = s.length(s.get('bottom'), r.height);
      if (left !== null) value.x = r.x + a.bleft + left;
      else if (right !== null) value.x = r.x + r.width - right - value.width;
      if (top !== null) value.y = r.y + a.btop + top;
      else if (bottom !== null) value.y = r.y + r.height - bottom - value.height;
      value.x += box.edges.mleft;
      value.y += box.edges.mtop;
    } else if (s.position === 'relative') {
      const left = s.length(s.get('left'), parentRect.width),
        right = s.length(s.get('right'), parentRect.width),
        top = s.length(s.get('top'), parentRect.height),
        bottom = s.length(s.get('bottom'), parentRect.height);
      value.x += left ?? -(right || 0);
      value.y += top ?? -(bottom || 0);
    }
    value.left = value.x;
    value.top = value.y;
    value.right = value.x + value.width;
    value.bottom = value.y + value.height;
    // Offset coordinates use the offset parent's padding edge, not the DOM
    // parent's box. A static BODY denotes the initial containing block.
    let offsetParent = null;
    if (s.position !== 'fixed')
      for (let p = parent; p; p = geometryParent(p))
        if (state(p).position !== 'static' || ['BODY', 'TD', 'TH', 'TABLE'].includes(tag(p))) {
          offsetParent = p;
          break;
        }
    const origin =
        offsetParent && !(tag(offsetParent) === 'BODY' && state(offsetParent).position === 'static')
          ? rect(offsetParent)
          : null,
      oe = origin ? size(offsetParent).edges : null;
    value.clientWidth = Math.max(0, value.width - box.edges.bleft - box.edges.bright);
    value.clientHeight = Math.max(0, value.height - box.edges.btop - box.edges.bbottom);
    value.offsetLeft = value.x - (origin ? origin.x + oe.bleft : 0);
    value.offsetTop = value.y - (origin ? origin.y + oe.btop : 0);
    plans.set(element, { state: 'ready', value });
    return value;
  };
  // Taffy owns flex/grid formatting-context geometry. Mimic supplies the
  // normalized computed style and intrinsic leaf measurements in one snapshot.
  const taffyBox = (element) => {
    // Custom elements retain the legacy intrinsic-width path; their authored
    // display can be upgraded or stylesheet-mutated after construction.
    if (tag(element).includes('-')) return null;
    // This cache belongs to one observation and dies with its box graph. Keep
    // one weak entry per queried/path-compressed node; no DOM wrappers survive
    // navigation or Page teardown and there is no independent size model.
    const roots = styleReadCache.taffyRoots || (styleReadCache.taffyRoots = new WeakMap());
    let root;
    if (roots.has(element)) root = roots.get(element);
    else {
      let usesTaffy = false;
      const compress = [];
      root = null;
      for (let node = element; node; node = geometryParent(node)) {
        const s = state(node),
          display = s.display,
          flex = /^(?:inline-)?(?:flex|grid)$/.test(display);
        if (!usesTaffy && !flex) compress.push(node);
        if (!/^(?:block|(?:inline-)?(?:flex|grid))$/.test(display)) {
          if (root) break;
          continue;
        }
        if (flex) usesTaffy = true;
        if (usesTaffy) root = node;
        const width = s.get('width');
        if (root && node !== element && width && width !== 'auto') break;
      }
      roots.set(element, root);
      // Nodes below the first flex/grid ancestor have the same result. Do not
      // compress custom elements or the flex/width boundary itself: those have
      // different semantics when queried as the starting element.
      for (const node of compress) if (!tag(node).includes('-')) roots.set(node, root);
    }
    if (!root) return null;
    let cache = styleReadCache.taffyLayouts;
    if (!cache) cache = styleReadCache.taffyLayouts = new WeakMap();
    let boxes = cache.get(root);
    if (!boxes) {
      const plans = styleReadCache.taffyPlans || (styleReadCache.taffyPlans = new WeakMap());
      // Taffy input construction can re-enter through intrinsic measurement.
      // The recursive edge stays on legacy geometry instead of observing a
      // partially assembled node/box map.
      if (plans.get(root)?.state === 'computing') return null;
      plans.set(root, { state: 'computing' });
      const rootLegacy = rect(root),
        rootState = state(root),
        rootBasis = containingWidth(root),
        rootEdges = rootState.edges(rootBasis);
      let rootWidth = rootState.length(rootState.get('width'), rootBasis);
      if (rootWidth === null)
        rootWidth = Math.max(0, rootBasis - rootEdges.mleft - rootEdges.mright);
      else if (rootState.get('box-sizing') !== 'border-box')
        rootWidth += rootEdges.pleft + rootEdges.pright + rootEdges.bleft + rootEdges.bright;
      const rootMin = rootState.length(rootState.get('min-width'), rootBasis),
        rootMax = rootState.length(rootState.get('max-width'), rootBasis);
      if (rootMin !== null) rootWidth = Math.max(rootWidth, rootMin);
      if (rootMax !== null) rootWidth = Math.min(rootWidth, rootMax);
      const nodes = [],
        tracks = [],
        elements = new Map();
      const length = (s, value, auto = true) => {
        if (value == null || value === '' || ['auto', 'none', 'normal'].includes(value))
          return { kind: auto ? 0 : 1, value: 0 };
        const percent = /^([+-]?[\d.]+)%$/.exec(value);
        if (percent) return { kind: 2, value: Number(percent[1]) / 100 };
        const resolved = s.length(value, 0);
        return resolved === null ? { kind: auto ? 0 : 1, value: 0 } : { kind: 1, value: resolved };
      };
      const edge = (s, prefix, allowAuto = false) => {
        const one = (side) => {
          const property = prefix === 'border-' ? prefix + side + '-width' : prefix + side,
            value = s.get(property);
          return allowAuto && value === 'auto' ? { kind: 0, value: 0 } : length(s, value, false);
        };
        return { Top: one('top'), Right: one('right'), Bottom: one('bottom'), Left: one('left') };
      };
      const align = (value) =>
        ({
          start: 1,
          end: 2,
          'flex-start': 3,
          'flex-end': 4,
          center: 5,
          baseline: 6,
          stretch: 7,
          'space-between': 8,
          'space-around': 9,
          'space-evenly': 10,
        })[value] || 0;
      const trackList = (s, value) => {
        const offset = tracks.length,
          append = (token) => {
            let match = /^repeat\(\s*([1-9]\d*)\s*,\s*(.+)\)$/i.exec(token);
            if (match) {
              const repeated = cssValueTokens(match[2]) || [],
                count = Math.min(1024, Number(match[1]));
              for (let i = 0; i < count; i++) for (const item of repeated) append(item);
              return;
            }
            match = /^([+-]?[\d.]+)fr$/i.exec(token);
            if (match) tracks.push({ kind: 3, value: Number(match[1]) });
            else if (token === 'min-content') tracks.push({ kind: 4, value: 0 });
            else if (token === 'max-content') tracks.push({ kind: 5, value: 0 });
            else if (token === 'auto') tracks.push({ kind: 0, value: 0 });
            else {
              const item = length(s, token);
              if (item.kind) tracks.push(item);
            }
          };
        for (const token of cssValueTokens(value || '') || []) append(token);
        return [offset, tracks.length - offset];
      };
      const placement = (value) => {
        if (!value || value === 'auto') return 0;
        const span = /^span\s+([1-9]\d*)$/.exec(value);
        if (span) return -Number(span[1]);
        return /^-?[1-9]\d*$/.test(value) ? Number(value) : 0;
      };
      const append = (node, parent) => {
        const s = state(node),
          index = nodes.length,
          text = textOf(node),
          elementChildren = children(node).filter(
            (child) => elementSlot(child)?.type === 'element' && rendered(child),
          ),
          inlineFormatting =
            elementChildren.length > 0 &&
            elementChildren.every((child) => {
              const display = state(child).display;
              return (
                display === 'inline' ||
                display === 'inline-block' ||
                replacedGeometryTags.has(tag(child))
              );
            }),
          canLayoutChildren = /^(?:block|(?:inline-)?flex|(?:inline-)?grid)$/.test(s.display),
          list = canLayoutChildren ? elementChildren : [],
          own = controlSize(node),
          measure = own
            ? [own.width, own.height]
            : list.length
              ? [-1, -1]
              : text || elementChildren.length
                ? [
                    intrinsic(node),
                    styleReadCache.boxSizes?.get(node)?.height ||
                      (text ? textInfo(node, text).height : 0),
                  ]
                : [0, 0],
          parentNode = geometryParent(node),
          parentIsFlex = parentNode && /^(?:inline-)?flex$/.test(state(parentNode).display),
          authoredMinHeight = length(s, s.get('min-height')),
          inlineContentHeight =
            inlineFormatting && text ? styleReadCache.boxSizes?.get(node)?.height || 0 : 0,
          minimumHeight =
            inlineContentHeight &&
            (authoredMinHeight.kind !== 1 || inlineContentHeight > authoredMinHeight.value)
              ? { kind: 1, value: inlineContentHeight }
              : authoredMinHeight,
          minWidth =
            parentIsFlex && (!s.get('min-width') || s.get('min-width') === 'auto')
              ? { kind: 1, value: 0 }
              : length(s, s.get('min-width')),
          columns = trackList(s, s.get('grid-template-columns')),
          rows = trackList(s, s.get('grid-template-rows'));
        nodes.push({
          ID: elementSlot(node).nodeId,
          Parent: parent,
          Display:
            s.display === 'none'
              ? 0
              : /^(?:inline-)?flex$/.test(s.display)
                ? 2
                : /^(?:inline-)?grid$/.test(s.display)
                  ? 3
                  : 1,
          Position: ['absolute', 'fixed'].includes(s.position) ? 1 : 0,
          BoxSizing: s.get('box-sizing') === 'content-box' ? 1 : 0,
          FlexDirection:
            { column: 1, 'row-reverse': 2, 'column-reverse': 3 }[s.get('flex-direction')] || 0,
          FlexWrap: { wrap: 1, 'wrap-reverse': 2 }[s.get('flex-wrap')] || 0,
          AlignItems: align(s.get('align-items')) || 7,
          AlignSelf: align(s.get('align-self')),
          AlignContent: align(s.get('align-content')),
          JustifyContent: align(s.get('justify-content')),
          JustifySelf: align(s.get('justify-self')),
          JustifyItems: align(s.get('justify-items')) || 7,
          Width: node === root ? { kind: 1, value: rootWidth } : length(s, s.get('width')),
          Height: length(s, s.get('height')),
          MinWidth: minWidth,
          MinHeight: minimumHeight,
          MaxWidth: length(s, s.get('max-width')),
          MaxHeight: length(s, s.get('max-height')),
          FlexBasis: length(s, s.get('flex-basis')),
          FlexGrow: Number(s.get('flex-grow')) || 0,
          FlexShrink: Number.isFinite(Number(s.get('flex-shrink')))
            ? Number(s.get('flex-shrink'))
            : 1,
          Margin: edge(s, 'margin-', true),
          Padding: edge(s, 'padding-'),
          Border: edge(s, 'border-'),
          Inset: {
            Top: length(s, s.get('top'), true),
            Right: length(s, s.get('right'), true),
            Bottom: length(s, s.get('bottom'), true),
            Left: length(s, s.get('left'), true),
          },
          GapX: length(s, s.get('column-gap'), false),
          GapY: length(s, s.get('row-gap'), false),
          MeasureWidth: measure[0],
          MeasureHeight: measure[1],
          GridColumnOffset: columns[0],
          GridColumnCount: columns[1],
          GridRowOffset: rows[0],
          GridRowCount: rows[1],
          GridColumnStart: placement(s.get('grid-column-start')),
          GridColumnEnd: placement(s.get('grid-column-end')),
          GridRowStart: placement(s.get('grid-row-start')),
          GridRowEnd: placement(s.get('grid-row-end')),
        });
        for (const child of list) append(child, index);
      };
      append(root, -1);
      for (let i = 0; i < nodes.length; i++)
        elements.set(nodes[i].ID, i === 0 ? root : wrap(nodes[i].ID));
      const response = JSON.parse(
        host.layoutTaffy(
          JSON.stringify({ viewport: [rootWidth, host.viewport().height], nodes, tracks }),
        ),
      );
      if (response.error) throw new Error(response.error);
      boxes = new Map();
      const nodeIndexes = new Map(nodes.map((node, index) => [node.ID, index]));
      for (const box of response.boxes) {
        const node = elements.get(box.id),
          edges = state(node).edges(box.width),
          index = nodeIndexes.get(box.id);
        let positionedParent = index >= 0 ? nodes[index].Parent : -1;
        while (positionedParent >= 0 && nodes[positionedParent].Position !== 1)
          positionedParent = nodes[positionedParent].Parent;
        const containingBox = positionedParent >= 0 ? boxes.get(nodes[positionedParent].ID) : null;
        let x =
            box.x + (containingBox && box.x < containingBox.width ? containingBox.x : rootLegacy.x),
          y =
            box.y +
            (containingBox && box.y < containingBox.height ? containingBox.y : rootLegacy.y);
        if (state(node).position === 'fixed') {
          const viewport = host.viewport(),
            left = state(node).length(state(node).get('left'), viewport.width),
            right = state(node).length(state(node).get('right'), viewport.width),
            top = state(node).length(state(node).get('top'), viewport.height),
            bottom = state(node).length(state(node).get('bottom'), viewport.height);
          if (left !== null) x = left;
          else if (right !== null) x = viewport.width - right - box.width;
          if (top !== null) y = top;
          else if (bottom !== null) y = viewport.height - bottom - box.height;
        }
        boxes.set(box.id, {
          ...box,
          x,
          y,
          left: x,
          top: y,
          right: x + box.width,
          bottom: y + box.height,
          clientWidth: Math.max(0, box.width - edges.bleft - edges.bright),
          clientHeight: Math.max(0, box.height - edges.btop - edges.bbottom),
          offsetLeft: x,
          offsetTop: y,
        });
      }
      for (const [id, value] of boxes) {
        const node = elements.get(id);
        let offsetParent = null;
        if (state(node).position !== 'fixed')
          for (let p = geometryParent(node); p; p = geometryParent(p))
            if (state(p).position !== 'static' || ['BODY', 'TD', 'TH', 'TABLE'].includes(tag(p))) {
              offsetParent = p;
              break;
            }
        if (
          offsetParent &&
          !(tag(offsetParent) === 'BODY' && state(offsetParent).position === 'static')
        ) {
          const origin = boxes.get(elementSlot(offsetParent).nodeId) || rect(offsetParent),
            edges = state(offsetParent).edges(origin.width);
          value.offsetLeft -= origin.x + edges.bleft;
          value.offsetTop -= origin.y + edges.btop;
        }
      }
      cache.set(root, boxes);
      plans.set(root, { state: 'ready' });
    }
    return boxes.get(elementSlot(element).nodeId) || null;
  };
  const nativeOffsets = (element) => {
    const native = blitzLayoutRect(element);
    if (native !== null) {
      let parent = null;
      if (blitzStyleValue(element, 'position') !== 'fixed') {
        for (let node = geometryParent(element); node; node = geometryParent(node)) {
          if (
            blitzStyleValue(node, 'position') !== 'static' ||
            ['BODY', 'TABLE', 'TD', 'TH'].includes(tag(node))
          ) {
            parent = node;
            break;
          }
        }
      }
      const origin =
        parent && !(tag(parent) === 'BODY' && blitzStyleValue(parent, 'position') === 'static')
          ? blitzLayoutRect(parent)
          : null;
      native.offsetLeft =
        native.x -
        (origin ? origin.x + (parseFloat(blitzStyleValue(parent, 'border-left-width')) || 0) : 0);
      native.offsetTop =
        native.y -
        (origin ? origin.y + (parseFloat(blitzStyleValue(parent, 'border-top-width')) || 0) : 0);
      return native;
    }
    return null;
  };
  const observedRect = (element) => {
    const native = blitzLayoutRect(element);
    if (native !== null) return native;
    const projected = taffyBox(element);
    if (projected) return projected;
    const legacy = rect(element);
    for (let ancestor = geometryParent(element); ancestor; ancestor = geometryParent(ancestor)) {
      if (state(ancestor).position === 'fixed') break;
      const anchor = taffyBox(ancestor);
      if (!anchor) continue;
      const prior = rect(ancestor),
        dx = anchor.x - prior.x,
        dy = anchor.y - prior.y;
      if (!dx && !dy) return legacy;
      return {
        ...legacy,
        x: legacy.x + dx,
        y: legacy.y + dy,
        left: legacy.left + dx,
        top: legacy.top + dy,
        right: legacy.right + dx,
        bottom: legacy.bottom + dy,
      };
    }
    return legacy;
  };
  // A width observation does not need ancestor positions or descendant heights.
  // Table allocation and replaced/shadow boxes retain the complete size path.
  const widthBox = (element) => {
    if (
      !rendered(element) ||
      !computedStyleDocumentAvailable(element) ||
      !computedStyleAvailable(element)
    )
      return { width: 0, clientWidth: 0, edges: { pleft: 0, pright: 0, bleft: 0, bright: 0 } };
    let complete = replacedGeometryTags.has(tag(element));
    for (let node = element; node && !complete; node = geometryParent(node))
      complete =
        state(node).display.startsWith('table') ||
        elementShadows.has(node) ||
        !!containingShadowRoot(node);
    const box = complete
      ? size(element)
      : { width: width(element), edges: state(element).edges(containingWidth(element)) };
    return {
      width: box.width,
      clientWidth: Math.max(0, box.width - box.edges.bleft - box.edges.bright),
      edges: box.edges,
    };
  };
  // Height needs the element's own content flow, but not its position among
  // unrelated siblings. Keep coupled table/shadow/control layout on the full
  // rectangle path; all caches retain their existing observation lifetime.
  const heightBox = (element) => {
    if (
      !rendered(element) ||
      !computedStyleDocumentAvailable(element) ||
      !computedStyleAvailable(element)
    )
      return { height: 0, clientHeight: 0, edges: { ptop: 0, pbottom: 0, btop: 0, bbottom: 0 } };
    let complete = replacedGeometryTags.has(tag(element));
    for (let node = element; node && !complete; node = geometryParent(node))
      complete =
        state(node).display.startsWith('table') ||
        elementShadows.has(node) ||
        !!containingShadowRoot(node);
    if (complete) return { ...rect(element), edges: size(element).edges };
    // A definite height observation needs the border/padding projection, not
    // descendant line breaking or positions. Keep size() authoritative for flow
    // and rect(child): neither receives an incomplete entry in boxSizes here.
    // Percentage heights retain their containing-block dependency in size().
    const s = state(element),
      raw = s.get('height'),
      height = raw && !raw.includes('%') ? s.length(raw) : null;
    if (height !== null) {
      const edges = s.edges(containingWidth(element));
      let value =
        height +
        (s.get('box-sizing') === 'border-box'
          ? 0
          : edges.ptop + edges.pbottom + edges.btop + edges.bbottom);
      const min = s.length(s.get('min-height')),
        max = s.length(s.get('max-height'));
      if (min !== null) value = Math.max(value, min);
      if (max !== null) value = Math.min(value, max);
      return {
        height: value,
        clientHeight: Math.max(0, value - edges.btop - edges.bbottom),
        edges,
      };
    }
    const box = size(element);
    return {
      height: box.height,
      clientHeight: Math.max(0, box.height - box.edges.btop - box.edges.bbottom),
      edges: box.edges,
    };
  };
  const hasBox = (element) =>
    withStyleReadCache(
      () =>
        (blitzPackedRecord(element) ? null : foreignCSSObservation(element, 'box')) ??
        (computedStyleDocumentAvailable(element) &&
          computedStyleAvailable(element) &&
          rendered(element)),
    );
  const nativeBox = (element) => {
    const box = blitzLayoutRect(element);
    if (box === null) return null;
    const edges = state(element).edges(box.width);
    // Native control scroll extents include their padding; the common scroll
    // aggregator expects content-only overflow and adds padding itself.
    if (tag(element) === 'INPUT')
      box.overflowWidth = Math.max(0, box.contentWidth - edges.pleft - edges.pright);
    return { ...box, edges };
  };
  const observedWidth = (element) =>
    blitzLayoutRect(element)?.width ?? taffyBox(element)?.width ?? width(element);
  const observedWidthBox = (element) => {
    const native = nativeBox(element);
    if (native !== null) return native;
    const box = taffyBox(element);
    return box
      ? { width: box.width, clientWidth: box.clientWidth, edges: state(element).edges(box.width) }
      : widthBox(element);
  };
  const observedHeightBox = (element) => {
    const native = nativeBox(element);
    if (native !== null) return native;
    const box = taffyBox(element);
    return box
      ? {
          height: box.height,
          clientHeight: box.clientHeight,
          edges: state(element).edges(box.width),
        }
      : heightBox(element);
  };
  return {
    nativeOffsets,
    width: observedWidth,
    rect: observedRect,
    state,
    size: (element) => nativeBox(element) ?? size(element),
    widthBox: observedWidthBox,
    heightBox: observedHeightBox,
    hasBox,
    fixedContainer,
  };
})();
