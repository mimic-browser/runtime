// prettier-ignore
(async () => {
  const result = { events: [] };
  const source = new MediaSource();
  const video = document.createElement('video');
  const record = (name, event) => {
    result.events.push({
      name,
      state: source.readyState,
      trusted: event.isTrusted,
      buffers: source.sourceBuffers.length,
      active: source.activeSourceBuffers.length,
    });
  };
  for (const name of ['sourceopen', 'sourceended', 'sourceclose']) {
    source.addEventListener(name, (event) => record(name, event));
  }
  for (const name of ['addsourcebuffer', 'removesourcebuffer']) {
    source.sourceBuffers.addEventListener(name, (event) => record('list.' + name, event));
    source.activeSourceBuffers.addEventListener(name, (event) => record('active.' + name, event));
  }
  const waitFor = (object, name, action) =>
    new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(Error('Timeout waiting for ' + name)), 2000);
      object.addEventListener(name, () => {
        clearTimeout(timer);
        resolve();
      }, { once: true });
      action();
    });
  const attempt = (name, action) => {
    try {
      const value = action();
      result[name] = value === undefined ? { success: true } : value;
    } catch (error) {
      result[name] = { error: error.name, message: error.message };
    }
  };
  const url = URL.createObjectURL(source);
  document.body.appendChild(video);
  try {
    await waitFor(source, 'sourceopen', () => {
      video.src = url;
      video.load();
      result.immediateAttachment = source.readyState;
    });
    result.open = {
      duration: String(source.duration),
      videoNetworkState: video.networkState,
      videoReadyState: video.readyState,
      sourceBuffers: source.sourceBuffers.length,
    };
    let buffer;
    attempt('addSourceBuffer', () => {
      buffer = source.addSourceBuffer('video/mp4; codecs="avc1.42E01E"');
      return {
        tag: Object.prototype.toString.call(buffer),
        mode: buffer.mode,
        updating: buffer.updating,
        timestampOffset: buffer.timestampOffset,
        appendWindowStart: buffer.appendWindowStart,
        appendWindowEnd: String(buffer.appendWindowEnd),
        sourceBuffers: source.sourceBuffers.length,
        activeSourceBuffers: source.activeSourceBuffers.length,
        indexIdentity: source.sourceBuffers[0] === buffer,
        iterableIdentity: Array.from(source.sourceBuffers)[0] === buffer,
      };
    });
    if (buffer) {
      for (const name of ['updatestart', 'update', 'updateend', 'error', 'abort']) {
        buffer.addEventListener(name, (event) => {
          result.events.push({ name, trusted: event.isTrusted, updating: buffer.updating });
        });
      }
      await waitFor(buffer, 'updateend', () => {
        buffer.appendBuffer(new Uint8Array());
        result.immediateAppend = buffer.updating;
      });
      result.emptyAppend = { updating: buffer.updating, buffered: buffer.buffered.length };
      source.duration = 5;
      result.durationAfterSet = source.duration;
      await waitFor(source, 'sourceended', () => {
        source.endOfStream();
        result.immediateEndOfStream = { state: source.readyState, duration: String(source.duration) };
      });
      result.ended = { state: source.readyState, duration: String(source.duration) };
      attempt('removeEnded', () => source.removeSourceBuffer(buffer));
      result.afterRemove = source.sourceBuffers.length;
    }
    URL.revokeObjectURL(url);
    result.immediateRevocation = source.readyState;
    await waitFor(source, 'sourceclose', () => {
      video.removeAttribute('src');
      video.load();
      result.immediateDetachment = source.readyState;
    });
    result.closed = { state: source.readyState, duration: String(source.duration), sourceBuffers: source.sourceBuffers.length };
  } catch (error) {
    result.failure = { error: error.name, message: error.message };
  } finally {
    URL.revokeObjectURL(url);
    video.remove();
  }
  return result;
})()
