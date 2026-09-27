// CSS parsing and matching are supplied by the pinned css-select bundle.
// This adapter reads canonical wrappers; query-local memoization never outlives
// the synchronous selector operation and cannot become a second mutable DOM.
const compatibilitySelectors = (() => {
  const library = mimicSelectorLibrary;
  const astCache = new Map();
  const compiledCache = new WeakMap();
  const cacheLimit = 128;
  const extensions = new Set([
    'contains',
    'icontains',
    'matches',
    'parent',
    'header',
    'selected',
    'button',
    'input',
    'text',
    'checkbox',
    'file',
    'password',
    'radio',
    'reset',
    'image',
    'submit',
  ]);
  const legacyElements = new Set(['before', 'after', 'first-line', 'first-letter']);
  // css-select's built-in :checked alias tests attributes before consulting
  // user pseudos. Rewrite parsed tokens only, keeping this name inaccessible
  // to author selectors and leaving strings/attribute values untouched.
  const checkedPseudo = 'mimic-internal-checked';
  const pseudoElements = new Set([
    ...legacyElements,
    'selection',
    'marker',
    'placeholder',
    'backdrop',
    'file-selector-button',
    'spelling-error',
    'grammar-error',
    'target-text',
    'part',
    'slotted',
    'cue',
  ]);
  const emptySelector = () => [
    { type: 'pseudo', name: 'not', data: [[{ type: 'universal', namespace: null }]] },
  ];
  const syntax = (message) => platformDOMException(message, 'SyntaxError');
  // Filtering library extensions belongs at the DOM API boundary. Parsing
  // escapes, combinators, attribute operators and an+b remains upstream code.
  function validate(groups, nested = false, forgiving = false, relative = false, strict = false) {
    const result = [];
    for (const group of groups) {
      try {
        let pseudoElement = false;
        const tokens = group.map((token, index) => {
          const next = { ...token };
          if (token.type === 'parent' || (token.type === 'attribute' && token.action === 'not'))
            throw syntax('Non-standard selector');
          const legacy = token.type === 'pseudo' && legacyElements.has(token.name);
          if (token.type === 'pseudo-element' || legacy) {
            if (nested || !pseudoElements.has(token.name) || index !== group.length - 1)
              throw syntax('Invalid pseudo-element');
            pseudoElement = true;
            return { ...emptySelector()[0] };
          }
          if (token.type === 'pseudo') {
            if (token.name === checkedPseudo) throw syntax('Unknown pseudo-class');
            if (extensions.has(token.name)) throw syntax('Non-standard pseudo-class');
            if (Array.isArray(token.data)) {
              const inner = validate(
                token.data,
                true,
                !strict && (token.name === 'is' || token.name === 'where'),
                token.name === 'has',
                strict,
              );
              next.data = inner.length ? inner : [emptySelector()];
            }
          }
          return next;
        });
        // Ask the library to validate unknown pseudo-classes and function args
        // even for branches that contain a non-selectable pseudo-element.
        library.compileToken(copy(tokens.length ? [tokens] : [emptySelector()]), {
          ...options(null),
          relativeSelector: relative,
        });
        if (!pseudoElement) result.push(tokens);
      } catch (error) {
        if (!forgiving) throw error;
      }
    }
    return result;
  }
  function copy(groups) {
    return groups.map((group) =>
      group.map((token) => ({
        ...token,
        ...(token.type === 'pseudo' && token.name === 'checked' ? { name: checkedPseudo } : {}),
        ...(Array.isArray(token.data) ? { data: copy(token.data) } : {}),
      })),
    );
  }
  function parsed(selector) {
    selector = String(selector);
    if (astCache.has(selector)) return astCache.get(selector);
    let value;
    try {
      value = validate(library.parse(selector));
    } catch (error) {
      throw syntax(error.message || 'Invalid selector');
    }
    if (astCache.size === cacheLimit) astCache.delete(astCache.keys().next().value);
    astCache.set(selector, value);
    return value;
  }
  let reads;
  function memo(node, key, read) {
    let record = reads.get(node);
    if (!record) reads.set(node, (record = new Map()));
    if (!record.has(key)) record.set(key, read());
    return record.get(key);
  }
  let documentID = host.documentRootID();
  bootstrapRestoreHooks.push(() => {
    documentID = host.documentRootID();
  });
  const children = (node) =>
    memo(node, 'children', () => {
      if (styleReadCache && reads === styleReadCache.selectorReads)
        return cssObservationChildren(node);
      const slot = elementSlot(node);
      if (node === document || slot)
        return host.childIDs(node === document ? documentID : slot.nodeId).map(wrap);
      return fragmentSlots.get(node)?.children.slice() || [];
    });
  const parent = (node) =>
    memo(node, 'parent', () => {
      if (styleReadCache && reads === styleReadCache.selectorReads)
        return cssObservationParent(node);
      if (syntheticParents.has(node)) return syntheticParents.get(node);
      const slot = elementSlot(node);
      return slot ? wrap(host.parentNode(slot.nodeId)) : null;
    });
  const attribute = (node, name) =>
    styleReadCache && reads === styleReadCache.selectorReads
      ? (cssObservationAttribute(node, name) ?? undefined)
      : memo(
          node,
          'attribute:' + name,
          () => host.getAttribute(elementSlot(node).nodeId, name) ?? undefined,
        );
  const attributeNames = (node) =>
    memo(node, 'attributeNames', () =>
      styleReadCache && reads === styleReadCache.selectorReads
        ? Object.keys(cssObservationNodeState(node).attributes)
        : host.attributeNames(elementSlot(node).nodeId),
    );
  const adapter = {
    isTag: (node) => elementSlot(node)?.type === 'element',
    getName: (node) => elementSlot(node).tagName.toLowerCase(),
    getChildren: children,
    getParent: parent,
    getSiblings: (node) => (parent(node) ? children(parent(node)) : [node]),
    getAttributeValue: attribute,
    hasAttrib: (node, name) => attribute(node, name) !== undefined,
    getText: (node) => memo(node, 'text', () => node.textContent),
    equals: (a, b) => a === b,
  };
  const statePseudos = {
    [checkedPseudo]: (node) => compatibilityElementState.selectorChecked(node),
    modal: (node) => compatibilityElementState.modal(node),
    target: (node) => elementSlot(node).nodeId === host.selectorTargetID(),
    defined: (node) => compatibilityElementState.isDefined(node),
    focus: (node) => compatibilityElementState.focused() === node,
    'focus-within': (node) => {
      for (let current = compatibilityElementState.focused(); current; current = parent(current))
        if (current === node) return true;
      return false;
    },
    'focus-visible': (node) => compatibilityElementState.focusVisible(node),
  };
  function options(scope) {
    return {
      adapter,
      equals: adapter.equals,
      context: scope,
      relativeSelector: false,
      cacheResults: false,
      pseudos: statePseudos,
    };
  }
  function predicate(scope, selector) {
    const ast = parsed(selector);
    let cache = compiledCache.get(scope);
    if (!cache) compiledCache.set(scope, (cache = new Map()));
    if (cache.has(selector)) return cache.get(selector);
    let matcher;
    try {
      matcher = ast.length ? library.compileToken(copy(ast), options(scope)) : () => false;
    } catch (error) {
      throw syntax(error.message || 'Invalid selector');
    }
    if (cache.size === cacheLimit) cache.delete(cache.keys().next().value);
    cache.set(selector, matcher);
    return matcher;
  }
  function run(callback, styleRead = false) {
    const previous = reads;
    // Match all elements against the same immutable DOM reads during one
    // internal geometry observation. Public queries retain independent scopes.
    reads =
      styleRead && styleReadCache
        ? styleReadCache.selectorReads || (styleReadCache.selectorReads = new WeakMap())
        : new WeakMap();
    try {
      return callback();
    } finally {
      reads = previous;
    }
  }
  const scopeFor = (root) => (root === document ? document.documentElement : root);
  function simpleNative(selector) {
    const groups = parsed(selector);
    const leaves = groups.map(simpleNativeGroup);
    return leaves.length && leaves.every(Boolean) ? leaves.join(',') : null;
  }
  function simpleNativeGroup(tokens) {
    // Preserve exactly the legacy native leaf grammar. Other compounds use
    // the same upstream matcher as structural selectors.
    if (
      tokens.length > 2 ||
      (tokens.length === 2 &&
        !(
          tokens[0].type === 'tag' &&
          tokens[1].type === 'attribute' &&
          tokens[1].name === 'class' &&
          tokens[1].action === 'element'
        ))
    )
      return null;
    let source = '';
    const identifier = (value) => /^[a-zA-Z0-9_-]+$/.test(value);
    for (const token of tokens) {
      if (token.type === 'universal' && token.namespace === null) source += '*';
      else if (token.type === 'tag' && token.namespace === null && identifier(token.name))
        source += token.name;
      else if (
        token.type === 'attribute' &&
        token.namespace === null &&
        token.ignoreCase === 'quirks' &&
        identifier(token.value) &&
        ((token.name === 'id' && token.action === 'equals') ||
          (token.name === 'class' && token.action === 'element'))
      )
        source += (token.name === 'id' ? '#' : '.') + token.value;
      else if (
        tokens.length === 1 &&
        token.type === 'attribute' &&
        token.namespace === null &&
        identifier(token.name) &&
        token.action === 'exists'
      )
        source = '[' + token.name + ']';
      else if (
        tokens.length === 1 &&
        token.type === 'attribute' &&
        token.namespace === null &&
        identifier(token.name) &&
        token.action === 'equals' &&
        (token.ignoreCase === false ||
          (token.ignoreCase === null &&
            !library.caseInsensitiveAttributes.has(token.name.toLowerCase()))) &&
        /^[a-zA-Z0-9_.-]*$/.test(token.value)
      )
        source = '[' + token.name + '="' + token.value + '"]';
      else return null;
    }
    return source || null;
  }
  function query(root, selector, first = false, records = false) {
    selector = String(selector);
    const simple = simpleNative(selector),
      slot = elementSlot(root);
    if (simple && (root === document || slot)) {
      const id = root === document ? documentID : slot.nodeId;
      if (first) return wrap(host.queryWithin(id, simple));
      const ids = host.queryAllWithin(id, simple);
      return records ? ids : ids.map(wrap);
    }
    return run(() => {
      const scope = scopeFor(root),
        match = predicate(scope, selector);
      // Select candidates from canonical DOM in one host call. The final
      // compound's leaf is only a necessary condition: the upstream matcher
      // still decides the complete selector, including scope and pseudos.
      // This avoids materializing wrappers for every text node and unrelated
      // element just to traverse a large tree. No DOM results survive a query.
      if (root === document || slot) {
        const leaves = parsed(selector).map((group) => {
          let leaf = null;
          for (let i = group.length - 1; i >= 0; i--) {
            const token = group[i];
            if (
              [
                'descendant',
                'child',
                'adjacent',
                'sibling',
                'parent',
                'column-combinator',
              ].includes(token.type)
            )
              break;
            // The legacy native attribute matcher folds names. Do not narrow
            // a case-sensitive foreign attribute such as SVG viewBox through it.
            if (token.type === 'attribute' && token.name !== token.name.toLowerCase()) continue;
            const candidate = simpleNativeGroup([token]);
            if (candidate && candidate !== '*') {
              leaf = candidate;
              break;
            }
          }
          return leaf;
        });
        if (leaves.length && leaves.every(Boolean)) {
          const ids = host.queryAllWithin(
            root === document ? documentID : slot.nodeId,
            leaves.join(','),
          );
          const result = [];
          for (const id of ids) {
            const node = wrap(id);
            if (match(node)) {
              if (first) return node;
              result.push(records ? id : node);
            }
          }
          return first ? null : result;
        }
      }
      const result = (first ? library.findOne : library.findAll)(
        match,
        children(root),
        options(scope),
      );
      return records ? result.map((node) => elementSlot(node)) : result;
    });
  }
  function matches(node, selector) {
    selector = String(selector);
    const simple = simpleNative(selector);
    if (simple) return host.matches(elementSlot(node).nodeId, simple);
    return run(() => predicate(node, selector)(node));
  }
  // Exact primitive/ancestor contexts permit reuse of attribute-only matches.
  // Weak keys do not retain retired nodes or stylesheet programs. Structural
  // and stateful selectors are always re-evaluated against the current graph.
  let styleContexts = new WeakMap();
  let styleContextGroups = new WeakMap(),
    detachedStyleContextGroups = new Map();
  bootstrapRestoreHooks.push(() => {
    styleContexts = new WeakMap();
    styleContextGroups = new WeakMap();
    detachedStyleContextGroups = new Map();
  });
  const staticStyleSelector = (groups) =>
    groups.every((group) =>
      group.every(
        (token) =>
          ['tag', 'universal', 'attribute', 'descendant', 'child'].includes(token.type) ||
          (token.type === 'pseudo' &&
            ['is', 'where', 'not'].includes(token.name) &&
            Array.isArray(token.data) &&
            staticStyleSelector(token.data)),
      ),
    );
  const styleContext = (node) => {
    const pending = [];
    let current = node,
      context;
    while (current && adapter.isTag(current)) {
      context = reads.get(current)?.get('styleContext');
      if (context) break;
      pending.push(current);
      current = parent(current);
    }
    context = context || current;
    for (let i = pending.length - 1; i >= 0; i--) {
      const element = pending[i],
        signature = cssObservationNodeState(element).selectorSignature,
        prior = styleContexts.get(element);
      // Exact attribute contents and canonical ancestor identities, not a hash,
      // determine reuse. The tree is re-read lazily on every observation epoch.
      let next = prior && prior.parent === context && prior.signature === signature ? prior : null;
      if (!next) {
        let group;
        if (context && (typeof context === 'object' || typeof context === 'function')) {
          group = styleContextGroups.get(context);
          if (!group) styleContextGroups.set(context, (group = new Map()));
        } else group = detachedStyleContextGroups;
        next = group.get(signature);
        if (!next) {
          next = { parent: context, signature, sheets: new WeakMap() };
          group.set(signature, next);
        }
      }
      styleContexts.set(element, next);
      memo(element, 'styleContext', () => next);
      context = next;
    }
    return context;
  };
  function compileStyle(selector) {
    try {
      // A style read matches many rules against the same element. Keep even
      // leaf predicates in the shared read scope so id/class/attribute facts
      // cross the host boundary once, not once for every stylesheet rule.
      const ast = parsed(selector);
      const scoped = (groups) =>
        groups.some((group) =>
          group.some(
            (token) =>
              (token.type === 'pseudo' && token.name === 'scope') ||
              (Array.isArray(token.data) && scoped(token.data)),
          ),
        );
      if (scoped(ast)) return (node) => predicate(node, selector)(node);
      return ast.length ? library.compileToken(copy(ast), options(null)) : () => false;
    } catch (error) {
      // Invalid/unsupported CSS rules are ignored, but DOM selector methods
      // continue to throw SyntaxError through their existing boundary.
      if (error?.name === 'SyntaxError') return () => false;
      throw error;
    }
  }
  // Index immutable rule programs by a necessary leaf in the rightmost
  // compound. Functional/compound selectors still use the complete matcher;
  // unsupported shapes go in the fallback bucket. The index holds programs,
  // while matchingStyles below owns separately validated derived matches.
  const styleIndexes = new WeakMap();
  // A necessary-ancestor bloom filter rejects impossible descendant selectors
  // before the full matcher walks the same ancestry for every rule/element.
  // Collisions only admit extra work; the upstream matcher still decides every
  // match. These summaries live in the canonical observation's read memo, so
  // reparenting, attributes, shadow membership and state changes cannot stale it.
  const addStyleBits = (bits, key) => {
    let hash = 2166136261;
    for (let i = 0; i < key.length; i++) hash = Math.imul(hash ^ key.charCodeAt(i), 16777619);
    for (const bit of [hash & 127, (hash >>> 7) & 127]) bits[bit >>> 5] |= 1 << (bit & 31);
  };
  const tokenStyleKey = (token) => {
    if (token.namespace !== null) return null;
    if (token.type === 'tag') return 't:' + token.name.toLowerCase();
    if (token.type === 'attribute' && token.name === 'id' && token.action === 'equals')
      return '#' + token.value.toLowerCase();
    if (token.type === 'attribute' && token.name === 'class' && token.action === 'element')
      return '.' + token.value.toLowerCase();
    return null;
  };
  const ownStyleBits = (node) =>
    memo(node, 'styleBits', () => {
      const bits = [0, 0, 0, 0];
      addStyleBits(bits, 't:' + adapter.getName(node));
      const id = attribute(node, 'id'),
        classes = attribute(node, 'class');
      if (id !== undefined) addStyleBits(bits, '#' + id.toLowerCase());
      if (classes)
        for (const name of classes.split(/[\t\n\f\r ]+/))
          if (name) addStyleBits(bits, '.' + name.toLowerCase());
      return bits;
    });
  const ancestorStyleBits = (node) => {
    const pending = [];
    let current = node,
      bits;
    while (current && adapter.isTag(current)) {
      bits = reads.get(current)?.get('ancestorStyleBits');
      if (bits) break;
      pending.push(current);
      current = parent(current);
    }
    bits = bits || [0, 0, 0, 0];
    for (let i = pending.length - 1; i >= 0; i--) {
      const element = pending[i],
        p = parent(element),
        own = p && adapter.isTag(p) ? ownStyleBits(p) : [0, 0, 0, 0];
      bits = bits.map((value, j) => value | own[j]);
      memo(element, 'ancestorStyleBits', () => bits);
    }
    return bits;
  };
  const ancestorRequirements = (rule) => {
    const selector = rule.pseudo ? rule.selector.replace(/::?(before|after)$/, '') : rule.selector;
    let groups;
    try {
      groups = library.parse(selector);
    } catch {
      return null;
    }
    if (groups.length !== 1) return null;
    const bits = [0, 0, 0, 0];
    let ancestor = false;
    for (let i = groups[0].length - 1; i >= 0; i--) {
      const token = groups[0][i];
      if (token.type === 'descendant' || token.type === 'child') {
        ancestor = true;
        continue;
      }
      // Do not infer anything about a sibling or relative-selector boundary.
      if (['adjacent', 'sibling', 'parent', 'column-combinator'].includes(token.type)) break;
      if (ancestor) {
        const key = tokenStyleKey(token);
        if (key) addStyleBits(bits, key);
      }
    }
    return bits.some(Boolean) ? bits : null;
  };
  function styleKey(rule) {
    const selector = rule.pseudo ? rule.selector.replace(/::?(before|after)$/, '') : rule.selector;
    let groups;
    try {
      groups = library.parse(selector);
    } catch {
      return '*';
    }
    if (groups.length !== 1) return '*';
    let key = '*';
    for (let i = groups[0].length - 1; i >= 0; i--) {
      const token = groups[0][i];
      if (
        ['descendant', 'child', 'adjacent', 'sibling', 'parent', 'column-combinator'].includes(
          token.type,
        )
      )
        break;
      if (token.type === 'attribute' && token.namespace === null) {
        if (token.name === 'id' && token.action === 'equals')
          return '#' + token.value.toLowerCase();
        if (token.name === 'class' && token.action === 'element')
          key = '.' + token.value.toLowerCase();
        else if (key === '*' || key.startsWith('t:')) key = 'a:' + token.name.toLowerCase();
      } else if (key === '*' && token.type === 'tag' && token.namespace === null)
        key = 't:' + token.name.toLowerCase();
    }
    return key;
  }
  const styleSelectorDependencies = (rule) => {
    let groups;
    try {
      groups = library.parse(
        rule.pseudo ? rule.selector.replace(/::?(before|after)$/, '') : rule.selector,
      );
    } catch {
      return null;
    }
    const attributes = new Set();
    let local = groups.length === 1;
    const visit = (tokens, nested) => {
      for (const token of tokens) {
        if (token.type === 'attribute') attributes.add(token.name.toLowerCase());
        if (Array.isArray(token.data)) for (const group of token.data) visit(group, true);
        if (nested || !['attribute', 'tag', 'universal'].includes(token.type)) local = false;
      }
    };
    for (const group of groups) visit(group, false);
    return { attributes, local };
  };
  const retainedStyleMatchLimit = 32768;
  let retainedStyleMatches = new WeakMap(),
    retainedStyleMatchAdmissions = 0;
  bootstrapRestoreHooks.push(() => {
    retainedStyleMatches = new WeakMap();
    retainedStyleMatchAdmissions = 0;
  });
  const matchingStyles = (node, rules, pseudo = '') =>
    run(() => {
      let retained;
      if (styleReadCache?.retainable) {
        const context = styleContext(node);
        const environment = styleReadCache.environmentVersion;
        retained = context.sheets.get(rules);
        if (!retained || retained.environment !== environment) {
          retained = { environment, pseudos: new Map() };
          context.sheets.set(rules, retained);
        }
        const previous = retained.pseudos.get(pseudo);
        if (previous) {
          const matched = previous.matches.slice();
          for (const rule of previous.dynamic) if (rule.matches(node)) matched.push(rule);
          return matched.sort((a, b) => a.order - b.order);
        }
      }
      let index = styleIndexes.get(rules);
      if (!index) {
        index = new Map();
        index.reusable = true;
        index.local = true;
        index.attributes = new Set();
        for (const rule of rules) {
          const leaf = styleKey(rule),
            key = rule.pseudo + '|' + leaf;
          if (leaf.startsWith('a:')) index.hasAttributeKeys = true;
          let bucket = index.get(key);
          if (!bucket) index.set(key, (bucket = []));
          let stable = false;
          try {
            stable = staticStyleSelector(
              library.parse(
                rule.pseudo ? rule.selector.replace(/::?(before|after)$/, '') : rule.selector,
              ),
            );
          } catch {}
          const dependencies = styleSelectorDependencies(rule);
          if (!dependencies) index.reusable = false;
          else {
            index.local &&= dependencies.local;
            for (const attribute of dependencies.attributes) index.attributes.add(attribute);
          }
          bucket.push({ rule, stable, ancestors: ancestorRequirements(rule) });
        }
        styleIndexes.set(rules, index);
      }
      let retainedAcrossRevision;
      if (index.reusable && styleReadCache?.retainable) {
        let byRules = retainedStyleMatches.get(node);
        retainedAcrossRevision = byRules?.get(rules)?.get(pseudo);
        if (retainedAcrossRevision?.environment === styleReadCache.environmentVersion) {
          const journals =
              styleReadCache.mutationJournals || (styleReadCache.mutationJournals = new Map()),
            revision = retainedAcrossRevision.revision;
          let journal = journals.get(revision);
          if (!journal) {
            journal = JSON.parse(host.observationMutationJournal(revision));
            if (journals.size === 64) journals.delete(journals.keys().next().value);
            journals.set(revision, journal);
          }
          const nodeID = elementSlot(node).nodeId,
            safe =
              !journal.overflow &&
              journal.records.every(
                (mutation) =>
                  mutation.kind === 'attribute' &&
                  (!index.attributes.has(mutation.attribute.toLowerCase()) ||
                    (index.local && mutation.target !== nodeID)),
              );
          if (safe) {
            const matched = retainedAcrossRevision.matches.slice();
            for (const rule of retainedAcrossRevision.dynamic)
              if (rule.matches(node)) matched.push(rule);
            return matched.sort((a, b) => a.order - b.order);
          }
        }
      }
      const keys = new Set(['*', 't:' + adapter.getName(node)]),
        id = attribute(node, 'id'),
        classes = attribute(node, 'class');
      if (id !== undefined) keys.add('#' + id.toLowerCase());
      if (classes)
        for (const name of classes.split(/[\t\n\f\r ]+/))
          if (name) keys.add('.' + name.toLowerCase());
      // Attribute selectors require the attribute to exist. Reading canonical
      // names once avoids testing every data-/aria- rule on unrelated elements.
      // Full matching still decides values, operators, casing and combinators.
      if (index.hasAttributeKeys)
        for (const name of attributeNames(node)) keys.add('a:' + name.toLowerCase());
      const matched = [],
        staticMatches = [],
        dynamic = [];
      let available;
      for (const key of keys)
        for (const { rule, ancestors, stable } of index.get(pseudo + '|' + key) || []) {
          if (ancestors) {
            available = available || ancestorStyleBits(node);
            if (ancestors.some((required, i) => (available[i] & required) !== required)) continue;
          }
          if (!stable) dynamic.push(rule);
          if (rule.matches(node)) {
            matched.push(rule);
            if (stable) staticMatches.push(rule);
          }
        }
      // Only attribute/ancestor selectors can reuse a result. Dynamic candidates
      // are retained as a program, never as a truth value, and are always matched
      // anew. Exact context and environment changes replace the entire record.
      if (retained) retained.pseudos.set(pseudo, { matches: staticMatches, dynamic });
      const sorted = matched.sort((a, b) => a.order - b.order);
      if (index.reusable && styleReadCache?.retainable) {
        let byRules = retainedStyleMatches.get(node);
        let byPseudo = byRules?.get(rules);
        const admitted = byPseudo?.has(pseudo);
        if (admitted || retainedStyleMatchAdmissions < retainedStyleMatchLimit) {
          if (!byRules) retainedStyleMatches.set(node, (byRules = new WeakMap()));
          if (!byPseudo) byRules.set(rules, (byPseudo = new Map()));
          if (!admitted) retainedStyleMatchAdmissions++;
          byPseudo.set(pseudo, {
            revision: host.observationRevision(),
            environment: styleReadCache.environmentVersion,
            matches: staticMatches,
            dynamic,
          });
        }
      }
      return sorted;
    }, true);
  function closest(node, selector) {
    selector = String(selector);
    return run(() => {
      const match = predicate(node, selector);
      for (let current = node; current; current = parent(current))
        if (adapter.isTag(current) && match(current)) return current;
      return null;
    });
  }
  function getElementById(root, id) {
    id = String(id);
    if (!id) return null;
    const slot = elementSlot(root);
    if (root === document || slot) {
      const found = host.elementByID(root === document ? documentID : slot.nodeId, id);
      return found ? wrap(found) : null;
    }
    return run(() =>
      library.findOne(
        (node) => attribute(node, 'id') === id,
        children(root),
        options(scopeFor(root)),
      ),
    );
  }
  function supports(selector) {
    try {
      const groups = library.parse(selector);
      if (groups.length !== 1 || !groups[0].length) return false;
      validate(groups, false, false, false, true);
      return true;
    } catch {
      return false;
    }
  }
  return { query, matches, closest, getElementById, compileStyle, matchingStyles, supports };
})();
// These late semantic replacements are WebIDL operations too. Validate the
// private brand before arity and conversion, outside selector-parser error
// handling, so conversion exceptions keep their identity and caller realm.
const installSelectorOperation = (prototype, name, operation) => {
  const fn = {
    [name](selector) {
      const slot = elementSlot(this);
      const valid =
        prototype === Document.prototype
          ? this === document || slot?.type === 'document'
          : slot?.type === 'element';
      if (!valid) throw new TypeError('Illegal invocation');
      if (arguments.length === 0) throw new TypeError('Not enough arguments');
      if (typeof selector === 'symbol')
        throw new TypeError('Cannot convert a Symbol value to a string');
      return operation(this, String(selector));
    },
  }[name];
  markNative(fn, name);
  Object.defineProperty(prototype, name, {
    value: fn,
    writable: true,
    enumerable: true,
    configurable: true,
  });
};
for (const prototype of [Document.prototype, Element.prototype]) {
  installSelectorOperation(prototype, 'querySelector', (receiver, selector) =>
    compatibilitySelectors.query(receiver, selector, true),
  );
  installSelectorOperation(prototype, 'querySelectorAll', (receiver, selector) =>
    nodeList(compatibilitySelectors.query(receiver, selector, false, true)),
  );
}
for (const name of ['matches', 'webkitMatchesSelector', 'closest'])
  installSelectorOperation(Element.prototype, name, (receiver, selector) =>
    compatibilitySelectors[name === 'webkitMatchesSelector' ? 'matches' : name](receiver, selector),
  );
installSelectorOperation(Document.prototype, 'getElementById', (receiver, id) =>
  compatibilitySelectors.getElementById(receiver, id),
);
// Synthetic DocumentFragments still lack a cross-realm private-brand bridge.
// Keep their existing bindings until borrowed calls can be validated without
// rejecting genuine foreign fragments or accepting prototype forgeries.
Object.defineProperties(DocumentFragment.prototype, {
  querySelector: {
    value: function (selector) {
      return compatibilitySelectors.query(this, selector, true);
    },
    writable: true,
    configurable: true,
  },
  querySelectorAll: {
    value: function (selector) {
      return nodeList(compatibilitySelectors.query(this, selector, false, true));
    },
    writable: true,
    configurable: true,
  },
  getElementById: {
    value: function (id) {
      return compatibilitySelectors.getElementById(this, id);
    },
    writable: true,
    configurable: true,
  },
});

// CDP calls the same closed-over engine instead of evaluating page-controlled
// querySelector properties or maintaining an independent native CSS grammar.
registerBootstrapCallback('setDOMQueryCallback', (nodeID, selector, all) => {
  try {
    const root = nodeID === host.documentRootID() ? document : wrap(nodeID);
    const result = compatibilitySelectors.query(root, selector, !all);
    return JSON.stringify({
      ids: (all ? result : result ? [result] : []).map((node) => elementSlot(node).nodeId),
    });
  } catch (error) {
    return JSON.stringify({ error: String(error.name) + ': ' + String(error.message) });
  }
});
