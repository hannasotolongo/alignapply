package jobs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// US places and ZIP codes with their coordinates, from the US Census
// Bureau's Gazetteer files (public domain). They let a search for any town,
// city or ZIP code find the jobs within driving distance, instead of only
// the jobs whose location text names the same city.
//
//go:embed geodata/us_places.tsv.gz
var usPlacesGz []byte

//go:embed geodata/us_zips.tsv.gz
var usZipsGz []byte

type geoPoint struct{ lat, lon float64 }

type geoPlace struct {
	point geoPoint
	state string
	size  int // land area, used to pick the main place when no state is given
}

var (
	geoOnce        sync.Once
	geoByState     map[string]geoPoint // "name|ST"
	geoByName      map[string]geoPlace // name -> largest place with that name
	geoZips        map[string]geoPoint
	geoMaxWords    int
	geoNameStates  map[string]int // how many states have a place with this name
	geoStateMiddle = map[string]geoPoint{}
)

func loadGeo() {
	geoOnce.Do(func() {
		geoByState = map[string]geoPoint{}
		geoByName = map[string]geoPlace{}
		geoZips = map[string]geoPoint{}
		geoNameStates = map[string]int{}
		add := func(name, state string, point geoPoint, size int, city bool) {
			name = geoNormalize(name)
			if name == "" {
				return
			}
			key := name + "|" + state
			if _, ok := geoByState[key]; !ok {
				geoByState[key] = point
				geoNameStates[name]++
			}
			// A bare name means the biggest city or town with it;
			// townships only count when no city has the name.
			if city {
				size += 1 << 20
			}
			if current, ok := geoByName[name]; !ok || size > current.size {
				geoByName[name] = geoPlace{point, state, size}
			}
			if words := len(strings.Fields(name)); words > geoMaxWords {
				geoMaxWords = words
			}
		}
		stateSum := map[string][3]float64{}
		readGzipLines(usPlacesGz, func(fields []string) {
			if len(fields) < 5 {
				return
			}
			lat, _ := strconv.ParseFloat(fields[2], 64)
			lon, _ := strconv.ParseFloat(fields[3], 64)
			size, _ := strconv.Atoi(fields[4])
			name, state := fields[0], fields[1]
			city := len(fields) < 6 || fields[5] == "p"
			if strings.HasSuffix(name, " ccd") || strings.HasSuffix(name, " division") {
				return // census county divisions aren't names people use
			}
			add(name, state, geoPoint{lat, lon}, size, city)
			// "Nashville-Davidson" and "Louisville/Jefferson County" are
			// also known by their first part.
			if i := strings.IndexAny(name, "-/"); i > 0 {
				add(name[:i], state, geoPoint{lat, lon}, size, city)
			}
			sum := stateSum[state]
			stateSum[state] = [3]float64{sum[0] + lat, sum[1] + lon, sum[2] + 1}
		})
		for state, sum := range stateSum {
			geoStateMiddle[state] = geoPoint{sum[0] / sum[2], sum[1] / sum[2]}
		}
		readGzipLines(usZipsGz, func(fields []string) {
			if len(fields) < 3 {
				return
			}
			lat, _ := strconv.ParseFloat(fields[1], 64)
			lon, _ := strconv.ParseFloat(fields[2], 64)
			geoZips[fields[0]] = geoPoint{lat, lon}
		})
	})
}

func readGzipLines(data []byte, each func([]string)) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		each(strings.Split(scanner.Text(), "\t"))
	}
}

var geoWordFixes = map[string]string{"saint": "st", "ste": "st", "ft": "fort", "mt": "mount", "pt": "point"}

// geoNormalize makes place names comparable: "St. Petersburg",
// "Saint Petersburg" and "st petersburg" are the same.
func geoNormalize(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer(".", "", "'", "", "’", "").Replace(value)
	words := strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '/')
	})
	for i, word := range words {
		if fixed, ok := geoWordFixes[word]; ok {
			words[i] = fixed
		}
	}
	return strings.Join(words, " ")
}

// Words that are also tiny place names somewhere but almost always mean
// something else in a job's location text.
var geoSkipWords = map[string]bool{
	"remote": true, "hybrid": true, "onsite": true, "clinic": true, "hospital": true, "center": true,
	"office": true, "campus": true, "main": true, "home": true, "us": true, "usa": true, "united": true,
	"states": true, "north": true, "south": true, "east": true, "west": true, "central": true,
	"union": true, "city": true, "county": true, "area": true, "metro": true, "region": true,
	"location": true, "locations": true, "store": true, "building": true, "suite": true, "street": true,
	"st": true, "road": true, "health": true, "medical": true, "care": true, "dental": true, "office of": true,
}

var zipRE = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\b`)

// geoLocate finds where a location text points: a ZIP code, or a town or
// city with its state. hintState is used when the text names no state
// ("ADVENTHEALTH ORLANDO" in a Florida search). anyState lets a bare city
// name ("Gainesville") mean the largest place with that name.
func geoLocate(text, hintState string, anyState bool) (geoPoint, string, bool) {
	key := text + "|" + hintState + "|" + strconv.FormatBool(anyState)
	geoCacheMu.Lock()
	if found, ok := geoCache[key]; ok {
		geoCacheMu.Unlock()
		return found.point, found.state, found.ok
	}
	geoCacheMu.Unlock()
	point, state, ok := geoLocateUncached(text, hintState, anyState)
	geoCacheMu.Lock()
	if len(geoCache) > 100000 {
		geoCache = map[string]geoResult{}
	}
	geoCache[key] = geoResult{point, state, ok}
	geoCacheMu.Unlock()
	return point, state, ok
}

type geoResult struct {
	point geoPoint
	state string
	ok    bool
}

var (
	geoCacheMu sync.Mutex
	geoCache   = map[string]geoResult{}
)

func geoLocateUncached(text, hintState string, anyState bool) (geoPoint, string, bool) {
	loadGeo()
	if strings.TrimSpace(text) == "" {
		return geoPoint{}, "", false
	}
	states := usStatesIn(text)
	if match := zipRE.FindStringSubmatch(text); match != nil {
		if point, ok := geoZips[match[1]]; ok {
			state := ""
			for code := range states {
				state = code
			}
			if state == "" {
				state = zipState(match[1])
			}
			if state == "" {
				state = geoNearestState(point)
			}
			return point, state, true
		}
	}
	var candidates []string
	for code := range states {
		candidates = append(candidates, code)
	}
	if len(candidates) == 0 && hintState != "" {
		candidates = []string{hintState}
	}
	words := strings.Fields(strings.NewReplacer("-", " ", "/", " ").Replace(geoNormalize(text)))
	for size := min(geoMaxWords, len(words)); size >= 1; size-- {
		for start := 0; start+size <= len(words); start++ {
			phrase := strings.Join(words[start:start+size], " ")
			if size == 1 && (len(phrase) < 3 || geoSkipWords[phrase]) {
				continue
			}
			if _, isState := usStateNames[phrase]; isState && size <= 2 && phrase != "new york" {
				// "Florida" alone is a state, not the town of Florida, NY.
				continue
			}
			for _, state := range candidates {
				point, ok := geoByState[phrase+"|"+state]
				if !ok {
					continue
				}
				// With only the search's state to go on, "Cleveland Clinic
				// Main Campus" is Cleveland, Ohio, not Cleveland, Florida:
				// trust the hint only when the best-known place with this
				// name is in that state.
				// "Clear Lake" and "Lake Hallie" name towns in many states;
				// without the job naming its state, only a name that is
				// best known in the searched state and used in few states
				// counts.
				if len(states) == 0 && (geoByName[phrase].state != state || geoNameStates[phrase] > 4) {
					continue
				}
				return point, state, true
			}
			if len(candidates) == 0 && anyState {
				if place, ok := geoByName[phrase]; ok && size >= 1 {
					return place.point, place.state, true
				}
			}
		}
	}
	return geoPoint{}, "", false
}

// geoStateOnly reports the state of a search that names only a state
// ("Florida", "TX").
func geoStateOnly(text string) (string, bool) {
	if _, _, ok := geoLocate(text, "", true); ok {
		return "", false
	}
	states := usStatesIn(text)
	if len(states) != 1 {
		return "", false
	}
	for code := range states {
		return code, true
	}
	return "", false
}

func distanceMiles(a, b geoPoint) float64 {
	const earthMiles = 3958.8
	toRad := math.Pi / 180
	dLat := (b.lat - a.lat) * toRad
	dLon := (b.lon - a.lon) * toRad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.lat*toRad)*math.Cos(b.lat*toRad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthMiles * math.Asin(math.Sqrt(h))
}

// Search radius: jobs this many miles from the searched place count as
// local. A thin result widens it step by step (see Pipeline.Process).
const (
	defaultRadiusMiles = 40
	// wholeStateRadius means "anywhere in the searched state, or within
	// 150 miles".
	wholeStateRadius = -1
)

// jobDistance is how far a job is from the searched place, when both
// can be placed on the map.
func jobDistance(request SearchRequest, job Job) (miles float64, jobState string, ok bool) {
	from, requestState, ok := geoLocate(request.Location, "", true)
	if !ok {
		return 0, "", false
	}
	to, state, ok := geoLocate(job.Location, requestState, false)
	if !ok {
		return 0, "", false
	}
	return distanceMiles(from, to), state, true
}

// geoNearestState is the state of the place nearest a point (for a bare
// ZIP code).
func geoNearestState(point geoPoint) string {
	best, bestMiles := "", math.MaxFloat64
	for key, place := range geoByState {
		if miles := distanceMiles(point, place); miles < bestMiles {
			bestMiles = miles
			best = key[strings.LastIndexByte(key, '|')+1:]
		}
	}
	return best
}
