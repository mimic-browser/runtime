package cliui

import (
	"os"
	"strings"
)

// SupportsColor enables portable ANSI output only for a capable terminal.
func SupportsColor(output *os.File) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	return IsInteractive(output) && os.Getenv("TERM") != "dumb" && enableTerminalColor(output)
}

// TerminalText makes a diagnostic excerpt safe to echo. Original child output
// remains in its log file; terminal controls must not clear the screen, emit
// hyperlinks or override the CLI's plain/NO_COLOR presentation.
func TerminalText(input string) string {
	var output strings.Builder
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if char == '\x1b' {
			i++
			if i >= len(runes) {
				break
			}
			switch runes[i] {
			case '[':
				for i++; i < len(runes); i++ {
					if runes[i] >= 0x40 && runes[i] <= 0x7e {
						break
					}
				}
			case ']':
				for i++; i < len(runes); i++ {
					if runes[i] == '\a' {
						break
					}
					if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '\\' {
						i++
						break
					}
				}
			}
			continue
		}
		if char == '\n' || char == '\t' || char >= 0x20 && !(char >= 0x7f && char <= 0x9f) {
			output.WriteRune(char)
		}
	}
	return output.String()
}
