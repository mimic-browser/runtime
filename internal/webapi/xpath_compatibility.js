// XPath is consumed directly by browser automation clients, notably by
// waitForXPath and element.$x. Results reuse the canonical DOM wrappers.
// This remains a bounded descendant/element query implementation, now shared
// by Document, XPathEvaluator and compiled XPathExpression. Namespace resolution,
// other axes, scalar results, result-object reuse and mutation-invalidated
// iterators are not implemented here; this is not a full XPath 1.0 engine.
(() => {
  const constants = {
    ANY_TYPE: 0,
    NUMBER_TYPE: 1,
    STRING_TYPE: 2,
    BOOLEAN_TYPE: 3,
    UNORDERED_NODE_ITERATOR_TYPE: 4,
    ORDERED_NODE_ITERATOR_TYPE: 5,
    UNORDERED_NODE_SNAPSHOT_TYPE: 6,
    ORDERED_NODE_SNAPSHOT_TYPE: 7,
    ANY_UNORDERED_NODE_TYPE: 8,
    FIRST_ORDERED_NODE_TYPE: 9,
  };
  if (globalThis.XPathResult)
    for (const [name, value] of Object.entries(constants))
      for (const owner of [XPathResult, XPathResult.prototype])
        if (owner[name] === undefined)
          Object.defineProperty(owner, name, { value, enumerable: true });
  const syntax = () => {
    throw platformDOMException('The string is not a valid XPath expression.', 'SyntaxError');
  };
  const quoted = (value) => {
    value = value.trim();
    if (value.length < 2 || value[0] !== value.at(-1) || !['\"', "'"].includes(value[0])) syntax();
    return value.slice(1, -1);
  };
  const descendants = (root) =>
    root instanceof Document
      ? Array.from(root.querySelectorAll('*'))
      : root instanceof Element || root instanceof DocumentFragment
        ? Array.from(root.querySelectorAll('*'))
        : [];
  const normalize = (value) => String(value).trim().replace(/\s+/g, ' ');
  // Split only outside strings, predicates and function arguments. Attribute
  // node-set predicates use the same grammar as element predicates; no query
  // text or attribute spelling belongs to an individual site's implementation.
  const split = (source, separator) => {
    const parts = [];
    let start = 0,
      quote = '',
      brackets = 0,
      parentheses = 0;
    for (let i = 0; i < source.length; i++) {
      const c = source[i];
      if (quote) {
        if (c === quote) quote = '';
        continue;
      }
      if (c === '"' || c === "'") quote = c;
      else if (c === '[') brackets++;
      else if (c === ']') brackets--;
      else if (c === '(') parentheses++;
      else if (c === ')') parentheses--;
      else if (!brackets && !parentheses && source.startsWith(separator, i)) {
        parts.push(source.slice(start, i).trim());
        i += separator.length - 1;
        start = i + 1;
      }
      if (brackets < 0 || parentheses < 0) syntax();
    }
    if (quote || brackets || parentheses) syntax();
    parts.push(source.slice(start).trim());
    return parts;
  };
  const stringValue = (source) => {
    source = source.trim();
    if (source === 'name()') return (node) => node.nodeName;
    if (source === '.' || source === 'text()') return (node) => node.textContent || '';
    if (/^normalize-space\((?:\.|text\(\))\)$/.test(source))
      return (node) => normalize(node.textContent || '');
    const attribute = source.match(/^@([\w:-]+)$/);
    if (attribute) return (node) => node.getAttribute(attribute[1]) || '';
    const value = quoted(source);
    return () => value;
  };
  const predicate = (source) => {
    source = source.trim();
    for (const [operator, every] of [
      [' or ', false],
      [' and ', true],
    ]) {
      const parts = split(source, operator);
      if (parts.length > 1) {
        const tests = parts.map(predicate);
        return (node, index, length) =>
          every
            ? tests.every((test) => test(node, index, length))
            : tests.some((test) => test(node, index, length));
      }
    }
    const attributes = source.match(/^@\*\[([\s\S]*)\]$/);
    if (attributes) {
      const test = predicate(attributes[1]);
      return (node) => {
        const values = Array.from(node.attributes || []);
        return values.some((value, index) => test(value, index, values.length));
      };
    }
    if (source === '@*') return (node) => (node.attributes?.length || 0) > 0;
    if (/^\d+$/.test(source)) return (node, index) => index + 1 === Number(source);
    let m = source.match(/^position\(\)\s*=\s*(\d+)$/);
    if (m) return (node, index) => index + 1 === Number(m[1]);
    if (source === 'last()') return (node, index, length) => index + 1 === length;
    m = source.match(/^@([\w:-]+)$/);
    if (m) return (node) => node.hasAttribute(m[1]);
    m = source.match(/^@([\w:-]+)\s*=\s*(.+)$/);
    if (m) {
      const value = quoted(m[2]);
      return (node) => node.getAttribute(m[1]) === value;
    }
    m = source.match(/^(contains|starts-with)\(([\s\S]*)\)$/);
    if (m) {
      const args = split(m[2], ',');
      if (args.length !== 2) syntax();
      const left = stringValue(args[0]),
        right = stringValue(args[1]),
        prefix = m[1] === 'starts-with';
      return (node) =>
        prefix ? left(node).startsWith(right(node)) : left(node).includes(right(node));
    }
    m = source.match(/^((?:\.|text\(\)|normalize-space\((?:\.|text\(\))\)))\s*=\s*(.+)$/);
    if (m) {
      const read = stringValue(m[1]),
        value = quoted(m[2]);
      return (node) => read(node) === value;
    }
    syntax();
  };
  const compile = (expression) => {
    expression = String(expression).trim();
    const match = expression.match(/^(\.\/\/|\/\/)(\*|[A-Za-z_][\w:.-]*)(.*)$/);
    if (!match) syntax();
    const name = match[2].toLowerCase();
    let rest = match[3];
    const predicates = [];
    while (rest.trim()) {
      rest = rest.trimStart();
      if (rest[0] !== '[') syntax();
      let depth = 1,
        quote = '',
        end = 1;
      for (; end < rest.length && depth; end++) {
        const c = rest[end];
        if (quote) {
          if (c === quote) quote = '';
        } else if (c === '"' || c === "'") quote = c;
        else if (c === '[') depth++;
        else if (c === ']') depth--;
      }
      if (depth || quote) syntax();
      predicates.push(predicate(rest.slice(1, end - 1)));
      rest = rest.slice(end);
    }
    return (context) => {
      if (
        !(
          context instanceof Document ||
          context instanceof Element ||
          context instanceof DocumentFragment
        )
      )
        throw platformDOMException('The node provided is not supported.', 'NotSupportedError');
      let nodes = descendants(context).filter((node) => name === '*' || node.localName === name);
      for (const test of predicates) {
        const current = nodes;
        nodes = current.filter((node, index) => test(node, index, current.length));
      }
      return nodes;
    };
  };
  const result = (nodes, type) => {
    type = Number(type) >>> 0;
    if (type === 0) type = 4;
    if (![4, 5, 6, 7, 8, 9].includes(type))
      throw platformDOMException(
        'The result type is not supported for this expression.',
        'TypeError',
      );
    let cursor = 0;
    const value = Object.create(globalThis.XPathResult?.prototype || Object.prototype);
    Object.defineProperties(value, {
      resultType: { get: () => type },
      invalidIteratorState: { get: () => false },
      snapshotLength: { get: () => (type === 6 || type === 7 ? nodes.length : 0) },
      singleNodeValue: { get: () => (type === 8 || type === 9 ? nodes[0] || null : null) },
      iterateNext: { value: () => (type === 4 || type === 5 ? nodes[cursor++] || null : null) },
      snapshotItem: {
        value: (index) => (type === 6 || type === 7 ? nodes[Number(index)] || null : null),
      },
    });
    return value;
  };
  const evaluate = function (expression, contextNode, resolver, type = 0) {
    if (arguments.length < 2)
      throw new TypeError("Failed to execute 'evaluate': 2 arguments required");
    return result(compile(expression)(contextNode), type);
  };
  Object.defineProperty(Document.prototype, 'evaluate', {
    value: evaluate,
    writable: true,
    enumerable: true,
    configurable: true,
  });
  const evaluatorPrototype = globalThis.XPathEvaluator?.prototype,
    expressionPrototype = globalThis.XPathExpression?.prototype;
  if (!evaluatorPrototype || !expressionPrototype) return;
  const evaluators = new WeakSet(),
    expressions = new WeakMap();
  function XPathEvaluator() {
    if (!new.target)
      throw new TypeError(
        "Failed to construct 'XPathEvaluator': Please use the 'new' operator, this DOM object constructor cannot be called as a function.",
      );
    evaluators.add(this);
  }
  XPathEvaluator.prototype = evaluatorPrototype;
  function XPathExpression() {
    throw new TypeError("Failed to construct 'XPathExpression': Illegal constructor");
  }
  XPathExpression.prototype = expressionPrototype;
  const member = (prototype, name, value) =>
    Object.defineProperty(prototype, name, {
      value,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  for (const [name, constructor, prototype] of [
    ['XPathEvaluator', XPathEvaluator, evaluatorPrototype],
    ['XPathExpression', XPathExpression, expressionPrototype],
  ]) {
    // Bound callables avoid exposing authored-function caller/arguments slots.
    // Their construction still uses the implementation's canonical prototype,
    // including a subclass's newTarget, while reflection keeps the captured
    // native interface object's prototype descriptor.
    const exposed = constructor.bind(null);
    Object.defineProperty(exposed, 'name', { value: name, configurable: true });
    Object.defineProperty(exposed, 'prototype', {
      ...Object.getOwnPropertyDescriptor(globalThis[name], 'prototype'),
      value: prototype,
    });
    Object.defineProperty(globalThis, name, {
      ...Object.getOwnPropertyDescriptor(globalThis, name),
      value: exposed,
    });
    Object.defineProperty(prototype, 'constructor', {
      ...Object.getOwnPropertyDescriptor(prototype, 'constructor'),
      value: exposed,
    });
  }
  const createExpression = (expression, owner) => {
    let select;
    try {
      select = compile(expression);
    } catch (error) {
      if (error.name !== 'SyntaxError') throw error;
      throw platformDOMException(
        `Failed to execute 'createExpression' on '${owner}': The string '${expression}' is not a valid XPath expression.`,
        'SyntaxError',
      );
    }
    const compiled = Object.create(expressionPrototype);
    expressions.set(compiled, select);
    return compiled;
  };
  member(evaluatorPrototype, 'createExpression', function createExpressionBinding(expression) {
    if (!evaluators.has(this)) throw new TypeError('Illegal invocation');
    if (!arguments.length)
      throw new TypeError(
        "Failed to execute 'createExpression' on 'XPathEvaluator': 1 argument required, but only 0 present.",
      );
    return createExpression(bindingString(expression), 'XPathEvaluator');
  });
  member(evaluatorPrototype, 'evaluate', function evaluateBinding(expression, contextNode) {
    if (!evaluators.has(this)) throw new TypeError('Illegal invocation');
    return functionSourceApply(evaluate, this, arguments);
  });
  member(Document.prototype, 'createExpression', function createExpressionBinding(expression) {
    if (!(this instanceof Document)) throw new TypeError('Illegal invocation');
    if (!arguments.length)
      throw new TypeError(
        "Failed to execute 'createExpression' on 'Document': 1 argument required, but only 0 present.",
      );
    return createExpression(bindingString(expression), 'Document');
  });
  member(expressionPrototype, 'evaluate', function evaluateBinding(contextNode, type = 0) {
    const select = expressions.get(this);
    if (!select) throw new TypeError('Illegal invocation');
    if (!arguments.length)
      throw new TypeError(
        "Failed to execute 'evaluate' on 'XPathExpression': 1 argument required, but only 0 present.",
      );
    return result(select(contextNode), type);
  });
})();
