(() => {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  try {
    frame.contentWindow.eval(`
      Error.prepareStackTrace = (error, frames) => frames.map(frame => ({
        name: frame.getFunctionName(), file: frame.getFileName()
      }));
      Object.defineProperty(globalThis, 'authorGetter', {
        get: function childGetter() { return new Error().stack; }
      });
      globalThis.authorMethod = function childMethod() { return new Error().stack; };
    `);
    const cases = {
      childGetter: frame.contentWindow.authorGetter,
      childMethod: frame.contentWindow.authorMethod(),
    };
    for (const [name, frames] of Object.entries(cases)) {
      if (frames[0]?.name !== name || frames.some((frame) => /mimic:/.test(frame.file || ''))) {
        throw new Error('frame platform provenance: ' + JSON.stringify(frames));
      }
    }
    return 'ok';
  } finally {
    frame.remove();
  }
})();
