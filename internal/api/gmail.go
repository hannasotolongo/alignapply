package api

import (
	"net/http"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	emailservice "github.com/hannasotolongo/casemade-backend/internal/email"
)

// handleGmailConnect starts the Google OAuth flow for the currently
// authenticated AlignApply user.
func (s *Server) handleGmailConnect(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	state, err := emailservice.NewOAuthState(userID)
	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create OAuth state",
			},
		)
		return
	}

	authorizationURL, err :=
		emailservice.GmailAuthorizationURL(state)

	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create Gmail authorization URL",
			},
		)
		return
	}

	http.Redirect(
		w,
		r,
		authorizationURL,
		http.StatusFound,
	)
}

// handleGmailProfile verifies that the authenticated AlignApply user's
// saved Gmail connection can successfully access the Gmail API.
func (s *Server) handleGmailProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	if s.emailConnectionsRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "email connection persistence unavailable",
			},
		)
		return
	}

	connection, err :=
		s.emailConnectionsRepo.GetByUserAndProvider(
			r.Context(),
			userID,
			"gmail",
		)
	if err != nil {
		writeJSON(
			w,
			http.StatusNotFound,
			map[string]string{
				"error": "Gmail connection not found",
			},
		)
		return
	}

	profile, err := emailservice.GetGmailProfile(
		r.Context(),
		connection.AccessToken,
	)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]string{
				"error": "failed to read Gmail profile",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"connected":     true,
			"provider":      "gmail",
			"email_address": profile.EmailAddress,
			"history_id":    profile.HistoryID,
		},
	)
}

// handleGmailMessages verifies that AlignApply can retrieve recent
// messages from the authenticated user's connected Gmail account.
func (s *Server) handleGmailMessages(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	if s.emailConnectionsRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "email connection persistence unavailable",
			},
		)
		return
	}

	connection, err :=
		s.emailConnectionsRepo.GetByUserAndProvider(
			r.Context(),
			userID,
			"gmail",
		)
	if err != nil {
		writeJSON(
			w,
			http.StatusNotFound,
			map[string]string{
				"error": "Gmail connection not found",
			},
		)
		return
	}

	messages, err :=
		emailservice.FetchRecentGmailMessages(
			r.Context(),
			connection.AccessToken,
			10,
		)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]string{
				"error": "failed to read Gmail messages",
			},
		)
		return
	}

	type messageResponse struct {
		ID         string `json:"id"`
		From       string `json:"from"`
		Subject    string `json:"subject"`
		ReceivedAt string `json:"received_at"`
	}

	result := make(
		[]messageResponse,
		0,
		len(messages),
	)

	for _, message := range messages {
		result = append(
			result,
			messageResponse{
				ID:      message.ID,
				From:    message.From,
				Subject: message.Subject,
				ReceivedAt: message.ReceivedAt.
					UTC().
					Format("2006-01-02T15:04:05Z"),
			},
		)
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"provider": "gmail",
			"count":    len(result),
			"messages": result,
		},
	)
}

// handleGmailClassificationPreview retrieves recent Gmail messages and
// previews which recruiting events AlignApply's classifier detects.
// This endpoint does not modify any application state.
func (s *Server) handleGmailClassificationPreview(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	if s.emailConnectionsRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "email connection persistence unavailable",
			},
		)
		return
	}

	connection, err :=
		s.emailConnectionsRepo.GetByUserAndProvider(
			r.Context(),
			userID,
			"gmail",
		)
	if err != nil {
		writeJSON(
			w,
			http.StatusNotFound,
			map[string]string{
				"error": "Gmail connection not found",
			},
		)
		return
	}

	messages, err :=
		emailservice.FetchRecentGmailMessages(
			r.Context(),
			connection.AccessToken,
			25,
		)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]string{
				"error": "failed to read Gmail messages",
			},
		)
		return
	}

	type classificationPreview struct {
		MessageID  string  `json:"message_id"`
		From       string  `json:"from"`
		Subject    string  `json:"subject"`
		Detected   bool    `json:"detected"`
		EventType  string  `json:"event_type,omitempty"`
		NewStatus  string  `json:"new_status,omitempty"`
		Summary    string  `json:"summary,omitempty"`
		Confidence float64 `json:"confidence,omitempty"`
	}

	result := make(
		[]classificationPreview,
		0,
		len(messages),
	)

	for _, message := range messages {
		signal, detected :=
			emailservice.Classify(message)

		preview := classificationPreview{
			MessageID: message.ID,
			From:      message.From,
			Subject:   message.Subject,
			Detected:  detected,
		}

		if detected {
			preview.EventType = signal.EventType
			preview.NewStatus = signal.NewStatus
			preview.Summary = signal.Summary
			preview.Confidence = signal.Confidence
		}

		result = append(result, preview)
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"provider": "gmail",
			"count":    len(result),
			"messages": result,
		},
	)
}
func (s *Server) handleGmailSync(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{
				"error": "unauthorized",
			},
		)
		return
	}

	result, err := s.syncGmailApplications(
		r.Context(),
		userID,
	)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadGateway,
			map[string]string{
				"error": "Gmail sync failed",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"provider": "gmail",
			"synced":   true,
			"result":   result,
		},
	)
}
