async function xhrCORSObservations(otherOrigin) {
  const request = (url, method = 'GET', withCredentials = false, header = false, upload = false) =>
    new Promise((resolve) => {
      const xhr = new XMLHttpRequest();
      let event = '';
      xhr.open(method, url);
      xhr.withCredentials = withCredentials;
      if (header) xhr.setRequestHeader('X-Probe', 'probe');
      if (upload) xhr.upload.onload = () => {};
      xhr.onload = () => {
        event = 'load';
      };
      xhr.onerror = () => {
        event = 'error';
      };
      xhr.onloadend = () =>
        resolve({
          event,
          status: xhr.status,
          visible: xhr.getResponseHeader('X-Visible'),
          hidden: xhr.getResponseHeader('X-Hidden'),
          cookie: xhr.getResponseHeader('Set-Cookie'),
          cookie2: xhr.getResponseHeader('Set-Cookie2'),
          allLeaksCookie: /set-cookie/i.test(xhr.getAllResponseHeaders()),
          text: xhr.responseText,
        });
      xhr.send(method === 'POST' ? 'body' : null);
    });
  return JSON.stringify({
    sameOrigin: await request('/headers'),
    allowed: await request(otherOrigin + '/allow'),
    denied: await request(otherOrigin + '/deny'),
    preflight: await request(otherOrigin + '/preflight', 'PUT', false, true),
    uploadListener: await request(otherOrigin + '/upload', 'POST', false, false, true),
    credentialWildcard: await request(otherOrigin + '/wildcard', 'GET', true),
  });
}
