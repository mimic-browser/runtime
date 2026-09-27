// prettier-ignore
(() => {
  const xhr = new XMLHttpRequest();
  const upload = xhr.upload;
  const descriptor = Object.getOwnPropertyDescriptor(XMLHttpRequest.prototype, 'upload');
  const error = call => { try { call(); return null; } catch (error) { return {name: error.name, message: error.message}; } };
  const result = {
    same: upload === xhr.upload,
    distinct: upload !== new XMLHttpRequest().upload,
    brand: Object.prototype.toString.call(upload),
    prototype: Object.getPrototypeOf(upload) === XMLHttpRequestUpload.prototype,
    parent: Object.getPrototypeOf(XMLHttpRequestUpload.prototype) === XMLHttpRequestEventTarget.prototype,
    eventTarget: upload instanceof EventTarget,
    xhrEventTarget: upload instanceof XMLHttpRequestEventTarget,
    descriptor: {enumerable: descriptor.enumerable, configurable: descriptor.configurable, setter: typeof descriptor.set},
    invalidReceiver: error(() => descriptor.get.call({})),
    construct: error(() => new XMLHttpRequestUpload()),
  };
  result.handlers = ['onloadstart', 'onprogress', 'onabort', 'onerror', 'onload', 'ontimeout', 'onloadend'].map(name => {
    const initial = upload[name];
    const callback = () => {};
    upload[name] = callback;
    const same = upload[name] === callback;
    const isolated = xhr[name] === null;
    upload[name] = null;
    return {name, initial, same, isolated, cleared: upload[name]};
  });
  let observed = null;
  upload.addEventListener('probe', function (event) { observed = {thisUpload: this === upload, target: event.target === upload, trusted: event.isTrusted}; });
  result.dispatch = upload.dispatchEvent(new Event('probe'));
  result.observed = observed;
  const handlerEvents = [];
  xhr.onabort = function (event) { handlerEvents.push({owner: 'xhr', receiver: this === xhr, target: event.target === xhr, trusted: event.isTrusted}); };
  upload.onabort = function (event) { handlerEvents.push({owner: 'upload', receiver: this === upload, target: event.target === upload, trusted: event.isTrusted}); };
  upload.dispatchEvent(new Event('abort'));
  xhr.dispatchEvent(new Event('abort'));
  result.handlerEvents = handlerEvents;
  xhr.onabort = upload.onabort = null;
  xhr.open('GET', '/upload-probe');
  xhr.abort();
  result.sameAfterReset = upload === xhr.upload;
  return result;
})()
