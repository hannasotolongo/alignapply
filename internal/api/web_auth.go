package api

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

type passwordSignUpRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName,omitempty"`
}

type passwordLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type webSessionResponse struct {
	User authUserResponse `json:"user"`
}

func (s *Server) handlePasswordSignUp(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.webAuthAvailable(w) {
		return
	}

	if !s.authLimiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	var request passwordSignUpRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email, err := normalizeEmail(request.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := auth.ValidatePassword(request.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	displayName := strings.TrimSpace(request.DisplayName)
	if len(displayName) > 120 {
		writeError(w, http.StatusBadRequest, "displayName must not exceed 120 characters")
		return
	}

	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		log.Printf("hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	user, err := s.userCareerRepo.CreatePasswordUser(
		r.Context(),
		email,
		displayName,
		passwordHash,
	)
	if errors.Is(err, repository.ErrEmailAlreadyRegistered) {
		writeError(w, http.StatusConflict, "an account with this email already exists")
		return
	}
	if err != nil {
		log.Printf("create password user: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	s.startWebSession(w, user)
}

func (s *Server) handlePasswordLogin(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.webAuthAvailable(w) {
		return
	}

	if !s.authLimiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	var request passwordLoginRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, passwordHash, err := s.userCareerRepo.GetPasswordUserByEmail(
		r.Context(),
		strings.TrimSpace(request.Email),
	)

	switch {
	case errors.Is(err, repository.ErrPasswordUserNotFound):
		// Spend the same hashing time as a real check so that response
		// timing does not reveal whether the email is registered.
		_ = auth.VerifyPassword(request.Password, auth.DummyPasswordHash())
		writeError(w, http.StatusUnauthorized, "incorrect email or password")
		return

	case err != nil:
		log.Printf("password login lookup: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to sign in")
		return
	}

	if err := auth.VerifyPassword(request.Password, passwordHash); err != nil {
		writeError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	s.startWebSession(w, user)
}

func (s *Server) handleLogout(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	auth.ClearSessionCookie(w, s.web.CookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startWebSession(
	w http.ResponseWriter,
	user repository.User,
) {
	token, err := s.sessionManager.Create(user.ID)
	if err != nil {
		log.Printf("create web session: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	auth.SetSessionCookie(
		w,
		token,
		s.sessionManager.TTL(),
		s.web.CookieSecure,
	)

	writeJSON(w, http.StatusOK, webSessionResponse{
		User: authUserResponse{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}

func (s *Server) webAuthAvailable(w http.ResponseWriter) bool {
	if s.sessionManager == nil || s.userCareerRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
		return false
	}
	return true
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)

	if value == "" || len(value) > 254 {
		return "", errors.New("a valid email is required")
	}

	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return "", errors.New("a valid email is required")
	}

	return value, nil
}

// ---------------------------------------------------------------------------
// Rate limiting for unauthenticated auth endpoints
// ---------------------------------------------------------------------------

// attemptLimiter is a fixed-window, in-memory limiter keyed by client IP. It
// slows down password guessing on a single instance; it is not a substitute
// for an edge rate limiter when running several instances.
type attemptLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string]attemptWindow
}

type attemptWindow struct {
	start time.Time
	count int
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		limit:    limit,
		window:   window,
		attempts: make(map[string]attemptWindow),
	}
}

func (l *attemptLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	if len(l.attempts) > 10_000 {
		for k, v := range l.attempts {
			if now.Sub(v.start) > l.window {
				delete(l.attempts, k)
			}
		}
	}

	current := l.attempts[key]
	if now.Sub(current.start) > l.window {
		current = attemptWindow{start: now}
	}

	current.count++
	l.attempts[key] = current

	return current.count <= l.limit
}

// clientIP uses the connection address. Behind a proxy (e.g. Render), set
// TRUST_PROXY_HEADERS so the first X-Forwarded-For hop is used instead.
func clientIP(r *http.Request) string {
	if trustProxyHeaders {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var trustProxyHeaders bool

// SetTrustProxyHeaders enables X-Forwarded-For for client IP detection.
func SetTrustProxyHeaders(enabled bool) {
	trustProxyHeaders = enabled
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
