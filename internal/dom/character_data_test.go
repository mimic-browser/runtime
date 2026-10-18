package dom

import "testing"

func TestParsedCommentsRetainHydrationMarkers(t *testing.T) {
	d, err := Parse("<main><!--$-->content<!--/$--></main>")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := d.Find("main")
	children := d.Children(root.ID)
	if len(children) != 3 || children[0].Type != "comment" || children[0].Text != "$" || children[2].Text != "/$" {
		t.Fatalf("markers: %+v", children)
	}
	if text := d.TextContent(root.ID); text != "content" {
		t.Fatalf("descendant text: %q", text)
	}
}

func TestCanonicalDOMStringPreservesLoneSurrogates(t *testing.T) {
	d, _ := Parse("<main></main>")
	root, _ := d.Find("main")
	node := d.CreateText("initial")
	d.InsertNode(root.ID, node.ID, 0)
	if err := d.SetCharacterDataJSON(node.ID, `"\ud800x"`); err != nil {
		t.Fatal(err)
	}
	if got := d.TextContentJSON(root.ID); got != `"\ud800x"` {
		t.Fatal(got)
	}
	d.SetTextContent(node.ID, "new")
	if got := d.TextContentJSON(root.ID); got != `"new"` {
		t.Fatal(got)
	}
}

func TestCharacterDataSubscriptionsPreserveWritesAndRelease(t *testing.T) {
	d, _ := Parse("<main> </main>")
	root, _ := d.Find("main")
	text := d.Children(root.ID)[0]
	var first, second []CharacterDataChange
	removeFirst := d.SubscribeCharacterData(func(change CharacterDataChange) { first = append(first, change) })
	removeSecond := d.SubscribeCharacterData(func(change CharacterDataChange) { second = append(second, change) })
	for _, write := range []func() error{
		func() error { return d.SetTextContent(text.ID, " ") },
		func() error { return d.SetCharacterDataJSON(text.ID, `" "`) },
	} {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("same-value writes lost: %v %v", first, second)
	}
	for _, change := range first {
		if change.NodeID != text.ID || change.ParentID != root.ID || change.Type != "text" || change.Text != " " {
			t.Fatalf("change: %v", change)
		}
	}
	removeFirst()
	if err := d.SetTextContent(text.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 3 {
		t.Fatalf("subscriber isolation: %v %v", first, second)
	}
	removeSecond()
	if d.characterDataSubscribers != nil {
		t.Fatal("last subscription retained callbacks")
	}
	if got := d.TextContent(root.ID); got != "x" {
		t.Fatalf("observer changed DOM: %q", got)
	}
}
