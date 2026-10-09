(async () => {
  const descriptor = Object.getOwnPropertyDescriptor(Navigator.prototype, 'webdriver');
  const environment = {
    webdriver: navigator.webdriver,
    webdriverDescriptor: {enumerable: descriptor.enumerable, configurable: descriptor.configurable, getter: Function.prototype.toString.call(descriptor.get)},
    userAgent: navigator.userAgent, languages: Array.from(navigator.languages), platform: navigator.platform,
    secureContext: isSecureContext, crossOriginIsolated, visibilityState: document.visibilityState, hasFocus: document.hasFocus(),
    viewport: {width: innerWidth, height: innerHeight, deviceScaleFactor: devicePixelRatio},
    window: {x: screenX, y: screenY, outerWidth, outerHeight}
  };
  const urls = ['http:\\\\www.google.com\\foo','http://foo:80','http://你好你好/','http://ExAmPlE.CoM/'].map(input => new URL(input).href);
  document.body.innerHTML='<ul><li id="a" class="x"></li><li id="b"></li><li id="c" class="x"></li><li id="d" class="y"></li><li id="e" class="x"></li></ul>';
  const selectors=['li:nth-child(2 of .x)','li:nth-last-child(2 of .x)','li:nth-child(2n of .x,.y)','ul:has(> li:nth-child(3 of .x)) > li:nth-child(-n+2 of :is(.x,.y))'].map(selector=>Array.from(document.querySelectorAll(selector),e=>e.id).join(','));
  const errors = {};
  for (const [name,operation] of Object.entries({
    emptyUsages:()=>crypto.subtle.generateKey({name:'HMAC',hash:'SHA-256'},true,[]),
    emptyRaw:()=>crypto.subtle.importKey('raw',new Uint8Array(),{name:'HMAC',hash:'SHA-256'},false,['sign']),
    partialByte:()=>crypto.subtle.importKey('raw',new Uint8Array([255]),{name:'HMAC',hash:'SHA-256',length:1},true,['sign']),
    hashAlias:()=>crypto.subtle.digest('SHA256',new Uint8Array()),
    hashUnderscore:()=>crypto.subtle.digest('SHA_256',new Uint8Array()),
    aesLength:()=>crypto.subtle.generateKey({name:'AES-GCM',length:129},true,['encrypt'])
  })) {try {const key=await operation();errors[name]=key.algorithm||'ok';}catch(error){errors[name]=error.name;}}
  const hmac=await crypto.subtle.importKey('raw',new Uint8Array([1,2,3]),{name:'HMAC',hash:'SHA-256'},true,['sign','verify']);
  const bytes=new Uint8Array([99,97,98,99,88]);const view=new DataView(bytes.buffer,1,3);
  const signature=await crypto.subtle.sign('HMAC',hmac,view);
  const hmacResult={algorithm:hmac.algorithm,signature:Array.from(new Uint8Array(signature)),valid:await crypto.subtle.verify('HMAC',hmac,signature,view),jwk:await crypto.subtle.exportKey('jwk',hmac)};
  const aes = [];
  for (const tagLength of [32,64,96,104,112,120,128]) {
    const key=await crypto.subtle.importKey('raw',new Uint8Array(16),'AES-GCM',true,['encrypt','decrypt']);
    const algorithm={name:'AES-GCM',iv:new Uint8Array(16),tagLength,additionalData:new Uint8Array([1,2])};
    const ciphertext=await crypto.subtle.encrypt(algorithm,key,view);
    aes.push({tagLength,ciphertext:Array.from(new Uint8Array(ciphertext)),plaintext:Array.from(new Uint8Array(await crypto.subtle.decrypt(algorithm,key,ciphertext)))});
  }
  return {environment,urls,selectors,errors,hmacResult,aes};
})()