import { value, dependencyURL } from './dependency.js';
import { cycleValue } from './cycle.js';
const [first, second] = await Promise.all([import('./dynamic.js'), import('./dynamic.js')]);
let importScriptsError;
try {
  importScripts();
} catch (error) {
  importScriptsError = error instanceof TypeError;
}
postMessage({
  value,
  cycleValue,
  dynamic: first.value,
  sameNamespace: first === second,
  evaluations: globalThis.dynamicEvaluations,
  dependencyURL: dependencyURL.endsWith('/module/dependency.js'),
  entryURL: import.meta.url.endsWith('/module/entry.js'),
  resolve: import.meta.resolve('./dependency.js') === dependencyURL,
  topThis: this === undefined,
  lexicalGlobal: Object.hasOwn(globalThis, 'value'),
  importScriptsError,
});
