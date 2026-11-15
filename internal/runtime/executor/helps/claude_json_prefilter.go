package helps

import "bytes"

















func jsonMayContainASCII(body []byte, needles ...string) bool {
	if hasPrintableASCIIUnicodeEscape(body) {
		return true
	}
	for _, needle := range needles {
		if bytes.Contains(body, []byte(needle)) {
			return true
		}
	}
	return false
}







func hasPrintableASCIIUnicodeEscape(body []byte) bool {
	for rest := body; ; {
		i := bytes.Index(rest, []byte(`\u00`))
		if i < 0 || i+4 >= len(rest) {
			return false
		}
		if c := rest[i+4]; c >= '2' && c <= '7' {
			return true
		}
		rest = rest[i+4:]
	}
}
