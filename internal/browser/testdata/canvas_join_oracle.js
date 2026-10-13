(() => {
  const observe = (join, limit, mutate) => {
    const canvas = new OffscreenCanvas(20, 20);
    const context = canvas.getContext('2d');
    context.lineWidth = 6;
    context.lineJoin = join;
    context.miterLimit = limit;
    context.beginPath();
    context.moveTo(3, 16);
    context.lineTo(10, 3);
    context.lineTo(17, 16);
    context.stroke();
    if (mutate) {
      context.lineJoin = 'bevel';
      context.miterLimit = 1;
    }
    const pixels = context.getImageData(0, 0, 20, 20).data;
    return {
      alpha: Array.from(pixels).filter((_, index) => index % 4 === 3),
      state: [context.lineJoin, context.miterLimit],
    };
  };
  const output = {
    round: observe('round', 10, false),
    bevel: observe('bevel', 10, false),
    miter: observe('miter', 10, false),
    limited: observe('miter', 1, false),
    snapshot: observe('round', 10, true),
  };
  const same = (a, b) => a.length === b.length && a.every((value, index) => value === b[index]);
  const sum = (values) => values.reduce((total, value) => total + value, 0);
  return {
    state: Object.fromEntries(Object.entries(output).map(([name, value]) => [name, value.state])),
    limitedMatchesBevel: same(output.limited.alpha, output.bevel.alpha),
    snapshotMatchesRound: same(output.snapshot.alpha, output.round.alpha),
    distinct: !same(output.round.alpha, output.bevel.alpha) && !same(output.round.alpha, output.miter.alpha),
    coverageOrder:
      sum(output.miter.alpha) > sum(output.round.alpha) &&
      sum(output.round.alpha) > sum(output.bevel.alpha),
  };
})()
