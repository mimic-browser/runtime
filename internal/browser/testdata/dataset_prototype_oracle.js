// prettier-ignore
(() => {
  const element = document.createElement('div');
  element.setAttribute('data-long-name', 'value');
  const dataset = element.dataset;
  const result = {
    sameObject: dataset === element.dataset,
    hasOwnPropertyType: typeof dataset.hasOwnProperty,
    hasOwnData: dataset.hasOwnProperty('longName'),
    hasOwnMethod: dataset.hasOwnProperty('hasOwnProperty'),
    inheritedMethod: dataset.hasOwnProperty === Object.prototype.hasOwnProperty,
    inheritedToString: dataset.toString === Object.prototype.toString,
    brand: Object.prototype.toString.call(dataset),
    prototype: Object.getPrototypeOf(dataset) === DOMStringMap.prototype,
    prototypeParent: Object.getPrototypeOf(DOMStringMap.prototype) === Object.prototype,
    constructor: dataset.constructor === DOMStringMap,
    ownKeys: Object.keys(dataset),
    dataIn: 'longName' in dataset,
    methodIn: 'hasOwnProperty' in dataset,
    missingIn: 'absent' in dataset,
  };
  element.setAttribute('data-has-own-property', 'shadow');
  result.shadowMethod = dataset.hasOwnProperty;
  result.shadowOwn = Object.prototype.hasOwnProperty.call(dataset, 'hasOwnProperty');
  delete dataset.hasOwnProperty;
  result.restoredMethod = dataset.hasOwnProperty === Object.prototype.hasOwnProperty;
  element.setAttribute('data-constructor', 'own-constructor');
  result.shadowConstructor = dataset.constructor;
  element.setAttribute('data-__proto__', 'attribute-value');
  result.protoAttribute = dataset.__proto__;
  result.prototypeUnchanged = Object.getPrototypeOf(dataset) === DOMStringMap.prototype;
  element.getAttribute = () => 'author override';
  result.canonicalRead = dataset.longName;
  const error = call => { try { call(); return null; } catch (error) { return {name: error.name, message: error.message}; } };
  result.construct = error(() => new DOMStringMap());
  return result;
})()
