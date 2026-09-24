package email

import "time"

// Message is the provider-neutral representation of an email that may contain
// information about a tracked job application.
type Message struct {
	ID         string
	Provider   string
	From       string
	Subject    string
	Body       string
	ReceivedAt time.Time
}

// ApplicationSignal is the result of analyzing an email for recruiting activity.
type ApplicationSignal struct {
	EventType  string
	NewStatus  string
	Company    string
	JobTitle   string
	Summary    string
	Confidence float64
}

// Supported application statuses.
const (
	StatusApplied          = "applied"
	StatusReceived         = "received"
	StatusRecruiterContact = "recruiter_contact"
	StatusInterviewing     = "interviewing"
	StatusOffer            = "offer"
	StatusHired            = "hired"
	StatusRejected         = "rejected"
)

// Supported application event types.
const (
	EventApplicationReceived = "application_received"
	EventRecruiterContact    = "recruiter_contact"
	EventPhoneScreen         = "phone_screen"
	EventInterviewInvitation = "interview_invitation"
	EventInterviewScheduled  = "interview_scheduled"
	EventOfferReceived       = "offer_received"
	EventHired               = "hired"
	EventRejected            = "rejected"
)
