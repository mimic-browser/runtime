package cdp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProtocolCoverageUsesCompleteGeneratedInventory(t *testing.T) {
	rows := protocolMatrix(protocolCommandNames(), protocolEventNames())
	if len(rows) != len(protocolCommands)+len(protocolEvents) {
		t.Fatalf("coverage dropped wire entries: %d", len(rows))
	}
	seen := map[string]protocolCoverage{}
	for i, row := range rows {
		if i > 0 && rows[i-1].Name >= row.Name {
			t.Fatalf("coverage is not uniquely sorted: %s", row.Name)
		}
		if !row.SurfaceRegistered || !row.WireSchemaGenerated {
			t.Fatalf("generated entry lost its schema: %#v", row)
		}
		if row.Status == "partial" && (row.SemanticsImplemented || row.SemanticsVerified) {
			t.Fatalf("partial behavior promoted by legacy flags: %#v", row)
		}
		seen[row.Name] = row
	}
	for _, name := range []string{"Page.captureScreenshot", "Debugger.enable", "Schema.getDomains", "Audits.enable", "WebMCP.enable", "Page.bringToFront"} {
		if row := seen[name]; row.Status != "unsupported" || row.SemanticsImplemented || row.SemanticsVerified {
			t.Errorf("wire/acknowledgment promoted to implementation: %#v", row)
		}
	}
	if row := seen["Runtime.evaluate"]; row.Status != "partial" || len(row.Tests) == 0 || row.Notes == "" {
		t.Fatal("Runtime scope/evidence disappeared", row)
	}
	if row := seen["Input.setIgnoreInputEvents"]; row.Status != "implemented" || !row.SemanticsVerified {
		t.Fatal("implemented command evidence disappeared", row)
	}
	for i := range rows {
		if len(rows[i].Tests) > 0 {
			rows[i].Tests[0] = "changed by caller"
		}
	}
	for _, row := range protocolMatrix(protocolCommandNames(), protocolEventNames()) {
		if len(row.Tests) > 0 && row.Tests[0] == "changed by caller" {
			t.Fatal("coverage exposes shared mutable evidence")
		}
	}
}

func TestProtocolSupportManifestHasLiveEvidenceAndNoLostHandlers(t *testing.T) {
	raw, err := os.ReadFile("protocol_support.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]protocolSupport
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "generated") || path == "coverage.go" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources.Write(raw)
	}
	for name, support := range manifest {
		for _, evidence := range support.Tests {
			path, anchor, _ := strings.Cut(evidence, "#")
			if filepath.IsAbs(path) || strings.Contains(path, "..") {
				t.Fatalf("evidence outside repository: %s", evidence)
			}
			raw, err := os.ReadFile(filepath.Join("../..", path))
			if err != nil || anchor == "" || !strings.Contains(string(raw), anchor) {
				t.Errorf("%s evidence no longer exists: %s (%v)", name, evidence, err)
			}
		}
		_, command := protocolCommands[name]
		if (command || strings.HasPrefix(name, "Mimic.")) && support.Status != "unsupported" && !strings.Contains(sources.String(), `"`+name+`"`) {
			t.Errorf("declared command no longer has a handler: %s", name)
		}
	}
	for _, row := range protocolExtensionMatrix() {
		_, chromeCommand := protocolCommands[row.Name]
		if (!row.WireSchemaGenerated && !manifest[row.Name].Experimental) || chromeCommand || !strings.HasPrefix(row.Name, "Mimic.") || row.Kind != "extension" {
			t.Fatalf("extension incorrectly promoted to Chrome wire schema: %+v", row)
		}
	}
	var contract struct {
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	contractRaw, err := os.ReadFile("protocol/mimic.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contractRaw, &contract); err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, command := range contract.Commands {
		declared[command.Name] = true
	}
	for _, match := range regexp.MustCompile(`"(Mimic\.[A-Za-z][A-Za-z0-9]*)"`).FindAllStringSubmatch(sources.String(), -1) {
		if !declared[match[1]] && !manifest[match[1]].Experimental {
			t.Errorf("Mimic handler absent from pinned SDK schema: %s", match[1])
		}
		if _, ok := manifest[match[1]]; !ok {
			t.Errorf("Mimic handler missing support declaration: %s", match[1])
		}
	}
	// Dispatch currently names each supported method explicitly. A new handler
	// must receive a reviewed scope even if its status remains unsupported.
	for name := range protocolCommands {
		if strings.Contains(sources.String(), `"`+name+`"`) {
			if _, declared := manifest[name]; !declared {
				t.Errorf("handler missing a reviewed support declaration: %s", name)
			}
		}
	}
}
