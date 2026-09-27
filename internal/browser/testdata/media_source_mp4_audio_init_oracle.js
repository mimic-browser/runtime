// Synthetic AAC initialization metadata only.
// prettier-ignore
(async () => {
  const init = Uint8Array.from(atob('AAAAHGZ0eXBpc281AAACAGlzbzVpc282bXA0MQAAArxtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAAAAABAAABAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAABv3RyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAQEAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAVttZGlhAAAAIG1kaGQAAAAAAAAAAAAAAAAAALuAAAAAAFXEAAAAAAAtaGRscgAAAAAAAAAAc291bgAAAAAAAAAAAAAAAFNvdW5kSGFuZGxlcgAAAAEGbWluZgAAABBzbWhkAAAAAAAAAAAAAAAkZGluZgAAABxkcmVmAAAAAAAAAAEAAAAMdXJsIAAAAAEAAADKc3RibAAAAH5zdHNkAAAAAAAAAAEAAABubXA0YQAAAAAAAAABAAAAAAAAAAAAAQAQAAAAALuAAAAAAAA2ZXNkcwAAAAADgICAJQABAASAgIAXQBUAAAAAAQ2IAAENiAWAgIAFEYhW5QAGgICAAQIAAAAUYnRydAAAAAAAAQ2IAAENiAAAABBzdHRzAAAAAAAAAAAAAAAQc3RzYwAAAAAAAAAAAAAAFHN0c3oAAAAAAAAAAAAAAAAAAAAQc3RjbwAAAAAAAAAAAAAAKG12ZXgAAAAgdHJleAAAAAAAAAABAAAAAQAAAAAAAAAAAAAAAAAAAGF1ZHRhAAAAWW1ldGEAAAAAAAAAIWhkbHIAAAAAAAAAAG1kaXJhcHBsAAAAAAAAAAAAAAAALGlsc3QAAAAkqXRvbwAAABxkYXRhAAAAAQAAAABMYXZmNjIuOC4xMDI='), value => value.charCodeAt(0));
  const result = { events: [] };
  const source = new MediaSource();
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  document.body.append(video);
  await new Promise(resolve => {
    source.addEventListener('sourceopen', resolve, {once: true});
    video.src = url;
    video.load();
  });
  const buffer = source.addSourceBuffer('audio/mp4; codecs="mp4a.40.2"');
  const observe = () => ({
    updating: buffer.updating,
    active: source.activeSourceBuffers.length,
    duration: String(source.duration),
    readyState: video.readyState,
    videoDuration: String(video.duration),
    width: video.videoWidth,
    height: video.videoHeight,
    ranges: buffer.buffered.length,
  });
  for (const name of ['updatestart', 'update', 'updateend', 'error']) {
    buffer.addEventListener(name, event => result.events.push({name, trusted:event.isTrusted}));
  }
  const append = bytes => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error('append did not settle')), 2000);
    buffer.addEventListener('updateend', () => { clearTimeout(timer); resolve(); }, {once:true});
    buffer.appendBuffer(bytes);
  });
  try {
    await append(init.subarray(0, 37));
    result.partial = observe();
    await append(init.subarray(37));
    await new Promise(resolve => setTimeout(resolve, 20));
    result.complete = observe();
    result.activeIdentity = source.activeSourceBuffers[0] === buffer;
  } catch (error) {
    result.failure = {name:error.name, message:error.message};
  }
  video.removeAttribute('src');
  video.load();
  URL.revokeObjectURL(url);
  video.remove();
  return result;
})()
