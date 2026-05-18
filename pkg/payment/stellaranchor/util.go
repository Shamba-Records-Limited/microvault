package stellaranchor

// truncate returns the first n characters of s with an ellipsis appended when
// the string was longer. Used to keep error messages bounded so a misbehaving
// anchor cannot dump unbounded payloads into logs.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
