package dom

import "testing"

func TestMutationJournalTracksAttributesAndRejectsGaps(t *testing.T) {
	d, err := Parse("<div id='probe'></div>")
	if err != nil {
		t.Fatal(err)
	}
	ids := d.FindAllIDs(0, "#probe")
	if len(ids) != 1 {
		t.Fatal("missing probe")
	}
	probe, _ := d.Get(ids[0])
	start := d.Revision()
	if err := d.SetAttribute(probe.ID, "data-unused", "1"); err != nil {
		t.Fatal(err)
	}
	journal := d.MutationsSince(start)
	if journal.Overflow || len(journal.Records) != 1 {
		t.Fatalf("attribute journal: %#v", journal)
	}
	if got := journal.Records[0]; got.Kind != "attribute" || got.Target != probe.ID || got.Attribute != "data-unused" {
		t.Fatalf("attribute record: %#v", got)
	}
	beforeText := d.Revision()
	if err := d.SetTextContent(probe.ID, "changed"); err != nil {
		t.Fatal(err)
	}
	if journal := d.MutationsSince(beforeText); !journal.Overflow {
		t.Fatalf("uninstrumented structural revision must overflow: %#v", journal)
	}
}

func TestMutationJournalIsBounded(t *testing.T) {
	d, err := Parse("<div></div>")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := d.AppendElement(d.Root().ID, "div", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := d.Revision()
	for i := 0; i <= mutationJournalLimit; i++ {
		if err := d.SetAttribute(probe.ID, "data-value", string(rune(i))); err != nil {
			t.Fatal(err)
		}
	}
	if journal := d.MutationsSince(start); !journal.Overflow || journal.Records != nil {
		t.Fatalf("evicted journal did not overflow: %#v", journal)
	}
	recent := d.Revision() - 1
	if journal := d.MutationsSince(recent); journal.Overflow || len(journal.Records) != 1 {
		t.Fatalf("recent bounded journal unavailable: %#v", journal)
	}
}

func TestMutationJournalKeepsRecentOrderAfterWrap(t *testing.T) {
	d, err := Parse("<div id='probe'></div>")
	if err != nil {
		t.Fatal(err)
	}
	id := d.FindAllIDs(0, "#probe")[0]
	for i := 0; i < mutationJournalLimit+7; i++ {
		if err := d.SetAttribute(id, "data-value", string(rune(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	journal := d.MutationsSince(d.Revision() - 7)
	if journal.Overflow || len(journal.Records) != 7 {
		t.Fatalf("recent mutation window: %#v", journal)
	}
	for i, record := range journal.Records {
		if record.Revision != d.Revision()-6+uint64(i) {
			t.Fatalf("mutation order at %d: %#v", i, record)
		}
	}
}
