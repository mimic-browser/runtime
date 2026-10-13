(() => {
  const render = (filter, operation = 'fill', shadow = false) => {
    const canvas = new OffscreenCanvas(14, 12),
      context = canvas.getContext('2d');
    context.filter = filter;
    if (shadow) {
      context.shadowColor = 'rgba(0, 0, 255, .75)';
      context.shadowOffsetX = 3;
      context.shadowOffsetY = 2;
      context.shadowBlur = 2;
    }
    if (operation === 'image') {
      const source = new OffscreenCanvas(4, 4),
        sourceContext = source.getContext('2d');
      sourceContext.fillStyle = 'rgba(200, 40, 20, .8)';
      sourceContext.fillRect(0, 0, 4, 4);
      context.drawImage(source, 4, 3);
    } else {
      context.fillStyle = 'rgba(200, 40, 20, .8)';
      context.fillRect(4, 3, 4, 4);
    }
    const data = context.getImageData(0, 0, 14, 12).data,
      alpha = Array.from(data).filter((_, index) => index % 4 === 3);
    return {
      alpha,
      sum: alpha.reduce((total, value) => total + value, 0),
      nonzero: alpha.filter(Boolean).length,
      center: alpha[4 * 14 + 5],
      edge: alpha[4 * 14 + 3],
    };
  };
  const same = (a, b) => a.length === b.length && a.every((value, index) => value === b[index]);
  const property = (() => {
    const context = new OffscreenCanvas(1, 1).getContext('2d'),
      output = [];
    for (const value of [
      'blur(1px)',
      'blur(-1px)',
      'drop-shadow(2px 1px 0 rgb(0 255 0))',
      'drop-shadow(1px 1px -1px red)',
    ]) {
      context.filter = value;
      output.push([value, context.filter]);
    }
    return output;
  })();
  const none = render('none'),
    blur = render('blur(1px)'),
    drop = render('drop-shadow(2px 1px 0 rgb(0 255 0))'),
    blurOpacity = render('blur(1px) opacity(40%)'),
    opacityBlur = render('opacity(40%) blur(1px)'),
    imageBlur = render('blur(1px)', 'image'),
    shadowBlur = render('blur(1px)', 'fill', true),
    snapshot = (() => {
      const canvas = new OffscreenCanvas(14, 12),
        context = canvas.getContext('2d');
      context.filter = 'blur(1px)';
      context.fillStyle = 'rgba(200, 40, 20, .8)';
      context.fillRect(4, 3, 4, 4);
      context.filter = 'none';
      return Array.from(context.getImageData(0, 0, 14, 12).data).filter(
        (_, index) => index % 4 === 3,
      );
    })();
  return {
    property,
    blurSpreads: blur.nonzero > none.nonzero && blur.edge > 0 && blur.center < none.center,
    blurConservesAlpha: Math.abs(blur.sum - none.sum) < none.sum / 50,
    dropShadowExtends: drop.nonzero > none.nonzero && drop.sum > none.sum,
    chainAttenuates: blurOpacity.sum < blur.sum && opacityBlur.sum < blur.sum,
    chainOrderClose: Math.abs(blurOpacity.sum - opacityBlur.sum) < none.sum / 50,
    imageMatchesPath: same(imageBlur.alpha, blur.alpha),
    shadowParticipates: shadowBlur.nonzero > blur.nonzero && shadowBlur.sum > blur.sum,
    snapshotMatches: same(snapshot, blur.alpha),
  };
})()
