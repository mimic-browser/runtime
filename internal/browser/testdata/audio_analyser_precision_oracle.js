(async () => {
  const context = new OfflineAudioContext(1, 5000, 44100),
    analyser = context.createAnalyser(),
    compressor = context.createDynamicsCompressor(),
    oscillator = context.createOscillator();

  oscillator.type = 'triangle';
  oscillator.frequency.value = 10000;
  compressor.threshold.value = -50;
  compressor.knee.value = 40;
  compressor.attack.value = 0;
  analyser.connect(context.destination);
  compressor.connect(analyser);
  compressor.connect(context.destination);
  oscillator.connect(compressor);
  oscillator.start(0);

  const rendered = await context.startRendering(),
    frequency = new Float32Array(analyser.frequencyBinCount),
    time = new Float32Array(analyser.fftSize);
  analyser.getFloatFrequencyData(frequency);
  analyser.getFloatTimeDomainData(time);
  const absoluteSum = (values) =>
    Array.from(values).reduce((sum, value) => sum + Math.abs(value), 0);

  return {
    reduction: compressor.reduction.value || compressor.reduction,
    firstRenderedSample: rendered.getChannelData(0)[0],
    renderedPrefix: Array.from(rendered.getChannelData(0).slice(0, 32)),
    frequency: Array.from(frequency),
    frequencyAbsoluteSum: absoluteSum(frequency),
    time: Array.from(time),
    timeAbsoluteSum: absoluteSum(time),
  };
})()
