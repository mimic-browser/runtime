package dom

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ShadowSnapshot describes realm-owned shadow attachment state. Its children
// reference the canonical node store; it is not a second mutable DOM.
type ShadowSnapshot struct {
	RootID         int64    `json:"rootID,omitempty"`
	HostID         int64    `json:"hostID"`
	Mode           string   `json:"mode"`
	DelegatesFocus bool     `json:"delegatesFocus"`
	Children       []int64  `json:"children"`
	HTML           string   `json:"html"`
	Styles         []string `json:"styles"`
}

func (d *Document) SerializeNodeList(ids []int64) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out bytes.Buffer
	for _, id := range ids {
		n := d.htmlNode(id)
		if n == nil {
			return "", fmt.Errorf("node %d does not exist", id)
		}
		if err := html.Render(&out, n); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

// SnapshotTree creates an immutable serialization projection. Shadow roots are
// declarative templates so a script-free browser export retains composition.
func (d *Document) SnapshotTree(rootID int64, shadows []ShadowSnapshot) (*html.Node, error) {
	return d.SnapshotTreeWithFormState(rootID, shadows, nil)
}

func (d *Document) SnapshotTreeWithFormState(rootID int64, shadows []ShadowSnapshot, forms []FormSnapshot) (*html.Node, error) {
	return d.snapshotTree(rootID, shadows, forms, "")
}

// PreviewTree adds stable identities only to the immutable debug projection.
// Ordinary exports and canonical attributes are unaffected.
func (d *Document) PreviewTree(rootID int64, shadows []ShadowSnapshot, forms []FormSnapshot, realmID string) (*html.Node, error) {
	return d.snapshotTree(rootID, shadows, forms, realmID)
}

func (d *Document) snapshotTree(rootID int64, shadows []ShadowSnapshot, forms []FormSnapshot, previewRealm string) (*html.Node, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.nodes[rootID] == nil {
		return nil, fmt.Errorf("node %d does not exist", rootID)
	}
	byHost := make(map[int64]ShadowSnapshot, len(shadows))
	for _, s := range shadows {
		byHost[s.HostID] = s
	}
	byControl := make(map[int64]FormSnapshot, len(forms))
	for _, form := range forms {
		byControl[form.NodeID] = form
	}
	active := map[int64]bool{}
	var decorate func(int64, *html.Node) error
	decorate = func(id int64, out *html.Node) error {
		if active[id] {
			return fmt.Errorf("cyclic shadow tree at node %d", id)
		}
		active[id] = true
		defer delete(active, id)
		if previewRealm != "" && out.Type == html.ElementNode {
			attrs := out.Attr[:0]
			for _, a := range out.Attr {
				if a.Key != "data-mimic-preview-node" {
					attrs = append(attrs, a)
				}
			}
			out.Attr = append(attrs, html.Attribute{Key: "data-mimic-preview-node", Val: fmt.Sprintf("%s:%d", previewRealm, id)})
		}
		n := d.nodes[id]
		children := n.Children
		if n.TemplateContent != 0 {
			children = d.nodes[n.TemplateContent].Children
		}
		c := out.FirstChild
		for _, childID := range children {
			if d.nodes[childID] == nil {
				continue
			}
			if c == nil {
				return fmt.Errorf("inconsistent serialization tree at node %d", id)
			}
			if err := decorate(childID, c); err != nil {
				return err
			}
			c = c.NextSibling
		}
		if form, ok := byControl[id]; ok {
			projectFormSnapshot(out, form)
		}
		s, ok := byHost[id]
		if !ok {
			return nil
		}
		appendStyles := func(target *html.Node) {
			for _, css := range s.Styles {
				style := &html.Node{Type: html.ElementNode, Data: "style", DataAtom: atom.Style}
				style.AppendChild(&html.Node{Type: html.TextNode, Data: css})
				target.AppendChild(style)
			}
		}
		if out.Type == html.DocumentNode {
			var head *html.Node
			var findHead func(*html.Node)
			findHead = func(node *html.Node) {
				if node.Type == html.ElementNode && node.DataAtom == atom.Head {
					head = node
					return
				}
				for child := node.FirstChild; child != nil && head == nil; child = child.NextSibling {
					findHead(child)
				}
			}
			findHead(out)
			if head != nil {
				appendStyles(head)
			}
			return nil
		}
		if s.Mode != "open" && s.Mode != "closed" {
			return fmt.Errorf("invalid shadow mode %q", s.Mode)
		}
		t := &html.Node{Type: html.ElementNode, Data: "template", DataAtom: atom.Template, Attr: []html.Attribute{{Key: "shadowrootmode", Val: s.Mode}}}
		if s.DelegatesFocus {
			t.Attr = append(t.Attr, html.Attribute{Key: "shadowrootdelegatesfocus"})
		}
		for _, childID := range s.Children {
			child := d.htmlNode(childID)
			if child == nil {
				return fmt.Errorf("shadow child %d does not exist", childID)
			}
			if err := decorate(childID, child); err != nil {
				return err
			}
			t.AppendChild(child)
		}
		if len(s.Children) == 0 && s.HTML != "" {
			context := &html.Node{Type: html.ElementNode, Data: out.Data, DataAtom: out.DataAtom, Namespace: out.Namespace}
			parsed, err := html.ParseFragment(strings.NewReader(s.HTML), context)
			if err != nil {
				return err
			}
			for _, child := range parsed {
				t.AppendChild(child)
			}
		}
		appendStyles(t)
		out.InsertBefore(t, out.FirstChild)
		return nil
	}
	root := d.htmlNode(rootID)
	if err := decorate(rootID, root); err != nil {
		return nil, err
	}
	return root, nil
}
