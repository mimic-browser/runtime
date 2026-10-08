package cdp

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

type protocolCoverage struct {
	Name                 string   `json:"name"`
	Kind                 string   `json:"kind"`
	SurfaceRegistered    bool     `json:"surfaceRegistered"`
	WireSchemaGenerated  bool     `json:"wireSchemaGenerated"`
	Status               string   `json:"status"`
	Notes                string   `json:"notes,omitempty"`
	Tests                []string `json:"tests,omitempty"`
	SemanticsImplemented bool     `json:"semanticsImplemented"`
	SemanticsVerified    bool     `json:"semanticsVerified"`
}

type protocolSupport struct {
	Status              string   `json:"status"`
	Notes               string   `json:"notes"`
	Tests               []string `json:"tests"`
	Experimental        bool     `json:"experimental,omitempty"`
	wireSchemaGenerated bool
}

// Ordinary commands only need wire descriptors. Load support lazily from the
// generated projection of protocol_support.json, without a second claim list.
var protocolSupportByName = sync.OnceValue(func() map[string]protocolSupport {
	type entry struct {
		Name                string          `json:"name"`
		Support             protocolSupport `json:"support"`
		WireSchemaGenerated bool            `json:"wireSchemaGenerated"`
	}
	var inventory struct {
		Entries    []entry `json:"entries"`
		Extensions []entry `json:"extensions"`
	}
	if err := json.Unmarshal(protocolInventoryJSON, &inventory); err != nil {
		panic("invalid generated CDP inventory: " + err.Error())
	}
	result := make(map[string]protocolSupport, len(inventory.Entries))
	for _, row := range inventory.Entries {
		row.Support.wireSchemaGenerated = row.WireSchemaGenerated
		result[row.Name] = row.Support
	}
	for _, row := range inventory.Extensions {
		row.Support.wireSchemaGenerated = row.WireSchemaGenerated
		result[row.Name] = row.Support
	}
	return result
})

func protocolExtensionMatrix() []protocolCoverage {
	names := map[string]struct{}{}
	for name := range protocolSupportByName() {
		if strings.HasPrefix(name, "Mimic.") {
			names[name] = struct{}{}
		}
	}
	rows := protocolMatrix(names, nil)
	for i := range rows {
		rows[i].Kind = "extension"
	}
	return rows
}

func protocolMatrix(methods, events map[string]struct{}) []protocolCoverage {
	manifest := protocolSupportByName()
	out := make([]protocolCoverage, 0, len(methods)+len(events))
	appendRow := func(name, kind string) {
		support, ok := manifest[name]
		if !ok {
			support.Status = "unsupported"
		}
		implemented := support.Status == "implemented"
		out = append(out, protocolCoverage{
			Name: name, Kind: kind, SurfaceRegistered: ok, WireSchemaGenerated: support.wireSchemaGenerated,
			Status: support.Status, Notes: support.Notes, Tests: append([]string(nil), support.Tests...),
			// Legacy booleans stay conservative: partial is false. Evidence describes
			// specific regressions, never exhaustive Chrome parity.
			SemanticsImplemented: implemented, SemanticsVerified: implemented && len(support.Tests) != 0,
		})
	}
	for name := range methods {
		appendRow(name, "method")
	}
	for name := range events {
		appendRow(name, "event")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
