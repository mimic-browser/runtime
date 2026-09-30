async function hydrateProduct() {
  const query = new URLSearchParams(location.search);
  const response = await fetch(`/state?${query}`);
  if (!response.ok) throw new Error(`State fetch failed: ${response.status}`);
  const state = await response.json();
  await new Promise((resolve) => setTimeout(resolve, 20));
  const product = document.querySelector('#product');
  product.replaceChildren();
  for (const [name, value] of Object.entries(state.product)) {
    const paragraph = document.createElement('p');
    paragraph.className = name;
    paragraph.textContent = value;
    product.append(paragraph);
  }
  const noise = document.createElement('p');
  noise.className = 'noise';
  noise.textContent = `Unwatched counter: ${query.get('noise')}`;
  product.append(noise);
  const catalog = document.querySelector('#catalog');
  for (const item of state.catalog) {
    const article = document.createElement('article');
    const title = document.createElement('h2');
    title.textContent = item.name;
    const description = document.createElement('p');
    description.textContent = item.description;
    article.append(title, description);
    catalog.append(article);
  }
  await Promise.resolve();
  product.dataset.ready = 'true';
}
hydrateProduct().catch((error) => {
  document.body.dataset.error = String(error);
  throw error;
});
