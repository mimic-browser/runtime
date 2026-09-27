// prettier-ignore
(() => {
  const element = document.createElement('div');
  const result = { initial: element.dir, cases: [] };
  for (const value of ['ltr', 'RTL', 'Auto', ' ltr ', 'unknown', '', null]) {
    element.dir = value;
    result.cases.push({attribute: element.getAttribute('dir'), value: element.dir});
  }
  element.setAttribute('dir', 'rtl');
  element.setAttribute('inputmode', 'numeric');
  element.getAttribute = () => 'ltr';
  result.authorOverride = {dir: element.dir, inputMode: element.inputMode};
  const error = call => {
    try { call(); return null; }
    catch (error) { return {name: error.name, message: error.message}; }
  };
  const descriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'dir');
  result.invalidReceiver = error(() => descriptor.get.call({}));
  result.svgReceiver = error(() => descriptor.get.call(document.createElementNS('http://www.w3.org/2000/svg', 'svg')));
  result.symbol = error(() => { element.dir = Symbol('dir'); });
  result.descriptor = {enumerable: descriptor.enumerable, configurable: descriptor.configurable};
  return result;
})()
