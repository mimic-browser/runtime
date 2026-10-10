const compatibilityCSSSupports = {};
(() => {
  if (!globalThis.CSS) return;
  const prior = CSS.supports;
  const customPropertyValue = (value, allowEmpty) => {
    if (!allowEmpty && value === '') return false;
    if (cssTopLevelBang(value) >= 0 || value.includes(';')) return false;
    const pairs = { '(': ')', '[': ']', '{': '}' },
      stack = [];
    let quote = '';
    for (let i = 0; i < value.length; i++) {
      const c = value[i];
      if (quote) {
        if (c === '\\') i++;
        else if (c === quote) quote = '';
        continue;
      }
      if (c === '"' || c === "'") quote = c;
      else if (pairs[c]) stack.push(pairs[c]);
      else if (c === ')' || c === ']' || c === '}') {
        if (stack.pop() !== c) return false;
      }
    }
    return stack.length === 0;
  };
  const declaration = (property, value, allowEmptyCustom = false) => {
    const name = cssName(property);
    if (/^--[\w-]+$/.test(name)) return customPropertyValue(String(value), allowEmptyCustom);
    // Chrome's ordinary declaration query accepts a terminal priority marker;
    // custom-property queries retain their separate token-stream grammar.
    value = String(value)
      .replace(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\/\*[\s\S]*?(?:\*\/|$)/g, (token) =>
        token.startsWith('/*') ? ' ' : token,
      )
      .replace(/\s*!\s*important\s*$/i, '');
    // These longhands already have canonical declarations and initial values.
    // CSS-wide values do not require a property-specific value parser.
    if (cssInitialValues.has(name) && cssWideValue(value.trim().toLowerCase())) return true;
    if (cssShorthandParsers.has(name) || cssLonghandParsers.has(name)) {
      const normalized = normalizeCSSValue(name, value, property);
      return normalized !== null && normalized !== '';
    }
    if (webkitCSSKeywords.has(name)) {
      const normalized = normalizeCSSValue(name, value, property);
      return normalized !== null && normalized !== '';
    }
    if (
      webkitCSSAliases.has(String(property).toLowerCase()) &&
      ['initial', 'inherit', 'unset', 'revert', 'revert-layer'].includes(
        String(value).trim().toLowerCase(),
      )
    )
      return true;
    return typeof prior === 'function' ? !!prior.call(CSS, property, value) : false;
  };
  const condition = (text, allowNot = true) => {
    text = text.trim();
    const negate = /^not\s+/i.exec(text);
    if (negate) {
      if (!allowNot) return null;
      const tail = text.slice(negate[0].length).trim();
      if (!/^(?:\(|selector\()/i.test(tail)) return null;
      const result = condition(tail, false);
      return result === null ? null : !result;
    }
    let depth = 0,
      quote = '',
      parts = [],
      start = 0,
      operator = '';
    for (let i = 0; i < text.length; i++) {
      const c = text[i];
      if (quote) {
        if (c === '\\') i++;
        else if (c === quote) quote = '';
        continue;
      }
      if (c === '"' || c === "'") {
        quote = c;
        continue;
      }
      if (c === '(') depth++;
      else if (c === ')') {
        if (--depth < 0) return null;
      }
      if (depth === 0) {
        const match = /^\s+(and|or)\s+/i.exec(text.slice(i));
        if (match) {
          const op = match[1].toLowerCase();
          if (operator && operator !== op) return null;
          operator = op;
          parts.push(text.slice(start, i));
          i += match[0].length - 1;
          start = i + 1;
        }
      }
    }
    if (depth || quote) return null;
    if (operator) {
      parts.push(text.slice(start));
      if (!allowNot) return null;
      const values = parts.map((part) => condition(part, false));
      if (values.includes(null)) return null;
      return operator === 'and' ? values.every(Boolean) : values.some(Boolean);
    }
    const selector = /^selector\(([\s\S]*)\)$/i.exec(text);
    if (selector) return compatibilitySelectors.supports(selector[1]);
    if (text[0] !== '(' || text.at(-1) !== ')') return null;
    const inner = text.slice(1, -1).trim(),
      match = /^([-\w]+)\s*:\s*([\s\S]*)$/.exec(inner);
    return match
      ? declaration(match[1], match[2].replace(/\s*!important\s*$/i, ''), true)
      : (condition(inner) ?? false);
  };
  const supports = {
    supports(property, value) {
      if (arguments.length === 0) throw new TypeError('Not enough arguments');
      if (arguments.length > 1) return declaration(String(property), String(value));
      const text = String(property)
        .replace(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\/\*[\s\S]*?(?:\*\/|$)/g, (token) =>
          token.startsWith('/*') ? ' ' : token,
        )
        .trim();
      return condition(/^[-\w]+\s*:/.test(text) ? '(' + text + ')' : text) === true;
    },
  }.supports;
  // Stylesheet conditions are immutable grammar queries, not element state.
  // Keep author-facing conversions and API calls on the ordinary path, but do
  // not parse the same @supports condition for every element and observation.
  const conditions = new Map();
  compatibilityCSSSupports.matches = (text) => {
    if (conditions.has(text)) return conditions.get(text);
    const result = supports(text);
    if (text.length <= 8192) {
      if (conditions.size >= 128) conditions.delete(conditions.keys().next().value);
      conditions.set(text, result);
    }
    return result;
  };
  Object.defineProperty(supports, 'length', { value: 1, configurable: true });
  markNative(supports, 'supports');
  Object.defineProperty(CSS, 'supports', {
    value: supports,
    writable: true,
    enumerable: true,
    configurable: true,
  });
})();
