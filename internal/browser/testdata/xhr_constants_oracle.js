// prettier-ignore
(() => {
  const names = ['UNSENT', 'OPENED', 'HEADERS_RECEIVED', 'LOADING', 'DONE'];
  const xhr = new XMLHttpRequest();
  const result = names.map(name => {
    const descriptor = object => {
      const value = Object.getOwnPropertyDescriptor(object, name);
      return value ? {value: value.value, writable: value.writable, enumerable: value.enumerable, configurable: value.configurable} : null;
    };
    return {name, constructor: descriptor(XMLHttpRequest), prototype: descriptor(XMLHttpRequest.prototype), instance: descriptor(xhr), inherited: xhr[name]};
  });
  return {constants: result, initialState: xhr.readyState === XMLHttpRequest.UNSENT};
})()
