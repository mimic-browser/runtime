globalThis.dynamicEvaluations = (globalThis.dynamicEvaluations || 0) + 1;
await new Promise((resolve) => setTimeout(resolve, 5));
export const value = 9;
