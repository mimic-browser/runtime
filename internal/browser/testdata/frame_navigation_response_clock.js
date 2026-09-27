new Promise((resolve) => {
  const start = performance.now();
  const frame = document.createElement('iframe');
  frame.onload = () => {
    const child = frame.contentWindow.performance;
    const entry = child.getEntriesByType('navigation')[0];
    // Absolute timestamp coarsening can move the 5 s interval by one bucket.
    resolve(
      entry.responseEnd >= 4999 &&
        entry.responseEnd <= entry.domInteractive &&
        entry.domInteractive <= entry.loadEventStart &&
        child.now() >= entry.responseEnd &&
        performance.now() >= start + 4999,
    );
  };
  frame.src = 'http://clock.invalid/frame';
  document.body.append(frame);
});
