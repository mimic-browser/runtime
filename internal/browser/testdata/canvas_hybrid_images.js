const image = new Image();
image.crossOrigin = 'anonymous';
const canvas = new OffscreenCanvas(3, 3);
const ctx = canvas.getContext('2d');
const load = async (path) => {
  image.src = base + path;
  await image.decode();
};
const pixel = (x, y) => String(ctx.getImageData(x, y, 1, 1).data);
await load('/red');
for (let i = 0; i < 8; i++) {
  ctx.clearRect(0, 0, 3, 3);
  ctx.drawImage(image, 0, 0);
  if (pixel(1, 0) !== '255,0,0,255') return 'repeat';
}
await load('/tall');
ctx.clearRect(0, 0, 3, 3);
ctx.drawImage(image, 0, 0);
if (pixel(0, 1) !== '255,0,0,255' || pixel(1, 0) !== '0,0,0,0') return 'dimensions';
await load('/green');
ctx.drawImage(image, 0, 0);
if (pixel(0, 0) !== '0,255,0,255') return 'source changed';
const bitmap = await createImageBitmap(image);
await load('/red');
ctx.drawImage(image, 0, 0);
if (pixel(0, 0) !== '255,0,0,255') return 'reload';
ctx.drawImage(bitmap, 0, 0);
if (pixel(0, 0) !== '0,255,0,255') return 'snapshot';
bitmap.close();
try {
  ctx.drawImage(bitmap, 0, 0);
  return 'closed bitmap accepted';
} catch (e) {
  if (e.name !== 'InvalidStateError') return e.name;
}
image.src = base + '/missing';
await image.decode().catch(() => {});
try {
  ctx.drawImage(image, 0, 0);
  return 'broken source accepted';
} catch (e) {
  if (e.name !== 'InvalidStateError') return e.name;
}
return 'ok';
