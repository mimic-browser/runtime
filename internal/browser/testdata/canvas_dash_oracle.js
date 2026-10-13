(() => {
  const observe = (dash, offset, mutate) => {
    const canvas = new OffscreenCanvas(24, 8);
    const context = canvas.getContext('2d');
    context.lineWidth = 2;
    context.lineCap = 'butt';
    context.setLineDash(dash);
    context.lineDashOffset = offset;
    context.beginPath();
    context.moveTo(2, 4);
    context.lineTo(22, 4);
    context.stroke();
    if (mutate) {
      context.setLineDash([1, 1]);
      context.lineDashOffset = 7;
    }
    const pixels = context.getImageData(0, 0, 24, 8).data;
    return {
      alpha: Array.from({ length: 24 }, (_, x) => pixels[(4 * 24 + x) * 4 + 3]),
      sum: Array.from(pixels).filter((_, index) => index % 4 === 3).reduce((a, b) => a + b, 0),
      state: [context.getLineDash(), context.lineDashOffset],
    };
  };
  return {
    basic: observe([4, 2], 0, false),
    positive: observe([4, 2], 2, false),
    negative: observe([4, 2], -2, false),
    odd: observe([3], 0, false),
    zeros: observe([0, 0], 0, false),
    snapshot: observe([4, 2], 0, true),
  };
})()
