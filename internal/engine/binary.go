package engine

// BinaryBuffer asks the runtime to copy bytes into a realm-owned ArrayBuffer.
// The caller keeps ownership of the source slice and may reuse it after the
// synchronous conversion returns. JavaScript writes must never alias the source.
// Unlike ordinary byte slices, this explicit transport value is not a JS array
// or a base64 string. It can be nested in host records and Promise results.
type BinaryBuffer []byte

// BinaryString projects bytes directly to JavaScript U+0000..U+00FF code units.
// This is Latin-1 binary text, never UTF-8 decoding or a base64 representation.
// JavaScript strings own a copy; source bytes must remain unchanged until the
// runtime conversion (or enclosing synchronous host callback) has returned.
type BinaryString []byte

// UTF8 supplies the same code-unit mapping to runtimes accepting Go strings.
func (b BinaryString) UTF8() string {
	text := make([]rune, len(b))
	for i, value := range b {
		text[i] = rune(value)
	}
	return string(text)
}
