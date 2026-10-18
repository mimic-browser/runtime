package dom

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/moreveal/mimic/internal/layoutflat"
)

// ParseInertDocument imports a newly parsed tree into the existing node arena.
// It never attaches the root to the active document or starts script/resources.
func (d *Document) ParseInertDocument(source, mime, documentURL string) (Node, error) {
	var parsed *Document
	var err error
	if mime == "text/html" {
		parsed, err = Parse(source)
	} else {
		parsed = parseXMLDocument(source)
	}
	if err != nil {
		return Node{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make(map[int64]int64, len(parsed.nodes))
	for old := int64(1); old <= parsed.next; old++ {
		if parsed.nodes[old] != nil {
			d.next++
			ids[old] = d.next
		}
	}
	rootID := ids[parsed.root]
	for old, node := range parsed.nodes {
		n := *node
		n.ID = ids[old]
		n.Parent = ids[node.Parent]
		n.OwnerDocument = rootID
		n.Children = make([]int64, len(node.Children))
		for i, id := range node.Children {
			n.Children[i] = ids[id]
		}
		n.TemplateContent = ids[node.TemplateContent]
		n.TemplateHost = ids[node.TemplateHost]
		if n.TagName == "SCRIPT" {
			n.ScriptAlreadyStarted = true
		}
		d.hasFrameElements = d.hasFrameElements || n.TagName == "IFRAME"
		d.nodes[n.ID] = &n
	}
	root := d.nodes[rootID]
	root.OwnerDocument = 0
	root.ContentType = mime
	root.DocumentURL = documentURL
	root.ParsedDocument = true
	return *root, nil
}

// XML parsing shares canonical Node storage with HTML. The decoder does not
// resolve external entities or perform network I/O. DTD entity expansion and
// Chrome's exact localized parser diagnostics remain outside this parser.
func parseXMLDocument(source string) *Document {
	d := &Document{nodeArena: &nodeArena{nodes: map[int64]*Node{}, next: 1, flatLayout: new(layoutflat.State)}, root: 1}
	d.nodes[1] = &Node{ID: 1, Type: "document"}
	add := func(n *Node, parent int64) {
		d.next++
		n.ID = d.next
		n.Parent = parent
		d.nodes[n.ID] = n
		d.nodes[parent].Children = append(d.nodes[parent].Children, n.ID)
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	stack := []int64{1}
	names := []xml.Name{}
	spaces := []map[string]string{{"xml": "http://www.w3.org/XML/1998/namespace", "xmlns": "http://www.w3.org/2000/xmlns/"}}
	rootSeen := false
	var failure error
	for {
		token, err := decoder.RawToken()
		if err == io.EOF {
			if len(stack) > 1 {
				failure = fmt.Errorf("unexpected end of document")
			}
			break
		}
		if err != nil {
			failure = err
			break
		}
		parent := stack[len(stack)-1]
		switch t := token.(type) {
		case xml.StartElement:
			if parent == 1 && rootSeen {
				failure = fmt.Errorf("multiple document elements")
				break
			}
			if parent == 1 {
				rootSeen = true
			}
			ns := map[string]string{}
			for k, v := range spaces[len(spaces)-1] {
				ns[k] = v
			}
			// XML namespace declarations precede ordinary attributes in the DOM
			// attribute collection, even when they follow them in source text.
			// Keep the source order within each group.
			orderedAttrs := make([]xml.Attr, 0, len(t.Attr))
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					orderedAttrs = append(orderedAttrs, a)
				}
			}
			for _, a := range t.Attr {
				if a.Name.Space != "xmlns" && !(a.Name.Space == "" && a.Name.Local == "xmlns") {
					orderedAttrs = append(orderedAttrs, a)
				}
			}
			for _, a := range orderedAttrs {
				if a.Name.Space == "xmlns" {
					ns[a.Name.Local] = a.Value
				} else if a.Name.Space == "" && a.Name.Local == "xmlns" {
					ns[""] = a.Value
				}
			}
			if t.Name.Space != "" && ns[t.Name.Space] == "" {
				failure = fmt.Errorf("unbound namespace prefix")
				break
			}
			name := t.Name.Local
			if t.Name.Space != "" {
				name = t.Name.Space + ":" + name
			}
			n := &Node{Type: "element", TagName: name, QualifiedName: name, Namespace: ns[t.Name.Space]}
			for _, a := range orderedAttrs {
				key := a.Name.Local
				uri := ""
				if a.Name.Space != "" {
					key = a.Name.Space + ":" + key
					uri = ns[a.Name.Space]
				} else if key == "xmlns" {
					uri = ns["xmlns"]
				}
				if _, exists := n.Attributes[key]; exists {
					failure = fmt.Errorf("duplicate attribute")
					break
				}
				n.setAttribute(key, a.Value)
				if uri != "" {
					if n.AttributeNamespaces == nil {
						n.AttributeNamespaces = map[string]string{}
					}
					n.AttributeNamespaces[key] = uri
				}
			}
			add(n, parent)
			stack = append(stack, n.ID)
			names = append(names, t.Name)
			spaces = append(spaces, ns)
		case xml.EndElement:
			if len(names) == 0 || names[len(names)-1] != t.Name {
				failure = fmt.Errorf("mismatched closing element")
				break
			}
			stack = stack[:len(stack)-1]
			names = names[:len(names)-1]
			spaces = spaces[:len(spaces)-1]
		case xml.CharData:
			if parent == 1 {
				if strings.TrimSpace(string(t)) != "" {
					failure = fmt.Errorf("text outside document element")
				}
			} else if len(t) > 0 {
				add(&Node{Type: "text", Text: string(t)}, parent)
			}
		case xml.Comment:
			add(&Node{Type: "comment", Text: string(t)}, parent)
		case xml.ProcInst:
			if strings.ToLower(t.Target) != "xml" {
				add(&Node{Type: "processing-instruction", TagName: t.Target, Text: string(t.Inst)}, parent)
			}
		case xml.Directive:
			text := strings.TrimSpace(string(t))
			if strings.HasPrefix(text, "DOCTYPE ") {
				fields := strings.Fields(text)
				if len(fields) > 1 {
					add(&Node{Type: "doctype", TagName: fields[1]}, 1)
				}
			}
		}
		if failure != nil {
			break
		}
	}
	if !rootSeen && failure == nil {
		failure = fmt.Errorf("document has no root element")
	}
	if failure != nil {
		parent := int64(1)
		for _, id := range d.nodes[1].Children {
			if d.nodes[id].Type == "element" {
				parent = id
				break
			}
		}
		n := &Node{Type: "element", TagName: "parsererror", QualifiedName: "parsererror", Namespace: "http://www.w3.org/1999/xhtml"}
		add(n, parent)
		add(&Node{Type: "text", Text: failure.Error()}, n.ID)
	}
	return d
}
