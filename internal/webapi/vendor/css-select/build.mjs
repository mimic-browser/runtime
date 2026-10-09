import { build } from 'esbuild';
import { writeFileSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import valueDefinitions from 'css-tree/dist/data';
const result = await build({
  metafile: true,
  entryPoints: ['entry.js'],
  bundle: true,
  format: 'iife',
  globalName: 'mimicSelectorLibrary',
  minify: true,
  legalComments: 'none',
  outfile: '../../selectors_vendor.js',
  plugins: [
    {
      name: 'export-attribute-policy',
      setup(build) {
        build.onLoad({ filter: /css-select[\\/]dist[\\/]esm[\\/]attributes\.js$/ }, (args) => ({
          contents: readFileSync(args.path, 'utf8') + '\nexport {caseInsensitiveAttributes};',
          loader: 'js',
        }));
      },
    },
    {
      name: 'selector-parser-only',
      setup(build) {
        build.onResolve({ filter: /^css-tree$/ }, () => ({ path: resolve('css-tree-small.js') }));
      },
    },
    {
      name: 'value-types-only',
      setup(build) {
        // The value lexer consumes only types. Project the pinned data at build
        // time rather than cloning unused property and at-rule databases per Page.
        build.onLoad({ filter: /css-tree[\\/]dist[\\/]data\.js$/ }, () => ({
          contents: 'export default ' + JSON.stringify({ types: valueDefinitions.types }) + ';',
          loader: 'js',
        }));
      },
    },
  ],
});

writeFileSync(
  'bundle-manifest.json',
  JSON.stringify(
    {
      inputs: Object.fromEntries(
        Object.entries(result.metafile.outputs['../../selectors_vendor.js'].inputs)
          .filter(([, v]) => v.bytesInOutput > 0)
          .map(([k, v]) => [k, v.bytesInOutput]),
      ),
    },
    null,
    2,
  ) + '\n',
);
