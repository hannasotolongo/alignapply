package jobs

import "strings"

// StateOfPlace returns the two-letter US state a searched place is in
// ("Gainesville, FL", "32601", "Florida"), or "" when it can't tell.
func StateOfPlace(text string) string {
	if _, state, ok := geoLocate(text, "", true); ok {
		return state
	}
	if state, ok := geoStateOnly(text); ok {
		return state
	}
	return ""
}

// LocationNamesState reports whether a job's location text names the
// given state (by code or full name), like "Ocala, FL" for "FL".
func LocationNamesState(location, state string) bool {
	if state == "" {
		return false
	}
	return usStatesIn(location)[state]
}

// StateName is a state's full name ("Florida" for "FL"), or "" if unknown.
func StateName(code string) string {
	words := strings.Fields(usStateFullName(code))
	for i, word := range words {
		if word == "of" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// StatesIn lists the US state codes a job location names ("Tampa, FL" ->
// ["FL"]).
func StatesIn(location string) []string {
	found := usStatesIn(location)
	codes := make([]string, 0, len(found))
	for code := range found {
		codes = append(codes, code)
	}
	return codes
}
