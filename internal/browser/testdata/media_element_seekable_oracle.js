// prettier-ignore
(() => {
  const error = call => {
    try { call(); return null; }
    catch (error) { return {name:error.name, message:error.message}; }
  };
  const observe = element => {
    const first = element.seekable;
    const second = element.seekable;
    return {
      tag: Object.prototype.toString.call(first),
      instance: first instanceof TimeRanges,
      length: first.length,
      fresh: first !== second,
      startMissing: error(() => first.start()),
      startZero: error(() => first.start(0)),
      endNegative: error(() => first.end(-1)),
    };
  };
  const getter = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'seekable').get;
  return {
    video: observe(document.createElement('video')),
    audio: observe(document.createElement('audio')),
    invalidReceiver: error(() => getter.call({})),
  };
})()
