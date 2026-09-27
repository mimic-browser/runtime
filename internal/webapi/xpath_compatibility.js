// XPath is consumed directly by browser automation clients, notably by
// waitForXPath and element.$x. Results reuse the canonical DOM wrappers.
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
  const predicate = (source, node, index, length) => {
    source = source.trim();
    if (/^\d+$/.test(source)) return index + 1 === Number(source);
    let m = source.match(/^position\(\)\s*=\s*(\d+)$/);
    if (m) return index + 1 === Number(m[1]);
    if (source === 'last()') return index + 1 === length;
    m = source.match(/^@([\w:-]+)$/);
    if (m) return node.hasAttribute(m[1]);
    m = source.match(/^@([\w:-]+)\s*=\s*(.+)$/);
    if (m) return node.getAttribute(m[1]) === quoted(m[2]);
    m = source.match(/^contains\(\s*@([\w:-]+)\s*,\s*(.+)\)$/);
    if (m) return String(node.getAttribute(m[1]) || '').includes(quoted(m[2]));
    m = source.match(
      /^contains\(\s*(?:\.|text\(\)|normalize-space\((?:\.|text\(\))\))\s*,\s*(.+)\)$/,
    );
    if (m) return String(node.textContent || '').includes(quoted(m[1]));
    m = source.match(/^(?:\.|text\(\))\s*=\s*(.+)$/);
    if (m) return String(node.textContent || '') === quoted(m[1]);
    m = source.match(/^normalize-space\((?:\.|text\(\))\)\s*=\s*(.+)$/);
    if (m) return normalize(node.textContent || '') === quoted(m[1]);
    syntax();
  };
  const select = (expression, context) => {
    expression = String(expression).trim();
    const match = expression.match(/^(\.\/\/|\/\/)(\*|[A-Za-z_][\w:.-]*)(.*)$/);
    if (!match) syntax();
    if (
      !(
        context instanceof Document ||
        context instanceof Element ||
        context instanceof DocumentFragment
      )
    )
      throw platformDOMException('The node provided is not supported.', 'NotSupportedError');
    const name = match[2].toLowerCase();
    let nodes = descendants(context).filter((node) => name === '*' || node.localName === name),
      rest = match[3];
    const predicates = [];
    while (rest) {
      const part = rest.match(/^\s*\[((?:[^'\"]|'[^']*'|\"[^\"]*\")*)\](.*)$/s);
      if (!part) syntax();
      predicates.push(part[1]);
      rest = part[2];
    }
    for (const test of predicates) {
      const current = nodes;
      nodes = current.filter((node, index) => predicate(test, node, index, current.length));
    }
    return nodes;
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
    return result(select(expression, contextNode), type);
  };
  Object.defineProperty(Document.prototype, 'evaluate', {
    value: evaluate,
    writable: true,
    enumerable: true,
    configurable: true,
  });
  if (globalThis.XPathEvaluator?.prototype)
    Object.defineProperty(XPathEvaluator.prototype, 'evaluate', {
      value: evaluate,
      writable: true,
      enumerable: true,
      configurable: true,
    });
})();
