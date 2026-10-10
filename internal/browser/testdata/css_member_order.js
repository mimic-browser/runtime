(() => {
  const style = getComputedStyle(document.body);
  const names = Object.getOwnPropertyNames(Object.getPrototypeOf(style));
  const base = [
    'cssText',
    'length',
    'parentRule',
    'cssFloat',
    'getPropertyPriority',
    'getPropertyValue',
    'item',
    'removeProperty',
    'setProperty',
    'constructor',
  ];
  const tail = Object.keys(style).filter((name) => isNaN(Number(name)) && !name.includes('-'));
  const sorted = tail.slice().sort();
  return (
    names.length === base.length &&
    base.every((name, index) => names[index] === name) &&
    tail.length > 0 &&
    tail.every((name, index) => name === sorted[index])
  );
})();
