package jobs

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
