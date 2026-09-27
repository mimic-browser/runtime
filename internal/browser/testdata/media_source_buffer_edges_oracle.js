// prettier-ignore
(async () => {
  const result = {};
  const attempt = (name, action) => {
    try {
      const value = action();
      result[name] = value === undefined ? {success: true} : value;
    } catch (error) {
      result[name] = {error: error.name, message: error.message};
    }
  };
  const source = new MediaSource();
  attempt('durationNegative', () => { source.duration = -1; });
  attempt('durationNaN', () => { source.duration = NaN; });
  attempt('endOfStreamEnum', () => source.endOfStream('invalid'));
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  await new Promise(resolve => {
    source.addEventListener('sourceopen', resolve, {once: true});
    video.src = url;
    video.load();
  });
  const buffer = source.addSourceBuffer('video/mp4; codecs="avc1.42E01E"');
  result.bufferedIdentity = buffer.buffered === buffer.buffered;
  attempt('rangeStart', () => buffer.buffered.start(0));
  attempt('rangeEnd', () => buffer.buffered.end(0));
  attempt('rangeMissing', () => buffer.buffered.start());
  attempt('windowStart', () => { buffer.appendWindowStart = 1; return buffer.appendWindowStart; });
  attempt('windowEnd', () => { buffer.appendWindowEnd = 10; return buffer.appendWindowEnd; });
  attempt('windowStartNegative', () => { buffer.appendWindowStart = -1; });
  attempt('windowStartPastEnd', () => { buffer.appendWindowStart = 11; });
  attempt('windowEndBeforeStart', () => { buffer.appendWindowEnd = 0; });
  attempt('windowEndNaN', () => { buffer.appendWindowEnd = NaN; });
  attempt('timestamp', () => { buffer.timestampOffset = -2; return buffer.timestampOffset; });
  attempt('mode', () => { buffer.mode = 'sequence'; return buffer.mode; });
  attempt('modeEnum', () => { buffer.mode = 'invalid'; });
  attempt('appendWrongType', () => buffer.appendBuffer('wrong'));
  const ended = new Promise(resolve => buffer.addEventListener('updateend', resolve, {once: true}));
  buffer.appendBuffer(new DataView(new ArrayBuffer(0)));
  attempt('appendUpdating', () => buffer.appendBuffer(new Uint8Array()));
  attempt('durationUpdating', () => { source.duration = 5; });
  attempt('endOfStreamUpdating', () => source.endOfStream());
  await ended;
  source.removeSourceBuffer(buffer);
  attempt('bufferedRemoved', () => buffer.buffered.length);
  attempt('appendRemoved', () => buffer.appendBuffer(new Uint8Array()));
  attempt('timestampRemoved', () => { buffer.timestampOffset = 0; });
  attempt('windowStartRemoved', () => { buffer.appendWindowStart = 0; });
  attempt('modeRemoved', () => { buffer.mode = 'segments'; });
  attempt('removeAgain', () => source.removeSourceBuffer(buffer));
  source.endOfStream();
  result.emptyEndedDuration = String(source.duration);
  video.removeAttribute('src');
  video.load();
  URL.revokeObjectURL(url);
  return result;
})()
