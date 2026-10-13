(() => {
  const observe = (configure) => {
    const canvas = new OffscreenCanvas(32, 24);
    const context = canvas.getContext('2d');
    context.lineWidth = 2;
    context.beginPath();
    configure(context);
    context.stroke();
    const alpha = Array.from(context.getImageData(0, 0, 32, 24).data).filter(
      (_, index) => index % 4 === 3,
    );
    return {
      alpha,
      png: canvas.convertToBlob ? null : undefined,
    };
  };
  const cases = {
    rounded: observe((context) => {
      context.moveTo(3, 20);
      context.arcTo(3, 3, 26, 3, 7);
      context.lineTo(29, 3);
    }),
    zero: observe((context) => {
      context.moveTo(3, 20);
      context.arcTo(3, 3, 26, 3, 0);
      context.lineTo(29, 3);
    }),
    transformed: observe((context) => {
      context.translate(2, 1);
      context.scale(1.5, 0.75);
      context.moveTo(2, 24);
      context.arcTo(2, 4, 16, 4, 5);
      context.resetTransform();
      context.lineTo(29, 4);
    }),
    snapshot: (() => {
      const canvas = new OffscreenCanvas(32, 24);
      const context = canvas.getContext('2d');
      context.lineWidth = 2;
      context.beginPath();
      context.moveTo(3, 20);
      context.arcTo(3, 3, 26, 3, 7);
      context.lineTo(29, 3);
      context.stroke();
      context.beginPath();
      context.moveTo(20, 20);
      context.lineTo(29, 20);
      const alpha = Array.from(context.getImageData(0, 0, 32, 24).data).filter(
        (_, index) => index % 4 === 3,
      );
      return { alpha };
    })(),
  };
  const exception = (radius) => {
    const context = new OffscreenCanvas(2, 2).getContext('2d');
    try {
      context.arcTo(0, 0, 1, 1, radius);
      return 'none';
    } catch (error) {
      return [error.name, error instanceof DOMException];
    }
  };
  const same = (a, b) => a.length === b.length && a.every((value, index) => value === b[index]);
  return {
    negativeRadius: exception(-1),
    nanRadius: exception(NaN),
    roundedDiffersFromZero: !same(cases.rounded.alpha, cases.zero.alpha),
    transformDiffers: !same(cases.rounded.alpha, cases.transformed.alpha),
    snapshotMatchesRounded: same(cases.rounded.alpha, cases.snapshot.alpha),
    alpha: Object.fromEntries(Object.entries(cases).map(([name, value]) => [name, value.alpha])),
  };
})()
