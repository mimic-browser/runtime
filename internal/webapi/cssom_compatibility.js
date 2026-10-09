// Constructed sheets keep their parsed rules in the realm. Adoption references
// the same sheet, so replacement and rule edits are visible to every adopter.
// Snapshots serialize this state without inserting nodes into the live DOM.
const nativeRelationalSelector = (() => {
  const pseudos = new Set([
    'has',
    'is',
    'where',
    'not',
    'root',
    'empty',
    'first-child',
    'last-child',
    'only-child',
    'first-of-type',
    'last-of-type',
    'only-of-type',
    'nth-child',
    'nth-last-child',
    'nth-of-type',
    'nth-last-of-type',
    'checked',
    'focus',
    'focus-within',
    'focus-visible',
    'target',
  ]);
  return (selector) => {
    if (!/:has\s*\(/i.test(selector) && !selector.includes('\\')) return null;
    let relational = false,
      unsupported = false;
    const visit = (groups) => {
      for (const group of groups)
        for (const token of group) {
          if (token.type !== 'pseudo') continue;
          relational ||= token.name === 'has';
          // Keep the semantic fallback for predicates not projected by the
          // native state owner, or grammar disabled in the pinned parser.
          unsupported ||= !pseudos.has(token.name) || Boolean(token.nthOf);
          if (Array.isArray(token.data)) visit(token.data);
        }
    };
    try {
      visit(mimicSelectorLibrary.parse(selector));
    } catch {
      return { unsupported: true };
    }
    return relational ? { unsupported } : null;
  };
})();
const constructedStyleSheets = (() => {
  if (typeof globalThis.StyleSheet !== 'function' || typeof globalThis.CSSRuleList !== 'function')
    return {
      snapshot() {
        return [];
      },
    };
  const parse = mimicSelectorLibrary.parseStylesheet,
    generate = mimicSelectorLibrary.generateCSS;
  let revision = 0;
  const changed = () => {
    revision++;
    host.invalidateStyleObservations(true);
  };
  const sourceCache = new WeakMap();
  const nativeEligibilityCache = new WeakMap();
  const sheets = new WeakMap(),
    rules = new WeakMap(),
    adopted = new WeakMap(),
    owners = new WeakMap(),
    ownerLists = new WeakMap();
  const requireSheet = (sheet) => {
    const state = sheets.get(sheet);
    if (!state) throw new TypeError('Illegal invocation');
    return state;
  };
  const list = (values) =>
    new Proxy(Object.create(globalThis.CSSRuleList.prototype), {
      get(target, key, receiver) {
        if (key === 'length') return values.length;
        if (key === 'item') return (index) => values[Number(index)] || null;
        if (key === Symbol.iterator) return values[Symbol.iterator].bind(values);
        if (typeof key === 'string' && /^\d+$/.test(key)) return values[Number(key)];
        return Reflect.get(target, key, receiver);
      },
    });
  function declarations(block) {
    const values = [];
    if (block)
      block.children.forEach((node) => {
        if (node.type === 'Declaration') values.push(node);
      });
    return values;
  }
  const blockDeclarations = new WeakMap();
  const editedDeclarationBlocks = new WeakSet();
  const entriesForBlock = (block) => {
    if (!block) return [];
    let entries = blockDeclarations.get(block);
    if (!entries) {
      entries = parseCSS(
        declarations(block)
          .map(
            (node) =>
              node.property +
              ': ' +
              generate(node.value).trim() +
              (node.important ? ' !important' : '') +
              ';',
          )
          .join(' '),
      );
      blockDeclarations.set(block, entries);
    }
    return entries;
  };
  function declarationText(block, precise = false) {
    return serializeCSS(
      entriesForBlock(block).map((entry) =>
        precise && entry.parsedValue !== undefined ? { ...entry, value: entry.parsedValue } : entry,
      ),
    );
  }
  function preludeText(node) {
    if (!node) return '';
    const children = () => Array.from(node.children, preludeText);
    if (node.type === 'Selector') return children().join('').trim();
    if (node.type === 'Combinator') return node.name === ' ' ? ' ' : ' ' + node.name + ' ';
    if (node.type === 'SelectorList' || node.type === 'MediaQueryList')
      return children().join(', ');
    if (node.type === 'AtrulePrelude' || node.type === 'Condition') return children().join(' ');
    if (node.type === 'Feature')
      return '(' + node.name + (node.value ? ': ' + generate(node.value) : '') + ')';
    if (node.type === 'FeatureRange')
      return (
        '(' +
        [
          generate(node.left),
          node.leftComparison,
          generate(node.middle),
          node.rightComparison,
          node.right && generate(node.right),
        ]
          .filter(Boolean)
          .join(' ') +
        ')'
      );
    if (node.type === 'MediaQuery')
      return [
        node.modifier,
        node.mediaType,
        node.condition && (node.mediaType ? 'and ' : '') + preludeText(node.condition),
      ]
        .filter(Boolean)
        .join(' ');
    return generate(node);
  }
  function ruleText(rule, precise = false) {
    const state = rules.get(rule),
      node = state.node;
    if (node.type === 'Rule') {
      let selector = preludeText(node.prelude);
      if (rules.get(state.parent)?.node.name === 'keyframes')
        selector = selector
          .split(',')
          .map((part) => {
            part = part.trim();
            return part === 'from' ? '0%' : part === 'to' ? '100%' : part;
          })
          .join(', ');
      return (
        selector +
        ' { ' +
        declarationText(node.block, precise) +
        (state.children.length
          ? ' ' + state.children.map((child) => ruleText(child, precise)).join(' ')
          : '') +
        ' }'
      );
    }
    const prelude = node.prelude ? ' ' + preludeText(node.prelude) : '';
    if (node.name === 'keyframes')
      return (
        '@' +
        node.name +
        prelude +
        ' { \n' +
        state.children.map((child) => '  ' + ruleText(child, precise) + '\n').join('') +
        '}'
      );
    return (
      '@' +
      node.name +
      prelude +
      (node.block
        ? state.children.length
          ? ' {\n' +
            state.children
              .map((child) => '  ' + ruleText(child, precise).replace(/\n/g, '\n  '))
              .join('\n') +
            '\n}'
          : ' { ' + declarationText(node.block, precise) + ' }'
        : ';')
    );
  }
  function makeStyle(state) {
    const target = Object.create(CSSStyleDeclaration.prototype);
    const setText = (text) => {
      blockDeclarations.set(state.node.block, parseCSS(String(text)));
      editedDeclarationBlocks.add(state.node.block);
      changed();
    };
    const names = () => entriesForBlock(state.node.block);
    Object.defineProperties(target, {
      cssText: { get: () => declarationText(state.node.block), set: setText, configurable: true },
      length: { get: () => names().length, configurable: true },
      parentRule: { get: () => state.rule, configurable: true },
      item: { value: (index) => names()[Number(index)]?.name || '' },
      getPropertyValue: { value: (name) => readCSSDeclaration(names(), cssName(name)) },
      getPropertyPriority: {
        value: (name) => {
          name = cssName(name);
          const components = cssShorthandComponents[name] || [name];
          return components.every(
            (n) => names().find((e) => e.name === n)?.priority === 'important',
          )
            ? 'important'
            : '';
        },
      },
      setProperty: {
        value: (name, value, priority = '') => {
          const inputName = name;
          name = cssName(name);
          if (/^webkit/i.test(name)) return;
          const inputValue = String(value);
          value = normalizeCSSValue(name, inputValue, inputName);
          if (value === null) return;
          priority = String(priority).toLowerCase();
          if (priority && priority !== 'important') return;
          const entries = names().slice(),
            components = cssShorthandComponents[name] || [name];
          if (value === '') {
            for (let i = entries.length - 1; i >= 0; i--)
              if (components.includes(entries[i].name) || entries[i].name === name)
                entries.splice(i, 1);
          } else
            for (const entry of expandCSSDeclaration(
              cssPrecisionDeclaration({ name, value, priority }, inputValue),
            )) {
              const index = entries.findIndex((e) => e.name === entry.name);
              if (index < 0) entries.push(entry);
              else entries[index] = entry;
            }
          blockDeclarations.set(state.node.block, entries);
          editedDeclarationBlocks.add(state.node.block);
          changed();
        },
      },
      removeProperty: {
        value: (name) => {
          const old = target.getPropertyValue(name);
          target.setProperty(name, '');
          return webkitCSSLegacyBreakShorthands.has(String(name).toLowerCase()) ||
            cssShorthandComponents[cssName(name)]
            ? ''
            : old;
        },
      },
    });
    return new Proxy(target, {
      get(object, key, receiver) {
        if (typeof key === 'string' && /^\d+$/.test(key)) return names()[Number(key)]?.name;
        if (typeof key === 'string' && !(key in object))
          return target.getPropertyValue(cssJSName(key));
        return Reflect.get(object, key, receiver);
      },
      set(object, key, value, receiver) {
        if (typeof key === 'string' && key !== 'cssText' && !(key in object)) {
          target.setProperty(cssJSInputName(key), value);
          return true;
        }
        return Reflect.set(object, key, value, receiver);
      },
    });
  }
  function makeRule(node, sheet, parent = null) {
    const name =
      node.type === 'Rule'
        ? 'CSSStyleRule'
        : {
            media: 'CSSMediaRule',
            supports: 'CSSSupportsRule',
            'font-face': 'CSSFontFaceRule',
            keyframes: 'CSSKeyframesRule',
            layer: node.block ? 'CSSLayerBlockRule' : 'CSSLayerStatementRule',
            container: 'CSSContainerRule',
            scope: 'CSSScopeRule',
            'starting-style': 'CSSStartingStyleRule',
            property: 'CSSPropertyRule',
          }[node.name] || 'CSSRule';
    const rule = Object.create((globalThis[name] || globalThis.CSSRule).prototype),
      state = { node, sheet, parent, rule, children: [] };
    rules.set(rule, state);
    if (node.block)
      node.block.children.forEach((child) => {
        if (child.type === 'Rule' || child.type === 'Atrule')
          state.children.push(makeRule(child, sheet, rule));
      });
    Object.defineProperties(rule, {
      cssText: { get: () => ruleText(rule), configurable: true },
      parentStyleSheet: { get: () => state.sheet, configurable: true },
      parentRule: { get: () => state.parent, configurable: true },
      type: {
        get: () =>
          node.type === 'Rule'
            ? 1
            : { media: 4, 'font-face': 5, keyframes: 7, supports: 12 }[node.name] || 0,
        configurable: true,
      },
    });
    if (node.type === 'Rule')
      Object.defineProperty(rule, 'selectorText', {
        get: () => preludeText(node.prelude),
        set: (value) => {
          try {
            node.prelude = parse(String(value), { context: 'selectorList' });
            changed();
          } catch {}
        },
        configurable: true,
      });
    if (node.block)
      Object.defineProperty(rule, 'style', { value: makeStyle(state), configurable: true });
    if (node.type === 'Atrule' && node.block) {
      Object.defineProperty(rule, 'cssRules', { value: list(state.children), configurable: true });
      Object.defineProperty(rule, 'conditionText', {
        get: () => preludeText(node.prelude),
        configurable: true,
      });
    }
    return rule;
  }
  function parsedRules(text, sheet) {
    const result = [];
    parse(String(text), { context: 'stylesheet' }).children.forEach((node) => {
      // Constructed stylesheets cannot fetch @import rules. CSS parser recovery
      // preserves other valid rules while discarding invalid raw fragments.
      if (
        (node.type === 'Rule' && node.prelude.type === 'SelectorList') ||
        (node.type === 'Atrule' && !['import', 'charset'].includes(node.name))
      )
        result.push(makeRule(node, sheet));
    });
    return result;
  }
  class CSSStyleSheet extends globalThis.StyleSheet {
    constructor(options = {}) {
      // The generated StyleSheet constructor is deliberately illegal; returning
      // the branded derived object avoids invoking that interface constructor.
      const sheet = Object.create(new.target.prototype),
        state = {
          rules: [],
          disabled: !!options.disabled,
          media: String(options.media || ''),
          locked: false,
        };
      sheets.set(sheet, state);
      state.list = list(state.rules);
      return sheet;
    }
    get cssRules() {
      const s = requireSheet(this);
      if (s.crossOrigin)
        throw platformDOMException('Cannot access cross-origin stylesheet', 'SecurityError');
      return s.list;
    }
    get rules() {
      return this.cssRules;
    }
    get ownerNode() {
      const s = requireSheet(this);
      if (!s.owner) return null;
      return ownerSheet(s.owner) === this ? s.owner : null;
    }
    get ownerRule() {
      requireSheet(this);
      return null;
    }
    get href() {
      return requireSheet(this).href || null;
    }
    get parentStyleSheet() {
      requireSheet(this);
      return null;
    }
    get title() {
      const s = requireSheet(this);
      return s.owner ? s.owner.getAttribute('title') || null : null;
    }
    get type() {
      requireSheet(this);
      return 'text/css';
    }
    get disabled() {
      return requireSheet(this).disabled;
    }
    set disabled(value) {
      requireSheet(this).disabled = !!value;
      changed();
    }
    get media() {
      const state = requireSheet(this);
      if (!state.mediaList) {
        const media = Object.create(globalThis.MediaList.prototype);
        Object.defineProperties(media, {
          mediaText: {
            get: () => state.media,
            set: (value) => {
              state.media = String(value);
              changed();
            },
          },
          length: { get: () => (state.media ? state.media.split(',').length : 0) },
          item: { value: (index) => state.media.split(',')[Number(index)]?.trim() || '' },
        });
        state.mediaList = media;
      }
      return state.mediaList;
    }
    replaceSync(text) {
      const state = requireSheet(this);
      if (state.owner || state.locked)
        throw platformDOMException('Stylesheet cannot be replaced', 'NotAllowedError');
      state.rules.splice(0, state.rules.length, ...parsedRules(text, this));
      changed();
    }
    replace(text) {
      const state = requireSheet(this);
      if (state.owner || state.locked)
        return platformPromiseReject(
          platformDOMException('Stylesheet cannot be replaced', 'NotAllowedError'),
        );
      text = String(text);
      state.locked = true;
      return platformPromiseResolve().then(() => {
        try {
          state.rules.splice(0, state.rules.length, ...parsedRules(text, this));
          changed();
          return this;
        } finally {
          state.locked = false;
        }
      });
    }
    insertRule(text, index = 0) {
      const state = requireSheet(this);
      index = Number(index) >>> 0;
      if (state.locked)
        throw platformDOMException('Stylesheet is being replaced', 'NotAllowedError');
      if (index > state.rules.length)
        throw platformDOMException('Index exceeds rule count', 'IndexSizeError');
      const ast = parse(String(text), { context: 'stylesheet' });
      if (ast.children.size !== 1) throw platformDOMException('Expected one rule', 'SyntaxError');
      if (ast.children.first.type === 'Atrule' && ast.children.first.name === 'import')
        throw platformDOMException('Cannot insert @import into a constructed sheet', 'SyntaxError');
      const inserted = parsedRules(text, this);
      if (inserted.length !== 1) throw platformDOMException('Invalid rule', 'SyntaxError');
      state.rules.splice(index, 0, inserted[0]);
      changed();
      return index;
    }
    deleteRule(index) {
      const state = requireSheet(this);
      index = Number(index) >>> 0;
      if (state.locked)
        throw platformDOMException('Stylesheet is being replaced', 'NotAllowedError');
      if (index >= state.rules.length)
        throw platformDOMException('Index exceeds rule count', 'IndexSizeError');
      const old = state.rules.splice(index, 1)[0];
      rules.get(old).sheet = null;
      changed();
    }
  }
  Object.defineProperty(CSSStyleSheet.prototype, Symbol.toStringTag, {
    value: 'CSSStyleSheet',
    configurable: true,
  });
  Object.defineProperty(globalThis, 'CSSStyleSheet', {
    value: CSSStyleSheet,
    writable: true,
    configurable: true,
  });
  const ownerConnected = (owner) => {
    if (!shadowHosts.size) return host.isConnected(elementSlot(owner).nodeId);
    // Synthetic shadow membership lives in the canonical JS slots, outside
    // the native arena. Walk both kinds of ancestry without author getters.
    for (let node = owner; node; ) {
      if (node === document) return true;
      const parent = syntheticParents.get(node) || shadowSlots.get(node)?.host;
      if (parent) {
        node = parent;
        continue;
      }
      const data = elementSlot(node);
      if (!data) return false;
      node = wrap(host.parentNode(data.nodeId));
    }
    return false;
  };
  function ownerSheet(owner) {
    if (!elementSlot(owner)) throw new TypeError('Illegal invocation');
    const data = elementSlot(owner),
      attribute = (name) => host.getAttribute(data.nodeId, name);
    const prior = owners.get(owner),
      tag = data.tagName.toLowerCase(),
      type = (attribute('type') || '').trim().toLowerCase();
    if (
      !ownerConnected(owner) ||
      (type && type !== 'text/css') ||
      (tag === 'link' &&
        !String(attribute('rel') || '')
          .toLowerCase()
          .split(/\s+/)
          .includes('stylesheet'))
    ) {
      owners.delete(owner);
      return null;
    }
    const resource =
      tag === 'link'
        ? host.stylesheetResource(attribute('href') || '', prior?.key || '', data.nodeId)
        : null;
    if (tag === 'link' && !resource) {
      owners.delete(owner);
      return null;
    }
    const source = tag === 'link' ? resource.body : canonicalTextContent(data.nodeId) || '',
      key = tag === 'link' ? resource.url : source;
    let sheet = prior?.key === key ? prior.sheet : null;
    if (!sheet) {
      sheet = new CSSStyleSheet();
      const s = sheets.get(sheet);
      s.owner = owner;
      s.href = resource?.url || null;
      // A sheet's URL context is captured when the sheet is created, not
      // recomputed from the current history entry for later CSSOM edits.
      s.baseURL = resource?.url || host.documentBaseURI();
      s.crossOrigin = !!resource?.crossOrigin;
      s.rules.push(...parsedRules(source, sheet));
      owners.set(owner, { key, sheet });
    }
    const state = sheets.get(sheet);
    state.media = attribute('media') || '';
    return sheet;
  }
  for (const type of ['HTMLStyleElement', 'SVGStyleElement', 'HTMLLinkElement'])
    if (globalThis[type]) {
      Object.defineProperty(globalThis[type].prototype, 'sheet', {
        get() {
          return ownerSheet(this);
        },
        enumerable: true,
        configurable: true,
      });
      Object.defineProperty(globalThis[type].prototype, 'disabled', {
        get() {
          const sheet = ownerSheet(this);
          return sheet ? sheets.get(sheet).disabled : false;
        },
        set(value) {
          const sheet = ownerSheet(this);
          if (sheet) {
            sheets.get(sheet).disabled = !!value;
            changed();
          }
        },
        enumerable: true,
        configurable: true,
      });
    }
  function ownerCollection(root) {
    let list = ownerLists.get(root);
    if (!list) {
      let domVersion, candidates;
      const values = () => {
        // Membership depends on canonical DOM writes; sheet availability and
        // CSSOM state are still rechecked even when membership is unchanged.
        const current = canonicalDOMRevision();
        if (current !== domVersion) {
          candidates = compatibilitySelectors.query(root, 'style,link', false, false);
          domVersion = current;
        }
        return candidates.map(ownerSheet).filter(Boolean);
      };
      list = new Proxy(Object.create(StyleSheetList.prototype), {
        get(target, key, receiver) {
          const all = values();
          if (key === 'length') return all.length;
          if (key === 'item') return (index) => values()[+index >>> 0] || null;
          if (key === Symbol.iterator) return all[Symbol.iterator].bind(all);
          if (typeof key === 'string' && /^\d+$/.test(key)) return all[Number(key)];
          return Reflect.get(target, key, receiver);
        },
      });
      ownerLists.set(root, list);
    }
    return list;
  }
  for (const type of ['Document', 'ShadowRoot'])
    if (globalThis[type])
      Object.defineProperty(globalThis[type].prototype, 'styleSheets', {
        get() {
          if (!(this instanceof globalThis[type])) throw new TypeError('Illegal invocation');
          return ownerCollection(this);
        },
        enumerable: true,
        configurable: true,
      });
  // The cache is derived from the canonical CSSOM; rule edits invalidate it.
  // DOM-owned sheets are still revalidated against their current owner text.
  const sourceText = (sheet) => {
    const state = sheets.get(sheet);
    if (state.disabled || (state.media && !cssMediaMatches(state.media))) return '';
    let cached = sourceCache.get(sheet);
    if (!cached || cached.revision !== revision) {
      cached = { revision, text: state.rules.map((rule) => ruleText(rule, true)).join('\n') };
      sourceCache.set(sheet, cached);
    }
    return cached.text;
  };
  function adoption(root) {
    if (!(root instanceof Document) && !shadowSlots.has(root))
      throw new TypeError('Illegal invocation');
    let value = adopted.get(root);
    if (!value) {
      value = new Proxy([], {
        set(target, key, sheet) {
          if (typeof key === 'string' && /^\d+$/.test(key) && !sheets.has(sheet))
            throw new TypeError('Value is not a constructed CSSStyleSheet');
          changed();
          return Reflect.set(target, key, sheet);
        },
        deleteProperty(target, key) {
          changed();
          return Reflect.deleteProperty(target, key);
        },
      });
      adopted.set(root, value);
    }
    return value;
  }
  for (const proto of [Document.prototype, ShadowRoot.prototype])
    Object.defineProperty(proto, 'adoptedStyleSheets', {
      configurable: true,
      enumerable: true,
      get() {
        return adoption(this);
      },
      set(value) {
        const next = Array.from(value);
        for (const sheet of next)
          if (!sheets.has(sheet)) throw new TypeError('Value is not a constructed CSSStyleSheet');
        const current = adoption(this);
        current.splice(0, current.length, ...next);
      },
    });
  // Inspector text and handles exist only while a CDP CSS consumer is active.
  // The rules remain the same canonical objects used by the style resolver.
  let inspector = null;
  const inspect = (request) => {
    if (request.method === 'status') return { active: inspector !== null };
    if (request.method === 'view')
      return {
        styles: constructedStyleSheets.sources(document),
        scroll: [windowScrollX, windowScrollY],
        positions: compatibilityScrolling.snapshot(),
      };
    if (request.method === 'release') {
      inspector = null;
      return {};
    }
    inspector ||= { next: 0, ids: new Map(), sources: new Map() };
    const active = new Set();
    const roots = [
      document,
      ...Array.from(shadowHosts, (host) => elementShadows.get(host)).filter(Boolean),
    ];
    for (const root of roots)
      for (const sheet of [...ownerCollection(root), ...adoption(root)]) active.add(sheet);
    for (const sheet of inspector.ids.keys())
      if (!active.has(sheet)) {
        inspector.sources.delete(inspector.ids.get(sheet));
        inspector.ids.delete(sheet);
      }
    const sheetSource = (sheet) => {
      let id = inspector.ids.get(sheet);
      if (!id) inspector.ids.set(sheet, (id = 'sheet-' + ++inspector.next));
      let source = inspector.sources.get(id);
      if (!source || source.revision !== revision) {
        const text = requireSheet(sheet)
          .rules.map((rule) => ruleText(rule, true))
          .join('\n');
        source = { sheet, text, revision };
        inspector.sources.set(id, source);
      }
      return { id, ...source };
    };
    const position = (text, offset) => {
      const lines = text.slice(0, offset).split('\n');
      return { line: lines.length - 1, column: lines.at(-1).length };
    };
    const range = (text, start, end) => {
      const a = position(text, start),
        b = position(text, end);
      return { startLine: a.line, startColumn: a.column, endLine: b.line, endColumn: b.column };
    };
    const offset = (text, line, column) => {
      const lines = text.split('\n');
      if (
        !Number.isInteger(line) ||
        !Number.isInteger(column) ||
        line < 0 ||
        line >= lines.length ||
        column < 0 ||
        column > lines[line].length
      )
        throw new Error('Invalid source range');
      return lines.slice(0, line).reduce((sum, value) => sum + value.length + 1, 0) + column;
    };
    const style = (text, start, end, id) => {
      const cssText = text.slice(start, end),
        ast = parse(cssText, { context: 'declarationList', positions: true });
      const cssProperties = [];
      ast.children.forEach((declaration) => {
        if (declaration.type !== 'Declaration') return;
        const property = {
          name: declaration.property,
          value: generate(declaration.value).trim(),
          important: !!declaration.important,
          implicit: false,
          disabled: false,
          parsedOk: parseCSS(generate(declaration)).length > 0,
          text: cssText.slice(declaration.loc.start.offset, declaration.loc.end.offset),
          range: range(
            text,
            start + declaration.loc.start.offset,
            start + declaration.loc.end.offset,
          ),
        };
        cssProperties.push(property);
      });
      const result = {
        cssProperties,
        shorthandEntries: [],
        cssText,
        range: range(text, start, end),
      };
      if (id) result.styleSheetId = id;
      return result;
    };
    const inline = (node) => {
      const id = elementSlot(node)?.nodeId;
      if (!id || elementSlot(node).type !== 'element') throw new Error('Node is not an element');
      const text = host.getAttribute(id, 'style') || '';
      return style(text, 0, text.length, 'inline-' + id);
    };
    const headers = Array.from(active, (sheet) => {
      const source = sheetSource(sheet),
        state = requireSheet(sheet),
        end = position(source.text, source.text.length);
      return {
        styleSheetId: source.id,
        inspectorText: source.text,
        sourceURL: state.href || host.location(),
        origin: 'regular',
        title: '',
        ownerNode: state.owner ? elementSlot(state.owner).nodeId : undefined,
        disabled: state.disabled,
        isInline: !!state.owner && !state.href,
        isMutable: true,
        isConstructed: !state.owner,
        startLine: 0,
        startColumn: 0,
        length: source.text.length,
        endLine: end.line,
        endColumn: end.column,
      };
    });
    if (request.method === 'headers') return { headers };
    const node = request.nodeId ? wrap(request.nodeId) : null;
    if (request.method === 'inline') return { inlineStyle: inline(node) };
    const rootFor = (element) => {
      for (let node = element; node; ) {
        if (shadowSlots.has(node) || node === document) return node;
        node = syntheticParents.get(node) || wrap(host.parentNode(elementSlot(node)?.nodeId || 0));
      }
      return document;
    };
    const matchRules = (element, pseudo = '') => {
      const result = [];
      for (const sheet of active) {
        const scopes = roots.filter((root) =>
          [...ownerCollection(root), ...adoption(root)].includes(sheet),
        );
        const scope = scopes.find(
          (root) => root === rootFor(element) || root === elementShadows.get(element),
        );
        if (!scope) continue;
        const state = requireSheet(sheet);
        if (state.disabled || (state.media && !cssMediaMatches(state.media))) continue;
        const source = sheetSource(sheet);
        const ast = parse(source.text, { positions: true });
        const visit = (items, media = []) =>
          items.forEach((rule) => {
            if (rule.type === 'Atrule') {
              if (rule.name === 'media') {
                const query = preludeText(rule.prelude);
                if (!cssMediaMatches(query)) return;
                if (rule.block)
                  visit(rule.block.children, [...media, { text: query, source: 'mediaRule' }]);
              } else if (rule.name === 'supports') {
                if (!compatibilityCSSSupports.matches(preludeText(rule.prelude))) return;
                if (rule.block) visit(rule.block.children, media);
              } else if (rule.block && ['layer', 'container'].includes(rule.name)) {
                if (rule.name === 'container') return; // no inspector container evaluator
                visit(rule.block.children, media);
              }
              return;
            }
            if (rule.type !== 'Rule') return;
            const selectors = Array.from(rule.prelude.children, (selector) =>
              preludeText(selector),
            );
            const matchingSelectors = [];
            selectors.forEach((selector, index) => {
              try {
                const selectorPseudo = /::?(before|after)$/.exec(selector)?.[1] || '';
                if (selectorPseudo !== pseudo) return;
                const matched = compatibilitySelectors.matchesStyle(
                  element,
                  selectorPseudo ? selector.replace(/::?(before|after)$/, '') : selector,
                  scope === document ? null : scope,
                );
                if (matched) matchingSelectors.push(index);
              } catch (error) {
                if (error?.name !== 'SyntaxError') throw error;
              }
            });
            if (!matchingSelectors.length) return;
            result.push({
              matchingSelectors,
              rule: {
                styleSheetId: source.id,
                origin: 'regular',
                selectorList: {
                  text: preludeText(rule.prelude),
                  selectors: selectors.map((text) => ({ text })),
                  range: range(
                    source.text,
                    rule.prelude.loc.start.offset,
                    rule.prelude.loc.end.offset,
                  ),
                },
                style: style(
                  source.text,
                  rule.block.loc.start.offset + 1,
                  rule.block.loc.end.offset - 1,
                  source.id,
                ),
                media,
              },
            });
          });
        visit(ast.children);
      }
      return result;
    };
    if (request.method === 'matched') {
      const inherited = [];
      for (
        let parent = cssFontParent(node);
        elementSlot(parent)?.type === 'element';
        parent = cssFontParent(parent)
      )
        inherited.push({ inlineStyle: inline(parent), matchedCSSRules: matchRules(parent) });
      return {
        inlineStyle: inline(node),
        matchedCSSRules: matchRules(node),
        inherited,
        pseudoElements: ['before', 'after']
          .map((pseudoType) => ({ pseudoType, matches: matchRules(node, pseudoType) }))
          .filter((entry) => entry.matches.length),
        inheritedPseudoElements: [],
        cssKeyframesRules: [],
      };
    }
    const id = request.styleSheetId;
    let source;
    if (id?.startsWith('inline-')) {
      const element = wrap(Number(id.slice(7)));
      if (!elementSlot(element)) throw new Error('No stylesheet with given id found');
      source = { element, text: host.getAttribute(elementSlot(element).nodeId, 'style') || '' };
    } else source = inspector.sources.get(id);
    if (!source) throw new Error('No stylesheet with given id found');
    if (request.method === 'text') return { text: source.text };
    let text = String(request.text);
    if (request.range) {
      const r = request.range,
        start = offset(source.text, r.startLine, r.startColumn),
        end = offset(source.text, r.endLine, r.endColumn);
      if (end < start) throw new Error('Invalid source range');
      text = source.text.slice(0, start) + text + source.text.slice(end);
    }
    if (source.element) host.setAttribute(elementSlot(source.element).nodeId, 'style', text);
    else {
      const state = requireSheet(source.sheet);
      if (request.range && request.method === 'editSelector') {
        const ast = parse(source.text, { positions: true });
        const start = offset(source.text, request.range.startLine, request.range.startColumn);
        const end = offset(source.text, request.range.endLine, request.range.endColumn);
        let target;
        const find = (nodes, canonical) => {
          let index = 0;
          nodes.forEach((node) => {
            if (node.type !== 'Rule' && node.type !== 'Atrule') return;
            const rule = canonical[index++];
            if (!rule) return;
            if (
              node.type === 'Rule' &&
              node.prelude.loc.start.offset === start &&
              node.prelude.loc.end.offset === end
            )
              target = rules.get(rule);
            if (node.block) find(node.block.children, rules.get(rule).children);
          });
        };
        find(ast.children, state.rules);
        if (!target) throw new Error('Source range does not identify a selector');
        target.node.prelude = parse(String(request.text), { context: 'selectorList' });
      } else if (request.range && request.method === 'editStyle') {
        const ast = parse(source.text, { positions: true });
        const start = offset(source.text, request.range.startLine, request.range.startColumn);
        const end = offset(source.text, request.range.endLine, request.range.endColumn);
        let target;
        const find = (nodes, canonical) => {
          let index = 0;
          nodes.forEach((node) => {
            if (node.type !== 'Rule' && node.type !== 'Atrule') return;
            const rule = canonical[index++];
            if (!rule) return;
            if (
              node.block &&
              start >= node.block.loc.start.offset + 1 &&
              end <= node.block.loc.end.offset - 1
            )
              target = rules.get(rule);
            if (node.block) find(node.block.children, rules.get(rule).children);
          });
        };
        find(ast.children, state.rules);
        if (!target) throw new Error('Source range does not identify a declaration block');
        const nextAst = parse(text, { positions: true });
        const nextStart = start,
          nextEnd = start + String(request.text).length;
        let block;
        const findBlock = (nodes) =>
          nodes.forEach((node) => {
            if (
              node.block &&
              nextStart >= node.block.loc.start.offset + 1 &&
              nextEnd <= node.block.loc.end.offset - 1
            )
              block = node.block;
            if (node.block) findBlock(node.block.children);
          });
        findBlock(nextAst.children);
        if (!block) throw new Error('Edited declaration block is invalid');
        blockDeclarations.set(target.node.block, parseCSS(generate(block).slice(1, -1)));
        editedDeclarationBlocks.add(target.node.block);
      } else state.rules.splice(0, state.rules.length, ...parsedRules(text, source.sheet));
      changed();
      inspector.sources.set(id, { sheet: source.sheet, text, revision });
    }
    if (request.method === 'editStyle') {
      const start = request.range
        ? offset(text, request.range.startLine, request.range.startColumn)
        : 0;
      return { style: style(text, start, start + String(request.text).length, id) };
    }
    return {};
  };
  return {
    inspect,
    /* dev_preview_sources */
    revision: () => revision,
    nativeSources(root) {
      if ((adopted.get(root) || []).length)
        return { unsupported: 'adopted stylesheet adapter pending', sheets: [] };
      const collection = Array.from(ownerCollection(root));
      let restyleOnChange = false;
      for (const sheet of collection) {
        let cached = nativeEligibilityCache.get(sheet);
        if (!cached || cached.revision !== revision) {
          const firstReason = (items, get) => {
            for (const item of items) {
              const reason = get(item);
              if (reason) return reason;
            }
            return '';
          };
          let relational = false;
          const visit = (rule) => {
            const state = rules.get(rule);
            let relationalUnsupported = false;
            if (state.node.type === 'Rule') {
              const selector = preludeText(state.node.prelude);
              // The pinned native matcher supports :has(), but its incremental
              // invalidator does not cover all ancestor/sibling dependencies.
              const admission = nativeRelationalSelector(selector);
              relational ||= Boolean(admission);
              relationalUnsupported = admission?.unsupported;
            }
            return (
              (relationalUnsupported ? 'native relational predicate adapter pending' : '') ||
              (!editedDeclarationBlocks.has(state.node.block) &&
                firstReason(declarations(state.node.block), blitzUnsupportedDeclaration)) ||
              (entriesForBlock(state.node.block).some(
                (entry) => entry.name === 'content-visibility',
              )
                ? 'authored content-visibility requires canonical fallback'
                : '') ||
              firstReason(state.children, visit) ||
              ''
            );
          };
          const unsupported = firstReason(requireSheet(sheet).rules, visit);
          cached = { revision, unsupported, relational };
          nativeEligibilityCache.set(sheet, cached);
        }
        if (cached.unsupported)
          return {
            unsupported: cached.unsupported,
            sheets: [],
          };
        restyleOnChange ||= cached.relational;
      }
      return {
        unsupported: '',
        restyleOnChange,
        sheets: collection.map((sheet) => ({
          id: elementSlot(requireSheet(sheet).owner).nodeId,
          text: sourceText(sheet),
          baseURL: requireSheet(sheet).baseURL,
        })),
      };
    },
    ownerSheet,
    resourceSources(root) {
      const collection = new Set([...ownerCollection(root), ...adoption(root)]);
      return Array.from(collection, (sheet) => sourceText(sheet));
    },
    fontFaceRules(root) {
      const result = [];
      const visit = (rule, base) => {
        const state = rules.get(rule);
        if (state.node.type === 'Atrule' && state.node.name === 'font-face')
          result.push({
            rule,
            base,
            declarations: Object.fromEntries(
              entriesForBlock(state.node.block).map((entry) => [entry.name, entry.value]),
            ),
          });
        for (const child of state.children) visit(child, base);
      };
      for (const sheet of Array.from(ownerCollection(root)).concat(adopted.get(root) || [])) {
        const state = requireSheet(sheet);
        if (state.disabled || (state.media && !cssMediaMatches(state.media))) continue;
        const base = state.href || host.location();
        for (const rule of state.rules) visit(rule, base);
      }
      return result;
    },
    sources(root) {
      return Array.from(ownerCollection(root))
        .concat(adopted.get(root) || [])
        .map(sourceText);
    },
    snapshot(root) {
      return (adopted.get(root) || [])
        .filter((sheet) => !requireSheet(sheet).disabled)
        .map((sheet) => {
          const state = requireSheet(sheet),
            text = state.rules.map((rule) => ruleText(rule, true)).join('\n');
          return state.media ? '@media ' + state.media + ' {\n' + text + '\n}' : text;
        });
    },
  };
})();
// Work from parsed Declaration nodes, then decode their CSS identifier escapes.
// Text searches would mistake strings/custom-property payloads for declarations
// and miss valid escaped property names.
const blitzUnsupportedDeclaration = (node) => {
  if (node.type !== 'Declaration') return false;
  const input = node.property;
  let name = '';
  for (let i = 0; i < input.length; i++) {
    if (input[i] !== '\\') {
      name += input[i];
      continue;
    }
    let hex = '';
    while (i + 1 < input.length && hex.length < 6 && /[0-9a-f]/i.test(input[i + 1]))
      hex += input[++i];
    if (hex) {
      const code = parseInt(hex, 16);
      name += String.fromCodePoint(
        code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff) ? 0xfffd : code,
      );
      if (i + 1 < input.length && /[\t\n\r\f ]/.test(input[i + 1])) {
        i++;
        if (input[i] === '\r' && input[i + 1] === '\n') i++;
      }
    } else if (i + 1 < input.length) name += input[++i];
  }
  name = name.toLowerCase();
  if (name === 'content-visibility')
    return 'authored content-visibility requires canonical fallback';
  // The native style producer does not consume the Context's system-font
  // palette. The JS cascade resolves these declarations per Page.
  return name === 'font' &&
    parseCSS(`font:${mimicSelectorLibrary.generateCSS(node.value)}`).some(
      (entry) => entry.pending?.systemFont,
    )
    ? 'system font requires per-Context CSS fallback'
    : '';
};
// Admission depends on declaration syntax, not DOM state or the environment.
// Retain only the exact source and its scalar result, within a per-realm bound.
let blitzInlineAdmissionCache = new Map(),
  blitzInlineAdmissionBytes = 0;
bootstrapRestoreHooks.push(() => {
  blitzInlineAdmissionCache = new Map();
  blitzInlineAdmissionBytes = 0;
});
const blitzInlineAdmission = (source) => {
  if (blitzInlineAdmissionCache.has(source)) return blitzInlineAdmissionCache.get(source);
  let unsupported = '';
  try {
    const block = mimicSelectorLibrary.parseStylesheet(source, { context: 'declarationList' });
    block.children.forEach((node) => {
      unsupported ||= blitzUnsupportedDeclaration(node);
    });
  } catch {
    unsupported = 'native inline declaration admission failed';
  }
  if (source.length * 2 <= 1024 * 1024) {
    if (
      blitzInlineAdmissionCache.size >= 256 ||
      blitzInlineAdmissionBytes + source.length * 2 > 1024 * 1024
    ) {
      blitzInlineAdmissionCache.clear();
      blitzInlineAdmissionBytes = 0;
    }
    blitzInlineAdmissionCache.set(source, unsupported);
    blitzInlineAdmissionBytes += source.length * 2;
  }
  return unsupported;
};
let blitzControlMembershipRevision,
  blitzControlMembership = [];
bootstrapRestoreHooks.push(() => {
  blitzControlMembershipRevision = undefined;
  blitzControlMembership = [];
});
let blitzSheetInputs = [],
  blitzSheetInputRevision = 0;
bootstrapRestoreHooks.push(() => {
  blitzSheetInputs = [];
  blitzSheetInputRevision = 0;
});
const readBlitzInputs = (knownSheetRevision = 0) => {
  if (document.compatMode === 'BackCompat')
    return JSON.stringify({ unsupported: 'quirks mode adapter pending' });
  if (styleObservationDynamic)
    return JSON.stringify({ unsupported: 'animation lifecycle adapter pending' });
  if (cssRegisteredProperties.names().size)
    return JSON.stringify({
      unsupported: 'registered custom properties require semantic style resolution',
    });
  // A composed shadow tree can change the host's flow box and the positions
  // of following siblings. Only a zero-sized host with no light children is
  // independent of native light-tree flow; all other cases retain the
  // composed-tree layout path for the whole document.
  if (shadowHosts.size) {
    for (const shadowHost of shadowHosts) {
      const slot = elementSlot(shadowHost);
      if (!slot || !host.isConnected(slot.nodeId)) continue;
      if (cssObservationChildren(shadowHost).length)
        return JSON.stringify({ unsupported: 'shadow host changes document flow' });
      let box;
      blitzShadowAdmissionDepth++;
      try {
        // Native admission can be requested from an isolated world's read.
        // Its canonical owner callback does not inherit that world's snapshot.
        box = withStyleReadCache(() => cssBoxModel.rect(shadowHost));
      } finally {
        blitzShadowAdmissionDepth--;
      }
      if (box.width || box.height)
        return JSON.stringify({ unsupported: 'shadow host changes document flow' });
    }
  }
  const inputs = constructedStyleSheets.nativeSources(document);
  if (!inputs.unsupported) {
    const current = inputs.sheets;
    if (
      !blitzSheetInputRevision ||
      current.length !== blitzSheetInputs.length ||
      current.some(
        (sheet, i) =>
          sheet.id !== blitzSheetInputs[i].id ||
          sheet.text !== blitzSheetInputs[i].text ||
          sheet.baseURL !== blitzSheetInputs[i].baseURL,
      )
    ) {
      blitzSheetInputs = current;
      blitzSheetInputRevision++;
    }
    inputs.sheetRevision = blitzSheetInputRevision;
    if (knownSheetRevision === blitzSheetInputRevision) {
      inputs.sheetsUnchanged = true;
      delete inputs.sheets;
    }
  }
  const membershipRevision = canonicalDOMRevision();
  if (blitzControlMembershipRevision !== membershipRevision) {
    blitzControlMembership = compatibilitySelectors.query(
      document,
      'input,textarea,select,option,video',
      false,
      false,
    );
    blitzControlMembershipRevision = membershipRevision;
  }
  inputs.controls = [];
  for (const control of blitzControlMembership) {
    const slot = elementSlot(control);
    if (slot.tagName === 'VIDEO') {
      inputs.unsupported = 'native video intrinsic metadata adapter pending';
      break;
    }
    if (slot.tagName === 'SELECT') {
      const size = /^\s*\+?(\d+)/.exec(host.getAttribute(slot.nodeId, 'size') || '');
      if (host.getAttribute(slot.nodeId, 'multiple') !== null || (size && Number(size[1]) > 1)) {
        inputs.unsupported = 'native select listbox adapter pending';
        break;
      }
    }
    if (compatibilityElementState.nativeControlContentUnsupported?.(control)) {
      inputs.unsupported = 'native live control content adapter pending';
      break;
    }
    const value = compatibilityElementState.nativeControlValue?.(control);
    if (value) inputs.controls.push(value);
  }
  if (!inputs.unsupported) {
    for (const inline of host.blitzInlineStyles()) {
      const unsupported = blitzInlineAdmission(inline);
      if (unsupported) {
        inputs.unsupported = unsupported;
        break;
      }
    }
  }
  inputs.states = [];
  const focused = compatibilityElementState.focused?.();
  const focusAncestors = new Set();
  for (let node = focused; node; node = cssObservationParent(node)) focusAncestors.add(node);
  // Default HTML state and dirty slots have one canonical owner. The native
  // producer does not derive CHECKED from markup, so include controls even
  // when no author script has read their private slots yet.
  const stateNodes = new Set(compatibilityElementState.nativeStateControls?.() || []);
  for (const node of blitzControlMembership)
    if (['INPUT', 'OPTION'].includes(elementSlot(node)?.tagName)) stateNodes.add(node);
  for (const node of focusAncestors) stateNodes.add(node);
  for (const node of stateNodes) {
    const slot = elementSlot(node);
    if (!slot || !cssObservationNodeState(node).connected) continue;
    let mask = 14,
      flags = focusAncestors.has(node) ? 8 : 0;
    if (node === focused) {
      flags |= 2;
      if (compatibilityElementState.focusVisible(node)) flags |= 4;
    }
    if (slot.tagName === 'INPUT' || slot.tagName === 'OPTION') {
      mask |= 1;
      if (compatibilityElementState.selectorChecked(node)) flags |= 1;
    }
    // Only controls need explicit false overrides. Focus removals are handled
    // against the previous native transaction, without wrapping the whole DOM.
    if (flags || mask & 1) inputs.states.push({ id: slot.nodeId, mask, flags });
  }
  return JSON.stringify(inputs);
};
host.registerBlitzInputs(readBlitzInputs);
bootstrapRestoreHooks.push(() => host.registerBlitzInputs(readBlitzInputs));
