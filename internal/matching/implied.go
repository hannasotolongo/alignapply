package matching

import (
	"regexp"
	"strings"

	"github.com/hannasotolongo/casemade-backend/internal/candidate"
)

// Some requirements are shown by a résumé without being named. Someone who
// writes Go and Python does object-oriented programming; React plus a
// backend language is full-stack work; "cold calls" is cold calling. These
// rules add the implied wording as one extra piece of skill evidence, so
// the word matcher can see it. Each rule needs specific, unambiguous words
// on the résumé; nothing is implied from job titles alone except where the
// title is the skill (a software engineer writes code).
type impliedRule struct {
	when    []*regexp.Regexp // every pattern must match the résumé
	implies string
}

func anyOf(words ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^a-z0-9+#])(?:` + strings.Join(words, "|") + `)(?:$|[^a-z0-9+#])`)
}

var (
	oopLanguages  = anyOf(`java`, `python`, `c\+\+`, `c#`, `golang`, `go`, `typescript`, `ruby`, `kotlin`, `swift`, `scala`, `php`, `\.net`, `objective-c`)
	frontendTech  = anyOf(`react`, `angular`, `vue(?:\.js)?`, `svelte`, `next\.js`, `frontend`, `front-end`)
	backendTech   = anyOf(`node(?:\.js)?`, `django`, `flask`, `rails`, `spring`, `express`, `golang`, `go`, `python`, `java`, `ruby`, `php`, `\.net`, `backend`, `back-end`)
	csGrounding   = anyOf(`computer science`, `computer engineering`, `software engineer(?:ing)?`, `software developer`)
	cloudTech     = anyOf(`aws`, `amazon web services`, `gcp`, `google cloud`, `azure`)
	containerTech = anyOf(`docker`, `kubernetes`, `k8s`, `eks`, `gke`, `aks`, `ecs`)
	sqlTech       = anyOf(`sql`, `postgres(?:ql)?`, `mysql`, `sql server`, `oracle`, `sqlite`, `snowflake`, `bigquery`, `redshift`)
	apiTech       = anyOf(`rest(?:ful)?`, `grpc`, `graphql`, `apis?`)
	coldCalls     = anyOf(`cold calls?`, `cold calling`, `cold-calling`, `outbound calls?`)
	quotaWork     = anyOf(`quota`, `closed \$`, `arr`, `new business`, `account executive`)
	fullCycle     = anyOf(`full sales cycle`, `full-cycle`, `full cycle`, `prospecting to close`)
	nursingCare   = anyOf(`registered nurse`, `rn`, `bsn`, `lpn`, `nursing`)
	patientCare   = anyOf(`patients?`, `patient care`, `bedside`)
)

var impliedRules = []impliedRule{
	{[]*regexp.Regexp{oopLanguages}, "object oriented programming oop languages coding"},
	{[]*regexp.Regexp{frontendTech, backendTech}, "full stack fullstack web applications"},
	{[]*regexp.Regexp{csGrounding}, "data structures algorithms software design programming coding"},
	{[]*regexp.Regexp{cloudTech}, "cloud infrastructure cloud platforms"},
	{[]*regexp.Regexp{containerTech}, "containers containerization"},
	{[]*regexp.Regexp{sqlTech}, "relational databases sql databases"},
	{[]*regexp.Regexp{apiTech}, "apis web services"},
	{[]*regexp.Regexp{coldCalls}, "cold calling outbound prospecting high volume calls"},
	{[]*regexp.Regexp{quotaWork}, "closing sales quota carrying closing role"},
	{[]*regexp.Regexp{fullCycle}, "full cycle sales closing discovery"},
	{[]*regexp.Regexp{nursingCare, patientCare}, "patient care clinical nursing"},
}

// addImpliedEvidence appends one skill evidence line holding everything the
// résumé implies.
func addImpliedEvidence(profile *candidate.Profile) {
	var all strings.Builder
	all.WriteString(profile.RawResumeText)
	for _, item := range profile.AllEvidence {
		all.WriteString("\n")
		all.WriteString(item.Text)
	}
	text := all.String()
	if strings.TrimSpace(text) == "" {
		return
	}
	var implied []string
	for _, rule := range impliedRules {
		matched := true
		for _, pattern := range rule.when {
			if !pattern.MatchString(text) {
				matched = false
				break
			}
		}
		if matched {
			implied = append(implied, rule.implies)
		}
	}
	if len(implied) == 0 {
		return
	}
	evidence := candidate.Evidence{Text: strings.Join(implied, ", "), Category: candidate.EvidenceSkill, Source: "implied"}
	profile.Skills = append(profile.Skills, evidence)
	profile.AllEvidence = append(profile.AllEvidence, evidence)
}
