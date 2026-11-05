package matching

import "strings"

// oneLineError flattens an API error body so the whole message shows up on
// one log line (OpenAI returns pretty-printed JSON).
func oneLineError(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}
