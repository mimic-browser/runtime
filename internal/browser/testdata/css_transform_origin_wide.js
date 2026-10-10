(() => {
  const result = {};
  const style = document.createElement('style');
  style.textContent =
    '#outer #box {width:100px;height:100px;position:absolute;left:0;top:0;transform:rotate(45deg)!important;transform-origin:unset!important;}';
  const outer = document.createElement('div');
  outer.id = 'outer';
  const box = document.createElement('div');
  box.id = 'box';
  outer.append(style, box);
  document.body.append(outer);
  function read() {
    const computed = getComputedStyle(box);
    const rect = box.getBoundingClientRect();
    const client = box.getClientRects()[0];
    return {
      computed: [computed.transform, computed.transformOrigin, computed.position],
      rect: [rect.x, rect.y, rect.width, rect.height],
      client: [client.x, client.y, client.width, client.height],
    };
  }
  result.unset = read();
  style.textContent = style.textContent.replace('unset', 'initial');
  result.initial = read();
  style.textContent = style.textContent.replace('initial', '50% 50%');
  result.explicit = read();
  outer.remove();
  return result;
})();
