package dom

import (
	"sync/atomic"
	"testing"
)

func TestRevisionSubscriptionCoversSharedArenaAndUnsubscribe(t *testing.T) {
	d, err := Parse("<div></div>")
	if err != nil {
		t.Fatal(err)
	}
	var first, second atomic.Uint64
	unsubscribe := d.SubscribeRevision(first.Store)
	defer unsubscribe()
	stopSecond := d.SubscribeRevision(second.Store)
	if first.Load() != d.Revision() || second.Load() != d.Revision() {
		t.Fatal("subscriptions did not publish their initial canonical revision")
	}
	inert := d.CreateHTMLDocument(nil)
	d.CreateDocumentElement(inert.ID, "http://www.w3.org/1999/xhtml", "span")
	if first.Load() != d.Revision() || second.Load() != d.Revision() {
		t.Fatal("inert-document writes did not publish the shared arena revision")
	}
	stopSecond()
	stopped := second.Load()
	d.InvalidateObservations()
	if first.Load() != d.Revision() || second.Load() != stopped {
		t.Fatal("publication continued after unsubscribe or omitted a canonical write")
	}
}

func TestRevisionTracksCanonicalWritesNotReads(t *testing.T) {
	d, err := Parse("<div id='box'>before</div>")
	if err != nil {
		t.Fatal(err)
	}
	box := d.FindAllByTagName("div")[0]
	before := d.Revision()
	d.Get(box.ID)
	d.GetAttribute(box.ID, "id")
	d.FindAllByTagName("div")
	if d.Revision() != before {
		t.Fatal("read advanced mutation revision")
	}
	for name, write := range map[string]func(){
		"attribute":       func() { _ = d.SetAttribute(box.ID, "class", "changed") },
		"text":            func() { _ = d.SetTextContent(box.ID, "after") },
		"parsed children": func() { _ = d.SetInnerHTML(box.ID, "<span>child</span>") },
		"detached node":   func() { d.CreateElement("section") },
	} {
		before = d.Revision()
		write()
		if d.Revision() <= before {
			t.Errorf("%s did not advance mutation revision", name)
		}
	}
}
