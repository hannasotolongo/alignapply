package email

import "strings"

type TrackedApplication struct {
	ApplicationID string
	JobID         string
	Company       string
	JobTitle      string
	CurrentStatus string
}

type ApplicationMatch struct {
	Application TrackedApplication
	Confidence  float64
	Reason      string
}

func MatchApplication(
	message Message,
	applications []TrackedApplication,
) (ApplicationMatch, bool) {
	if len(applications) == 0 {
		return ApplicationMatch{}, false
	}

	subject := normalizeMatchText(message.Subject)
	from := normalizeMatchText(message.From)
	body := normalizeMatchText(message.Body)

	recruitingContext := containsRecruitingContext(
		subject + " " + body,
	)

	var best ApplicationMatch
	bestScore := 0.0
	secondBestScore := 0.0

	for _, application := range applications {
		company := normalizeMatchText(application.Company)
		title := normalizeMatchText(application.JobTitle)

		if company == "" {
			continue
		}

		score := 0.0
		reasons := make([]string, 0, 5)

		if strings.Contains(from, company) {
			score += .50
			reasons = append(
				reasons,
				"company matched sender",
			)
		}

		if strings.Contains(subject, company) {
			score += .40
			reasons = append(
				reasons,
				"company matched subject",
			)
		}

		companyInBody := strings.Contains(body, company)
		if companyInBody {
			score += .30
			reasons = append(
				reasons,
				"company matched body",
			)
		}

		if title != "" {
			if strings.Contains(subject, title) {
				score += .35
				reasons = append(
					reasons,
					"job title matched subject",
				)
			}

			if strings.Contains(body, title) {
				score += .25
				reasons = append(
					reasons,
					"job title matched body",
				)
			}
		}

		// ATS/recruiting platforms frequently send mail from their own
		// domains with generic subjects. In that case the employer may
		// appear only in the body. Recruiting context supplies supporting
		// evidence without making generic company mentions sufficient.
		if companyInBody && recruitingContext {
			score += .25
			reasons = append(
				reasons,
				"recruiting context matched",
			)
		}

		if score > 1 {
			score = 1
		}

		if score > bestScore {
			secondBestScore = bestScore
			bestScore = score

			best = ApplicationMatch{
				Application: application,
				Confidence:  score,
				Reason:      strings.Join(reasons, "; "),
			}
		} else if score > secondBestScore {
			secondBestScore = score
		}
	}

	if bestScore < .50 {
		return ApplicationMatch{}, false
	}

	// If two tracked applications score almost identically, don't guess.
	if secondBestScore > 0 &&
		bestScore-secondBestScore < .15 {
		return ApplicationMatch{}, false
	}

	return best, true
}

func containsRecruitingContext(text string) bool {
	phrases := []string{
		"thank you for applying",
		"thanks for applying",
		"application received",
		"received your application",
		"your application",
		"job application",
		"position",
		"role",
		"candidate",
		"interview",
		"phone screen",
		"screening call",
		"recruiting",
		"recruiter",
		"talent acquisition",
		"offer letter",
		"employment offer",
		"not moving forward",
		"not selected",
	}

	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}

func normalizeMatchText(value string) string {
	value = strings.ToLower(value)

	replacer := strings.NewReplacer(
		",", " ",
		".", " ",
		"-", " ",
		"_", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"<", " ",
		">", " ",
	)

	value = replacer.Replace(value)

	return strings.Join(
		strings.Fields(value),
		" ",
	)
}
