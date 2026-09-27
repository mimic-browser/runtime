(() => {
  for (const [tag, name] of [
    ["canvas", "HTMLCanvasElement"],
    ["div", "HTMLDivElement"],
    ["img", "HTMLImageElement"],
    ["script", "HTMLScriptElement"],
  ]) {
    const ctor = globalThis[name];
    for (const object of [document.createElement(tag), Object.create(ctor.prototype)]) {
      let message;
      try {
        Date.prototype.toString.call(object);
      } catch (error) {
        message = error.message;
      }
      if (message !== `Method Date.prototype.toString called on incompatible receiver #<${name}>`) {
        return message;
      }
      if (object.constructor !== ctor || Object.getPrototypeOf(object) !== ctor.prototype) {
        return "constructor identity";
      }
    }
    if (ctor.name !== name) return "constructor name";
  }
  return "ok";
})();
