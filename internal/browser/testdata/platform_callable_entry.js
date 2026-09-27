(() => {
  const functions = [setTimeout, Array.prototype.values];
  if (typeof Storage === 'function') functions.push(Storage.prototype.getItem);
  if (typeof Document === 'function') functions.push(Document.prototype.createElement);
  for (const fn of functions) {
    let caught;
    try { Date.prototype.toString.call(fn); } catch (error) { caught = error; }
    const source = 'function ' + fn.name + '() { [native code] }';
    if (caught.message !== 'Method Date.prototype.toString called on incompatible receiver ' + source)
      return caught.message;
    if (caught.stack.split('\n')[1] !== '    at ' + fn.name + '.toString (<anonymous>)')
      return caught.stack;
    if (Object.hasOwn(fn, 'prototype')) return 'constructible platform operation';
  }
  const authored = function userFunction() { return 'author source marker'; };
  let message;
  try { Date.prototype.toString.call(authored); } catch (error) { message = error.message; }
  if (!message.includes('author source marker')) return 'author source changed';
  if (typeof Storage === 'function') {
    const sentinel = {};
    let caught;
    try { localStorage.getItem({ toString() { throw sentinel; } }); }
    catch (error) { caught = error; }
    if (caught !== sentinel) return 'exception identity changed';
    if (NodeList.prototype.values !== Array.prototype.values) return 'intrinsic alias changed';
  }
  return 'ok';
})();
