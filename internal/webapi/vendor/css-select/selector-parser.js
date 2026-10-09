// AST translation only. Tokenization, CSS input preprocessing, escapes, string
// decoding and an+b syntax are owned by the pinned upstream implementations.
import { parseSelector } from './node_modules/@asamuzakjp/dom-selector/src/js/parser.js';
import { ident, generate } from 'css-tree';
const combinators = {
  ' ': 'descendant',
  '>': 'child',
  '+': 'adjacent',
  '~': 'sibling',
  '||': 'column-combinator',
};
const actions = {
  '=': 'equals',
  '~=': 'element',
  '|=': 'hyphen',
  '^=': 'start',
  '$=': 'end',
  '*=': 'any',
};
const decode = (value) => ident.decode(value);
function qualified(value) {
  const index = value.indexOf('|');
  return index < 0
    ? { name: decode(value), namespace: null }
    : { name: decode(value.slice(index + 1)), namespace: decode(value.slice(0, index)) };
}
function token(node) {
  switch (node.type) {
    case 'TypeSelector': {
      const q = qualified(node.name);
      return { type: q.name === '*' ? 'universal' : 'tag', ...q };
    }
    case 'IdSelector':
      return {
        type: 'attribute',
        name: 'id',
        action: 'equals',
        value: decode(node.name),
        ignoreCase: 'quirks',
        namespace: null,
      };
    case 'ClassSelector':
      return {
        type: 'attribute',
        name: 'class',
        action: 'element',
        value: decode(node.name),
        ignoreCase: 'quirks',
        namespace: null,
      };
    case 'Combinator': {
      const type = combinators[node.name];
      if (!type) throw new Error('Unsupported combinator');
      return { type };
    }
    case 'AttributeSelector': {
      if (node.flags !== null && !['i', 's', 'I', 'S'].includes(node.flags))
        throw new Error('Invalid attribute modifier');
      return {
        type: 'attribute',
        ...qualified(node.name.name),
        action: node.matcher ? actions[node.matcher] : 'exists',
        value: node.value
          ? node.value.type === 'String'
            ? node.value.value
            : decode(node.value.name)
          : '',
        ignoreCase: node.flags ? node.flags.toLowerCase() === 'i' : null,
      };
    }
    case 'PseudoClassSelector':
    case 'PseudoElementSelector': {
      const children = node.children?.toArray();
      if (children?.length === 1 && children[0].type === 'Nth' && children[0].selector) {
        if (!['nth-child', 'nth-last-child'].includes(node.name.toLowerCase()))
          throw new Error('Invalid filtered nth selector');
        return {
          type: 'pseudo',
          name: node.name.toLowerCase(),
          data: generate(children[0].nth),
          nthOf: groups(children[0].selector),
        };
      }
      let data = null;
      if (children?.length === 1 && children[0].type === 'SelectorList') data = groups(children[0]);
      else if (children) data = children.map(generate).join('');
      return {
        type: node.type === 'PseudoClassSelector' ? 'pseudo' : 'pseudo-element',
        name: decode(node.name).toLowerCase(),
        data,
      };
    }
    default:
      throw new Error('Unsupported selector AST node: ' + node.type);
  }
}
function groups(list) {
  return list.children.toArray().map((selector) => selector.children.toArray().map(token));
}
export const parse = (selector) => groups(parseSelector(selector));
