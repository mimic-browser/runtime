(() => {
  const element = document.createElement('div');
  element.style.cssText = 'width:123px;height:45px;padding:2px;border:1px solid';
  document.body.append(element);
  const root = document.documentElement,
    body = document.body;
  const expected = [element.clientWidth, element.clientHeight];
  let rootReads = 0,
    bodyReads = 0;
  Object.defineProperty(document, 'documentElement', {
    configurable: true,
    get() {
      rootReads++;
      return root;
    },
  });
  Object.defineProperty(document, 'body', {
    configurable: true,
    get() {
      bodyReads++;
      return body;
    },
  });
  try {
    for (let i = 0; i < 10; i++)
      if (element.clientWidth !== expected[0] || element.clientHeight !== expected[1])
        throw Error('dimensions changed');
    return rootReads === 0 && bodyReads === 0;
  } finally {
    delete document.documentElement;
    delete document.body;
    element.remove();
  }
})();
