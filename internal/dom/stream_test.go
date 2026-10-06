package dom

import (
	"errors"
	"strings"
	"testing"
)

func newTestStream(t *testing.T) (*Document, *Stream) {
	t.Helper()
	d, err := Parse("<!doctype html><body><p id=old>old</p></body>")
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.NewStream()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Abort)
	return d, s
}
func TestStreamPreservesDocumentAndDetachedNodeIdentity(t *testing.T) {
	d, _ := Parse("<body><p id=old>old</p>")
	root := d.Root().ID
	old, _ := d.FindWithin(d.Root().ID, "#old")
	s, err := d.NewStream()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Abort()
	if d.Root().ID != root || d.IsConnected(old.ID) {
		t.Fatal("document/root identity or detach")
	}
	if _, ok := d.Get(old.ID); !ok {
		t.Fatal("old node was destroyed")
	}
	if err := s.Write("<body><p id=new>new", nil); err != nil {
		t.Fatal(err)
	}
	if d.Root().ID != root {
		t.Fatal("root replaced")
	}
	found, ok := d.FindWithin(d.Root().ID, "#new")
	if !ok || d.TextContent(found.ID) != "new" {
		t.Fatalf("write not synchronously visible: %#v", found)
	}
	if found.TagName != "P" || found.QualifiedName != "" {
		t.Fatalf("HTML node name casing: %#v", found)
	}
}

func TestDocumentQueriesExcludeRetainedDetachedTrees(t *testing.T) {
	d, _ := Parse("<body><section id=old><b id=inside>inside</b></section></body>")
	old, _ := d.Find("#old")
	s, err := d.NewStream()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Abort()
	if err := s.Write("<body><b id=new>new</b>", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.FindWithin(d.Root().ID, "#old"); ok {
		t.Fatal("document query found old detached node")
	}
	if _, ok := d.FindWithin(d.Root().ID, "#inside"); ok {
		t.Fatal("document query found old detached descendant")
	}
	if nodes := d.FindAll("b"); len(nodes) != 1 || nodes[0].Attributes["id"] != "new" {
		t.Fatalf("querySelectorAll: %#v", nodes)
	}
	if nodes := d.FindAllByTagName("b"); len(nodes) != 1 || nodes[0].Attributes["id"] != "new" {
		t.Fatalf("tag name collection: %#v", nodes)
	}
	if _, ok := d.FindWithin(old.ID, "#inside"); !ok {
		t.Fatal("detached subtree lost its own query scope")
	}
	if retained, ok := d.Find("#old"); !ok || retained.ID != old.ID {
		t.Fatal("internal canonical-node lookup lost the retained detached node")
	}
}
func TestStreamSplitTokensAndCharacterReferences(t *testing.T) {
	d, s := newTestStream(t)
	for _, chunk := range []string{"<di", "v id='ke", "pt'>a&am", "p;b"} {
		if err := s.Write(chunk, nil); err != nil {
			t.Fatal(err)
		}
	}
	n, ok := d.FindWithin(d.Root().ID, "#kept")
	if !ok || d.TextContent(n.ID) != "a&b" {
		t.Fatalf("split entity: %#v %q", n, d.TextContent(n.ID))
	}
	if err := s.Write("<!--spl", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("it--></div>", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
	out, _ := d.InnerHTML(n.ID)
	if out != "a&amp;b<!--split-->" {
		t.Fatalf("markup: %s", out)
	}
}

func TestStreamPartialRawAndLiteralLessThanText(t *testing.T) {
	d, s := newTestStream(t)
	if err := s.Write("<body><p id=text>one < 2 &am", nil); err != nil {
		t.Fatal(err)
	}
	n, _ := d.Find("#text")
	if got := d.TextContent(n.ID); got != "one < 2 " {
		t.Fatalf("literal text: %q", got)
	}
	if err := s.Write("p; three</p><script id=script>if (1 < 2) x = '&amp;';", nil); err != nil {
		t.Fatal(err)
	}
	script, _ := d.Find("#script")
	if got := d.TextContent(script.ID); got != "if (1 < 2) x = '&amp;';" {
		t.Fatalf("raw text: %q", got)
	}
	calls := 0
	if err := s.Write("</scr", nil); err != nil {
		t.Fatal(err)
	}
	if got := d.TextContent(script.ID); got != "if (1 < 2) x = '&amp;';" {
		t.Fatalf("end tag exposed: %q", got)
	}
	if err := s.Write("ipt>", func(Node) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("script callbacks=%d", calls)
	}
}
func TestStreamReentrantWriteAndCanonicalMutations(t *testing.T) {
	d, s := newTestStream(t)
	scripts := 0
	var retained int64
	var execute func(Node) error
	execute = func(script Node) error {
		scripts++
		if scripts == 1 {
			holder, _ := d.FindWithin(d.Root().ID, "#holder")
			retained = holder.ID
			if err := d.SetAttribute(holder.ID, "data-live", "yes"); err != nil {
				return err
			}
			if err := s.Write("<span id=written>inserted</span><script>nested()</script>", execute); err != nil {
				return err
			}
			if _, exists := d.FindWithin(d.Root().ID, "#written"); !exists {
				t.Fatal("nested write was not synchronous")
			}
			if _, exists := d.FindWithin(d.Root().ID, "#tail"); exists {
				t.Fatal("outer suffix parsed during script")
			}
		}
		return nil
	}
	if err := s.Write("<div id=holder><script>first()</script><b id=tail>tail</b></div>", execute); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(execute); err != nil {
		t.Fatal(err)
	}
	holder, _ := d.FindWithin(d.Root().ID, "#holder")
	if holder.ID != retained || holder.Attributes["data-live"] != "yes" {
		t.Fatal("JS mutation overwritten")
	}
	if scripts != 2 {
		t.Fatalf("scripts: %d", scripts)
	}
	out, _ := d.InnerHTML(holder.ID)
	if out != "<script>first()</script><span id=\"written\">inserted</span><script>nested()</script><b id=\"tail\">tail</b>" {
		t.Fatal(out)
	}
}
func TestStreamParserSeesExternalTreeMutation(t *testing.T) {
	d, s := newTestStream(t)
	execute := func(script Node) error {
		holder, _ := d.FindWithin(d.Root().ID, "#holder")
		body, _ := d.FindWithin(d.Root().ID, "body")
		if err := d.RemoveNode(body.ID, holder.ID); err != nil {
			return err
		}
		added := d.CreateElement("i")
		if err := d.InsertNode(holder.ID, added.ID, 0); err != nil {
			return err
		}
		return d.SetTextContent(added.ID, "external")
	}
	if err := s.Write("<body><div id=holder><script>removeHolder()</script><b>tail</b></div>", execute); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(execute); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.FindWithin(d.Root().ID, "#holder"); ok {
		t.Fatal("parser resurrected removed container")
	}
	for _, node := range d.nodes {
		if node.Attributes["id"] == "holder" {
			out, _ := d.InnerHTML(node.ID)
			if !strings.Contains(out, "<i>external</i><b>tail</b>") {
				t.Fatal(out)
			}
		}
	}
}
func TestStreamSplitScriptAndNestedClose(t *testing.T) {
	d, s := newTestStream(t)
	count := 0
	execute := func(script Node) error {
		count++
		if got := d.TextContent(script.ID); got != "one & two < three" {
			t.Fatalf("script text duplicated: %q", got)
		}
		if err := s.Close(nil); err != nil {
			return err
		}
		if s.Closed() {
			t.Fatal("nested close completed before suffix")
		}
		return nil
	}
	for _, chunk := range []string{"<body><scr", "ipt>one &", " two < three</scr", "ipt><b id=tail>tail</b>"} {
		if err := s.Write(chunk, execute); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 || !s.Closed() {
		t.Fatalf("scripts=%d closed=%v", count, s.Closed())
	}
	if _, ok := d.FindWithin(d.Root().ID, "#tail"); !ok {
		t.Fatal("nested close dropped suffix")
	}
}
func TestStreamExternalScriptPauseQueuesWritesAndClose(t *testing.T) {
	d, s := newTestStream(t)
	ready := false
	calls := 0
	execute := func(script Node) error {
		calls++
		if !ready {
			return ErrStreamPaused
		}
		return s.Write("<i id=inserted>inside</i>", nil)
	}
	if err := s.Write("<body><script src=/external></script><b id=tail>tail</b>", execute); !errors.Is(err, ErrStreamPaused) {
		t.Fatalf("pause: %v", err)
	}
	if !s.Paused() {
		t.Fatal("not paused")
	}
	if err := s.Write("<u id=queued>queued</u>", execute); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(execute); err != nil {
		t.Fatal(err)
	}
	if s.Closed() {
		t.Fatal("close crossed paused script")
	}
	ready = true
	if err := s.Resume(execute); err != nil {
		t.Fatal(err)
	}
	body, _ := d.FindWithin(d.Root().ID, "body")
	out, _ := d.InnerHTML(body.ID)
	if out != "<script src=\"/external\"></script><i id=\"inserted\">inside</i><b id=\"tail\">tail</b><u id=\"queued\">queued</u>" {
		t.Fatal(out)
	}
	if calls != 2 || !s.Closed() {
		t.Fatalf("calls=%d closed=%v", calls, s.Closed())
	}
}

func TestStreamNestedWritePausePreservesBothInsertionTails(t *testing.T) {
	d, s := newTestStream(t)
	ready := false
	inlineCalls := 0
	var execute func(Node) error
	execute = func(script Node) error {
		if script.Attributes["src"] != "" {
			if !ready {
				return ErrStreamPaused
			}
			return nil
		}
		inlineCalls++
		err := s.Write(`<script src="/nested.js"></script><i id=inner-tail>inner</i>`, execute)
		// document.write returns to the author while the browser resumes its
		// parser later. The Stream must retain the outer insertion point.
		if errors.Is(err, ErrStreamPaused) {
			return nil
		}
		return err
	}
	if err := s.Write(`<body><script>write nested</script><b id=outer-tail>outer</b>`, execute); !errors.Is(err, ErrStreamPaused) {
		t.Fatalf("nested pause: %v", err)
	}
	if err := s.Close(execute); err != nil {
		t.Fatal(err)
	}
	ready = true
	if err := s.Resume(execute); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"#inner-tail", "#outer-tail"} {
		if _, ok := d.FindWithin(d.Root().ID, selector); !ok {
			t.Fatalf("lost insertion tail %s", selector)
		}
	}
	if inlineCalls != 1 || !s.Closed() {
		t.Fatalf("inline replayed or parser incomplete: calls %d closed %v", inlineCalls, s.Closed())
	}
}
func TestStreamHTML5TreeConstructionMatchesBatch(t *testing.T) {
	fixtures := []string{
		"<!doctype html><p>a<b>b<i>c</b>d</i>e",
		"<table>before<tr><td>cell</table>after",
		"<table> \n before<tr> \n <td>cell</table>after",
		"<table><b><tr><td>x</b>y</td></tr></table>",
		"<select><option>a<option>b</select>",
		"<template id=t><table><tr><td>cell</table><script>inert()</script></template><p>live",
		"<svg viewBox='0 0 1 1'><foreignObject><p>x</p></foreignObject><circle/></svg>",
		"<title>a&amp;b</title><textarea>\nfirst\r\nsecond</textarea>",
		"<body>one\x00two<!--comment--><p>last",
		"FOO&#x000D;ZOO",
		"<!DOCTYPE html><pre>\r\n\r\nA</pre><textarea>\n\nB</textarea>",
		"<!DOCTYPE html><script>\n</script>  <title>x</title>  </head>",
	}
	for _, source := range fixtures {
		t.Run(source, func(t *testing.T) {
			want, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := want.OuterHTML(want.Root().ID)
			d, s := newTestStream(t)
			for i := 0; i < len(source); i++ {
				if err := s.Write(source[i:i+1], nil); err != nil {
					t.Fatalf("byte %d: %v", i, err)
				}
			}
			if err := s.Close(nil); err != nil {
				t.Fatal(err)
			}
			actual, _ := d.OuterHTML(d.Root().ID)
			if actual != expected {
				t.Fatalf("stream=%s\nbatch=%s", actual, expected)
			}
		})
	}
}

func BenchmarkDocumentParser(b *testing.B) {
	source := "<!doctype html><title>stream</title><body>" + strings.Repeat("<section><p class=copy>text &amp; more</p><table><tr><td>cell</td></tr></table></section>", 24)
	b.Run("Batch", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for i := 0; i < b.N; i++ {
			if _, err := Parse(source); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Stream", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for i := 0; i < b.N; i++ {
			d, err := Parse("")
			if err != nil {
				b.Fatal(err)
			}
			s, err := d.NewStream()
			if err != nil {
				b.Fatal(err)
			}
			if err = s.Write(source, nil); err == nil {
				err = s.Close(nil)
			}
			s.Abort()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
