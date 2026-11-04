package jobs

import "strings"

// usMetros groups cities that job seekers treat as one commuting area.
// Job postings name the specific suburb ("Bay Harbor Islands, FL",
// "Fort Lauderdale") while people search for the core city ("Miami").
var usMetros = map[string][]string{
	"miami":          {"miami", "miami beach", "north miami", "north miami beach", "miami gardens", "miami lakes", "bay harbor islands", "coral gables", "doral", "hialeah", "homestead", "kendall", "aventura", "sunny isles", "key biscayne", "fort lauderdale", "ft lauderdale", "ft. lauderdale", "hollywood", "pembroke pines", "miramar", "davie", "plantation", "sunrise", "weston", "coral springs", "pompano beach", "boca raton", "delray beach", "boynton beach", "west palm beach", "palm beach", "jupiter", "deerfield beach", "dania beach", "hallandale"},
	"tampa":          {"tampa", "st petersburg", "st. petersburg", "saint petersburg", "clearwater", "brandon", "largo", "wesley chapel", "lakeland", "sarasota", "bradenton"},
	"orlando":        {"orlando", "kissimmee", "winter park", "sanford", "lake mary", "altamonte springs", "oviedo", "celebration", "daytona beach"},
	"jacksonville":   {"jacksonville", "orange park", "ponte vedra", "st augustine", "fernandina beach"},
	"gainesville":    {"gainesville", "alachua", "ocala"},
	"tallahassee":    {"tallahassee"},
	"new york":       {"new york", "new york city", "nyc", "manhattan", "brooklyn", "queens", "bronx", "staten island", "jersey city", "hoboken", "newark", "long island", "white plains", "yonkers", "stamford"},
	"los angeles":    {"los angeles", "santa monica", "pasadena", "burbank", "glendale", "long beach", "irvine", "anaheim", "torrance", "culver city", "el segundo", "west hollywood", "beverly hills", "costa mesa", "orange county"},
	"san francisco":  {"san francisco", "oakland", "berkeley", "san jose", "palo alto", "mountain view", "sunnyvale", "santa clara", "menlo park", "redwood city", "san mateo", "fremont", "cupertino", "bay area"},
	"chicago":        {"chicago", "evanston", "naperville", "schaumburg", "oak brook", "skokie", "aurora", "joliet"},
	"dallas":         {"dallas", "fort worth", "ft worth", "plano", "irving", "frisco", "arlington", "richardson", "addison", "denton", "mckinney"},
	"houston":        {"houston", "the woodlands", "sugar land", "katy", "pasadena", "pearland", "spring"},
	"austin":         {"austin", "round rock", "cedar park", "georgetown", "pflugerville", "san marcos"},
	"atlanta":        {"atlanta", "alpharetta", "marietta", "sandy springs", "decatur", "duluth", "kennesaw", "roswell", "smyrna"},
	"washington":     {"washington dc", "washington d c", "washington, dc", "dc", "arlington", "alexandria", "bethesda", "rockville", "silver spring", "mclean", "reston", "tysons", "herndon", "fairfax"},
	"boston":         {"boston", "cambridge", "somerville", "waltham", "quincy", "newton", "burlington", "woburn"},
	"seattle":        {"seattle", "bellevue", "redmond", "kirkland", "tacoma", "everett", "bothell"},
	"denver":         {"denver", "boulder", "aurora", "lakewood", "englewood", "littleton", "broomfield"},
	"phoenix":        {"phoenix", "scottsdale", "tempe", "mesa", "chandler", "glendale", "gilbert", "peoria"},
	"philadelphia":   {"philadelphia", "king of prussia", "camden", "cherry hill", "conshohocken", "wilmington"},
	"charlotte":      {"charlotte", "concord", "gastonia", "rock hill", "fort mill"},
	"nashville":      {"nashville", "franklin", "brentwood", "murfreesboro"},
	"minneapolis":    {"minneapolis", "st paul", "saint paul", "bloomington", "eden prairie", "edina"},
	"san diego":      {"san diego", "la jolla", "carlsbad", "chula vista", "escondido", "oceanside"},
	"detroit":        {"detroit", "ann arbor", "troy", "southfield", "dearborn", "warren"},
	"raleigh":        {"raleigh", "durham", "chapel hill", "cary", "research triangle", "morrisville"},
	"salt lake city": {"salt lake city", "lehi", "provo", "sandy", "draper", "south jordan", "ogden"},
	"las vegas":      {"las vegas", "henderson", "north las vegas", "paradise"},
	"portland":       {"portland", "beaverton", "hillsboro", "vancouver wa"},
}

var metroLookup = func() []struct{ city, metro string } {
	var list []struct{ city, metro string }
	for metro, cities := range usMetros {
		for _, city := range cities {
			list = append(list, struct{ city, metro string }{normalizePreferenceText(city), metro})
		}
	}
	return list
}()

// metrosIn returns every metro area whose city names appear as whole words
// in the normalized location text.
func metrosIn(normalized string) map[string]struct{} {
	found := make(map[string]struct{})
	if normalized == "" {
		return found
	}
	padded := " " + normalized + " "
	for _, entry := range metroLookup {
		if entry.city == "" {
			continue
		}
		// Very short aliases ("dc", "nyc") must match exactly as words.
		if strings.Contains(padded, " "+entry.city+" ") {
			found[entry.metro] = struct{}{}
		}
	}
	return found
}

// usStateTokens are tokens that identify a state, not a city. Sharing only a
// state ("Miami, FL" vs "Orlando, FL") is not a local match.
var usStateTokens = func() map[string]struct{} {
	tokens := []string{
		"al", "ak", "az", "ar", "ca", "co", "ct", "de", "fl", "ga", "hi", "id", "il", "in", "ia", "ks", "ky", "la", "me", "md", "ma", "mi", "mn", "ms", "mo", "mt", "ne", "nv", "nh", "nj", "nm", "ny", "nc", "nd", "oh", "ok", "or", "pa", "ri", "sc", "sd", "tn", "tx", "ut", "vt", "va", "wa", "wv", "wi", "wy",
		"alabama", "alaska", "arizona", "arkansas", "california", "colorado", "connecticut", "delaware", "florida", "georgia", "hawaii", "idaho", "illinois", "indiana", "iowa", "kansas", "kentucky", "louisiana", "maine", "maryland", "massachusetts", "michigan", "minnesota", "mississippi", "missouri", "montana", "nebraska", "nevada", "ohio", "oklahoma", "oregon", "pennsylvania", "tennessee", "texas", "utah", "vermont", "virginia", "washington", "wisconsin", "wyoming",
		"new", "north", "south", "west", "carolina", "dakota", "hampshire", "jersey", "mexico", "rhode", "island",
		"us", "usa", "united", "states", "america",
	}
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		set[token] = struct{}{}
	}
	return set
}()
