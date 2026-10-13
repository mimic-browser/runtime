(() => {
  const observe = (x, y, width, height, radii, transform) => {
    const canvas = new OffscreenCanvas(32, 24);
    const context = canvas.getContext('2d');
    if (transform) transform(context);
    context.beginPath();
    context.roundRect(x, y, width, height, radii);
    context.fill();
    return Array.from(context.getImageData(0, 0, 32, 24).data).filter(
      (_, index) => index % 4 === 3,
    );
  };
  const same = (a, b) => a.length === b.length && a.every((value, index) => value === b[index]);
  const reverseRows = (alpha) => {
    const output = [];
    for (let y = 0; y < 24; y++)
      for (let x = 0; x < 32; x++) output.push(alpha[y * 32 + 31 - x]);
    return output;
  };
  const attempt = (radii) => {
    const context = new OffscreenCanvas(2, 2).getContext('2d');
    try {
      context.roundRect(0, 0, 1, 1, radii);
      return 'none';
    } catch (error) {
      return [error.name, error instanceof DOMException];
    }
  };
  const scalar = observe(4, 3, 24, 18, 5);
  const single = observe(4, 3, 24, 18, [5]);
  const point = observe(4, 3, 24, 18, { x: 5, y: 3 });
  const four = observe(4, 3, 24, 18, [2, 4, 6, 8]);
  const oversize = observe(6, 4, 20, 16, 20);
  const scaled = observe(6, 4, 20, 16, 8);
  const negativeWidth = observe(28, 3, -24, 18, [2, 4, 6, 8]);
  const mirrored = observe(4, 3, 24, 18, [4, 2, 8, 6]);
  const transformed = observe(2, 2, 16, 12, [4, { x: 6, y: 2 }], (context) => {
    context.translate(2, 1);
    context.scale(1.25, 1.5);
  });
  const snapshot = (() => {
    const canvas = new OffscreenCanvas(32, 24);
    const context = canvas.getContext('2d');
    context.beginPath();
    context.roundRect(4, 3, 24, 18, 5);
    context.fill();
    context.beginPath();
    context.rect(0, 0, 1, 1);
    return Array.from(context.getImageData(0, 0, 32, 24).data).filter(
      (_, index) => index % 4 === 3,
    );
  })();
  return {
    empty: attempt([]),
    tooMany: attempt([1, 2, 3, 4, 5]),
    negative: attempt(-1),
    nan: attempt(NaN),
    scalarMatchesSingle: same(scalar, single),
    oversizedScales: same(oversize, scaled),
    negativeWidthSwapsCorners: same(negativeWidth, mirrored),
    pointIsElliptical: !same(point, scalar),
    transformDiffers: !same(transformed, four),
    snapshotMatchesScalar: same(snapshot, scalar),
    alpha: { scalar, point, four, transformed },
  };
})()
