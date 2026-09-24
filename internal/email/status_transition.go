package email

// CanAdvanceApplicationStatus determines whether an automatically detected
// email event is allowed to change the application's current status.
//
// Email automation should never move an application backward through the
// normal recruiting lifecycle.
func CanAdvanceApplicationStatus(currentStatus, newStatus string) bool {
	if currentStatus == newStatus {
		return false
	}

	// Terminal states should not be automatically overwritten by email.
	switch currentStatus {
	case StatusHired, StatusRejected, "withdrawn":
		return false
	}

	// Rejection is a terminal outcome and may occur from any active stage.
	if newStatus == StatusRejected {
		return true
	}

	// Hired may follow an offer.
	if newStatus == StatusHired {
		return currentStatus == StatusOffer
	}

	currentRank, currentOK := applicationStatusRank(currentStatus)
	newRank, newOK := applicationStatusRank(newStatus)

	if !currentOK || !newOK {
		return false
	}

	return newRank > currentRank
}

func applicationStatusRank(status string) (int, bool) {
	switch status {
	case "saved":
		return 0, true
	case StatusApplied:
		return 1, true
	case StatusReceived:
		return 2, true
	case StatusRecruiterContact:
		return 3, true
	case StatusInterviewing:
		return 4, true
	case StatusOffer:
		return 5, true
	case StatusHired:
		return 6, true
	default:
		return 0, false
	}
}
