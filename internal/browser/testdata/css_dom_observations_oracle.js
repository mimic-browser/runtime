(async () => {
  document.body.innerHTML='<style>#label::before{content:attr(data-label) " suffix";color:red}#label::after{content:"after"} @property --pi {syntax:"<length>";inherits:true;initial-value:1px} @property --pn {syntax:"<length>";inherits:false;initial-value:1px} #parent{--pi:5px;--pn:5px}</style><div id="label" data-label="first"></div><div id="parent"><span id="child"></span></div>';
  const label=document.getElementById('label'), child=document.getElementById('child');
  const before=getComputedStyle(label,'::before');
  const pseudo=[before.content,before.color,getComputedStyle(label,':after').content];
  label.setAttribute('data-label','next');pseudo.push(before.content);
  const registered=[getComputedStyle(child).getPropertyValue('--pi'),getComputedStyle(child).getPropertyValue('--pn')];
  CSS.registerProperty({name:'--size',syntax:'<length>',inherits:false,initialValue:'10px'});
  label.style.setProperty('--size','banana');registered.push(getComputedStyle(label).getPropertyValue('--size'));
  label.style.setProperty('--size','30px');registered.push(getComputedStyle(label).getPropertyValue('--size'));
  const errors=[];
  for (const descriptor of [
    {name:'--size',syntax:'*',inherits:true},
    {name:'bad',syntax:'*',inherits:true},
    {name:'--relative',syntax:'<length>',inherits:false,initialValue:'1em'},
    {name:'--missing',syntax:'<length>',inherits:false},
    {name:'--any',inherits:false}
  ]) {try {CSS.registerProperty(descriptor);errors.push('ok');}catch(error){errors.push(error.name);}}
  label.style.fontSize='21px';
  const map=label.computedStyleMap(),size=map.get('font-size');
  const typed={map:Object.prototype.toString.call(map),size:Object.prototype.toString.call(size),value:size.value,unit:size.unit,text:String(size),sizeIdentity:map.get('font-size')===size,has:map.has('font-size'),all:map.getAll('font-size').map(String)};
  label.style.fontSize='25px';typed.live=String(map.get('font-size'));
  const host=document.createElement('div'),root=host.attachShadow({mode:'open'});document.body.appendChild(host);
  const sheet=new CSSStyleSheet();sheet.replaceSync(':host{--value:72%;color:rgb(1,2,3)} p{font-size:21px}');root.adoptedStyleSheets=[sheet];root.innerHTML='<p>child</p>';
  const shadow=[getComputedStyle(host).getPropertyValue('--value'),getComputedStyle(host).color,getComputedStyle(root.querySelector('p')).fontSize];
  const rangeParent=document.createElement('div');rangeParent.innerHTML='Pre<span>MID</span>Post';document.body.appendChild(rangeParent);const span=rangeParent.querySelector('span'),range=document.createRange();range.selectNode(span);const wrapper=document.createElement('b');range.surroundContents(wrapper);
  const rangeResult={html:rangeParent.innerHTML,identity:wrapper.firstChild===span,text:range.toString(),start:range.startOffset,end:range.endOffset};
  const textParent=document.createElement('div');textParent.textContent='abcde';document.body.appendChild(textParent);const textRange=document.createRange();textRange.setStart(textParent.firstChild,1);textRange.setEnd(textParent.firstChild,4);const italic=document.createElement('i');textRange.surroundContents(italic);rangeResult.textHTML=textParent.innerHTML;
  const partial=document.createRange();partial.setStart(rangeParent.firstChild,1);partial.setEnd(span.firstChild,1);try {partial.surroundContents(document.createElement('u'));rangeResult.partial='ok';}catch(error){rangeResult.partial=error.name;}
  return {pseudo,registered,errors,typed,shadow,rangeResult};
})()