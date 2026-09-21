package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const userIDContextKey contextKey = "alignapply_user_id"

func (m *SessionManager) Middleware(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if next == nil {
				http.Error(
					w,
					"internal server error",
					http.StatusInternalServerError,
				)
				return
			}

			if m == nil {
				http.Error(
					w,
					"authentication unavailable",
					http.StatusInternalServerError,
				)
				return
			}

			tokenString, err := BearerToken(
				r.Header.Get("Authorization"),
			)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			claims, err := m.Verify(tokenString)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			userID := strings.TrimSpace(
				claims.UserID,
			)
			if userID == "" {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(
				r.Context(),
				userIDContextKey,
				userID,
			)

			next.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		},
	)
}

func UserIDFromContext(
	ctx context.Context,
) (string, error) {
	if ctx == nil {
		return "", errors.New(
			"auth: context is required",
		)
	}

	value := ctx.Value(
		userIDContextKey,
	)

	userID, ok := value.(string)
	if !ok {
		return "", errors.New(
			"auth: authenticated user not found",
		)
	}

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New(
			"auth: authenticated user not found",
		)
	}

	return userID, nil
}

func RequireUserID(
	r *http.Request,
) (string, error) {
	if r == nil {
		return "", errors.New(
			"auth: request is required",
		)
	}

	return UserIDFromContext(
		r.Context(),
	)
}

func writeUnauthorized(
	w http.ResponseWriter,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.Header().Set(
		"WWW-Authenticate",
		`Bearer realm="alignapply"`,
	)

	w.WriteHeader(
		http.StatusUnauthorized,
	)

	_, _ = w.Write(
		[]byte(
			`{"error":"unauthorized"}` + "\n",
		),
	)
}
