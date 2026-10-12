// Query programs are immutable; viewport/preference results are never retained
// across style reads. Stylesheets and MediaQueryList use the same evaluator.
const cssMediaPrograms = new Map();
const cssMediaMatches = (query) => {
  query = String(query).trim();
  if (!query) return true;
  const cache = styleReadCache?.mediaQueries;
  if (cache?.has(query)) return cache.get(query);
  let ast = cssMediaPrograms.get(query);
  if (!ast) {
    try {
      ast = mimicSelectorLibrary.parseStylesheet(query, { context: 'mediaQueryList' });
    } catch {
      return false;
    }
    if (cssMediaPrograms.size >= 64) cssMediaPrograms.delete(cssMediaPrograms.keys().next().value);
    cssMediaPrograms.set(query, ast);
  }
  let viewport;
  const dimensions = () => viewport || (viewport = host.viewport());
  let display;
  const screen = () => display || (display = host.screen());
  const number = (node) => {
    if (node?.type === 'Identifier' && ['width', 'height'].includes(node.name))
      return dimensions()[node.name];
    if (node?.type === 'Number') return Number(node.value) === 0 ? 0 : null;
    if (node?.type === 'Dimension')
      return cssResolveLength(node.value + node.unit, { em: 16, rem: 16, percent: NaN });
    return null;
  };
  const compare = (a, op, b) =>
    a !== null &&
    b !== null &&
    ({
      '=': () => a === b,
      '<': () => a < b,
      '<=': () => a <= b,
      '>': () => a > b,
      '>=': () => a >= b,
    }[op]?.() ??
      false);
  const evaluate = (node) => {
    if (!node) return true;
    if (node.type === 'MediaQueryList') return Array.from(node.children).some(evaluate);
    if (node.type === 'MediaQuery') {
      const value =
        (!node.mediaType || ['screen', 'all'].includes(node.mediaType)) && evaluate(node.condition);
      return node.modifier === 'not' ? !value : value;
    }
    if (node.type === 'Condition' || node.type === 'Parentheses') {
      let value = null,
        operator = 'and',
        negate = false;
      for (const child of node.children) {
        if (child.type === 'Identifier') {
          if (child.name === 'not') negate = !negate;
          else operator = child.name;
          continue;
        }
        let next = evaluate(child);
        if (negate) {
          next = !next;
          negate = false;
        }
        value = value === null ? next : operator === 'or' ? value || next : value && next;
      }
      return value === true;
    }
    if (node.type === 'FeatureRange')
      return (
        compare(number(node.left), node.leftComparison, number(node.middle)) &&
        (!node.right || compare(number(node.middle), node.rightComparison, number(node.right)))
      );
    if (node.type === 'Feature') {
      const name = node.name.replace(/^(min|max)-/, ''),
        value = node.value;
      if (['width', 'height'].includes(name))
        return value
          ? compare(
              dimensions()[name],
              node.name.startsWith('min-') ? '>=' : node.name.startsWith('max-') ? '<=' : '=',
              number(value),
            )
          : dimensions()[name] > 0;
      if (['device-width', 'device-height'].includes(name)) {
        const axis = name === 'device-width' ? 'width' : 'height';
        return value
          ? compare(
              screen()[axis],
              node.name.startsWith('min-') ? '>=' : node.name.startsWith('max-') ? '<=' : '=',
              number(value),
            )
          : screen()[axis] > 0;
      }
      if (name === '-webkit-device-pixel-ratio') {
        const ratio = value?.type === 'Number' ? Number(value.value) : null;
        return compare(
          screen().devicePixelRatio,
          node.name.startsWith('min-') ? '>=' : node.name.startsWith('max-') ? '<=' : '=',
          ratio,
        );
      }
      if (name === 'resolution') {
        const unit = value?.unit?.toLowerCase(),
          raw = value?.type === 'Number' ? Number(value.value) : Number(value?.value),
          ratio =
            unit === 'dppx' || unit === 'x'
              ? raw
              : unit === 'dpi'
                ? raw / 96
                : unit === 'dpcm'
                  ? (raw * 2.54) / 96
                  : null;
        return compare(
          screen().devicePixelRatio,
          node.name.startsWith('min-') ? '>=' : node.name.startsWith('max-') ? '<=' : '=',
          ratio,
        );
      }
      if (name === 'orientation')
        return (
          value?.name === (dimensions().width > dimensions().height ? 'landscape' : 'portrait')
        );
      if (name === 'monochrome') return value?.type === 'Number' && Number(value.value) === 0;
      if (name === 'color-gamut') return value?.name === 'srgb';
      if (name === 'any-hover' || name === 'hover') return value?.name === 'hover';
      if (name === 'any-pointer' || name === 'pointer') return value?.name === 'fine';
      if (name === 'display-mode') return value?.name === 'browser';
      if (name === 'forced-colors') return value?.name === 'none';
      if (name === 'prefers-contrast') return value?.name === 'no-preference';
      if (name === 'prefers-reduced-transparency') return value?.name === 'no-preference';
      if (['prefers-color-scheme', 'prefers-reduced-motion'].includes(name))
        return host.media('(' + name + ': ' + (value?.name || '') + ')');
    }
    return false;
  };
  const result = evaluate(ast);
  if (styleReadCache)
    (styleReadCache.mediaQueries || (styleReadCache.mediaQueries = new Map())).set(query, result);
  return result;
};
