const sequence = ['start'];
onmessage = () => {
  sequence.push('message');
  postMessage(sequence.join(','));
};
await new Promise((resolve) => setTimeout(resolve, 30));
sequence.push('await');
postMessage(sequence.join(','));
