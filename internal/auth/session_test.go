package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSessionSecret = "0123456789abcdef0123456789abcdef"

func TestSessionManagerCreateAndVerify(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	token, err := manager.Create("user-123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if strings.TrimSpace(token) == "" {
		t.Fatal("Create() returned an empty token")
	}

	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if claims.UserID != "user-123" {
		t.Fatalf(
			"Verify() UserID = %q, want %q",
			claims.UserID,
			"user-123",
		)
	}

	if claims.Subject != "user-123" {
		t.Fatalf(
			"Verify() Subject = %q, want %q",
			claims.Subject,
			"user-123",
		)
	}
}

func TestSessionManagerRejectsTamperedToken(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	token, err := manager.Create("user-123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf(
			"token has %d parts, want 3",
			len(parts),
		)
	}

	signature := []byte(parts[2])
	if len(signature) == 0 {
		t.Fatal("token signature is empty")
	}

	if signature[0] == 'a' {
		signature[0] = 'b'
	} else {
		signature[0] = 'a'
	}

	parts[2] = string(signature)

	tamperedToken := strings.Join(parts, ".")

	if _, err := manager.Verify(tamperedToken); err == nil {
		t.Fatal("Verify() accepted a tampered token")
	}
}

func TestSessionManagerRejectsWrongSecret(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	otherManager, err := NewSessionManager(
		"abcdef0123456789abcdef0123456789",
	)
	if err != nil {
		t.Fatalf(
			"NewSessionManager(other) error = %v",
			err,
		)
	}

	token, err := manager.Create("user-123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := otherManager.Verify(token); err == nil {
		t.Fatal(
			"Verify() accepted a token signed with another secret",
		)
	}
}

func TestSessionManagerRejectsEmptyUserID(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	if _, err := manager.Create("   "); err == nil {
		t.Fatal("Create() accepted an empty user ID")
	}
}

func TestNewSessionManagerRejectsShortSecret(t *testing.T) {
	_, err := NewSessionManager("too-short")
	if err == nil {
		t.Fatal(
			"NewSessionManager() accepted a short secret",
		)
	}
}

func TestBearerToken(t *testing.T) {
	token, err := BearerToken("Bearer abc123")
	if err != nil {
		t.Fatalf("BearerToken() error = %v", err)
	}

	if token != "abc123" {
		t.Fatalf(
			"BearerToken() = %q, want %q",
			token,
			"abc123",
		)
	}
}

func TestBearerTokenIsCaseInsensitive(t *testing.T) {
	token, err := BearerToken("bearer abc123")
	if err != nil {
		t.Fatalf("BearerToken() error = %v", err)
	}

	if token != "abc123" {
		t.Fatalf(
			"BearerToken() = %q, want %q",
			token,
			"abc123",
		)
	}
}

func TestBearerTokenRejectsMissingToken(t *testing.T) {
	if _, err := BearerToken(""); err == nil {
		t.Fatal(
			"BearerToken() accepted an empty header",
		)
	}
}

func TestBearerTokenRejectsMalformedHeader(t *testing.T) {
	headers := []string{
		"abc123",
		"Basic abc123",
		"Bearer",
		"Bearer abc123 extra",
	}

	for _, header := range headers {
		t.Run(
			header,
			func(t *testing.T) {
				if _, err := BearerToken(header); err == nil {
					t.Fatalf(
						"BearerToken(%q) unexpectedly succeeded",
						header,
					)
				}
			},
		)
	}
}

func TestSessionMiddlewareAddsUserIDToContext(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	token, err := manager.Create("user-456")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var receivedUserID string

	next := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			userID, err := RequireUserID(r)
			if err != nil {
				t.Fatalf(
					"RequireUserID() error = %v",
					err,
				)
			}

			receivedUserID = userID
			w.WriteHeader(http.StatusNoContent)
		},
	)

	handler := manager.Middleware(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/me",
		nil,
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+token,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusNoContent,
		)
	}

	if receivedUserID != "user-456" {
		t.Fatalf(
			"user ID = %q, want %q",
			receivedUserID,
			"user-456",
		)
	}
}

func TestSessionMiddlewareRejectsMissingAuthorization(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	nextCalled := false

	next := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	)

	handler := manager.Middleware(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/me",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusUnauthorized,
		)
	}

	if nextCalled {
		t.Fatal(
			"middleware called protected handler without authentication",
		)
	}

	if !strings.Contains(
		response.Body.String(),
		`"error":"unauthorized"`,
	) {
		t.Fatalf(
			"response body = %q",
			response.Body.String(),
		)
	}
}

func TestSessionMiddlewareRejectsInvalidToken(t *testing.T) {
	manager, err := NewSessionManager(testSessionSecret)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	nextCalled := false

	next := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	)

	handler := manager.Middleware(next)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/me",
		nil,
	)

	request.Header.Set(
		"Authorization",
		"Bearer definitely-not-a-valid-token",
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusUnauthorized,
		)
	}

	if nextCalled {
		t.Fatal(
			"middleware called protected handler with invalid token",
		)
	}
}
