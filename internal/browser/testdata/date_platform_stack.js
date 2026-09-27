(() => {
  Error.prepareStackTrace = (_, frames) =>
    frames.map((f) => ({ name: f.getFunctionName(), file: f.getFileName(), eval: f.isEval() }));
  function authorPrimitive() {
    return new Error().stack;
  }
  let captured;
  const argument = {
    [Symbol.toPrimitive]: function authorCoercion() {
      captured = authorPrimitive();
      return 0;
    },
  };
  function authorDate() {
    new Date(argument);
    return captured;
  }
  const frames = authorDate();
  const expected = ['authorPrimitive', 'authorCoercion', 'Date', 'authorDate', null, null];
  if (frames.length !== expected.length) throw new Error(JSON.stringify(frames));
  for (let index = 0; index < frames.length; index++) {
    const frame = frames[index];
    if (frame.name !== expected[index] || frame.eval || frame.file !== (index === 2 ? null : '')) {
      throw new Error(JSON.stringify(frames));
    }
  }
  return 'ok';
})();
