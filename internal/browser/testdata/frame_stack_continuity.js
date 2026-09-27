(() => {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  try {
    frame.contentWindow.eval(`
      Error.prepareStackTrace = (error, frames) => frames.map(frame => ({
        name: frame.getFunctionName(),
        file: frame.getFileName(),
        eval: frame.isEval(),
      }));
      Object.defineProperty(globalThis, 'authorGetter', {
        configurable: true,
        get: function childGetter() { return new Error().stack; },
      });
      globalThis.authorMethod = function childMethod() { return new Error().stack; };
    `);
    function parentRead() {
      return frame.contentWindow.authorGetter;
    }
    function parentCall() {
      return frame.contentWindow.authorMethod();
    }
    const cases = {
      getter: [parentRead(), ['childGetter', 'parentRead', null, null]],
      method: [parentCall(), ['childMethod', 'parentCall', null, null]],
    };
    for (const [name, [frames, expected]] of Object.entries(cases)) {
      if (
        JSON.stringify(frames.map((frame) => frame.name)) !== JSON.stringify(expected) ||
        !frames[0].eval ||
        frames.slice(1).some((frame) => frame.eval || /mimic:/.test(frame.file || ''))
      ) {
        throw new Error(name + ': connected author stack differs: ' + JSON.stringify(frames));
      }
    }
    return 'ok';
  } finally {
    frame.remove();
  }
})();
