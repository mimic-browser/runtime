(() => {
  const pixel = (data, width, x, y) =>
    Array.from(data.slice((y * width + x) * 4, (y * width + x) * 4 + 4));
  const render = (filter, operation = 'fill') => {
    const canvas = new OffscreenCanvas(10, 8),
      context = canvas.getContext('2d');
    context.filter = filter;
    if (operation === 'image') {
      const source = new OffscreenCanvas(4, 4),
        sourceContext = source.getContext('2d');
      sourceContext.fillStyle = 'rgba(100, 150, 200, .8)';
      sourceContext.fillRect(0, 0, 4, 4);
      context.drawImage(source, 2, 2);
    } else {
      context.fillStyle = 'rgba(100, 150, 200, .8)';
      context.fillRect(2, 2, 4, 4);
    }
    const data = context.getImageData(0, 0, 10, 8).data;
    return {
      center: pixel(data, 10, 3, 3),
      edge: pixel(data, 10, 1, 3),
      outside: pixel(data, 10, 8, 7),
      all: Array.from(data),
    };
  };
  const property = (() => {
    const context = new OffscreenCanvas(1, 1).getContext('2d'),
      output = [];
    for (const value of ['brightness(50%)', 'bogus(2)', 'contrast(-1)', 'none']) {
      context.filter = value;
      output.push([value, context.filter]);
    }
    return output;
  })();
  const names = [
    'none',
    'brightness(50%)',
    'contrast(150%)',
    'grayscale(75%)',
    'opacity(40%)',
    'invert(60%)',
    'sepia(80%)',
    'saturate(200%)',
    'hue-rotate(90deg)',
    'brightness(50%) opacity(40%)',
  ];
  const results = Object.fromEntries(names.map((name) => [name, render(name)]));
  const snapshot = (() => {
    const canvas = new OffscreenCanvas(10, 8),
      context = canvas.getContext('2d');
    context.filter = 'brightness(50%)';
    context.fillStyle = 'rgba(100, 150, 200, .8)';
    context.fillRect(2, 2, 4, 4);
    context.filter = 'none';
    return pixel(context.getImageData(0, 0, 10, 8).data, 10, 3, 3);
  })();
  const summarize = ({ center, edge, outside, all }) => ({
    // The selected Chrome surface retains filtered color precision that an
    // RGBA8 premultiplied surface cannot represent for every alpha. Four-byte
    // buckets keep this regression tight while alpha and extent remain exact.
    centerBucket: center.map((value) => Math.floor(value / 4)),
    edge,
    outside,
    alphaSum: all.filter((_, i) => i % 4 === 3).reduce((a, b) => a + b, 0),
    nonzero: all.filter((v, i) => i % 4 === 3 && v).length,
  });
  return {
    property,
    results: Object.fromEntries(Object.entries(results).map(([k, v]) => [k, summarize(v)])),
    drawImage: summarize(render('contrast(150%)', 'image')),
    snapshotBucket: snapshot.map((value) => Math.floor(value / 4)),
  };
})()
