// Import pinned matcher primitives, avoiding domutils' unrelated DOM model.
export { parse } from './selector-parser.js';
import { compileToken as compileNative } from './node_modules/css-select/dist/esm/compile.js';
import nthCheck from 'nth-check';

// Filter siblings through the same matcher and canonical DOM adapter before
// applying the upstream an+b predicate. No sibling state survives a query.
export function compileToken(groups, options, context) {
  const predicates = [];
  const transform = (groups) =>
    groups.map((group) =>
      group.map((token) => {
        if (token.type === 'pseudo' && ['host', 'host-context'].includes(token.name)) {
          const match = Array.isArray(token.data)
            ? compileToken(token.data, options, context)
            : () => true;
          const index = predicates.length;
          predicates.push((element) => {
            if (!options.shadowHost || element !== options.shadowHost) return false;
            if (token.name === 'host') return match(element);
            for (let current = element; current; current = options.hostParent(current))
              if (match(current)) return true;
            return false;
          });
          return { type: 'pseudo', name: 'mimic-internal-filtered-nth', data: String(index) };
        }
        if (token.nthOf) {
          const match = compileToken(token.nthOf, options, context);
          const check = nthCheck(token.data);
          const reverse = token.name === 'nth-last-child';
          const index = predicates.length;
          predicates.push((element) => {
            if (!match(element)) return false;
            const siblings = options.adapter
              .getSiblings(element)
              .filter((node) => options.adapter.isTag(node) && match(node));
            const position = siblings.indexOf(element);
            return position >= 0 && check(reverse ? siblings.length - position - 1 : position);
          });
          return { type: 'pseudo', name: 'mimic-internal-filtered-nth', data: String(index) };
        }
        return { ...token, ...(Array.isArray(token.data) ? { data: transform(token.data) } : {}) };
      }),
    );
  const tokens = transform(groups);
  return compileNative(
    tokens,
    {
      ...options,
      pseudos: {
        ...options.pseudos,
        'mimic-internal-filtered-nth': (element, index) => predicates[Number(index)](element),
      },
    },
    context,
  );
}
export { findAll, findOne } from './node_modules/css-select/dist/esm/helpers/querying.js';

export { caseInsensitiveAttributes } from './node_modules/css-select/dist/esm/attributes.js';
// Share the pinned tokenizer/AST implementation with constructed stylesheets.
export { default as parseStylesheet } from 'css-tree/parser';
export { default as generateCSS } from 'css-tree/generator';
import { Lexer } from 'css-tree/lexer';
import definitions from 'css-tree/dist/data';
const valueLexer = new Lexer({ generic: true, types: definitions.types });
export const matchesValueSyntax = (syntax, value) => {
  try {
    return valueLexer.match(syntax, value).matched !== null;
  } catch {
    return false;
  }
};
