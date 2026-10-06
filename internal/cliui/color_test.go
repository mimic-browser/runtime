package cliui

import "testing"

func TestTerminalDiagnosticExcerptsArePlain(t *testing.T) {
	for _, input := range []string{
		"\x1b[31massertion failed\x1b[0m\n",
		"\x1b]8;;https://example.test\aassertion failed\x1b]8;;\a\n",
		"\x1b]0;window title\x1b\\assertion failed\n\x1b[2J",
	} {
		if got := TerminalText(input); got != "assertion failed\n" {
			t.Fatalf("terminal control escaped into diagnostics: %q", got)
		}
	}
	if got := TerminalText("Ошибка\tassertion\n"); got != "Ошибка\tassertion\n" {
		t.Fatal("ordinary Unicode diagnostics changed")
	}
}
