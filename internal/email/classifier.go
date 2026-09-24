package email

import "strings"

// Classify analyzes an email and determines whether it represents a meaningful
// recruiting event for a tracked application.
//
// Classification is intentionally conservative. More specific signals take
// priority over generic recruiting language.
func Classify(message Message) (ApplicationSignal, bool) {
	subject := strings.ToLower(message.Subject)
	body := strings.ToLower(message.Body)
	text := strings.Join([]string{subject, body}, " ")

	// An offer is one of the strongest signals.
	if containsAny(
		text,
		"we are pleased to offer",
		"pleased to offer you",
		"offer of employment",
		"employment offer",
		"job offer",
		"offer letter",
	) {
		return ApplicationSignal{
			EventType:  EventOfferReceived,
			NewStatus:  StatusOffer,
			Summary:    "An employment offer was detected.",
			Confidence: 0.98,
		}, true
	}

	// Rejection language is also highly specific.
	if containsAny(
		text,
		"unfortunately",
		"not moving forward",
		"will not be moving forward",
		"won't be moving forward",
		"not selected",
		"other candidates",
		"decided not to proceed",
		"unable to move forward",
	) {
		return ApplicationSignal{
			EventType:  EventRejected,
			NewStatus:  StatusRejected,
			Summary:    "The employer indicated that the application will not move forward.",
			Confidence: 0.96,
		}, true
	}

	// Explicit interview language should advance the application.
	if containsAny(
		text,
		"schedule an interview",
		"schedule your interview",
		"interview availability",
		"interview invitation",
		"invite you to interview",
		"invitation to interview",
		"interview with",
	) {
		return ApplicationSignal{
			EventType:  EventInterviewInvitation,
			NewStatus:  StatusInterviewing,
			Summary:    "An interview invitation was detected.",
			Confidence: 0.97,
		}, true
	}

	if containsAny(
		text,
		"phone screen",
		"phone interview",
		"screening call",
		"introductory call",
		"recruiter call",
	) {
		return ApplicationSignal{
			EventType:  EventPhoneScreen,
			NewStatus:  StatusInterviewing,
			Summary:    "A recruiting or screening call was detected.",
			Confidence: 0.94,
		}, true
	}

	// Application confirmations must be checked before generic recruiter
	// language. Automated confirmation emails often contain words such as
	// "recruiting", "recruiter", or "next steps" in boilerplate text.
	if containsAny(
		text,
		"thank you for applying",
		"thanks for applying",
		"thank you for your application",
		"application received",
		"received your application",
		"we have received your application",
		"application has been received",
		"application was received",
	) {
		return ApplicationSignal{
			EventType:  EventApplicationReceived,
			NewStatus:  StatusReceived,
			Summary:    "The employer confirmed receipt of the application.",
			Confidence: 0.98,
		}, true
	}

	// Generic recruiter contact requires language that indicates an actual
	// request for interaction rather than simply mentioning recruiting.
	if containsAny(
		text,
		"would like to speak with you",
		"would like to connect with you",
		"would like to connect regarding",
		"please provide your availability",
		"please send your availability",
		"when are you available",
	) {
		return ApplicationSignal{
			EventType:  EventRecruiterContact,
			NewStatus:  StatusRecruiterContact,
			Summary:    "Recruiter contact requesting a response was detected.",
			Confidence: 0.90,
		}, true
	}

	return ApplicationSignal{}, false
}

func containsAny(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}
