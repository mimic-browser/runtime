(async () => {
 const labels=['utf8',' ascii ','latin1','GBK','shift-jis','utf-16','utf-16be','big5','euc-kr','gb18030','iso-2022-jp','x-user-defined'];
 const encodings=labels.map(label=>new TextDecoder(label).encoding);
 const cases=[['gbk',[0xc4,0xe3,0xba,0xc3]],['shift_jis',[0x82,0xa0,0x82,0xa2]],['gb18030',[0x84,0x31,0xa4,0x37]],['utf-16le',[0xff,0xfe,0x61,0,0xfd,0xff]],['utf-16be',[0xfe,0xff,0,0x61,0xff,0xfd]],['iso-2022-jp',[0x1b,0x24,0x42,0x24,0x22,0x24,0x24,0x1b,0x28,0x42,0x41]],['x-user-defined',[0x41,0x80,0xff]]];
 const decoded=cases.map(([encoding,bytes])=>{
  const decoder=new TextDecoder(encoding),chunks=[];
  for(const byte of bytes)chunks.push(decoder.decode(new Uint8Array([byte]),{stream:true}));chunks.push(decoder.decode());
  let fatal;try {fatal=new TextDecoder(encoding,{fatal:true}).decode(new Uint8Array(bytes));}catch(error){fatal=error.name;}
  return {encoding,decoded:new TextDecoder(encoding).decode(new Uint8Array(bytes)),stream:chunks.join(''),fatal};
 });
 const malformed=[['gbk',[0xff]],['shift_jis',[0x82]],['utf-16le',[0x61]],['gb18030',[0x84,0x31]],['iso-2022-jp',[0x1b,0x24]]].map(([name,bytes])=>{try {new TextDecoder(name,{fatal:true}).decode(new Uint8Array(bytes));return 'ok';}catch(error){return error.name;}});
 const hex=value=>Array.from(new Uint8Array(value),b=>b.toString(16).padStart(2,'0')).join('');
 const cryptography={};
 const base=await crypto.subtle.importKey('raw',new TextEncoder().encode('password'),'PBKDF2',false,['deriveBits','deriveKey']);
 const pb={name:'PBKDF2',hash:'SHA-256',salt:new TextEncoder().encode('salt'),iterations:2};cryptography.pbkdf2=hex(await crypto.subtle.deriveBits(pb,base,256));
 const derived=await crypto.subtle.deriveKey(pb,base,{name:'AES-GCM',length:128},true,['encrypt','decrypt']);cryptography.derived=hex(await crypto.subtle.exportKey('raw',derived));
 const hk=await crypto.subtle.importKey('raw',new Uint8Array(22).fill(0x0b),'HKDF',false,['deriveBits']);cryptography.hkdf=hex(await crypto.subtle.deriveBits({name:'HKDF',hash:'SHA-256',salt:new Uint8Array([0,1,2,3,4,5,6,7,8,9,10,11,12]),info:new Uint8Array([0xf0,0xf1,0xf2,0xf3,0xf4,0xf5,0xf6,0xf7,0xf8,0xf9])},hk,336));
 for(const name of ['AES-CBC','AES-CTR']){
  const key=await crypto.subtle.importKey('raw',new Uint8Array(16),name,true,['encrypt','decrypt']);const algorithm=name==='AES-CBC'?{name,iv:new Uint8Array(16)}:{name,counter:new Uint8Array(16),length:7};
  const ciphertext=await crypto.subtle.encrypt(algorithm,key,new Uint8Array([1,2,3]));cryptography[name]={ciphertext:hex(ciphertext),plain:hex(await crypto.subtle.decrypt(algorithm,key,ciphertext))};
 }
 const wrapping=await crypto.subtle.importKey('raw',new Uint8Array(16),'AES-KW',false,['wrapKey','unwrapKey']);const wrapped=await crypto.subtle.wrapKey('raw',derived,wrapping,'AES-KW');cryptography.kw=hex(wrapped);const unwrapped=await crypto.subtle.unwrapKey('raw',wrapped,wrapping,'AES-KW','AES-GCM',true,['encrypt']);cryptography.unwrapped=hex(await crypto.subtle.exportKey('raw',unwrapped));
 const hmac=await crypto.subtle.importKey('raw',new Uint8Array([255]),{name:'HMAC',hash:'SHA-256',length:1},true,['sign']);cryptography.partial={raw:hex(await crypto.subtle.exportKey('raw',hmac)),signature:hex(await crypto.subtle.sign('HMAC',hmac,new Uint8Array()))};
 return {encodings,decoded,malformed,cryptography};
})()
