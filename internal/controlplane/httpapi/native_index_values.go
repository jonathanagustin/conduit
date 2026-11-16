package httpapi

import "unicode/utf8"

// Match the replacement of malformed stored UTF-8 in decoded native JSON.
func nativeIndexString(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}
