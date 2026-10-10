import { value } from './dependency.js';
globalThis.sharedEvaluations = (globalThis.sharedEvaluations || 0) + 1;
export const ordinal = globalThis.sharedEvaluations;
export const url = import.meta.url;
export { value };
