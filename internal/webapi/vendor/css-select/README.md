# Pinned selector engine

`npm ci --ignore-scripts`, then `npm run build` in this directory reproduces
`../../selectors_vendor.js`. Go consumes the committed bundle without Node/npm.
`package-lock.json` records dependency versions and integrity hashes;
`bundle-manifest.json` records the upstream modules actually included.

The public selector grammar uses CSS-tree 3.2.1, with DOMSelector 9.1.1's selector
input preprocessing/parser entry. `selector-parser.js` only translates its AST to
css-select 6.0.0 tokens. Matching and an+b evaluation remain css-select/nth-check.
css-what is retained for css-select's own built-in alias expressions; it does not
parse user selectors. `css-tree-small.js` selects upstream parser/generator modules
without bundling CSS declaration grammar/MDN property data. The build does not patch
upstream algorithms. Third-party copyright and license notices are in
`THIRD_PARTY_LICENSES.txt`.

The bundle also exports CSS-tree's full stylesheet parser and serializer for
constructed CSSStyleSheet rules. It shares tokenizer/AST modules with selectors;
the CSS property validation database is still excluded.

Registered custom properties use the pinned CSS-tree value lexer. The build
projects only its type definitions from the upstream data module; unused
property and at-rule tables are excluded. Each realm creates its value lexer
on the first validation request, while ordinary selector and stylesheet parsing
remain available without constructing that validator.

The runtime adapter reads canonical Mimic node wrappers and host state. It retains
no second DOM. A synchronous query owns a temporary lazy memo of primitive reads;
AST and predicate caches are bounded and realm-local, with weak scope keys.

The build makes one upstream private data constant (`caseInsensitiveAttributes`)
exportable without changing its contents or any matching algorithm. The runtime
uses that exact pinned policy to select native exact-attribute lookup only when
case folding is unnecessary; other attributes remain in the upstream matcher.
