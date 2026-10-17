package network

import "unicode/utf8"

// PostDataText matches Chrome's textual request-body projection: decode UTF-8
// only when the entire body is valid, otherwise map every byte to U+0000..U+00FF.
// This is a display projection; retain original bytes for transport and replay.
func PostDataText(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	text := make([]rune, len(body))
	for i, value := range body {
		text[i] = rune(value)
	}
	return string(text)
}
