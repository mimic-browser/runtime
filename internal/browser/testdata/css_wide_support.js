(() => {
  const properties = [
    'color-scheme',
    'aspect-ratio',
    'border-end-end-radius',
    'contain-intrinsic-width',
    'contain-intrinsic-height',
    'transform-origin',
  ];
  const style = document.createElement('div').style;
  const rows = properties.map((property) => {
    const values = ['initial', 'inherit', 'unset', 'revert', 'revert-layer'];
    return values.every((value) => {
      style.setProperty(property, value.toUpperCase());
      return (
        CSS.supports(property, value.toUpperCase()) &&
        CSS.supports(`(${property}: ${value})`) &&
        style.getPropertyValue(property) === value
      );
    });
  });
  const sheet = document.createElement('style');
  const target = document.createElement('div');
  target.id = 'wide-support-target';
  sheet.textContent =
    '#wide-support-target {width:5px}' +
    '@supports (color-scheme: initial) {#wide-support-target {width:17px}}';
  document.head.append(sheet);
  document.body.append(target);
  const conditional = getComputedStyle(target).width === '17px';
  sheet.remove();
  target.remove();
  return {
    rows,
    conditional,
    priority: [
      CSS.supports('color-scheme', 'initial !important'),
      CSS.supports('width', 'initial /* comment */ ! important'),
      CSS.supports('width', 'initial !important !important'),
      CSS.supports('width', 'initial !important;'),
      CSS.supports('--custom', 'initial !important'),
    ],
    invalid: [
      CSS.supports('unknown-property', 'initial'),
      CSS.supports('aspectRatio', 'initial'),
      CSS.supports('color-scheme', 'initial junk'),
      CSS.supports('color-scheme', 'initial; width: 1px'),
    ],
  };
})();
