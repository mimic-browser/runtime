package webapi

import (
	"encoding/json"

	"github.com/moreveal/mimic/compatibility"
)

type shapeCatalogMember struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Static   bool            `json:"static,omitempty"`
	Readonly bool            `json:"readonly,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

type shapeCatalogSpec struct {
	Name                string               `json:"name"`
	Kind                string               `json:"kind"`
	Exposed             json.RawMessage      `json:"exposed"`
	Parent              string               `json:"parent,omitempty"`
	LegacyWindowAliases []string             `json:"legacyWindowAliases,omitempty"`
	Members             []shapeCatalogMember `json:"members"`
}

// The generated catalog contains rich IDL metadata, but selection needs only
// these raw fields. Decoding into maps would allocate one map per specification
// and member before discarding nearly all of that metadata.
type catalogInputSpec struct {
	Name                json.RawMessage `json:"name"`
	Kind                json.RawMessage `json:"kind"`
	Exposed             json.RawMessage `json:"exposed"`
	Parent              json.RawMessage `json:"parent"`
	LegacyWindowAliases json.RawMessage `json:"legacyWindowAliases"`
	Members             json.RawMessage `json:"members"`
}

type catalogInputMember struct {
	Name     json.RawMessage `json:"name"`
	Kind     json.RawMessage `json:"kind"`
	Static   json.RawMessage `json:"static"`
	Readonly json.RawMessage `json:"readonly"`
	Value    json.RawMessage `json:"value"`
}

// selectedCatalog removes fallback bindings which applyTargetExposure would
// immediately delete. The complete captured exposure still owns final shape;
// native members and handwritten implementations are not filtered here.
// This runs once per immutable bundle/profile, outside any Page runtime lock.
func selectedCatalog(source string, exposure compatibility.RealmExposure) string {
	if source == "" {
		return source
	}
	var specs []catalogInputSpec
	if json.Unmarshal([]byte(source), &specs) != nil {
		return source
	}
	decodeString := func(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }
	byName := make(map[string]*catalogInputSpec, len(specs))
	for i := range specs {
		byName[decodeString(specs[i].Name)] = &specs[i]
	}
	globals := make(map[string]bool, len(exposure.Properties))
	for _, property := range exposure.Properties {
		globals[property.Name] = true
	}
	keep := make(map[string]bool)
	var include func(string)
	include = func(name string) {
		if name == "" || keep[name] {
			return
		}
		keep[name] = true
		if spec := byName[name]; spec != nil {
			include(decodeString(spec.Parent))
		}
	}
	for name, spec := range byName {
		if globals[name] {
			include(name)
			continue
		}
		var aliases []string
		_ = json.Unmarshal(spec.LegacyWindowAliases, &aliases)
		for _, alias := range aliases {
			if globals[alias] {
				include(name)
				break
			}
		}
	}
	compact := make([]shapeCatalogSpec, 0, len(keep))
	neededByDescendants := map[string]map[string]bool{}
	for name, members := range exposure.Prototypes {
		seen := map[string]bool{}
		for current := name; current != "" && !seen[current]; {
			seen[current] = true
			if neededByDescendants[current] == nil {
				neededByDescendants[current] = map[string]bool{}
			}
			for _, member := range members {
				neededByDescendants[current][member.Name] = true
			}
			if spec := byName[current]; spec != nil {
				current = decodeString(spec.Parent)
			} else {
				current = ""
			}
		}
	}
	for _, spec := range specs {
		name := decodeString(spec.Name)
		if !keep[name] {
			continue
		}
		item := shapeCatalogSpec{
			Name:    name,
			Kind:    decodeString(spec.Kind),
			Exposed: append(json.RawMessage(nil), spec.Exposed...),
			Parent:  decodeString(spec.Parent),
		}
		_ = json.Unmarshal(spec.LegacyWindowAliases, &item.LegacyWindowAliases)
		var members []catalogInputMember
		if json.Unmarshal(spec.Members, &members) != nil {
			return source
		}
		item.Members = make([]shapeCatalogMember, 0, len(members))
		allowed := map[string]bool(nil)
		if _, captured := exposure.Prototypes[name]; captured {
			// Ancestors can exist solely to build an exposed child's chain.
			allowed = map[string]bool{}
			for member := range neededByDescendants[name] {
				allowed[member] = true
			}
			seen := map[string]bool{}
			for current := name; current != "" && !seen[current]; {
				seen[current] = true
				for _, property := range exposure.Prototypes[current] {
					allowed[property.Name] = true
				}
				if parent := byName[current]; parent != nil {
					current = decodeString(parent.Parent)
				} else {
					current = ""
				}
			}
		}
		for _, member := range members {
			entry := shapeCatalogMember{Name: decodeString(member.Name), Kind: decodeString(member.Kind)}
			_ = json.Unmarshal(member.Static, &entry.Static)
			if allowed != nil && !entry.Static && entry.Kind != "constant" && !allowed[entry.Name] {
				continue
			}
			_ = json.Unmarshal(member.Readonly, &entry.Readonly)
			entry.Value = append(json.RawMessage(nil), member.Value...)
			item.Members = append(item.Members, entry)
		}
		compact = append(compact, item)
	}
	encoded, err := json.Marshal(compact)
	if err != nil {
		return source
	}
	return string(encoded)
}
