package api

import (
	"context"
	"net/http"
	"time"

	emailservice "github.com/hannasotolongo/casemade-backend/internal/email"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

func (s *Server) handleGmailCallback(
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

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "missing authorization code",
			},
		)
		return
	}

	if state == "" {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "missing OAuth state",
			},
		)
		return
	}

	userID, err := emailservice.ParseOAuthState(state)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid or expired OAuth state",
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

	config, err := emailservice.GmailOAuthConfig()
	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to load Gmail OAuth configuration",
			},
		)
		return
	}

	token, err := config.Exchange(
		context.Background(),
		code,
	)
	if err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "failed to exchange Gmail authorization code",
			},
		)
		return
	}

	var tokenExpiry *time.Time
	if !token.Expiry.IsZero() {
		expiry := token.Expiry
		tokenExpiry = &expiry
	}

	connection, err := s.emailConnectionsRepo.Save(
		r.Context(),
		repository.SaveEmailConnectionParams{
			UserID:        userID,
			Provider:      "gmail",
			ProviderEmail: "",
			AccessToken:   token.AccessToken,
			RefreshToken:  token.RefreshToken,
			TokenExpiry:   tokenExpiry,
			Scopes: []string{
				"https://www.googleapis.com/auth/gmail.readonly",
			},
		},
	)
	if err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to save Gmail connection",
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"connected": true,
			"provider":  connection.Provider,
			"user_id":   connection.UserID,
			"saved":     true,
		},
	)
}
