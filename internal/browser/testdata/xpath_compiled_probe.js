(function () {
  document.body.innerHTML =
    '<section id="outside" data-action-out="1"></section><main id="root"><div id="first" data-action-one="1"></div><div id="none" aria-label="no"></div><p id="second" x-on:click="1"></p></main><pre id="out"></pre>';
  const output = {};
  function attempt(name, fn) {
    try {
      output[name] = fn();
    } catch (error) {
      output[name] = { name: error.name, message: error.message };
    }
  }
  const expression =
    './/*[@*[starts-with(name(), "data-action-") or starts-with(name(), "x-on:")]]';
  attempt('constructor', () => {
    const evaluator = new XPathEvaluator();
    return [
      evaluator instanceof XPathEvaluator,
      Object.getPrototypeOf(evaluator) === XPathEvaluator.prototype,
      Object.prototype.toString.call(evaluator),
    ];
  });
  attempt('compiled', () => {
    const compiled = new XPathEvaluator().createExpression(expression);
    const result = compiled.evaluate(document.querySelector('#root'));
    return [
      compiled instanceof XPathExpression,
      result instanceof XPathResult,
      result.resultType,
      result.iterateNext().id,
      result.iterateNext().id,
      result.iterateNext(),
    ];
  });
  attempt('documentCompiled', () => {
    const compiled = document.createExpression(expression, null);
    const result = compiled.evaluate(
      document.querySelector('#root'),
      XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,
      null,
    );
    return [
      result.snapshotLength,
      result.snapshotItem(0) === document.querySelector('#first'),
      result.snapshotItem(1) === document.querySelector('#second'),
    ];
  });
  attempt('documentEvaluate', () => {
    const result = document.evaluate(
      expression,
      document.querySelector('#root'),
      null,
      XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,
      null,
    );
    return [result.snapshotLength, result.snapshotItem(0).id, result.snapshotItem(1).id];
  });
  attempt('emptyRootValidation', () => new XPathEvaluator().createExpression('//*['));
  attempt('illegalExpressionConstructor', () => new XPathExpression());
  attempt('constructorCall', () => XPathEvaluator());
  attempt('illegalReceiver', () => XPathEvaluator.prototype.createExpression.call({}, '//*'));
  attempt('missingExpression', () => new XPathEvaluator().createExpression());
  attempt('metadata', () => [
    XPathEvaluator.length,
    XPathEvaluator.prototype.createExpression.length,
    XPathEvaluator.prototype.evaluate.length,
    XPathExpression.prototype.evaluate.length,
    document.createExpression.length,
  ]);
  document.querySelector('#out').textContent = JSON.stringify(output);

  return JSON.stringify(output);
})();
