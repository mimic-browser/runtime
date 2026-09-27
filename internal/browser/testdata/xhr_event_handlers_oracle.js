// prettier-ignore
(() => {
  const xhr = new XMLHttpRequest();
  const names = ['onloadstart', 'onprogress', 'onabort', 'onerror', 'onload', 'ontimeout', 'onloadend', 'onreadystatechange'];
  const handlers = names.map(name => {
    const owner = Object.hasOwn(XMLHttpRequest.prototype, name) ? XMLHttpRequest.prototype : XMLHttpRequestEventTarget.prototype;
    const descriptor = Object.getOwnPropertyDescriptor(owner, name);
    const errors = [() => descriptor.get.call({}), () => descriptor.set.call({}, null)].map(call => {
      try { call(); return null; } catch (error) { return {name: error.name, message: error.message}; }
    });
    const callback = () => {};
    const initial = xhr[name];
    xhr[name] = callback;
    const roundTrip = xhr[name] === callback;
    xhr[name] = null;
    return {name, owner: owner === XMLHttpRequest.prototype ? 'XMLHttpRequest' : 'XMLHttpRequestEventTarget', enumerable: descriptor.enumerable, configurable: descriptor.configurable, initial, roundTrip, cleared: xhr[name], errors};
  });
  return {handlers};
})()
