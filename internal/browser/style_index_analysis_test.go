package browser

import "testing"

func TestStyleIndexAnalysesPreserveSelectorDependencies(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		historyEval(t, p, `(() => {
  CSS.registerProperty({
    name: '--index-probe',
    syntax: '<number>',
    inherits: false,
    initialValue: '0',
  });
  const sheet = document.createElement('style');
  sheet.textContent = [
    '.probe { width: 10px; }',
    '#scope[data-wide="on"] > .probe[data-mode="on"] { width: 20px; }',
    '#scope:has(> .signal) > .probe { width: 30px; }',
    '.peer + .probe { width: 40px; }',
    '.probe:is(.chosen, .unused) { width: 50px; }',
    '.escaped\\:name { width: 60px; }',
    '.probe:unknown-selector { width: 999px; }',
  ].join('\n');
  document.head.append(sheet);
  document.body.innerHTML = '<div id="scope"><i></i><div class="probe"></div></div>';
  const scope = document.getElementById('scope');
  const node = scope.lastElementChild;
  const peer = scope.firstElementChild;
  const widths = [];
  const read = () => widths.push(getComputedStyle(node).width);
  read();
  scope.setAttribute('data-wide', 'on');
  node.setAttribute('data-mode', 'on');
  read();
  peer.className = 'signal';
  read();
  scope.removeAttribute('data-wide');
  peer.className = 'peer';
  read();
  node.classList.add('chosen');
  read();
  node.classList.remove('chosen');
  peer.className = '';
  node.classList.add('escaped:name');
  read();
  sheet.sheet.cssRules[5].style.width = '70px';
  read();
  node.classList.remove('escaped:name', 'chosen');
  scope.setAttribute('data-wide', 'on');
  peer.remove();
  read();
  scope.removeAttribute('data-wide');
  read();
  return widths.join('|');
})();`, "10px|20px|30px|40px|50px|60px|70px|20px|10px")
	})
}
