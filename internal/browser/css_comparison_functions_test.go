package browser

import (
	"encoding/json"
	"os"
	"testing"
)

// Chrome 152 headful, normal launch, navigator.webdriver=false. Retained
// capture and launch provenance: testdata/css_comparison_functions_chrome152.json.
func TestCSSComparisonLengthFunctions(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	data, err := os.ReadFile("testdata/css_comparison_functions_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct{ Result struct{ Rows [][2]string } }
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	rows, err := json.Marshal(capture.Result.Rows)
	if err != nil {
		t.Fatal(err)
	}
	historyEval(t, p, `(() => {
 const cases = `+string(rows)+`;
 const style = document.createElement('div').style;
 for (const [input, expected] of cases) {
  style.cssText = 'height:' + input;
  if (style.height !== expected) return [input, style.height, expected];
  const sheet = new CSSStyleSheet();
  sheet.replaceSync('.scene { height:' + input + ' }');
  if (sheet.cssRules[0].style.height !== expected) return ['sheet', input, sheet.cssRules[0].style.height];
 }
 return true;
})()`, true)
}
