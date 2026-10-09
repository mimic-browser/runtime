package layoutblitz

import "strings"

// BackgroundImages reads only resource declarations from the resolved canonical
// projection. It does not materialize geometry packets or styles for undisplayed
// descendants. Traversal preserves document order and excludes display:none
// subtrees while retaining display:contents and visibility:hidden backgrounds.
func (d *Document) BackgroundImages() ([]string, error) {
	values := make([]string, 0)
	pending := []int64{d.source.Root().ID}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		node := d.nodes[id]
		if node.Type == "element" {
			ready, err := d.Owner.HasComputedStyle(uint64(id))
			if err != nil {
				return nil, err
			}
			if !ready {
				continue
			}
			declarations, err := d.Owner.StyleBatch(uint64(id), []string{"display", "background-image"})
			if err != nil {
				return nil, err
			}
			if declarations[0] == "none" {
				continue
			}
			if strings.Contains(declarations[1], "url(") {
				values = append(values, declarations[1])
			}
		}
		for i := len(node.Children) - 1; i >= 0; i-- {
			pending = append(pending, node.Children[i])
		}
	}
	return values, nil
}
