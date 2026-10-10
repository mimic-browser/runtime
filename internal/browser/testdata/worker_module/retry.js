const result = [];
for (let index = 0; index < 2; index++) {
  try {
    await import('./retry-missing.js');
  } catch (error) {
    result.push(error instanceof TypeError);
  }
}
postMessage(result);
