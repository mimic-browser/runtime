package webapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moreveal/mimic/compatibility"
)

func TestSelectedCatalogRetainsAncestorsAliasesAndStaticMembers(t *testing.T) {
	source := `[
 {"name":"Base","parent":"","members":[{"name":"base","kind":"operation"},{"name":"absent","kind":"operation"}]},
 {"name":"Visible","parent":"Base","members":[{"name":"base","kind":"operation"},{"name":"own","kind":"operation"},{"name":"hidden","kind":"operation"},{"name":"from","kind":"operation","static":true},{"name":"CONST","kind":"constant"}]},
 {"name":"AliasTarget","parent":"","legacyWindowAliases":["Alias"],"members":[]},
 {"name":"Disabled","parent":"","members":[]} ]`
	exposure := compatibility.RealmExposure{
		Properties: []compatibility.SurfaceProperty{{Name: "Visible"}, {Name: "Alias"}},
		Prototypes: map[string][]compatibility.SurfaceProperty{"Visible": {{Name: "own"}}, "Base": {{Name: "base"}}},
	}
	result := selectedCatalog(source, exposure)
	if strings.Contains(result, `"origin"`) || strings.Contains(result, `"arguments"`) {
		t.Fatalf("shape catalog retained semantic metadata: %s", result)
	}
	var got []struct {
		Name    string
		Members []struct{ Name string }
	}
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "Base" || len(got[0].Members) != 1 || got[1].Name != "Visible" || len(got[1].Members) != 4 || got[2].Name != "AliasTarget" {
		t.Fatalf("filtered catalog: %#v", got)
	}
	if selectedCatalog("not JSON", exposure) != "not JSON" {
		t.Fatal("custom catalog fallback changed")
	}
	malformedMembers := `[{"name":"Visible","members":{}}]`
	if selectedCatalog(malformedMembers, exposure) != malformedMembers {
		t.Fatal("malformed member catalog fallback changed")
	}
}
