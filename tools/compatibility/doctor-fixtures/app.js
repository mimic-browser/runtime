document.querySelector('#ready').textContent = 'Ready';
document.querySelector('#run').addEventListener('click', () => {
  document.querySelector('#clicked').textContent = 'Clicked';
  const exception = new DOMException('Local probe', 'InvalidStateError');
  Object.setPrototypeOf(exception, null);
  document.querySelector('#brand').textContent = Object.prototype.toString.call(exception);
});
