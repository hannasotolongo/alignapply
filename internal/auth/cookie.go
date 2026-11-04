package auth

import (
	"net/http"
	"time"
)

// SessionCookieName is the HttpOnly cookie that carries the web session JWT.
// SameSite=Lax keeps the cookie off cross-site POST/PUT/DELETE requests,
// which protects the state-changing API from CSRF.
const SessionCookieName = "aa_session"

func (m *SessionManager) TTL() time.Duration {
	if m == nil {
		return 0
	}
	return m.ttl
}

func SetSessionCookie(
	w http.ResponseWriter,
	token string,
	ttl time.Duration,
	secure bool,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(
	w http.ResponseWriter,
	secure bool,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
