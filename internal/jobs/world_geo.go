package jobs

import (
	_ "embed"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Cities of 15,000+ people in the countries Jane searches outside the US,
// from GeoNames (https://www.geonames.org), licensed CC BY 4.0. Built by
// jane_world_places.py: name, country, region, latitude, longitude,
// population.
//
//go:embed geodata/world_places.tsv.gz
var worldPlacesGz []byte

// WorldCountries are the countries outside the US whose jobs Jane keeps
// in full and can search by city.
var WorldCountries = map[string]string{
	"CA": "Canada", "GB": "United Kingdom", "IE": "Ireland", "AU": "Australia",
	"NZ": "New Zealand", "IN": "India", "SG": "Singapore",
}

type worldPlace struct {
	point   geoPoint
	country string
	region  string
	people  int
}

var (
	worldOnce     sync.Once
	worldByName   map[string][]worldPlace // biggest first
	worldMaxWords int
	worldRegions  map[string][2]string // "ontario" -> CA, Ontario
)

// Words people write for each country.
var worldCountryNames = map[string]string{
	"canada": "CA", "united kingdom": "GB", "uk": "GB", "great britain": "GB", "britain": "GB",
	"england": "GB", "scotland": "GB", "wales": "GB", "northern ireland": "GB", "ireland": "IE",
	"republic of ireland": "IE", "eire": "IE", "australia": "AU", "new zealand": "NZ", "india": "IN",
	"singapore": "SG",
}

// Region abbreviations written after a comma in capitals ("Toronto, ON",
// "Sydney NSW"). WA and NT are left out: WA is Washington state.
var worldRegionCodes = map[string][2]string{
	"ON": {"CA", "Ontario"}, "BC": {"CA", "British Columbia"}, "QC": {"CA", "Quebec"}, "AB": {"CA", "Alberta"},
	"MB": {"CA", "Manitoba"}, "SK": {"CA", "Saskatchewan"}, "NS": {"CA", "Nova Scotia"}, "NB": {"CA", "New Brunswick"},
	"NL": {"CA", "Newfoundland and Labrador"}, "PE": {"CA", "Prince Edward Island"}, "PEI": {"CA", "Prince Edward Island"},
	"YT": {"CA", "Yukon"}, "NU": {"CA", "Nunavut"},
	"NSW": {"AU", "New South Wales"}, "VIC": {"AU", "Victoria"}, "QLD": {"AU", "Queensland"},
	"ACT": {"AU", "Australian Capital Territory"}, "TAS": {"AU", "Tasmania"},
}

var worldRegionCodeRE = regexp.MustCompile(`(?:,|\s)\s*(ON|BC|QC|AB|MB|SK|NS|NB|NL|PEI|PE|YT|NU|NSW|VIC|QLD|ACT|TAS)\b`)

// Region names that are also something else people type ("Victoria" is a
// city in Canada and a state in Australia), so they don't name a region
// on their own.
var worldRegionSkip = map[string]bool{"victoria": true, "auckland": true, "delhi": true, "chandigarh": true, "nelson region": true, "southland": true, "northland": true, "punjab": false}

func loadWorld() {
	worldOnce.Do(func() {
		worldByName = map[string][]worldPlace{}
		worldRegions = map[string][2]string{}
		readGzipLines(worldPlacesGz, func(fields []string) {
			if len(fields) < 6 {
				return
			}
			lat, _ := strconv.ParseFloat(fields[3], 64)
			lon, _ := strconv.ParseFloat(fields[4], 64)
			people, _ := strconv.Atoi(fields[5])
			name := geoNormalize(fields[0])
			if name == "" {
				return
			}
			place := worldPlace{geoPoint{lat, lon}, fields[1], fields[2], people}
			worldByName[name] = append(worldByName[name], place)
			if words := len(strings.Fields(name)); words > worldMaxWords {
				worldMaxWords = words
			}
			if region := geoNormalize(fields[2]); region != "" && !worldRegionSkip[region] {
				worldRegions[region] = [2]string{fields[1], fields[2]}
			}
		})
		for name := range worldByName {
			places := worldByName[name]
			sort.Slice(places, func(i, j int) bool { return places[i].people > places[j].people })
		}
	})
}

// worldKey is the "state" Jane uses for a place outside the US:
// "@CA:Ontario", or "@SG:" for a country without regions in use.
func worldKey(country, region string) string { return "@" + country + ":" + region }

func parseWorldKey(key string) (country, region string, ok bool) {
	if !strings.HasPrefix(key, "@") {
		return "", "", false
	}
	country, region, _ = strings.Cut(key[1:], ":")
	return country, region, country != ""
}

// worldCountryIn finds a country outside the US (and its region, when
// given) that a location names: "Toronto, ON", "London, UK", "Bengaluru,
// Karnataka, India", "Remote - Canada".
func worldCountryIn(text string) (country, region string) {
	if strings.TrimSpace(text) == "" {
		return "", ""
	}
	loadWorld()
	if match := worldRegionCodeRE.FindStringSubmatch(text); match != nil {
		found := worldRegionCodes[match[1]]
		return found[0], found[1]
	}
	words := strings.Fields(strings.NewReplacer("-", " ", "/", " ").Replace(geoNormalize(text)))
	joined := " " + strings.Join(words, " ") + " "
	for name, code := range worldCountryNames {
		if strings.Contains(joined, " "+name+" ") && !(name == "england" && strings.Contains(joined, " new england ")) {
			country = code
			break
		}
	}
	// "Ontario, CA" and "Canterbury, CT" are US towns: a US state wins over
	// a region name abroad unless the country is named too.
	if country == "" && len(usStatesIn(text)) > 0 {
		return "", ""
	}
	// "New South Wales" names Australia even without the country.
	for size := min(4, len(words)); size >= 1; size-- {
		for start := 0; start+size <= len(words); start++ {
			phrase := strings.Join(words[start:start+size], " ")
			if phrase == "england" && strings.Contains(joined, " new england ") {
				continue
			}
			if found, ok := worldRegions[phrase]; ok && (country == "" || country == found[0]) {
				return found[0], found[1]
			}
		}
	}
	return country, ""
}

// worldLocate finds a city in a country (and region, when given).
func worldLocate(text, country, region string) (geoPoint, string, bool) {
	loadWorld()
	words := strings.Fields(strings.NewReplacer("-", " ", "/", " ").Replace(geoNormalize(text)))
	for size := min(worldMaxWords, len(words)); size >= 1; size-- {
		for start := 0; start+size <= len(words); start++ {
			phrase := strings.Join(words[start:start+size], " ")
			if size == 1 && (len(phrase) < 3 || geoSkipWords[phrase]) {
				continue
			}
			if _, isCountry := worldCountryNames[phrase]; isCountry {
				continue
			}
			for _, place := range worldByName[phrase] {
				if place.country != country || (region != "" && place.region != region) {
					continue
				}
				return place.point, worldKey(place.country, place.region), true
			}
		}
	}
	return geoPoint{}, "", false
}

// worldBigCity finds a city of 2 million+ people named alone ("London",
// "Toronto", "Bengaluru"): someone typing just that means the big city,
// not a US town with the same name.
func worldBigCity(text string) (geoPoint, string, bool) {
	loadWorld()
	name := geoNormalize(text)
	if places := worldByName[name]; len(places) > 0 && places[0].people >= 2_000_000 {
		return places[0].point, worldKey(places[0].country, places[0].region), true
	}
	return geoPoint{}, "", false
}

// SearchCountry is the country a searched place is in, when it's one of
// the countries outside the US Jane searches.
func SearchCountry(location string) string {
	if country, _ := worldCountryIn(location); country != "" {
		return country
	}
	if len(usStatesIn(location)) == 0 {
		if _, key, ok := worldBigCity(location); ok {
			country, _, _ := parseWorldKey(key)
			return country
		}
	}
	return ""
}
