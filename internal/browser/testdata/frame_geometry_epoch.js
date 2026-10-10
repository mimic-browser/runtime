globalThis.frameGeometrySteps = [
  () => {
    document.body.style.margin = '0';
    globalThis.container = document.createElement('div');
    container.style.width = '200px';
    globalThis.frame = document.createElement('iframe');
    frame.style.cssText = 'width:100%;height:100px;border:0';
    container.append(frame);
    document.body.append(container);
    frame.contentDocument.head.innerHTML =
      '<style>body{margin:0}#box{width:50vw;height:20px}@media(min-width:300px){#box{height:40px}}</style>';
    frame.contentDocument.body.innerHTML = '<div id="box"></div>';
    frame.contentWindow.eval(`
      globalThis.boxSizeMatches = (width, height) => {
        const rect = document.getElementById('box').getBoundingClientRect();
        return rect.width === width && rect.height === height;
      };
    `);
    return frame.contentWindow.eval('boxSizeMatches(100, 20)');
  },
  () => frame.contentWindow.eval('boxSizeMatches(100, 20)'),
  () => {
    container.style.width = '400px';
    return frame.contentWindow.eval('boxSizeMatches(200, 40)');
  },
  () => {
    container.style.width = '200px';
    return frame.contentWindow.eval('boxSizeMatches(100, 20)');
  },
  () => {
    frame.contentDocument.getElementById('box').style.width = '30px';
    return frame.contentWindow.eval('boxSizeMatches(30, 20)');
  },
];

globalThis.setupFrameGeometryReuse = () => {
  globalThis.frame = document.createElement('iframe');
  document.body.append(frame);
  frame.contentDocument.body.innerHTML = '<button style="width:100px;height:30px">button</button>';
};
true;
