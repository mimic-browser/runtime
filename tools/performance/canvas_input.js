(() => {
  const canvas = document.createElement('canvas');
  canvas.width = 2000;
  canvas.height = 1880;
  canvas.style.width = '1000px';
  canvas.style.height = '940px';
  document.body.appendChild(canvas);
  const ctx = canvas.getContext('2d');
  const source = document.createElement('canvas');
  source.width = 1000;
  source.height = 700;
  const src = source.getContext('2d');
  src.fillStyle = '#4682b4';
  src.fillRect(0, 0, source.width, source.height);
  const points = [];
  canvas.addEventListener('mousedown', (e) => {
    const box = canvas.getBoundingClientRect();
    points.push([(e.clientX - box.left) * 2, (e.clientY - box.top) * 2]);
  });
  const rounded = () => {
    ctx.beginPath();
    ctx.moveTo(8, 240);
    ctx.lineTo(992, 240);
    ctx.quadraticCurveTo(1000, 240, 1000, 248);
    ctx.lineTo(1000, 932);
    ctx.quadraticCurveTo(1000, 940, 992, 940);
    ctx.lineTo(8, 940);
    ctx.quadraticCurveTo(0, 940, 0, 932);
    ctx.lineTo(0, 248);
    ctx.quadraticCurveTo(0, 240, 8, 240);
    ctx.closePath();
  };
  let frames = 0;
  const render = () => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    rounded();
    ctx.fillStyle = '#222222';
    ctx.fill();
    ctx.save();
    rounded();
    ctx.clip();
    ctx.drawImage(source, 0, 240);
    ctx.restore();
    for (const [x, y] of points) {
      ctx.beginPath();
      ctx.arc(x, y, 28, 0, 2 * Math.PI);
      ctx.strokeStyle = '#ff8800';
      ctx.lineWidth = 3;
      ctx.stroke();
    }
    frames++;
    if (globalThis.canvasProbe.running) requestAnimationFrame(render);
  };
  globalThis.canvasProbe = { running: true, state: () => ({ frames, points: points.length }) };
  requestAnimationFrame(render);
  return canvasProbe.state();
})();
