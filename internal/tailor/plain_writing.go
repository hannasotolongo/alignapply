package tailor

import (
	"regexp"
	"strings"
	"unicode"
)

// plainWritingRules is added to every prompt that writes words a candidate
// will read or say: résumé lines, interview approaches, coaching and the
// stronger answer. It describes how a good human coach writes.
const plainWritingRules = `
HOW TO WRITE:
- Write like a sharp, friendly person talking to someone they're helping, not like a brochure. Short, plain sentences. Concrete nouns and verbs.
- Be specific to THIS job and THIS person: name the actual tool, task, number or situation. If a sentence would fit any job or any candidate, rewrite it or cut it.
- Speak to the candidate as "you" in coaching and approaches. Résumé lines have no "I".
- Never use these words or anything like them: leverage, utilize, spearhead, spearheaded, orchestrate, facilitate, robust, seamless, seamlessly, cutting-edge, state-of-the-art, dynamic, passionate, synergy, results-driven, detail-oriented, self-starter, go-getter, proven track record, delve, foster, navigate, landscape, tapestry, elevate, empower, unlock, harness, holistic, pivotal, crucial, meticulous, adept, showcase, game-changer, best-in-class, world-class, thrive, journey.
- No openers like "In today's fast-paced world", "Great question", "Absolutely", "Certainly", "It's important to note". No closing pep talk.
- No em dashes, no exclamation marks, no emojis, no "not only... but also".
`

var verbSwaps = map[string]string{
	"utilize": "use", "utilized": "used", "utilizes": "uses", "utilizing": "using",
	"leverage": "use", "leveraged": "used", "leverages": "uses", "leveraging": "using",
	"spearhead": "lead", "spearheaded": "led", "spearheads": "leads", "spearheading": "leading",
	"orchestrated": "organized", "orchestrate": "organize", "orchestrating": "organizing",
	"facilitated": "ran", "facilitate": "run", "facilitating": "running",
	"seamlessly": "smoothly", "seamless": "smooth",
	"robust": "solid", "meticulous": "careful", "meticulously": "carefully",
	"showcase": "show", "showcased": "showed", "showcasing": "showing",
	"commence": "start", "commenced": "started",
	"endeavor": "effort", "endeavors": "efforts",
	"plethora": "lot", "myriad": "many",
}

var wordRE = regexp.MustCompile(`[A-Za-z]+`)

var fillerOpeners = regexp.MustCompile(`(?i)^(?:great question[.!,]?\s*|absolutely[.!,]?\s*|certainly[.!,]?\s*|in today'?s (?:fast-paced|competitive|ever-changing) [a-z ]+?,\s*|it'?s (?:important|worth) (?:to note|noting) that\s*)`)

// Plain rewrites text Jane wrote: safe word swaps, no em dashes or
// exclamation marks, no filler openers. Facts, names and numbers are left
// alone.
func Plain(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	text = fillerOpeners.ReplaceAllString(strings.TrimSpace(text), "")
	text = wordRE.ReplaceAllStringFunc(text, func(word string) string {
		replacement, ok := verbSwaps[strings.ToLower(word)]
		if !ok {
			return word
		}
		if unicode.IsUpper(rune(word[0])) {
			return strings.ToUpper(replacement[:1]) + replacement[1:]
		}
		return replacement
	})
	text = strings.NewReplacer(" — ", ", ", "—", ", ", " – ", ", ", "!", ".").Replace(text)
	text = strings.ReplaceAll(text, "..", ".")
	text = strings.ReplaceAll(text, ", ,", ",")
	if text != "" {
		runes := []rune(text)
		runes[0] = unicode.ToUpper(runes[0])
		text = string(runes)
	}
	return strings.TrimSpace(text)
}

func plainAll(items []string) []string {
	for i := range items {
		items[i] = Plain(items[i])
	}
	return items
}
