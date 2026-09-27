// Oracle expression embedded after await.
// prettier-ignore
(async () => {
  const result = {};
  const attempt = (name, action) => {
    try {
      const value = action();
      result[name] = value === undefined ? { success: true } : value;
    } catch (error) {
      result[name] = { error: error.name, message: error.message };
    }
  };
  let source;
  attempt('constructor', () => {
    source = new MediaSource();
    return {
      tag: Object.prototype.toString.call(source),
      eventTarget: source instanceof EventTarget,
      readyState: source.readyState,
      duration: String(source.duration),
      sameSourceBuffers: source.sourceBuffers === source.sourceBuffers,
      sameActiveSourceBuffers: source.activeSourceBuffers === source.activeSourceBuffers,
      differentLists: source.sourceBuffers !== source.activeSourceBuffers,
      listTag: Object.prototype.toString.call(source.sourceBuffers),
      length: source.sourceBuffers.length,
      activeLength: source.activeSourceBuffers.length,
    };
  });
  if (!source) return result;
  for (const [name, action] of [
    ['setDuration', () => (source.duration = 1)],
    ['addValidType', () => source.addSourceBuffer('video/mp4; codecs="avc1.42E01E"')],
    ['addInvalidType', () => source.addSourceBuffer('application/x-invalid')],
    ['endOfStream', () => source.endOfStream()],
    ['setLiveSeekableRange', () => source.setLiveSeekableRange(0, 1)],
    ['clearLiveSeekableRange', () => source.clearLiveSeekableRange()],
  ]) {
    attempt(name, action);
  }
  attempt('listIteration', () => Array.from(source.sourceBuffers));
  attempt('listItem', () => source.sourceBuffers[0] === undefined);
  let url;
  attempt('objectURL', () => {
    url = URL.createObjectURL(source);
    return { scheme: url.split(':')[0], readyState: source.readyState };
  });
  if (url) {
    URL.revokeObjectURL(url);
    result.afterRevocation = source.readyState;
  }
  return result;
})()
