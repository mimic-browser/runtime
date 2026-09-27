package dom

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SetTextContentJSON shares replace-all ownership and mutation bookkeeping with
// scalar textContent writes while retaining exact DOMString code units.
func (d *Document) SetTextContentJSON(id int64, data string) error {
	var scalar string
	if err := json.Unmarshal([]byte(data), &scalar); err != nil {
		return err
	}
	return d.setTextContent(id, scalar, data)
}

// SetCharacterDataJSON accepts JSON's lossless representation of JS code units.
// Keep the exact representation in the canonical node, never in a JS mirror.
func (d *Document) SetCharacterDataJSON(id int64, data string) error {
	var scalar string
	if err := json.Unmarshal([]byte(data), &scalar); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	n := d.nodes[id]
	if n == nil || (n.Type != "text" && n.Type != "comment") {
		return fmt.Errorf("not character data: %d", id)
	}
	d.markConnectedMutationLocked(id)
	n.Text, n.TextJSON = scalar, data
	return nil
}

func (d *Document) TextContentJSON(id int64) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out strings.Builder
	out.WriteByte('"')
	var collect func(int64, bool)
	collect = func(id int64, own bool) {
		n := d.nodes[id]
		if n == nil {
			return
		}
		if n.Type == "text" || (own && n.Type == "comment") {
			encoded := n.TextJSON
			if encoded == "" {
				data, _ := json.Marshal(n.Text)
				encoded = string(data)
			}
			out.WriteString(encoded[1 : len(encoded)-1])
			return
		}
		if n.Type == "comment" {
			return
		}
		for _, child := range n.Children {
			collect(child, false)
		}
	}
	collect(id, true)
	out.WriteByte('"')
	return out.String()
}
