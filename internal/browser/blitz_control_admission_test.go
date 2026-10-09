package browser

import (
	"context"
	"testing"
)

func TestBlitzControlContentAdmission(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_STYLE_ENGINE", "blitz")
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		for _, tc := range []struct {
			name, script string
			fallback     bool
		}{
			{"empty", `document.body.innerHTML='<div id="target" style="width:20px;height:10px"></div><textarea id="area"></textarea><input id="field"><select id="pick"><option>A</option></select>';`, false},
			{"textarea-default", `document.getElementById('area').textContent='one\ntwo';`, true},
			{"textarea-default-repair", `document.getElementById('area').textContent='';`, false},
			{"textarea-live", `document.getElementById('area').value='one\ntwo';`, true},
			{"textarea-live-repair", `document.getElementById('area').value='';`, false},
			{"input-live", `document.getElementById('field').value='search';`, false},
			{"input-live-repair", `document.getElementById('field').value='';`, false},
			{"listbox-size", `document.getElementById('pick').setAttribute('size','2');`, true},
			{"listbox-size-repair", `document.getElementById('pick').removeAttribute('size');`, false},
			{"listbox-multiple", `document.getElementById('pick').setAttribute('multiple','');`, true},
			{"listbox-multiple-repair", `document.getElementById('pick').removeAttribute('multiple');`, false},
			{"video", `document.body.append(document.createElement('video'));`, true},
			{"video-repair", `document.querySelector('video').remove();`, false},
			{"no-author-getters", `for(const id of ['area','field','pick']){const e=document.getElementById(id);Object.defineProperty(e,'value',{get(){throw Error('author value')}});e.getAttribute=()=>{throw Error('author attribute')};}document.getElementById('target').setAttribute('data-admission','refresh');`, false},
			{"no-author-selection-getters", `const pick=document.getElementById('pick');for(const key of ['size','multiple'])Object.defineProperty(pick,key,{get(){throw Error('author '+key)}});const option=pick.firstElementChild;for(const key of ['getAttribute','hasAttribute'])option[key]=()=>{throw Error('author option attribute')};Object.defineProperty(option,'disabled',{get(){throw Error('author disabled')}});document.getElementById('target').setAttribute('data-admission','selection-refresh');`, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := p.Evaluate(context.Background(), `(()=>{`+tc.script+`return document.getElementById('target').getBoundingClientRect().width})()`)
				if err != nil {
					t.Fatal(err)
				}
				state := p.Top.Realm.blitz
				if state == nil {
					t.Fatal("missing admission result")
				}
				if (state.fallback != "") != tc.fallback {
					t.Fatalf("fallback=%q want=%v", state.fallback, tc.fallback)
				}
				if !tc.fallback && state.document.Owner == nil {
					t.Fatal("supported control lost native owner")
				}
			})
		}
	})
}
