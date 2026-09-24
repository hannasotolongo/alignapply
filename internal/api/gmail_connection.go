package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	emailservice "github.com/hannasotolongo/casemade-backend/internal/email"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

type gmailConnectionClient struct {
	Connection *repository.EmailConnection
	Client     *http.Client
	current    func() emailservice.GmailToken
}

func (s *Server) gmailClientForUser(
	ctx context.Context,
	userID string,
) (*gmailConnectionClient, error) {
	if s.emailConnectionsRepo == nil {
		return nil, fmt.Errorf("email connection repository unavailable")
	}

	connection, err := s.emailConnectionsRepo.GetByUserAndProvider(
		ctx,
		userID,
		"gmail",
	)
	if err != nil {
		return nil, fmt.Errorf("get Gmail connection: %w", err)
	}

	client, current, err := emailservice.GmailHTTPClient(
		ctx,
		connection.AccessToken,
		connection.RefreshToken,
		connection.TokenExpiry,
	)
	if err != nil {
		return nil, fmt.Errorf("create Gmail client: %w", err)
	}

	return &gmailConnectionClient{
		Connection: connection,
		Client:     client,
		current:    current,
	}, nil
}

func (s *Server) persistGmailToken(
	ctx context.Context,
	userID string,
	connection *gmailConnectionClient,
) error {
	token := connection.current()
	if token.AccessToken == "" {
		return fmt.Errorf("refresh Gmail token")
	}

	return s.emailConnectionsRepo.UpdateCredentials(
		ctx,
		userID,
		"gmail",
		token.AccessToken,
		token.RefreshToken,
		token.Expiry,
	)
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
