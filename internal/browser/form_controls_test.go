package browser

import (
	"context"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"testing"
)

func TestTableRowsIsLiveAndExcludesNestedTables(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), `(()=>{
document.body.innerHTML='<table id="outer"><tbody><tr id="first"><th>x</th></tr><tr id="second"><td><table><tbody><tr id="nested"><td>y</td></tr></tbody></table></td></tr></tbody></table>';
const table=document.getElementById('outer'),rows=table.rows,third=document.createElement('tr');
table.querySelector('tbody').appendChild(third);
return rows===table.rows&&rows instanceof HTMLCollection&&rows.length===3&&rows[0].id==='first'&&rows.item(1).id==='second'&&rows[2]===third&&!Array.from(rows).includes(document.getElementById('nested'));
})()`)
		if err != nil || value != true {
			t.Fatalf("table rows collection: %v %v", value, err)
		}
	})
}

func TestFormControlAttributeReadsIgnoreAuthorMethods(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), `(() => {
 document.body.innerHTML='<form><input name="entry" value="text"><input type="checkbox" name="on" checked><select name="choice" size="invalid"><option value="a">A</option><option>B</option></select></form>';
 const form=document.querySelector('form'),input=form.firstElementChild,check=input.nextElementSibling,select=form.lastElementChild;
 const get=Element.prototype.getAttribute,has=Element.prototype.hasAttribute,fail=()=>{throw Error('author attribute method')};
 Element.prototype.getAttribute=Element.prototype.hasAttribute=fail;
 const reflected=input.name==='entry'&&input.type==='text'&&input.value==='text'&&check.checked&&select.size===0&&select.selectedIndex===0&&select.value==='a';
 Element.prototype.getAttribute=get;Element.prototype.hasAttribute=has;
 for(const control of [input,check,select,...select.options])control.getAttribute=control.hasAttribute=fail;
 return reflected&&JSON.stringify(Array.from(new FormData(form)))==='[["entry","text"],["on","on"],["choice","a"]]';
})()`)
		if err != nil || value != true {
			t.Fatalf("private form attribute reads: %v %v", value, err)
		}
	})
}

func TestFormControlDirtyValuesAndReset(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const f=document.createElement('form');f.innerHTML='<input value="server"><input type="checkbox" checked><textarea>initial</textarea><select><option>A</option><option selected>B</option></select>';document.body.appendChild(f);
 const [input,check]=f.querySelectorAll('input'),area=f.querySelector('textarea'),select=f.querySelector('select');
 input.value='edit';input.defaultValue='new';check.checked=false;area.value='edit';area.defaultValue='new';select.value='A';
 const dirty=input.value==='edit'&&input.getAttribute('value')==='new'&&!check.checked&&check.defaultChecked&&area.value==='edit'&&area.defaultValue==='new'&&select.selectedIndex===0;
 f.reset();return dirty&&input.value==='new'&&check.checked&&area.value==='new'&&select.value==='B'&&select.options.selectedIndex===1;
 })()`)
	if err != nil || value != true {
		t.Fatalf("dirty form state: %v %v", value, err)
	}
}

func TestFormControlInitialValueAndDirectChildText(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>{
 const input=document.createElement('input');let query='';if(input.value!==query)query=input.value;
 const initial=query.length===0&&input.defaultValue===''&&input.checked===false&&input.defaultChecked===false;
 const textarea=document.createElement('textarea');textarea.appendChild(document.createTextNode('direct'));const child=document.createElement('span');child.textContent='nested';textarea.appendChild(child);
 const select=document.createElement('select');select.innerHTML='<option value="a">A</option><option value="b">B</option>';select.selectedIndex=-1;const none=select.value===''&&select.selectedIndex===-1;select.remove(1);
 return initial&&textarea.defaultValue==='direct'&&textarea.value==='direct'&&none&&select.selectedIndex===0;
 })()`)
	if err != nil || value != true {
		t.Fatalf("initial/child-text controls: %v %v", value, err)
	}
}

func TestFormAssociationCollectionsAndFormData(t *testing.T) {
	parallelBrowserTest(t)
	p := validationPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{
 document.body.innerHTML='<input id="before" form="target" name="same" value="external"><form id="target"><fieldset id="group"><input id="inside" name="same" value="inside"><input name="off" disabled value="disabled"><input type="checkbox" name="checks" value="yes" checked><input type="checkbox" name="checks" value="no"><select name="choice" multiple><option selected>A</option><optgroup disabled><option selected>B</option></optgroup><option selected>C</option></select><button id="send" name="commit" value="save">Save</button></fieldset></form>';
 const form=document.getElementById('target'),before=document.getElementById('before'),group=document.getElementById('group'),inside=document.getElementById('inside'),send=document.getElementById('send'),events=[];
 form.addEventListener('formdata',event=>{events.push([event.formData.get('same'),event.isTrusted]);event.formData.append('added','listener')});
 const data=new FormData(form,send),same=form.elements.same;
 return form instanceof HTMLFormElement&&before.form===form&&group.form===form&&
   form.elements===form.elements&&form.elements.length===8&&form.elements[0]===before&&form.elements.namedItem('inside')===inside&&
   same.length===2&&same[0]===before&&same[1]===inside&&
   JSON.stringify(Array.from(data.entries()))===JSON.stringify([['same','external'],['same','inside'],['checks','yes'],['choice','A'],['choice','C'],['commit','save'],['added','listener']])&&
   JSON.stringify(events)==='[["external",false]]';
 })()`)
	if err != nil || value != true {
		t.Fatalf("form association/FormData: %v %v", value, err)
	}
}

func TestRequestSubmitValidationAndSubmitter(t *testing.T) {
	parallelBrowserTest(t)
	p := validationPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{
 const form=document.createElement('form');form.innerHTML='<input id="entry" required><button id="send" name="go" value="yes">Send</button>';document.body.appendChild(form);
 const entry=form.elements.entry,send=form.elements.send,events=[];
 entry.addEventListener('invalid',event=>events.push(['invalid',event.bubbles,event.cancelable]));
 form.addEventListener('submit',event=>{events.push(['submit',event.submitter===send,event.isTrusted]);event.preventDefault()});
 form.requestSubmit(send);entry.value='ok';form.requestSubmit(send);form.requestSubmit();
 let wrongType='',wrongOwner='';try{form.requestSubmit(entry)}catch(error){wrongType=error.name}
 const other=document.createElement('button');try{form.requestSubmit(other)}catch(error){wrongOwner=error.name}
 return JSON.stringify(events)===JSON.stringify([['invalid',false,true],['submit',true,false],['submit',false,false]])&&wrongType==='TypeError'&&wrongOwner==='NotFoundError';
 })()`)
	if err != nil || value != true {
		t.Fatalf("requestSubmit lifecycle: %v %v", value, err)
	}
}

func TestDetachedFormAssociationCollectionAndData(t *testing.T) {
	parallelBrowserTest(t)
	p := validationPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{
 const form=document.createElement('form');
 form.innerHTML='<input name="x" value="original"><input type="checkbox" name="enabled" checked><input name="ignored" disabled>';
 const input=form.elements.x,data=new FormData(form);
 input.value='changed';form.reset();
 return input.value==='original'&&data.get('x')==='original'&&data.get('enabled')==='on'&&data.get('ignored')===null;
})()`)
	if err != nil || value != true {
		t.Fatalf("detached form association/data/reset: %v %v", value, err)
	}
}
