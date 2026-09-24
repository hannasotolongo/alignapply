package api

import (
	"net/http"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
)

func (s *Server) handleGmailConnectionStatus(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{"error": "unauthorized"},
		)
		return
	}

	if s.emailConnectionsRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{"error": "email connection persistence unavailable"},
		)
		return
	}

	connection, err := s.emailConnectionsRepo.GetByUserAndProvider(
		r.Context(),
		userID,
		"gmail",
	)
	if err != nil {
		writeJSON(
			w,
			http.StatusOK,
			map[string]interface{}{
				"provider":  "gmail",
				"connected": false,
			},
		)
		return
	}

	response := map[string]interface{}{
		"provider":      "gmail",
		"connected":     true,
		"email_address": connection.ProviderEmail,
	}

	if connection.LastSyncedAt != nil {
		response["last_synced_at"] = connection.LastSyncedAt
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGmailDisconnect(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeJSON(
			w,
			http.StatusUnauthorized,
			map[string]string{"error": "unauthorized"},
		)
		return
	}

	if s.emailConnectionsRepo == nil {
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{"error": "email connection persistence unavailable"},
		)
		return
	}

	if err := s.emailConnectionsRepo.Delete(
		r.Context(),
		userID,
		"gmail",
	); err != nil {
		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{"error": "failed to disconnect Gmail"},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"provider":     "gmail",
			"connected":    false,
			"disconnected": true,
		},
	)
}
