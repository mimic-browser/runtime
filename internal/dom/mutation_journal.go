package dom

const mutationJournalLimit = 256

// MutationRecord is a bounded invalidation hint over the authoritative node
// arena. Consumers must fall back when Overflow is reported; the journal is
// never a second DOM state model.
type MutationRecord struct {
	Revision  uint64 `json:"revision"`
	Kind      string `json:"kind"`
	Target    int64  `json:"target"`
	Attribute string `json:"attribute,omitempty"`
}

type MutationJournal struct {
	Revision uint64           `json:"revision"`
	Overflow bool             `json:"overflow"`
	Records  []MutationRecord `json:"records"`
}

// recordMutationLocked records the revision produced by the pending unlock.
// At most one summary is required per arena revision. A missing revision is
// treated as overflow by readers, conservatively covering uninstrumented and
// non-DOM-state invalidations.
func (d *Document) recordMutationLocked(kind string, target int64, attribute string) {
	record := MutationRecord{
		Revision:  d.mu.revision + 1,
		Kind:      kind,
		Target:    target,
		Attribute: attribute,
	}
	if len(d.mutationJournal) == mutationJournalLimit {
		d.mutationJournal[d.mutationJournalStart] = record
		d.mutationJournalStart = (d.mutationJournalStart + 1) % mutationJournalLimit
		return
	}
	d.mutationJournal = append(d.mutationJournal, record)
}

// MutationsSince returns only a contiguous account of every arena revision
// after revision. Any missing or evicted revision forces a conservative miss.
func (d *Document) MutationsSince(revision uint64) MutationJournal {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := MutationJournal{Revision: d.mu.revision, Records: []MutationRecord{}}
	if revision == d.mu.revision {
		return result
	}
	if revision > d.mu.revision {
		result.Overflow = true
		return result
	}
	expected := revision + 1
	for i := range d.mutationJournal {
		record := d.mutationJournal[(d.mutationJournalStart+i)%len(d.mutationJournal)]
		if record.Revision < expected {
			continue
		}
		if record.Revision != expected {
			result.Overflow = true
			result.Records = nil
			return result
		}
		result.Records = append(result.Records, record)
		expected++
	}
	if expected != d.mu.revision+1 {
		result.Overflow = true
		result.Records = nil
	}
	return result
}

// ObservationMutationsSince reports mutations in the active document's
// connected-observation domain. Detached and inert construction does not
// appear here; structural and state changes deliberately leave a gap and force
// consumers to recompute.
func (d *Document) ObservationMutationsSince(revision uint64) MutationJournal {
	d.mu.RLock()
	defer d.mu.RUnlock()
	current := d.observationRevisions[d.root]
	result := MutationJournal{Revision: current, Records: []MutationRecord{}}
	if revision == current {
		return result
	}
	if revision > current {
		result.Overflow = true
		return result
	}
	expected := revision + 1
	journal := d.observationJournals[d.root]
	for i := range journal {
		record := journal[(d.observationJournalStarts[d.root]+i)%len(journal)]
		if record.Revision < expected {
			continue
		}
		if record.Revision != expected {
			result.Overflow = true
			result.Records = nil
			return result
		}
		result.Records = append(result.Records, record)
		expected++
	}
	if expected != current+1 {
		result.Overflow = true
		result.Records = nil
	}
	return result
}
