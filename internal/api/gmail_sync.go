package api

import (
	"context"
	"fmt"

	emailservice "github.com/hannasotolongo/casemade-backend/internal/email"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

type gmailSyncResult struct {
	MessagesFetched int `json:"messages_fetched"`
	SignalsDetected int `json:"signals_detected"`
	Matched         int `json:"matched"`
	Updated         int `json:"updated"`
	Skipped         int `json:"skipped"`
}

func (s *Server) syncGmailApplications(
	ctx context.Context,
	userID string,
) (gmailSyncResult, error) {
	var result gmailSyncResult

	if s.emailConnectionsRepo == nil ||
		s.userJobsRepo == nil ||
		s.jobRepo == nil {
		return result, fmt.Errorf("required repositories unavailable")
	}

	gmailConnection, err := s.gmailClientForUser(ctx, userID)
	if err != nil {
		return result, err
	}

	profile, err := emailservice.GetGmailProfileWithClient(
		ctx,
		gmailConnection.Client,
	)
	if err != nil {
		return result, fmt.Errorf("get Gmail profile: %w", err)
	}

	messages, err := emailservice.FetchRecentGmailMessagesWithClient(
		ctx,
		gmailConnection.Client,
		25,
	)
	if err != nil {
		return result, fmt.Errorf("fetch Gmail messages: %w", err)
	}

	if err := s.persistGmailToken(
		ctx,
		userID,
		gmailConnection,
	); err != nil {
		return result, fmt.Errorf("persist Gmail token: %w", err)
	}

	if err := s.emailConnectionsRepo.UpdateSyncMetadata(
		ctx,
		userID,
		"gmail",
		profile.EmailAddress,
		profile.HistoryID,
	); err != nil {
		return result, fmt.Errorf("update Gmail sync metadata: %w", err)
	}

	result.MessagesFetched = len(messages)

	applications, err := s.userJobsRepo.ListApplications(ctx, userID)
	if err != nil {
		return result, fmt.Errorf("list applications: %w", err)
	}

	tracked := make([]emailservice.TrackedApplication, 0, len(applications))

	for _, application := range applications {
		job, err := s.jobRepo.GetJobByID(ctx, application.JobID)
		if err != nil {
			continue
		}

		tracked = append(tracked, emailservice.TrackedApplication{
			ApplicationID: application.ID,
			JobID:         application.JobID,
			Company:       job.Company,
			JobTitle:      job.Title,
			CurrentStatus: application.Status,
		})
	}

	for _, message := range messages {
		signal, detected := emailservice.Classify(message)
		if !detected {
			continue
		}

		result.SignalsDetected++

		match, matched := emailservice.MatchApplication(
			message,
			tracked,
		)
		if !matched {
			result.Skipped++
			continue
		}

		result.Matched++

		if !emailservice.CanAdvanceApplicationStatus(
			match.Application.CurrentStatus,
			signal.NewStatus,
		) {
			result.Skipped++
			continue
		}

		// Combine classification confidence with matching confidence so that
		// the stored event reflects confidence in the complete decision.
		confidence := signal.Confidence * match.Confidence

		messageID := message.ID
		occurredAt := message.ReceivedAt

		updatedApplication, _, err :=
			s.userJobsRepo.RecordApplicationEvent(
				ctx,
				repository.RecordApplicationEventParams{
					UserID:          userID,
					ApplicationID:   match.Application.ApplicationID,
					EventType:       signal.EventType,
					NewStatus:       signal.NewStatus,
					Source:          "email",
					SourceProvider:  "gmail",
					SourceMessageID: &messageID,
					Confidence:      &confidence,
					Summary:         signal.Summary,
					OccurredAt:      &occurredAt,
				},
			)
		if err != nil {
			return result, fmt.Errorf(
				"record Gmail application event: %w",
				err,
			)
		}

		result.Updated++

		// Keep our in-memory application status current during this sync.
		// This prevents a later, older/lower-stage message in the same batch
		// from being evaluated against stale state.
		for i := range tracked {
			if tracked[i].ApplicationID == updatedApplication.ID {
				tracked[i].CurrentStatus = updatedApplication.Status
				break
			}
		}
	}

	return result, nil
}
