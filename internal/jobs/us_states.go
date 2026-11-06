package jobs

import (
	"strings"
	"unicode"
)

var usStateCodeSet = func() map[string]bool {
	set := map[string]bool{}
	for _, code := range strings.Fields("AL AK AZ AR CA CO CT DE DC FL GA HI ID IL IN IA KS KY LA ME MD MA MI MN MS MO MT NE NV NH NJ NM NY NC ND OH OK OR PA RI SC SD TN TX UT VT VA WA WV WI WY") {
		set[code] = true
	}
	return set
}()

var usStateNames = map[string]string{
	"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR", "california": "CA",
	"colorado": "CO", "connecticut": "CT", "delaware": "DE", "florida": "FL", "georgia": "GA",
	"hawaii": "HI", "idaho": "ID", "illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS",
	"kentucky": "KY", "louisiana": "LA", "maine": "ME", "maryland": "MD", "massachusetts": "MA",
	"michigan": "MI", "minnesota": "MN", "mississippi": "MS", "missouri": "MO", "montana": "MT",
	"nebraska": "NE", "nevada": "NV", "ohio": "OH", "oklahoma": "OK", "oregon": "OR",
	"pennsylvania": "PA", "tennessee": "TN", "texas": "TX", "utah": "UT", "vermont": "VT",
	"virginia": "VA", "wisconsin": "WI", "wyoming": "WY",
	"new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY",
	"north carolina": "NC", "north dakota": "ND", "rhode island": "RI", "south carolina": "SC",
	"south dakota": "SD", "west virginia": "WV", "district of columbia": "DC",
}

// usStatesIn returns the US states a location names: two-letter codes
// written in capitals or right after a comma ("Gainesville, fl"), and full
// state names. "Washington" counts as the state only when the place isn't
// Washington, D.C.
func usStatesIn(location string) map[string]bool {
	states := map[string]bool{}
	lower := strings.ToLower(location)
	isDC := strings.Contains(lower, "district of columbia") || strings.Contains(lower, "d.c") ||
		strings.Contains(" "+strings.Join(strings.FieldsFunc(lower, notLetter), " ")+" ", " dc ")

	fields := strings.FieldsFunc(location, func(r rune) bool { return notLetter(r) && r != ',' })
	var words []string
	afterComma := false
	for _, raw := range fields {
		for _, part := range strings.SplitAfter(raw, ",") {
			word := strings.TrimSuffix(part, ",")
			if word != "" {
				if len(word) == 2 && (afterComma || word == strings.ToUpper(word)) && usStateCodeSet[strings.ToUpper(word)] {
					states[strings.ToUpper(word)] = true
				}
				words = append(words, strings.ToLower(word))
				afterComma = false
			}
			if strings.HasSuffix(part, ",") {
				afterComma = true
			}
		}
	}
	for i := range words {
		if i+2 < len(words) {
			if code, ok := usStateNames[words[i]+" "+words[i+1]+" "+words[i+2]]; ok {
				states[code] = true
			}
		}
		if i+1 < len(words) {
			if code, ok := usStateNames[words[i]+" "+words[i+1]]; ok {
				states[code] = true
				continue
			}
		}
		if code, ok := usStateNames[words[i]]; ok {
			// "West Virginia" is not Virginia; "New York" is handled above.
			if i > 0 && (words[i-1] == "west" || words[i-1] == "new") {
				continue
			}
			states[code] = true
		}
		if words[i] == "washington" && !isDC {
			states["WA"] = true
		}
	}
	if isDC {
		states["DC"] = true
	}
	return states
}

func notLetter(r rune) bool { return !unicode.IsLetter(r) }

// statesConflict reports whether a job's location names US states and none
// of them is a state the search names ("Gainesville, FL" vs "Gainesville, GA").
func statesConflict(requested, jobLocation string) bool {
	want := usStatesIn(requested)
	have := usStatesIn(jobLocation)
	if len(want) == 0 || len(have) == 0 {
		return false
	}
	for state := range have {
		if want[state] {
			return false
		}
	}
	return true
}

// usStateFullName is the state's name for a code ("FL" -> "florida").
func usStateFullName(code string) string {
	for name, c := range usStateNames {
		if c == code {
			return name
		}
	}
	if code == "WA" {
		return "washington"
	}
	return ""
}
