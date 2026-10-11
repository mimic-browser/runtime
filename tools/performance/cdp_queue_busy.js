(() => {
  const end = performance.now() + 300;
  while (performance.now() < end) {}
  return 42;
})();
