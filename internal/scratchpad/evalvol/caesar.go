package evalvol

// Caesar shifts each ASCII letter in s by shift positions, wrapping within A-Z and a-z.
// Non-letter bytes are passed through unchanged. Negative and large shifts are supported.
func Caesar(s string, shift int) string {
	shift = ((shift % 26) + 26) % 26
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out[i] = 'A' + (c-'A'+byte(shift))%26
		case c >= 'a' && c <= 'z':
			out[i] = 'a' + (c-'a'+byte(shift))%26
		default:
			out[i] = c
		}
	}
	return string(out)
}
