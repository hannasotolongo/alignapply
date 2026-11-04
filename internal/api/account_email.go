package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/mailer"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

const (
	purposeVerifyEmail   = "verify_email"
	purposeResetPassword = "reset_password"

	verifyEmailTTL   = 48 * time.Hour
	resetPasswordTTL = time.Hour

	maxEmailsPerHour = 5
)

func newAuthToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashAuthToken(raw), nil
}

func hashAuthToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Server) emailEnabled() bool {
	return s.mailer != nil && s.accountsRepo != nil && s.web.BaseURL != ""
}

// sendAccountEmail creates a single-use link and emails it. It enforces a
// per-user hourly limit so the endpoint can't be used to spam an inbox.
func (s *Server) sendAccountEmail(ctx context.Context, user repository.User, purpose string) error {
	if !s.emailEnabled() {
		return errors.New("email is not configured")
	}

	recent, err := s.accountsRepo.CountRecentTokens(ctx, user.ID, purpose, time.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if recent >= maxEmailsPerHour {
		return errTooManyEmails
	}

	raw, hash, err := newAuthToken()
	if err != nil {
		return err
	}

	ttl, path, subject, intro, action := verifyEmailTTL, "/verify-email", "Confirm your Jane Careers email",
		"Confirm your email address to finish setting up Jane Careers. If you have a school license, confirming unlocks it.",
		"Confirm email"
	if purpose == purposeResetPassword {
		ttl, path, subject, intro, action = resetPasswordTTL, "/reset-password", "Reset your Jane Careers password",
			"We received a request to reset your Jane Careers password. This link expires in 1 hour. If you didn't ask for this, you can ignore this email.",
			"Reset password"
	}

	if err := s.accountsRepo.CreateAuthToken(ctx, hash, user.ID, purpose, user.Email, ttl); err != nil {
		return err
	}

	link := s.web.BaseURL + path + "?token=" + raw
	return s.mailer.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: subject,
		Text:    intro + "\n\n" + action + ": " + link + "\n\n— Jane Careers",
		HTML: `<div style="font-family:Arial,sans-serif;max-width:520px;margin:0 auto;color:#111">` +
			`<h2 style="margin:0 0 12px">Jane Careers</h2>` +
			`<p style="line-height:1.6">` + html.EscapeString(intro) + `</p>` +
			`<p style="margin:24px 0"><a href="` + html.EscapeString(link) + `" style="background:#111;color:#fff;padding:12px 20px;border-radius:10px;text-decoration:none;font-weight:600">` + action + `</a></p>` +
			`<p style="color:#666;font-size:13px;line-height:1.5">Or paste this link into your browser:<br>` + html.EscapeString(link) + `</p></div>`,
	})
}

var errTooManyEmails = errors.New("too many emails requested; try again in an hour")

// sendVerificationAfterSignUp is best-effort: sign-up succeeds even if the
// email fails, and the user can resend it from the app.
func (s *Server) sendVerificationAfterSignUp(user repository.User) {
	if !s.emailEnabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.sendAccountEmail(ctx, user, purposeVerifyEmail); err != nil {
			log.Printf("send verification email to user %s: %v", user.ID, err)
		}
	}()
}

// POST /api/v1/auth/verify-email/send
func (s *Server) handleSendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}
	if !s.emailEnabled() {
		writeError(w, http.StatusServiceUnavailable, "email verification isn't available yet")
		return
	}
	if verified, _ := s.accountsRepo.EmailVerified(r.Context(), user.ID); verified {
		writeJSON(w, http.StatusOK, map[string]any{"sent": false, "alreadyVerified": true})
		return
	}
	if err := s.sendAccountEmail(r.Context(), user, purposeVerifyEmail); err != nil {
		if errors.Is(err, errTooManyEmails) {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		log.Printf("send verification email to user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "couldn't send the email; try again shortly")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// GET /verify-email?token=… (the link in the email)
func (s *Server) handleVerifyEmailLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	result := "invalid"
	if s.accountsRepo != nil {
		if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
			userID, email, err := s.accountsRepo.ConsumeAuthToken(r.Context(), hashAuthToken(token), purposeVerifyEmail)
			if err == nil {
				if marked, err := s.accountsRepo.MarkEmailVerified(r.Context(), userID, email); err == nil && marked {
					result = "1"
				}
			} else if !errors.Is(err, repository.ErrAuthTokenInvalid) {
				log.Printf("verify email: %v", err)
			}
		}
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/app/billing?verified="+result, http.StatusFound)
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

// POST /api/v1/auth/password-reset/request
// Always answers the same way so it can't be used to discover accounts.
func (s *Server) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.emailEnabled() {
		writeError(w, http.StatusServiceUnavailable, "Password reset email isn't available right now. Call customer service at 646-821-2027 and we'll help you get back in.")
		return
	}
	if !s.authLimiter.Allow("reset:" + clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	var request passwordResetRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if user, _, err := s.userCareerRepo.GetPasswordUserByEmail(r.Context(), strings.TrimSpace(request.Email)); err == nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.sendAccountEmail(ctx, user, purposeResetPassword); err != nil && !errors.Is(err, errTooManyEmails) {
				log.Printf("send password reset to user %s: %v", user.ID, err)
			}
		}()
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists for that email, we've sent a reset link.",
	})
}

type passwordResetConfirm struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// POST /api/v1/auth/password-reset/confirm
func (s *Server) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.accountsRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "password reset unavailable")
		return
	}
	if !s.authLimiter.Allow("reset:" + clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}

	var request passwordResetConfirm
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := auth.ValidatePassword(request.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, email, err := s.accountsRepo.ConsumeAuthToken(r.Context(), hashAuthToken(strings.TrimSpace(request.Token)), purposeResetPassword)
	if errors.Is(err, repository.ErrAuthTokenInvalid) {
		writeError(w, http.StatusBadRequest, "this reset link is invalid or has expired; request a new one")
		return
	}
	if err != nil {
		log.Printf("consume reset token: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}

	hash, err := auth.HashPassword(request.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	if err := s.accountsRepo.SetPassword(r.Context(), userID, hash); err != nil {
		log.Printf("set password for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	// Receiving the reset email proves the user controls the address.
	_, _ = s.accountsRepo.MarkEmailVerified(r.Context(), userID, email)

	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated. You can sign in now."})
}
