(() => {
  const cases = [
    ['quirks-auto', '', '', '', '100%'],
    ['quirks-half', '', '', '', '50%'],
    ['quirks-edges', '', 'padding:3px;border:2px solid', '', '100%'],
    ['quirks-definite', '', '', 'height:80px;padding:5px;border:2px solid', '50%'],
    ['quirks-inline-block', '', '', 'display:inline-block', '100%'],
    ['quirks-flex', '', '', 'display:flex', '100%'],
    ['standards-auto', '<!doctype html>', '', '', '100%'],
    [
      'standards-definite',
      '<!doctype html>',
      '',
      'height:80px;padding:5px;border:2px solid',
      '50%',
    ],
  ];
  return JSON.stringify(
    cases.map(([name, doctype, body, parent, height]) => {
      const frame = document.createElement('iframe');
      frame.style.cssText = 'width:400px;height:202px;border:0';
      document.body.appendChild(frame);
      const doc = frame.contentDocument;
      doc.open();
      doc.write(
        doctype +
          '<style>body{margin:1px;' +
          body +
          '}#parent{' +
          parent +
          '}#target{display:inline-block;vertical-align:top;width:100px;height:' +
          height +
          '}</style><div id="parent"><section><div id="target"></div></section></div>',
      );
      doc.close();
      const result = [
        name,
        doc.compatMode,
        doc.getElementById('target').getBoundingClientRect().height,
      ];
      frame.remove();
      return result;
    }),
  );
})();
