// prettier-ignore
(async () => {
  const source = new MediaSource(), video = document.createElement('video');
  const url = URL.createObjectURL(source);
  document.body.append(video);
  await new Promise(resolve=>{source.addEventListener('sourceopen',resolve,{once:true});video.src=url;video.load();});
  source.duration = 7;
  const range = video.seekable;
  const error = call=>{try{call();return null;}catch(error){return {name:error.name,message:error.message};}};
  const result = {length:range.length,start:range.start(0),end:range.end(0),equalBound:error(()=>range.start(1)),greaterBound:error(()=>range.end(2))};
  video.removeAttribute('src');video.load();URL.revokeObjectURL(url);video.remove();
  return result;
})()
