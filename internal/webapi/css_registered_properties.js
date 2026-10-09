// Registered custom properties share cascade and variable substitution with
// ordinary declarations. Syntax matching belongs to the pinned CSS-tree lexer.
const cssRegisteredProperties = (() => {
  const scripted = new Map();
  const supportedTypes = new Set([
    'length',
    'number',
    'percentage',
    'length-percentage',
    'color',
    'image',
    'url',
    'integer',
    'angle',
    'time',
    'resolution',
    'transform-function',
    'transform-list',
    'custom-ident',
  ]);
  const definition = (name, syntax, inherits, initialValue) => {
    if (!/^--[^\s]+$/.test(name) || name === '--')
      throw platformDOMException('Invalid custom property name', 'SyntaxError');
    syntax = String(syntax).trim();
    if (
      syntax !== '*' &&
      !syntax.split('|').every((term) => {
        const match = /^\s*(?:<([a-z-]+)>|([-_a-zA-Z][-_a-zA-Z0-9]*))([+#])?\s*$/.exec(term);
        return match && (!match[1] || supportedTypes.has(match[1]));
      })
    )
      throw platformDOMException('Invalid custom property syntax', 'SyntaxError');
    if (initialValue === undefined && syntax !== '*')
      throw platformDOMException('Initial value is required', 'SyntaxError');
    const initial = initialValue === undefined ? null : String(initialValue).trim();
    if (
      initial !== null &&
      syntax !== '*' &&
      (!mimicSelectorLibrary.matchesValueSyntax(syntax, initial) ||
        /(?:var|env)\(|(?:^|[^\w-])(?:currentcolor|[+-]?(?:\d*\.)?\d+(?:em|rem|ex|ch|cap|ic|lh|rlh))\b/i.test(
          initial,
        ))
    )
      throw platformDOMException(
        'Initial value must match syntax and be computationally independent',
        'SyntaxError',
      );
    return { name, syntax, inherits: Boolean(inherits), initial };
  };
  let validatedVersion = null;
  let sourceVersion = null,
    fromSheets = new Map();
  bootstrapRestoreHooks.push(() => {
    // Epochs identify state within a Document, not across snapshot consumers.
    validatedVersion = null;
    sourceVersion = null;
    fromSheets = new Map();
  });
  const refresh = () => {
    // The canonical observation covers DOM, CSSOM, adopted sheets and media
    // inputs. Reuse its read memo instead of collecting every sheet for each
    // custom-property lookup, including recursive inheritance.
    const observation = styleReadCache;
    if (observation?.registeredPropertySheets === fromSheets) return;
    // Native projections may request variables outside a JS read scope.
    // Validate the same stylesheet program once per authoritative epoch there.
    const version =
      observation?.version || host.observationVersion() + ':' + constructedStyleSheets.revision();
    if (version === validatedVersion) {
      if (observation) observation.registeredPropertySheets = fromSheets;
      return;
    }
    const sources = constructedStyleSheets.sources(document);
    if (
      sourceVersion &&
      sources.length === sourceVersion.length &&
      sources.every((source, index) => source === sourceVersion[index])
    ) {
      validatedVersion = version;
      if (observation) observation.registeredPropertySheets = fromSheets;
      return;
    }
    sourceVersion = sources;
    fromSheets = new Map();
    const walk = (nodes) => {
      for (const node of nodes) {
        if (node.type !== 'Atrule') continue;
        if (
          node.name === 'media' &&
          node.block &&
          cssMediaMatches(mimicSelectorLibrary.generateCSS(node.prelude))
        )
          walk(node.block.children);
        if (node.name !== 'property' || !node.block) continue;
        const declarations = new Map(
          Array.from(node.block.children)
            .filter((n) => n.type === 'Declaration')
            .map((n) => [n.property, n.value]),
        );
        const syntax = declarations.get('syntax')?.children?.first;
        const inherits = declarations.get('inherits')?.children?.first;
        if (
          syntax?.type !== 'String' ||
          inherits?.type !== 'Identifier' ||
          !['true', 'false'].includes(inherits.name)
        )
          continue;
        try {
          const name = mimicSelectorLibrary.generateCSS(node.prelude);
          const initial = declarations.has('initial-value')
            ? mimicSelectorLibrary.generateCSS(declarations.get('initial-value'))
            : undefined;
          fromSheets.set(name, definition(name, syntax.value, inherits.name === 'true', initial));
        } catch {}
      }
    };
    for (const source of sources) {
      // Escaped names still go through the parser. Other sheets cannot contain
      // a property registration without a literal at-keyword.
      if (!/@property/i.test(source) && !source.includes('\\')) continue;
      try {
        walk(mimicSelectorLibrary.parseStylesheet(source).children);
      } catch {}
    }
    validatedVersion = version;
    if (observation) observation.registeredPropertySheets = fromSheets;
  };
  if (globalThis.CSS)
    Object.defineProperty(CSS, 'registerProperty', {
      value: function registerProperty(descriptor) {
        if (!descriptor || descriptor.name === undefined || descriptor.inherits === undefined)
          throw new TypeError('Name and inherits are required');
        const name = String(descriptor.name);
        if (scripted.has(name))
          throw platformDOMException('Property is already registered', 'InvalidModificationError');
        const record = definition(
          name,
          descriptor.syntax === undefined ? '*' : descriptor.syntax,
          descriptor.inherits,
          descriptor.initialValue,
        );
        scripted.set(name, record);
        host.invalidateStyleObservations();
      },
      enumerable: true,
      writable: true,
      configurable: true,
    });
  return {
    get(name) {
      refresh();
      return scripted.get(name) || fromSheets.get(name);
    },
    names() {
      refresh();
      return new Set([...fromSheets.keys(), ...scripted.keys()]);
    },
  };
})();
const cssCustomPropertyValue = (element, name, seen = new Set()) => {
  const registration = cssRegisteredProperties.get(name);
  // Inheritance keeps the dependency scope unchanged. Only resolving a
  // defining value needs a new scope, so empty ancestors allocate no Sets.
  for (let owner = element; ; ) {
    const key = elementSlot(owner).nodeId + ':' + name;
    if (seen.has(key)) return null;
    const declaration = computedCSSDeclarations(owner).find((entry) => entry.name === name);
    let value = declaration?.value;
    const inherits = registration?.inherits ?? true;
    if (
      value === 'inherit' ||
      ((value == null || value === 'unset' || value === 'revert' || value === 'revert-layer') &&
        inherits)
    ) {
      const parent = cssFontParent(owner);
      if (elementSlot(parent)?.type !== 'element') return registration?.initial ?? null;
      owner = parent;
      continue;
    }
    if (value == null || ['initial', 'unset', 'revert', 'revert-layer'].includes(value))
      value = registration?.initial ?? null;
    if (value === null) return null;
    const next = new Set(seen);
    next.add(key);
    value = geometryValue(owner, value, next);
    if (
      registration &&
      (value === null ||
        (registration.syntax !== '*' &&
          !mimicSelectorLibrary.matchesValueSyntax(registration.syntax, value)))
    ) {
      const parent = cssFontParent(owner);
      if (registration.inherits && elementSlot(parent)?.type === 'element') {
        owner = parent;
        continue;
      }
      return registration.initial;
    }
    if (registration && registration.syntax !== '*') {
      if (registration.syntax === '<color>') {
        const rgba = cssColorRGBA(value, cssUsedColorScheme(owner));
        if (rgba) return cssSerializeColor(rgba);
      }
      if (registration.syntax === '<length>') {
        const length = cssResolveLength(value, cssGeometryLengthContext(owner, 0));
        if (length !== null) return cssSerializeNumber(length) + 'px';
      }
      if (['<number>', '<integer>'].includes(registration.syntax) && cssNumberRegex.test(value))
        return cssSerializeNumber(Number(value));
    }
    return value;
  }
};
