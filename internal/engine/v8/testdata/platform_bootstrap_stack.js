(() => {
  const original = new Error('author failure');
  const savedPrepare = Error.prepareStackTrace;
  try {
    Error.prepareStackTrace = (error, frames) => frames;
    let platformError;
    try {
      platformEntry();
    } catch (error) {
      platformError = error;
    }
    const frames = platformError.stack;
    if (
      platformError.message !== 'platform failure' ||
      frames.length !== 2 ||
      frames.some((frame) => frame.getFileName() !== 'author-probe.js') ||
      frames.some((frame) => frame.getFunctionName() === 'platformEntry')
    ) {
      throw new Error('platform frames entered the structured stack');
    }
    let authorError;
    try {
      platformEntry(function authorCallback() {
        throw original;
      });
    } catch (error) {
      authorError = error;
    }
    if (authorError !== original || original.message !== 'author failure') {
      throw new Error('platform entry replaced the author exception');
    }
    let callbackError;
    try {
      platformEntry(function authorCallback() {
        throw new Error('callback failure');
      });
    } catch (error) {
      callbackError = error;
    }
    const callbackFrames = callbackError.stack;
    if (
      callbackFrames.length !== 3 ||
      callbackFrames[0].getFunctionName() !== 'authorCallback' ||
      callbackFrames.some((frame) => frame.getFileName() !== 'author-probe.js')
    ) {
      throw new Error('author callback frames were changed');
    }
    return 'ok';
  } finally {
    Error.prepareStackTrace = savedPrepare;
  }
})();
