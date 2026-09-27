package browser

// The factory is captured before user scripts execute. Each debugger session
// owns a separate closure and strong-reference table. No debugger bookkeeping
// is installed on window, and property inspection never calls user getters.
const debuggerFactorySource = `(() => {
  const apply = Reflect.apply, keys = Reflect.ownKeys, descriptor = Object.getOwnPropertyDescriptor;
  const proto = Object.getPrototypeOf, create = Object.create, define = Object.defineProperty;
  const isArray = Array.isArray, stringify = JSON.stringify, parse = JSON.parse;
  const numberString = Number.prototype.toString, functionString = Function.prototype.toString;
  const string = String, bigint = BigInt, is = Object.is;
  const mapGet = Map.prototype.get, mapSet = Map.prototype.set, mapDelete = Map.prototype.delete;
  const mapKeys = Map.prototype.keys, mapClear = Map.prototype.clear;
  const iteratorNext = proto(new Map().keys()).next;
  const mapSize = descriptor(Map.prototype, 'size').get, setSize = descriptor(Set.prototype, 'size').get;
  const dateTime = Date.prototype.getTime, dateString = Date.prototype.toString;
  const regexpSource = descriptor(RegExp.prototype,'source').get, regexpString = RegExp.prototype.toString;
  const arrayBufferLength = descriptor(ArrayBuffer.prototype,'byteLength').get;
  const typedArrayLength = descriptor(proto(Uint8Array.prototype),'length').get;
  const typedArrayName = descriptor(proto(Uint8Array.prototype),Symbol.toStringTag).get;
  const symbolString = Symbol.prototype.toString;
  const MapClass=Map,SetClass=Set,ErrorClass=Error,finite=Number.isFinite,setPrototype=Object.setPrototypeOf;
  const setHas=Set.prototype.has,setAdd=Set.prototype.add,indexOf=Array.prototype.indexOf,pop=Array.prototype.pop;
  const regexTest=RegExp.prototype.test;
  const errorStackGetter=descriptor(new ErrorClass(),'stack').get;
  return (node, prefix) => {
  const objects = new MapClass(); let sequence = 0;
  const get = id => { const entry=apply(mapGet,objects,[id]); if(!entry)throw new ErrorClass('Could not find object with given id'); return entry; };
  const plain = () => create(null);
  const put = (o,k,v) => define(o,k,{value:v,enumerable:true,writable:true,configurable:true});
  const array = () => setPrototype([],null), append = (a,v) => put(a,string(a.length),v);
  const envelope = (key,value) => {const result=plain();result[key]=value;return stringify(result)};
  const scalar = value => {
    const type=typeof value, result=plain(); result.type=type;
    if(value===null){result.type='object';result.subtype='null';result.value=null;return result}
    if(type==='undefined')return result;
    if(type==='string'||type==='boolean'){result.value=value;return result}
    if(type==='number'){
      result.description=apply(numberString,value,[]);
      if(is(value,-0))result.description='-0';
      if(!finite(value)||is(value,-0))result.unserializableValue=result.description;
      else result.value=value;
      return result;
    }
    if(type==='bigint'){result.unserializableValue=string(value)+'n';result.description=result.unserializableValue;return result}
    return null;
  };
  const clone = (value, chain, depth) => {
    if(value===null)return null;
    const type=typeof value;
    if(type==='bigint'||type==='symbol')throw new ErrorClass("Object couldn't be returned by value");
    if(type==='undefined')return undefined;
    if(type==='number')return finite(value)?(is(value,-0)?0:value):null;
    if(type!=='object'&&type!=='function')return value;
    if(depth>1000||apply(indexOf,chain,[value])!==-1)throw new ErrorClass('Object reference chain is too long');
    append(chain,value); const result=isArray(value)?array():plain();
    if(isArray(value)){
      const length=value.length;
      for(let i=0;i<length;i++){const next=clone(value[i],chain,depth+1);put(result,string(i),next===undefined?null:next)}
    }else{
      const names=keys(value);for(let index=0;index<names.length;index++){const key=names[index];
        if(typeof key!=='string')continue;
        const desc=descriptor(value,key); if(!desc||!desc.enumerable)continue;
        const next=clone(value[key],chain,depth+1); if(next!==undefined)put(result,key,next);
      }
    }
    apply(pop,chain,[]); return result;
  };
  const describe = (value, group, byValue) => {
    let result=scalar(value);if(result)return result;
    result=plain();result.type=typeof value;
    if(byValue){result.value=clone(value,array(),0);return result}
    let className='Object', description='Object', subtype;
    if(typeof value==='symbol'){description=apply(symbolString,value,[])}
    else if(typeof value==='function'){className='Function';description=apply(functionString,value,[])}
    else {
      let cursor=proto(value);
      for(let count=0;cursor&&count<100;count++,cursor=proto(cursor)){
        const c=descriptor(cursor,'constructor');
        if(c&&typeof c.value==='function'){
          const n=descriptor(c.value,'name');if(n&&typeof n.value==='string'&&n.value){className=n.value;break}
        }
      }
      description=className;
      const nodeID=node(value);
      if(nodeID){
        subtype='node';
        if(value.nodeType===9)description='#document';
        else if(value.nodeType===3)description='#text';
        else if(value.nodeType===8)description='<!--'+value.nodeValue+'-->';
        else if(value.nodeType===11)description=value.host?'#document-fragment':'#document-fragment';
        else {description=string(value.nodeName).toLowerCase(); if(value.id)description+='#'+value.id;
          if(typeof value.className==='string'&&value.className.trim())description+='.'+value.className.trim().split(/\s+/).join('.')}
      }else if(isArray(value)){subtype='array';className='Array';description='Array('+value.length+')'}
      else if(className==='Promise')subtype='promise';
      else if(className==='WeakMap')subtype='weakmap';
      else if(className==='WeakSet')subtype='weakset';
      else {
        let branded=false;
        try {const size=apply(mapSize,value,[]);subtype='map';className='Map';description='Map('+size+')';branded=true}catch(_){}
        if(!branded)try {const size=apply(setSize,value,[]);subtype='set';className='Set';description='Set('+size+')';branded=true}catch(_){}
        if(!branded)try {apply(dateTime,value,[]);subtype='date';className='Date';description=apply(dateString,value,[]);branded=true}catch(_){}
        if(!branded)try {apply(regexpSource,value,[]);subtype='regexp';className='RegExp';description=apply(regexpString,value,[]);branded=true}catch(_){}
        if(!branded)try {const size=apply(arrayBufferLength,value,[]);subtype='arraybuffer';className='ArrayBuffer';description='ArrayBuffer('+size+')';branded=true}catch(_){}
        if(!branded)try {const size=apply(typedArrayLength,value,[]);className=apply(typedArrayName,value,[]);subtype='typedarray';description=className+'('+size+')';branded=true}catch(_){}
        if(!branded&&apply(regexTest,/Error$/,[className])){
          subtype='error';const stack=descriptor(value,'stack'),message=descriptor(value,'message');
          description=stack&&typeof stack.value==='string'?stack.value:className+(message&&typeof message.value==='string'?': '+message.value:'');
        }
      }
    }
    if(typeof value!=='symbol')result.className=className;
    if(subtype)result.subtype=subtype;result.description=description;
    const id=prefix+'.'+(++sequence);apply(mapSet,objects,[id,{value,group}]);result.objectId=id;
    return result;
  };
  const argument = a => {
    if(a.objectId!==undefined)return get(a.objectId).value;
    if(a.unserializableValue!==undefined){
      const value=a.unserializableValue;
      if(value==='NaN')return NaN;if(value==='Infinity')return Infinity;if(value==='-Infinity')return -Infinity;if(value==='-0')return -0;
      if(apply(regexTest,/^-?\d+n$/,[value]))return bigint(value.slice(0,-1));throw new ErrorClass('Unserializable value: '+value);
    }
    return a.value;
  };
  return function(operation, encoded, value){
    const p=parse(encoded);
    if(operation==='lookup')return get(p.objectId).value;
    if (operation === 'prepareCall') {
      const values = array(),
        supplied = p.arguments || [];
      append(values, p.objectId ? get(p.objectId).value : globalThis);
      for (let index = 0; index < supplied.length; index++) append(values, argument(supplied[index]));
      return values;
    }
    if(operation==='release'){apply(mapDelete,objects,[p.objectId]);return '{}'}
    if(operation==='clear'){apply(mapClear,objects,[]);return '{}'}
    if(operation==='releaseGroup'){
      const iterator=apply(mapKeys,objects,[]);for(;;){const step=apply(iteratorNext,iterator,[]);if(step.done)break;
        if(get(step.value).group===p.objectGroup)apply(mapDelete,objects,[step.value])}return '{}';
    }
    if(operation==='group')return envelope('group',get(p.objectId).group);
    if(operation==='node')return envelope('nodeId',node(get(p.objectId).value));
    if(operation==='resolve')return envelope('result',describe(node(p.nodeId,true),p.objectGroup||'',false));
    if(operation==='hold')return envelope('result',describe(value,p.objectGroup||'',!!p.returnByValue));
    if(operation==='console'){
      const args=array();for(let i=0;i<value.length;i++){
        const item=describe(value[i],'console',false);
        // Materialize V8's lazy Error stack for the Runtime preview. This can
        // read Error.name, but author accessors and object values are not coerced.
        if(item.subtype==='error'){
          const preview=plain();preview.type='object';preview.subtype='error';preview.description=item.description;preview.overflow=false;preview.properties=array();
          const stack=plain();stack.name='stack';
          stack.type='accessor';const d=descriptor(value[i],'stack');
          if(d)try {
            if('value' in d||d.get===errorStackGetter){
              const v='value' in d?d.value:apply(errorStackGetter,value[i],[]);
              stack.type=v===null?'object':typeof v;
              stack.value=typeof v==='object'&&v!==null?'Object':typeof v==='function'?'':string(v);
            }
          }catch(_){}
          append(preview.properties,stack);item.preview=preview;
        }
        append(args,item);
      }return envelope('args',args);
    }
    if(operation==='properties'){
      const entry=get(p.objectId), value=entry.value, result=array(), seen=new SetClass();
      let cursor=value;while(cursor!==null){
        const names=keys(cursor);for(let index=0;index<names.length;index++){const key=names[index];
          if(apply(setHas,seen,[key]))continue;apply(setAdd,seen,[key]);
          if(p.nonIndexedPropertiesOnly&&typeof key==='string'&&apply(regexTest,/^(0|[1-9]\d*)$/,[key]))continue;
          const desc=descriptor(cursor,key);if(!desc)continue;
          if(p.accessorPropertiesOnly&&('value' in desc))continue;
          const out=plain();out.name=typeof key==='symbol'?apply(symbolString,key,[]):key;
          if(typeof key==='symbol')out.symbol=describe(key,entry.group,false);
          out.configurable=!!desc.configurable;out.enumerable=!!desc.enumerable;out.isOwn=cursor===value;
          if('value' in desc){out.value=describe(desc.value,entry.group,false);out.writable=!!desc.writable}
          else {out.get=describe(desc.get,entry.group,false);out.set=describe(desc.set,entry.group,false)}
          append(result,out);
        }
        if(p.ownProperties)break;cursor=proto(cursor);
      }
      const response=plain();response.result=result;const parent=proto(value);
      if(parent!==null){response.internalProperties=array();const property=plain();property.name='[[Prototype]]';property.value=describe(parent,entry.group,false);append(response.internalProperties,property)}
      return stringify(response);
    }
    throw new ErrorClass('Unknown debugger operation');
  };
  };
})()`
