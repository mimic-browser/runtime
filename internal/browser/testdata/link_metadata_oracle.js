// prettier-ignore
(async () => {
  const relations = ['', 'alternate', 'canonical', 'author', 'dns-prefetch', 'preconnect', 'stylesheet-other'];
  const links = [];
  const events = [];
  for (let index = 0; index < relations.length; index++) {
    const link = document.createElement('link');
    link.href = '/inert-link-' + index;
    link.rel = relations[index];
    if (link.rel === 'alternate') link.hreflang = 'en';
    link.addEventListener('load', () => events.push(index + ':load'));
    link.addEventListener('error', () => events.push(index + ':error'));
    document.head.append(link);
    links.push(link);
  }
  await new Promise(resolve => setTimeout(resolve, 100));
  const result = {
    events,
    resourceEntries: links.map(link => performance.getEntriesByName(link.href).length),
    connected: links.map(link => link.isConnected),
  };
  for (const link of links) link.remove();
  return result;
})()
