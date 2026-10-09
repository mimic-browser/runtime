let svgComputedTransform = null;
let webAnimationComputedValue = () => undefined;
// Computed values project the cascade, font resources and canonical box graph.
// The catalog supplies initial semantics, never measured element coordinates.
const cssComputedShorthand = (element, name) => {
  const components = cssComputedShorthands[name],
    values = components.map((n) => cssComputedValue(element, n)),
    get = (n) => cssComputedValue(element, n),
    same = values.every((v) => v === values[0]);
  if (values.some((v) => v === '')) return '';
  if (['background-position', 'mask-position', '-webkit-mask-position'].includes(name))
    return values.join(' ');
  if (name === 'border') {
    const sides = ['top', 'right', 'bottom', 'left'].map((side) =>
      ['width', 'style', 'color'].map((part) => get('border-' + side + '-' + part)).join(' '),
    );
    return sides.every((v) => v === sides[0]) ? sides[0] : '';
  }
  if (name === 'rule') {
    const row = get('row-rule'),
      column = get('column-rule');
    return row === column ? row : row + ' / ' + column;
  }
  if (
    name === 'text-decoration' &&
    get('text-decoration-line') === 'none' &&
    get('text-decoration-style') === 'solid' &&
    get('text-decoration-thickness') === 'auto'
  )
    return 'none';
  if (name === '-webkit-mask-box-image' && values.join(' ') === 'none 0 fill auto 0 stretch')
    return 'none';
  if (/^page-break-/.test(name)) return values[0] === 'page' ? 'always' : values[0];
  if (name === 'font')
    return [
      get('font-style') === 'normal' ? '' : get('font-style'),
      get('font-variant-caps') === 'normal' ? '' : get('font-variant-caps'),
      get('font-weight') === '400' ? '' : get('font-weight'),
      get('font-stretch') === '100%' ? '' : get('font-stretch'),
      get('font-size') + (get('line-height') === 'normal' ? '' : ' / ' + get('line-height')),
      get('font-family'),
    ]
      .filter(Boolean)
      .join(' ');
  if (name === 'flex-flow') return values.join(' ');
  if (
    name === 'animation' &&
    get('animation-name') === 'none' &&
    get('animation-duration') === '0s' &&
    get('animation-delay') === '0s' &&
    get('animation-timing-function') === 'ease' &&
    get('animation-iteration-count') === '1' &&
    get('animation-direction') === 'normal' &&
    get('animation-fill-mode') === 'none' &&
    get('animation-play-state') === 'running'
  )
    return 'none';
  if (name === 'background')
    return (
      get('background-color') +
      ' ' +
      get('background-image') +
      ' ' +
      get('background-repeat') +
      ' ' +
      get('background-attachment') +
      ' ' +
      get('background-position') +
      ' / ' +
      get('background-size') +
      ' ' +
      get('background-origin') +
      ' ' +
      get('background-clip')
    );
  if (
    name === 'border-image' &&
    get('border-image-source') === 'none' &&
    get('border-image-slice') === '100%' &&
    get('border-image-width') === '1' &&
    get('border-image-outset') === '0' &&
    get('border-image-repeat') === 'stretch'
  )
    return 'none';
  if (
    name === 'mask' &&
    get('mask-image') === 'none' &&
    get('mask-size') === 'auto' &&
    get('mask-repeat') === 'repeat' &&
    get('mask-origin') === 'border-box' &&
    get('mask-clip') === 'border-box' &&
    get('mask-composite') === 'add' &&
    get('mask-mode') === 'match-source'
  )
    return 'none';
  if (name === 'border-spacing')
    return get('-webkit-border-horizontal-spacing') === get('-webkit-border-vertical-spacing')
      ? get('-webkit-border-horizontal-spacing')
      : get('-webkit-border-horizontal-spacing') + ' ' + get('-webkit-border-vertical-spacing');
  if (name === 'border-block' || name === 'border-inline') {
    const prefix = name + '-start-';
    if (values.slice(0, 3).some((v, i) => v !== values[i + 3])) return '';
    return get(prefix + 'width') + ' ' + get(prefix + 'style') + ' ' + get(prefix + 'color');
  }
  if (name === 'outline')
    return get('outline-color') + ' ' + get('outline-style') + ' ' + get('outline-width');
  if (name === 'list-style')
    return (
      get('list-style-position') + ' ' + get('list-style-image') + ' ' + get('list-style-type')
    );
  if (name === 'font-synthesis')
    return (
      ['weight', 'style', 'small-caps']
        .filter((v) => get('font-synthesis-' + v) === 'auto')
        .join(' ') || 'none'
    );
  if (name === 'white-space') {
    const a = get('white-space-collapse'),
      b = get('text-wrap-mode');
    return a === 'collapse'
      ? b === 'wrap'
        ? 'normal'
        : 'nowrap'
      : a === 'preserve'
        ? b === 'wrap'
          ? 'pre-wrap'
          : 'pre'
        : a === 'preserve-breaks' && b === 'wrap'
          ? 'pre-line'
          : values.join(' ');
  }
  if (name === 'grid') return values.join(' / ');
  if (name === 'grid-template' && same) return values[0];
  if (name === 'offset')
    return (
      get('offset-path') +
      ' ' +
      get('offset-distance') +
      ' ' +
      get('offset-rotate') +
      (get('offset-anchor') === 'auto' ? '' : ' / ' + get('offset-anchor'))
    );
  if (
    [
      'container',
      'position-try',
      'text-wrap',
      'text-box',
      'scroll-timeline',
      'view-timeline',
      'timeline-trigger',
    ].includes(name)
  ) {
    const kept = values.filter((v, i) => v !== cssInitialValues.get(components[i]));
    return kept.length
      ? kept.join(' ')
      : {
          container: 'none',
          'position-try': 'none',
          'text-wrap': 'wrap',
          'text-box': 'normal',
          'scroll-timeline': 'none',
          'view-timeline': 'none',
          'timeline-trigger': 'none',
        }[name];
  }
  if (name === 'row-rule') return values.filter((v, i) => i !== 1 || v !== 'none').join(' ');
  if (same) return values[0];
  if (/^(grid-(area|row|column))$/.test(name)) return values.join(' / ');
  if (values.length === 4 && !/animation|transition/.test(name)) return cssCompressFour(values);
  const ordinary = serializeOrdinaryCSSShorthand(name, values);
  return ordinary || values.join(' ');
};
const cssSerializeComputedContent = (element, value) => {
  if (['normal', 'none'].includes(value)) return value;
  try {
    const ast = mimicSelectorLibrary.parseStylesheet(value, { context: 'value' });
    const quote = (text) =>
      '"' +
      text
        .replaceAll('\\', '\\\\')
        .replaceAll('"', '\\"')
        .replaceAll('\n', '\\a ')
        .replaceAll('\r', '\\d ')
        .replaceAll('\f', '\\c ') +
      '"';
    const result = [];
    let strings = '';
    let hasString = false;
    const flush = () => {
      if (hasString) result.push(quote(strings));
      strings = '';
      hasString = false;
    };
    for (const node of ast.children) {
      if (node.type === 'String') {
        strings += node.value;
        hasString = true;
        continue;
      }
      if (node.type === 'Function' && node.name.toLowerCase() === 'attr') {
        const argumentsList = Array.from(node.children);
        const attribute = argumentsList[0];
        if (attribute?.type !== 'Identifier') {
          flush();
          result.push(mimicSelectorLibrary.generateCSS(node));
          continue;
        }
        const observed = host.getAttribute(elementSlot(element).nodeId, attribute.name);
        if (observed !== null) {
          strings += observed;
          hasString = true;
          continue;
        }
        const fallback = argumentsList.find((child) => child.type === 'String');
        strings += fallback?.value || '';
        hasString = true;
        continue;
      }
      flush();
      result.push(mimicSelectorLibrary.generateCSS(node));
    }
    flush();
    return result.join(' ');
  } catch {
    return value;
  }
};
let foreignPseudoContent = null;
bootstrapRestoreHooks.push(() => {
  foreignPseudoContent = null;
});
const cssForeignPseudoValue = (element, name, pseudo) =>
  withStyleReadCache(() => {
    if (
      name === 'content' &&
      (pseudo === 'before' || pseudo === 'after') &&
      styleReadCache.retainable &&
      !referenceGet(element)
    ) {
      const { stableEpoch } = cssForeignComputedStyleEpoch();
      if (foreignPseudoContent?.version !== stableEpoch) foreignPseudoContent = null;
      const nodeID = elementSlot(element).nodeId;
      const known = foreignPseudoContent?.rows.get(nodeID);
      if (known) return known[pseudo === 'before' ? 0 : 1];
      if (
        !foreignPseudoContent?.unsupported &&
        host.isConnected(nodeID) &&
        cssObservationNodeState(element).ownerDocument === realmDocumentRootID
      ) {
        const projection = foreignCSSObservation(element, 'pseudoContent');
        if (typeof projection === 'string' && projection.length <= 4 * 1024 * 1024) {
          const rows = new Map(
            JSON.parse(projection).map(([id, before, after]) => [id, [before, after]]),
          );
          foreignPseudoContent = { version: stableEpoch, rows };
          const values = rows.get(nodeID);
          if (values) return values[pseudo === 'before' ? 0 : 1];
        } else {
          foreignPseudoContent = { version: stableEpoch, rows: new Map(), unsupported: true };
        }
      }
    }
    const foreign = foreignCSSObservation(element, 'pseudoValue', JSON.stringify({ name, pseudo }));
    return foreign ?? resolveCSSComputedValue(element, name, pseudo);
  });
const cssComputedValue = (element, name, pseudo = '') => {
  if (name.startsWith('--'))
    return withStyleReadCache(() => {
      const foreign = foreignCSSObservation(element, 'value', name);
      return foreign ?? cssCustomPropertyValue(element, name) ?? '';
    });
  if (pseudo)
    return styleObservationIsolated
      ? cssForeignPseudoValue(element, name, pseudo)
      : withStyleReadCache(() => resolveCSSComputedValue(element, name, pseudo));
  const native = blitzStyleValue(element, name);
  if (
    name === 'font-size' &&
    native !== null &&
    native.includes('.') &&
    host.isConnected(elementSlot(element)?.nodeId || 0) &&
    host.nodeOwnerDocument(elementSlot(element).nodeId) === realmDocumentRootID &&
    /(?:rem|v(?:w|h|min|max|i|b))\b/i.test(
      computedCSSDeclarations(element).find((entry) => entry.name === 'font-size')?.value || '',
    )
  ) {
    const size = cssComputedFontSize(element);
    if (size !== null) return cssSerializeNumber(size) + 'px';
  }
  if (native !== null) return native;
  if (styleObservationIsolated) {
    return withStyleReadCache(() => {
      const nodeID = String(elementSlot(element).nodeId),
        { selectorTarget, stableEpoch } = cssForeignComputedStyleEpoch();
      if (checkpointForeignComputedValueVersion !== stableEpoch) {
        checkpointForeignComputedValues = new Map();
        checkpointForeignComputedValueVersion = stableEpoch;
      }
      // Animated owner realms deliberately disable retained projections. Do
      // not let the isolated-world JS cache freeze an earlier animation sample
      // after the Go-side cache has been disabled.
      const activeBatches = styleReadCache.retainable ? checkpointForeignComputedValues : new Map(),
        cached = activeBatches.get(nodeID);
      if (
        cached?.values.has(name) &&
        (selectorTarget === 0 || cached.version === styleReadCache.version)
      )
        return cached.values.get(name);
      // A retained document projection is additive within one canonical epoch.
      // Role, visibility, scrolling and input ask for overlapping property
      // sets; replacing a row here made those consumers repeatedly transfer
      // and parse the complete document merely because the last property name
      // differed. Request only the missing columns and merge them below.
      const required = Array.from(new Set([name, 'display', 'visibility'])),
        properties = required.filter((property) => !cached?.values.has(property));
      if (checkpointForeignDocumentBatchVersion !== stableEpoch) {
        checkpointForeignDocumentBatchVersion = stableEpoch;
        checkpointForeignDocumentBatch =
          selectorTarget === 0 && compatibilitySelectors.query(document, '*').length > 128;
      }
      // A foreign read can invalidate retention for this observation. In that
      // state, a document-wide transfer on every scalar read is quadratic.
      const documentBatch =
          styleReadCache.retainable && selectorTarget === 0 && checkpointForeignDocumentBatch,
        foreign = foreignCSSObservation(
          element,
          documentBatch ? 'documentValues' : 'values',
          JSON.stringify(properties),
        );
      if (foreign !== null) {
        if (documentBatch) {
          for (const [nodeID, record, visibility] of JSON.parse(foreign)) {
            const key = String(nodeID),
              prior = activeBatches.get(key),
              values = prior?.version === styleReadCache.version ? prior.values : new Map();
            for (const [property, value] of Object.entries(record)) values.set(property, value);
            activeBatches.set(key, {
              version: styleReadCache.version,
              values,
              visibility,
            });
          }
          return activeBatches.get(nodeID)?.values.get(name) ?? '';
        }
        const values = cached?.version === styleReadCache.version ? cached.values : new Map();
        for (const [property, value] of Object.entries(JSON.parse(foreign)))
          values.set(property, value);
        activeBatches.set(nodeID, { version: styleReadCache.version, values });
        return values.get(name) ?? '';
      }
      return resolveCSSComputedValue(element, name);
    });
  }
  return withStyleReadCache(() => {
    const cache = styleReadCache.computedValues || (styleReadCache.computedValues = new WeakMap());
    let values = cache.get(element);
    if (!styleObservationDynamic && values?.has(name)) return values.get(name);
    const value = resolveCSSComputedValue(element, name);
    if (!styleObservationDynamic && name.length <= 128) {
      if (!values) cache.set(element, (values = new Map()));
      if (values.size < 32) values.set(name, value);
    }
    return value;
  });
};
const cssForeignComputedStyleEpoch = () => {
  const canonicalPrefix = styleReadCache.version.slice(0, styleReadCache.version.indexOf('|'));
  return {
    selectorTarget: Number(canonicalPrefix.slice(canonicalPrefix.lastIndexOf(':') + 1)),
    stableEpoch:
      canonicalPrefix +
      '|' +
      styleReadCache.environmentVersion +
      ':' +
      constructedStyleSheets.revision() +
      ':' +
      compatibilityElementState.observationVersion(),
  };
};
const cssBatchedForeignVisibility = (element) => {
  if (!styleObservationIsolated || !styleReadCache) return undefined;
  const { selectorTarget, stableEpoch } = cssForeignComputedStyleEpoch();
  if (selectorTarget !== 0 || checkpointForeignComputedValueVersion !== stableEpoch)
    return undefined;
  const cached = checkpointForeignComputedValues.get(String(elementSlot(element).nodeId));
  return cached?.version === styleReadCache.version ? cached.visibility : undefined;
};
// Remember only where inheritance found an explicit specified value. The
// computed value is still resolved at the source element, preserving relative
// units, currentcolor and animation sampling. This is observation-local path
// compression: dynamic observations bypass it and mutation epochs receive a
// fresh map from withStyleReadCache.
const cssInheritedSource = (element, name) => {
  const cache =
      styleReadCache && !styleObservationDynamic
        ? styleReadCache.inheritedSources || (styleReadCache.inheritedSources = new WeakMap())
        : null,
    visited = [];
  let source = null;
  for (let node = element; elementSlot(node)?.type === 'element'; node = cssFontParent(node)) {
    const known = cache?.get(node);
    if (known?.has(name)) {
      source = known.get(name);
      break;
    }
    if (cache) visited.push(node);
    const value = computedCSSDeclarations(node).find((entry) => entry.name === name)?.value;
    if (value != null && value !== 'inherit' && value !== 'unset') {
      source = { element: node, value };
      break;
    }
  }
  if (cache && name.length <= 128)
    for (const node of visited) {
      let values = cache.get(node);
      if (!values) cache.set(node, (values = new Map()));
      if (values.size < 32) values.set(name, source);
    }
  return source;
};
// Immutable scalar projections share the canonical observation epoch. Animation
// time is not a DOM mutation, so animated realms always resolve a fresh value.
const resolveCSSComputedValue = (element, name, pseudo = '') => {
  const foreign = pseudo ? null : foreignCSSObservation(element, 'value', name);
  if (foreign !== null) return foreign;
  if (!computedStyleDocumentAvailable(element) || !computedStyleAvailable(element)) return '';
  const initial = cssInitialValues.get(name),
    entries = pseudo ? uncachedCSSDeclarations(element, pseudo) : computedCSSDeclarations(element),
    animated = pseudo ? undefined : webAnimationComputedValue(element, name);
  const declaration =
      animated === undefined ? entries.find((e) => e.name === name) : { name, value: animated },
    specified = declaration?.value;
  let value = geometryValue(element, specified),
    inherit = cssInheritedProperties.has(name);
  if (value === 'inherit' || ((value == null || value === 'unset') && inherit)) {
    value = undefined;
    const source = cssInheritedSource(pseudo ? element : cssFontParent(element), name);
    if (source) {
      const v = source.value;
      value =
        v === 'currentcolor' ||
        (name === 'caret-color' && v === 'auto') ||
        (name === 'line-height' && cssNumberRegex.test(v))
          ? v
          : cssComputedValue(source.element, name);
    }
    if (value == null) value = initial;
  }
  if (value == null || ['initial', 'unset', 'revert', 'revert-layer'].includes(value))
    value = initial;
  if (cssComputedShorthands[name]) return cssComputedShorthand(element, name);
  if (value === undefined) {
    if (name === 'page') return 'auto';
    if (/^background-position-[xy]$/.test(name)) {
      const pair = (entries.find((e) => e.name === 'background-position')?.value || '0% 0%').split(
        ' ',
      );
      return pair[name.endsWith('x') ? 0 : 1] || '0%';
    }
    return '';
  }
  if (name === 'content') return cssSerializeComputedContent(element, value);
  if (name === 'transform' && value !== 'none') {
    const raw = declaration?.parsedValue ?? value;
    try {
      const svg = svgComputedTransform?.(element, raw);
      if (svg !== null && svg !== undefined) return svg;
      const box = layoutRectFor(element);
      const matrix = compatibilityMatrix.parse(raw, (text, axis) =>
        cssResolveLength(
          text,
          cssGeometryLengthContext(element, axis === 0 ? box.width : axis === 1 ? box.height : 0),
        ),
      );
      const two =
        [2, 3, 6, 7, 8, 9, 11, 14].every((i) => matrix[i] === 0) &&
        matrix[10] === 1 &&
        matrix[15] === 1;
      return (
        (two ? 'matrix(' : 'matrix3d(') +
        (two ? [0, 1, 4, 5, 12, 13].map((i) => matrix[i]) : matrix)
          .map(cssSerializeNumber)
          .join(', ') +
        ')'
      );
    } catch {
      return value;
    }
  }
  if (name === 'color') {
    if (pseudo && value !== 'currentcolor') {
      const rgba = cssColorRGBA(value, cssUsedColorScheme(element));
      if (rgba) return cssSerializeColor(rgba);
    }
    return cssResolvedColor(element);
  }
  if (
    ['opacity', 'fill-opacity', 'stroke-opacity', 'stop-opacity', 'flood-opacity'].includes(name) &&
    cssNumberRegex.test(value)
  )
    return cssSerializeNumber(Math.max(0, Math.min(1, Number(value))));
  if (value === 'currentcolor' || (name === 'caret-color' && value === 'auto'))
    return cssResolvedColor(element);
  if (/color$/.test(name) || ['fill', 'stroke'].includes(name)) {
    const rgba = cssColorRGBA(value, cssUsedColorScheme(element));
    if (rgba) return cssSerializeColor(rgba);
  }
  if (name === 'font-size') return cssSerializeNumber(cssComputedFontSize(element) ?? 16) + 'px';
  if (
    /^margin-(top|right|bottom|left)$/.test(name) &&
    specified == null &&
    elementSlot(element).tagName === 'BODY'
  )
    return '8px';
  if (name === 'font-weight') return value === 'normal' ? '400' : value === 'bold' ? '700' : value;
  if (
    name === 'display' &&
    elementSlot(element).tagName === 'INPUT' &&
    String(host.getAttribute(elementSlot(element).nodeId, 'type') || '').toLowerCase() === 'hidden'
  )
    return 'none';
  if (name === 'display') {
    const display = specified == null ? cssBoxModel.state(element).display : value;
    const position = entries.find((e) => e.name === 'position')?.value;
    return ['absolute', 'fixed'].includes(position) ? blockifiedDisplay(display) : display;
  }
  if (
    name === 'unicode-bidi' &&
    specified == null &&
    ['DIV', 'P', 'SECTION', 'ARTICLE', 'HEADER', 'FOOTER', 'MAIN', 'LI', 'TABLE'].includes(
      elementSlot(element).tagName,
    )
  )
    return 'isolate';
  // These scalar values have no logical-axis, length or box-dependent
  // serialization. Cascade, inheritance, animation and special initial-value
  // handling above are complete, so avoid constructing unrelated box state.
  if (
    name === 'cursor' ||
    name === 'visibility' ||
    name === 'direction' ||
    name === 'pointer-events' ||
    name === 'white-space-collapse' ||
    name === 'text-wrap-mode'
  )
    return value;
  if (
    /^(?:(?:min-|max-)?(?:block|inline)-size|inset-(?:block|inline)-(?:start|end)|(?:border|padding|margin)-(?:block|inline)-(?:start|end)(?:-.+)?)$/.test(
      name,
    )
  ) {
    const writing = (() => {
        for (let p = element; elementSlot(p)?.type === 'element'; p = cssFontParent(p)) {
          const v = computedCSSDeclarations(p).find((e) => e.name === 'writing-mode')?.value;
          if (v && !['inherit', 'unset'].includes(v)) return v === 'initial' ? 'horizontal-tb' : v;
        }
        return 'horizontal-tb';
      })(),
      vertical = writing.startsWith('vertical') || writing.startsWith('sideways'),
      rtl = (name === 'direction' ? value : cssComputedValue(element, 'direction')) === 'rtl';
    const sides = vertical
      ? {
          block: {
            start: writing.endsWith('-rl') ? 'right' : 'left',
            end: writing.endsWith('-rl') ? 'left' : 'right',
          },
          inline: { start: rtl ? 'bottom' : 'top', end: rtl ? 'top' : 'bottom' },
        }
      : {
          block: { start: 'top', end: 'bottom' },
          inline: { start: rtl ? 'right' : 'left', end: rtl ? 'left' : 'right' },
        };
    const logical = {
      'block-size': vertical ? 'width' : 'height',
      'inline-size': vertical ? 'height' : 'width',
      'min-block-size': vertical ? 'min-width' : 'min-height',
      'min-inline-size': vertical ? 'min-height' : 'min-width',
      'max-block-size': vertical ? 'max-width' : 'max-height',
      'max-inline-size': vertical ? 'max-height' : 'max-width',
      'inset-block-start': sides.block.start,
      'inset-block-end': sides.block.end,
      'inset-inline-start': sides.inline.start,
      'inset-inline-end': sides.inline.end,
    };
    if (logical[name] && (specified == null || declaration?.allReset || value === 'auto'))
      return cssComputedValue(element, logical[name]);
    const physical = /^(border|padding|margin)-(block|inline)-(start|end)(.*)$/.exec(name);
    if (physical && (specified == null || declaration?.allReset))
      return cssComputedValue(
        element,
        physical[1] + '-' + sides[physical[2]][physical[3]] + physical[4],
      );
  }
  // Scalar computed values do not observe boxes. Resolve box state only
  // inside the serialization branches that actually need it.
  let boxState;
  const box = () => cssBoxModel.size(element),
    state = () => boxState || (boxState = cssBoxModel.state(element)),
    length = (v, basis = 0) => cssResolveLength(v, cssGeometryLengthContext(element, basis));
  if (
    /^margin-(left|right)$/.test(name) &&
    value === 'auto' &&
    !['inline', 'inline-block'].includes(state().display)
  )
    return cssSerializeNumber(box().edges[name.endsWith('left') ? 'mleft' : 'mright']) + 'px';
  if (name === 'width' || name === 'height') {
    for (let p = element; p; p = geometryParent(p))
      if (cssBoxModel.state(p).display === 'none') {
        const n = value.endsWith('%') ? null : length(value);
        return n === null ? value : cssSerializeNumber(n) + 'px';
      }
    const dimensions =
        name === 'width' ? cssBoxModel.widthBox(element) : cssBoxModel.heightBox(element),
      dimension = dimensions[name],
      edges = dimensions.edges;
    return (
      cssSerializeNumber(
        Math.max(
          0,
          dimension -
            (state().get('box-sizing') === 'border-box'
              ? 0
              : name === 'width'
                ? edges.pleft + edges.pright + edges.bleft + edges.bright
                : edges.ptop + edges.pbottom + edges.btop + edges.bbottom),
        ),
      ) + 'px'
    );
  }
  if (/^min-(width|height)$/.test(name) && value === 'auto') return '0px';
  if (/^(top|right|bottom|left)$/.test(name) && ['fixed', 'absolute'].includes(state().position)) {
    const r = cssBoxModel.rect(element),
      parent = state().position === 'fixed' ? null : geometryParent(element),
      p = parent ? cssBoxModel.rect(parent) : { x: 0, y: 0, ...host.viewport() };
    return (
      cssSerializeNumber(
        name === 'left'
          ? r.x - p.x
          : name === 'top'
            ? r.y - p.y
            : name === 'right'
              ? p.width - r.x + p.x - r.width
              : p.height - r.y + p.y - r.height,
      ) + 'px'
    );
  }
  if (name === 'transform-origin' || name === 'perspective-origin') {
    if (
      state().display === 'inline' &&
      !['absolute', 'fixed'].includes(state().position) &&
      !replacedGeometryTags.has(elementSlot(element).tagName)
    )
      return '0px 0px';
    const words = value.split(/\s+/),
      axis = [box().width, box().height],
      keywords = { left: '0%', top: '0%', center: '50%', right: '100%', bottom: '100%' };
    return words
      .map((v, i) => {
        const n = length(keywords[v] || v, axis[i] || 0);
        return n === null ? v : cssSerializeNumber(n) + 'px';
      })
      .join(' ');
  }
  if (/^border-.*-width$/.test(name)) {
    const style = cssComputedValue(element, name.replace(/width$/, 'style'));
    if (style === 'none' || style === 'hidden') return '0px';
  }
  if (value === 'medium' && /width$/.test(name)) return '3px';
  if (value === 'thin' && /width$/.test(name)) return '1px';
  if (value === 'thick' && /width$/.test(name)) return '5px';
  if (name === 'line-height' && cssNumberRegex.test(value))
    return cssSerializeNumber(Number(value) * (cssComputedFontSize(element) ?? 16)) + 'px';
  if (
    /^(margin|padding)-/.test(name) ||
    /^(min|max)-(width|height)$/.test(name) ||
    /width$/.test(name) ||
    ['line-height', 'letter-spacing', 'word-spacing', 'text-indent', 'outline-offset'].includes(
      name,
    )
  ) {
    const n = length(
      value,
      name === 'line-height'
        ? (cssComputedFontSize(element) ?? 16)
        : geometryParent(element)
          ? cssBoxModel.width(geometryParent(element))
          : 0,
    );
    if (n !== null) return cssSerializeNumber(n) + 'px';
  }
  return value;
};
