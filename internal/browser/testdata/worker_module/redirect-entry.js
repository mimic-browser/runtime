import { ordinal as first, url as firstURL, value } from '/alias-a.js';
import { ordinal as second, url as secondURL } from '/alias-b.js';
postMessage({
  first,
  second,
  value,
  entryURL: import.meta.url.endsWith('/module/redirect-entry.js'),
  location: location.href.endsWith('/module/redirect-entry.js'),
  firstURL: firstURL.endsWith('/module/shared.js'),
  secondURL: firstURL === secondURL,
});
