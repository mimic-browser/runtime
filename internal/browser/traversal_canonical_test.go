package browser

import (
	"context"
	"strconv"
	"testing"
)

func TestTraversalCanonicalReadsInRestoredRealm(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	source := `(() => {
  const root=document.createElement('div');
  root.innerHTML='<i></i><b></b>';
  for(const node of [root,...root.querySelectorAll('*')])
    for(const name of ['firstChild','nextSibling','nodeType'])
      Object.defineProperty(node,name,{get(){throw Error('author '+name)}});
  const walker=document.createTreeWalker(root,NodeFilter.SHOW_ELEMENT),seen=[];
  for(let node;(node=walker.nextNode());)seen.push(node.localName);
  root.append(document.createElement('p'));
  walker.currentNode=root;
  const next=[];
  for(let node;(node=walker.nextNode());)next.push(node.localName);
  return seen.join()==='i,b'&&next.join()==='i,b,p';
})()`
	expression := `(() => {const frame=document.createElement('iframe');document.body.append(frame);return frame.contentWindow.eval(` + strconv.Quote(source) + `)})()`
	if got := bootstrapSnapshotEvaluate(t, p, expression); got != true {
		t.Fatalf("restored traversal: %v", got)
	}
	bootstrapSnapshotAssertRestored(t, p, 1)
}

func TestTraversalIgnoresShadowedNodeAccessors(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(() => {
  const root = document.createElement('div');
  root.innerHTML = '<section><i></i><b></b></section><p></p>';
  document.body.append(root);
  const nodes = [root, ...root.querySelectorAll('*')];
  for (const node of nodes)
    for (const name of ['firstChild', 'lastChild', 'nextSibling', 'previousSibling', 'parentNode', 'nodeType'])
      Object.defineProperty(node, name, {get() {throw Error('author ' + name)}});
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT);
  const seen = [];
  for (let node; (node = walker.nextNode());) seen.push(node.localName);
  if (seen.join() !== 'section,i,b,p') return 'forward:' + seen;
  if (walker.previousNode().localName !== 'b') return 'reverse';
  walker.currentNode = root;
  if (walker.firstChild().localName !== 'section' || walker.nextSibling().localName !== 'p') return 'siblings';
  const iterator = document.createNodeIterator(root, NodeFilter.SHOW_ELEMENT);
  let count = 0;
  while (iterator.nextNode()) count++;
  return count === 5;
})()`)
		if err != nil || got != true {
			t.Fatalf("canonical traversal: %v %v", got, err)
		}
	})
}

func TestTraversalSeesFilterMutationsAndRepeatedWalks(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(() => {
  const template = document.createElement('template');
  template.innerHTML = '<i></i><b></b>';
  const roots = [template.content, document.createElement('div')];
  roots[1].innerHTML = '<i></i><b></b>';
  for (const root of roots) {
    const walk = () => {
      const w = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT), names = [];
      for (let n; (n = w.nextNode());) names.push(n.localName);
      return names.join();
    };
    if (walk() !== 'i,b' || walk() !== 'i,b') return 'warm';
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT, node => {
      if (node.localName === 'i') root.append(document.createElement('p'));
      return NodeFilter.FILTER_ACCEPT;
    });
    const names = [];
    for (let node; (node = walker.nextNode());) names.push(node.localName);
    if (names.join() !== 'i,b,p') return 'filter:' + names;
    root.firstChild.remove();
    if (walk() !== 'b,p') return 'removal';
    root.prepend(document.createElement('u'));
    if (walk() !== 'u,b,p') return 'prepend';
  }
  return true;
})()`)
		if err != nil || got != true {
			t.Fatalf("live traversal: %v %v", got, err)
		}
	})
}
