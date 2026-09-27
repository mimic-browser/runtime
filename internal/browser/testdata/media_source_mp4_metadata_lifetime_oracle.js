// Synthetic initialization metadata only.
// prettier-ignore
(async () => {
  const bytes = Uint8Array.from(atob('AAAAHGZ0eXBpc281AAACAGlzbzVpc282bXA0MQAAAuVtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAAAAABAAABAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAAB6HRyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAEAAAABAAAAAAAYRtZGlhAAAAIG1kaGQAAAAAAAAAAAAAAAAAAEAAAAAAAFXEAAAAAAAtaGRscgAAAAAAAAAAdmlkZQAAAAAAAAAAAAAAAFZpZGVvSGFuZGxlcgAAAAEvbWluZgAAABR2bWhkAAAAAQAAAAAAAAAAAAAAJGRpbmYAAAAcZHJlZgAAAAAAAAABAAAADHVybCAAAAABAAAA73N0YmwAAACjc3RzZAAAAAAAAAABAAAAk2F2YzEAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAEAAQAEgAAABIAAAAAAAAAAEVTGF2YzYyLjIzLjEwMiBsaWJ4MjY0AAAAAAAAAAAAAAAY//8AAAAtYXZjQwFCwAr/4QAVZ0LACt3sBEAAAAMAQAAAAwCDxIngAQAFaM4PLIAAAAAQcGFzcAAAAAEAAAABAAAAEHN0dHMAAAAAAAAAAAAAABBzdHNjAAAAAAAAAAAAAAAUc3RzegAAAAAAAAAAAAAAAAAAABBzdGNvAAAAAAAAAAAAAAAobXZleAAAACB0cmV4AAAAAAAAAAEAAAABAAAAAAAAAAAAAAAAAAAAYXVkdGEAAABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSpdG9vAAAAHGRhdGEAAAABAAAAAExhdmY2Mi44LjEwMg=='), value => value.charCodeAt(0));
  const source = new MediaSource();
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  document.body.append(video);
  await new Promise(resolve => {
    source.addEventListener('sourceopen', resolve, {once:true});
    video.src = url;
    video.load();
  });
  const observe = () => ({sourceDuration: String(source.duration), videoDuration: String(video.duration), width:video.videoWidth, height:video.videoHeight, readyState:video.readyState});
  source.duration = 7;
  const result = {beforeInitialization: observe()};
  const buffer = source.addSourceBuffer('video/mp4; codecs="avc1.42C00A"');
  await new Promise(resolve => {
    buffer.addEventListener('updateend', resolve, {once:true});
    buffer.appendBuffer(bytes);
    bytes.fill(0);
  });
  await new Promise(resolve => setTimeout(resolve, 20));
  result.afterInitialization = observe();
  source.removeSourceBuffer(buffer);
  await new Promise(resolve => setTimeout(resolve, 20));
  result.afterBufferRemoval = observe();
  video.removeAttribute('src');
  video.load();
  await new Promise(resolve => setTimeout(resolve, 20));
  result.afterReset = observe();
  URL.revokeObjectURL(url);
  video.remove();
  return result;
})()
