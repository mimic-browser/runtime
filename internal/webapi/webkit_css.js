// CSS spelling and JavaScript spelling are different namespaces. CSSOM methods
// accept CSS names (case-insensitively), never camelCase aliases.
const splitCSSDeclarations = (text) => {
  const out = [];
  let token = '',
    quote = '',
    depth = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      token += c;
      if (c === '\\' && i + 1 < text.length) token += text[++i];
      else if (c === quote) quote = '';
      continue;
    }
    if (c === '/' && text[i + 1] === '*') {
      const end = text.indexOf('*/', i + 2);
      if (end < 0) break;
      i = end + 1;
      token += ' ';
      continue;
    }
    if (c === '"' || c === "'") quote = c;
    else if (c === '(' || c === '[' || c === '{') depth++;
    else if (c === ')' || c === ']' || c === '}') depth--;
    if (c === ';' && depth === 0) {
      out.push(token);
      token = '';
    } else token += c;
  }
  if (token && !quote && depth === 0) out.push(token);
  return out;
};
const webkitCSSAliases = new Map(),
  webkitJSNames = new Map();
// These legacy break shorthands expose a single canonical longhand, but Chrome
// removes them through the shorthand path, whose return value is empty.
const webkitCSSLegacyBreakShorthands = new Set([
  '-webkit-column-break-before',
  '-webkit-column-break-after',
  '-webkit-column-break-inside',
]);
for (const [js, canonical] of Object.entries(webkitCSSNames)) {
  const css = js.replace(/[A-Z]/g, (c) => '-' + c.toLowerCase()).replace(/^webkit-/, '-webkit-');
  webkitCSSAliases.set(css, canonical);
  for (const name of [
    js,
    js.replace(/^webkit/, 'Webkit'),
    css,
    canonical,
    canonical.replace(/-([a-z])/g, (_, c) => c.toUpperCase()),
  ])
    webkitJSNames.set(name, canonical);
}
const webkitCSSKeywords = new Map(
  Object.entries({
    appearance:
      'none auto base-select button checkbox listbox menulist menulist-button meter progress-bar radio searchfield textarea textfield',
    'user-select': 'auto none text all',
    '-webkit-user-drag': 'auto none element',
    '-webkit-user-modify': 'read-only read-write read-write-plaintext-only',
    '-webkit-box-align': 'stretch start end center baseline',
    '-webkit-box-pack': 'start end center justify',
    '-webkit-box-orient': 'horizontal vertical inline-axis block-axis',
    '-webkit-box-direction': 'normal reverse',
    '-webkit-font-smoothing': 'auto none antialiased subpixel-antialiased',
    '-webkit-text-security': 'none disc circle square',
    'print-color-adjust': 'economy exact',
    'backface-visibility': 'visible hidden',
    'transform-style': 'flat preserve-3d',
  }).map(([name, values]) => [name, new Set(values.split(' '))]),
);
const normalizeCSSValue = (name, value, inputName = name) => {
  value = String(value).trim();
  if (!value) return '';
  if (cssTopLevelBang(value) >= 0) return null;
  value = cssAliasInputValue(name, value, inputName);
  // Parsed transform lists serialize argument separators, while substitution
  // functions remain author token streams until computed-value resolution.
  if (name === 'transform' && !/\b(?:var|env)\s*\(/i.test(value)) {
    value = value
      .replace(/calc\([^()]*\)/gi, (term) => cssLengthValue(term) || term)
      .replace(/\s*,\s*/g, ', ');
  }
  const parser = cssShorthandParsers.get(name) || cssLonghandParsers.get(name);
  if ((parser || cssInitialValues.has(name)) && cssWideValue(value.toLowerCase()))
    return value.toLowerCase();
  if (parser && !cssWideValue(value.toLowerCase()) && !/^var\(/.test(value)) {
    const parsed = parser(value);
    return parsed === null
      ? null
      : Array.isArray(parsed)
        ? serializeOrdinaryCSSShorthand(name, parsed)
        : parsed;
  }
  const keywords = webkitCSSKeywords.get(name);
  if (!keywords) return value;
  const lower = value.toLowerCase();
  if (
    keywords.has(lower) ||
    ['initial', 'inherit', 'unset', 'revert', 'revert-layer'].includes(lower)
  )
    return lower;
  // Variable values remain token streams; do not resolve them at specified-value time.
  if (/^var\(\s*--[\w-]+\s*(?:,[^;{}]*)?\)$/.test(value)) return value;
  return null;
};
const webkitCSSInitial = new Map(
  Object.entries({
    appearance: 'none',
    'user-select': 'auto',
    '-webkit-user-drag': 'auto',
    '-webkit-user-modify': 'read-only',
    '-webkit-box-align': 'stretch',
    '-webkit-box-pack': 'start',
    '-webkit-box-orient': 'horizontal',
    '-webkit-box-direction': 'normal',
    '-webkit-font-smoothing': 'auto',
    '-webkit-text-security': 'none',
    'print-color-adjust': 'economy',
    'backface-visibility': 'visible',
    'transform-style': 'flat',
  }),
);
const webkitCSSInherited = new Set([
  'user-select',
  '-webkit-user-modify',
  '-webkit-font-smoothing',
  '-webkit-text-security',
  'print-color-adjust',
]);
const resolveWebkitCSS = (element, name, entries) => {
  const initial = webkitCSSInitial.get(name),
    entry = entries.find((e) => e.name === name),
    value = entry?.value;
  if (value === 'initial') return initial;
  if (value && !['inherit', 'unset', 'revert', 'revert-layer'].includes(value)) return value;
  if (value === 'inherit' || webkitCSSInherited.has(name)) {
    const parent = element.parentElement || element.getRootNode?.().host;
    if (parent) return resolveWebkitCSS(parent, name, computedCSSDeclarations(parent));
  }
  return initial;
};
