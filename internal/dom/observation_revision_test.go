package dom

import "testing"

func TestObservationRevisionIgnoresDetachedAndInertConstruction(t *testing.T) {
	d, err := Parse("<main id='root'></main><template id='template'></template>")
	if err != nil {
		t.Fatal(err)
	}
	before := d.ObservationRevision()
	detached := d.CreateElement("section")
	text := d.CreateText("before")
	if err := d.InsertNode(detached.ID, text.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAttribute(detached.ID, "class", "probe"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTextContent(text.ID, "after"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ParseInertDocument("<p class='inert'>text</p>", "text/html", "about:blank"); err != nil {
		t.Fatal(err)
	}
	template := d.FindAllIDs(0, "#template")[0]
	if err := d.SetInnerHTML(template, "<span class='template-child'>inert</span>"); err != nil {
		t.Fatal(err)
	}
	if got := d.ObservationRevision(); got != before {
		t.Fatalf("detached construction changed observation revision: %d -> %d", before, got)
	}
}

func TestObservationRevisionTracksConnectedMutationsOnce(t *testing.T) {
	d, err := Parse("<main id='root'></main>")
	if err != nil {
		t.Fatal(err)
	}
	root := d.FindAllIDs(0, "#root")[0]
	detached := d.CreateElement("section")
	text := d.CreateText("before")
	if err := d.InsertNode(detached.ID, text.ID, 0); err != nil {
		t.Fatal(err)
	}
	assertBump := func(name string, mutation func()) {
		t.Helper()
		before := d.ObservationRevision()
		mutation()
		if got := d.ObservationRevision(); got != before+1 {
			t.Fatalf("%s observation revision: got %d, want %d", name, got, before+1)
		}
	}
	assertBump("insertion", func() {
		if err := d.InsertNode(root, detached.ID, 0); err != nil {
			t.Fatal(err)
		}
	})
	assertBump("attribute", func() {
		if err := d.SetAttribute(detached.ID, "class", "connected"); err != nil {
			t.Fatal(err)
		}
	})
	assertBump("text", func() {
		if err := d.SetTextContent(text.ID, "connected"); err != nil {
			t.Fatal(err)
		}
	})
	assertBump("removal", func() {
		if err := d.RemoveNode(root, detached.ID); err != nil {
			t.Fatal(err)
		}
	})
	before := d.ObservationRevision()
	if err := d.SetAttribute(detached.ID, "class", "detached-again"); err != nil {
		t.Fatal(err)
	}
	if got := d.ObservationRevision(); got != before {
		t.Fatalf("post-removal detached mutation changed observation revision: %d -> %d", before, got)
	}
}

func TestObservationRevisionPublishesPlainFragmentOnce(t *testing.T) {
	d, err := Parse("<main id='root'></main>")
	if err != nil {
		t.Fatal(err)
	}
	root := d.FindAllIDs(0, "#root")[0]
	fragment := d.CreateDocumentFragment()
	for i := 0; i < 20; i++ {
		child := d.CreateElement("span")
		if err := d.InsertNode(fragment.ID, child.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	before := d.ObservationRevision()
	inserted, err := d.InsertPlainFragment(root, fragment.ID, 0)
	if err != nil || !inserted {
		t.Fatalf("insert fragment: inserted=%t err=%v", inserted, err)
	}
	if got := d.ObservationRevision(); got != before+1 {
		t.Fatalf("fragment publication: got %d, want %d", got, before+1)
	}
}

func TestObservationMutationJournalIgnoresDetachedAndRejectsStructuralGaps(t *testing.T) {
	d, err := Parse("<main id='root'></main>")
	if err != nil {
		t.Fatal(err)
	}
	root := d.FindAllIDs(0, "#root")[0]
	detached := d.CreateElement("div")
	before := d.ObservationRevision()
	if err := d.SetAttribute(detached.ID, "class", "detached"); err != nil {
		t.Fatal(err)
	}
	if journal := d.ObservationMutationsSince(before); journal.Overflow || len(journal.Records) != 0 {
		t.Fatalf("detached mutation entered observation journal: %#v", journal)
	}
	if err := d.InsertNode(root, detached.ID, 0); err != nil {
		t.Fatal(err)
	}
	if journal := d.ObservationMutationsSince(before); !journal.Overflow {
		t.Fatalf("structural mutation did not create conservative gap: %#v", journal)
	}
	before = d.ObservationRevision()
	if err := d.SetAttribute(detached.ID, "data-mode", "on"); err != nil {
		t.Fatal(err)
	}
	journal := d.ObservationMutationsSince(before)
	if journal.Overflow || len(journal.Records) != 1 || journal.Records[0].Attribute != "data-mode" {
		t.Fatalf("connected attribute journal: %#v", journal)
	}
}

func TestObservationMutationJournalKeepsRecentOrderAfterWrap(t *testing.T) {
	d, err := Parse("<main id='root'></main>")
	if err != nil {
		t.Fatal(err)
	}
	id := d.FindAllIDs(0, "#root")[0]
	for i := 0; i < mutationJournalLimit+7; i++ {
		if err := d.SetAttribute(id, "data-value", string(rune(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	journal := d.ObservationMutationsSince(d.ObservationRevision() - 7)
	if journal.Overflow || len(journal.Records) != 7 {
		t.Fatalf("recent observation window: %#v", journal)
	}
	for i, record := range journal.Records {
		if record.Revision != d.ObservationRevision()-6+uint64(i) {
			t.Fatalf("observation order at %d: %#v", i, record)
		}
	}
}

func TestObservationRevisionIsScopedPerDocumentRoot(t *testing.T) {
	d, err := Parse("<main></main>")
	if err != nil {
		t.Fatal(err)
	}
	title := "inert"
	root := d.CreateHTMLDocument(&title)
	inert := &Document{nodeArena: d.nodeArena, root: root.ID}
	body := inert.FindAllByTagName("body")[0]
	mainBefore := d.ObservationRevision()
	if err := d.SetAttribute(body.ID, "class", "changed"); err != nil {
		t.Fatal(err)
	}
	if got := d.ObservationRevision(); got != mainBefore {
		t.Fatalf("inert root invalidated active document: %d -> %d", mainBefore, got)
	}
}

func TestObservationRevisionUsesTargetRootAcrossSharedArena(t *testing.T) {
	main, err := Parse("<main id='parent'></main>")
	if err != nil {
		t.Fatal(err)
	}
	child, err := Parse("<section id='box'></section>")
	if err != nil {
		t.Fatal(err)
	}
	child.ShareNodeArena(main)
	parent := main.FindAllIDs(0, "#parent")[0]
	box := child.FindAllIDs(0, "#box")[0]
	mainBefore, childBefore := main.ObservationRevision(), child.ObservationRevision()
	if err := main.SetAttribute(box, "class", "borrowed-wrapper"); err != nil {
		t.Fatal(err)
	}
	if got := main.ObservationRevision(); got != mainBefore {
		t.Fatalf("foreign attribute invalidated receiver root: %d -> %d", mainBefore, got)
	}
	if got := child.ObservationRevision(); got != childBefore+1 {
		t.Fatalf("foreign attribute missed target root: got %d, want %d", got, childBefore+1)
	}
	mainBefore, childBefore = main.ObservationRevision(), child.ObservationRevision()
	if err := main.InsertNode(parent, box, 0); err != nil {
		t.Fatal(err)
	}
	if got := main.ObservationRevision(); got != mainBefore+1 {
		t.Fatalf("cross-root insertion missed destination: got %d, want %d", got, mainBefore+1)
	}
	if got := child.ObservationRevision(); got != childBefore+1 {
		t.Fatalf("cross-root insertion missed source: got %d, want %d", got, childBefore+1)
	}
}
