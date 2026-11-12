package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MatchAlert is a daily match email someone signed up for.
type MatchAlert struct {
	ID          int64
	Email       string
	TargetRole  string
	Location    string
	ResumeText  string
	Token       string
	Confirmed   bool
	SentJobIDs  []string
	LastSentAt  *time.Time
	EmailsSent  int
	ConfirmSent *time.Time
}

type MatchAlertsRepository struct {
	pool *pgxpool.Pool
}

func NewMatchAlertsRepository(pool *pgxpool.Pool) (*MatchAlertsRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}
	return &MatchAlertsRepository{pool: pool}, nil
}

const matchAlertColumns = `id, email, target_role, location, resume_text, token,
	confirmed_at IS NOT NULL, sent_job_ids, last_sent_at, emails_sent, confirm_sent_at`

func scanMatchAlert(row pgx.Row) (MatchAlert, error) {
	var alert MatchAlert
	err := row.Scan(&alert.ID, &alert.Email, &alert.TargetRole, &alert.Location, &alert.ResumeText, &alert.Token,
		&alert.Confirmed, &alert.SentJobIDs, &alert.LastSentAt, &alert.EmailsSent, &alert.ConfirmSent)
	return alert, err
}

// CountActiveAlerts is how many active alerts an email address has.
func (r *MatchAlertsRepository) CountActiveAlerts(ctx context.Context, email string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM match_alerts WHERE email = $1 AND active`, email).Scan(&count)
	return count, err
}

// SaveAlert creates an alert, or turns an existing one for the same email,
// job and place back on with the new résumé. token is used only when the
// alert is new.
func (r *MatchAlertsRepository) SaveAlert(ctx context.Context, email, role, location, resumeText, token string) (MatchAlert, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO match_alerts (email, target_role, location, resume_text, token)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email, target_role, location) DO UPDATE
			SET resume_text = EXCLUDED.resume_text, active = TRUE
		RETURNING `+matchAlertColumns, email, role, location, resumeText, token)
	alert, err := scanMatchAlert(row)
	if err != nil {
		return MatchAlert{}, fmt.Errorf("repository: save match alert: %w", err)
	}
	return alert, nil
}

// MarkConfirmSent records that a confirmation email went out.
func (r *MatchAlertsRepository) MarkConfirmSent(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE match_alerts SET confirm_sent_at = NOW() WHERE id = $1`, id)
	return err
}

// ConfirmAlert turns on the alert with this token. ok is false for an
// unknown token.
func (r *MatchAlertsRepository) ConfirmAlert(ctx context.Context, token string) (MatchAlert, bool, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE match_alerts SET confirmed_at = COALESCE(confirmed_at, NOW()), active = TRUE
		WHERE token = $1 AND resume_text <> ''
		RETURNING `+matchAlertColumns, token)
	alert, err := scanMatchAlert(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchAlert{}, false, nil
	}
	if err != nil {
		return MatchAlert{}, false, fmt.Errorf("repository: confirm match alert: %w", err)
	}
	return alert, true, nil
}

// Unsubscribe turns the alert off and deletes the résumé text kept for it.
func (r *MatchAlertsRepository) Unsubscribe(ctx context.Context, token string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE match_alerts SET active = FALSE, resume_text = '', sent_job_ids = '{}'
		WHERE token = $1`, token)
	if err != nil {
		return false, fmt.Errorf("repository: unsubscribe match alert: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DueAlerts are confirmed, active alerts not handled since before.
func (r *MatchAlertsRepository) DueAlerts(ctx context.Context, before time.Time, limit int) ([]MatchAlert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+matchAlertColumns+` FROM match_alerts
		WHERE active AND confirmed_at IS NOT NULL AND resume_text <> ''
			AND (last_sent_at IS NULL OR last_sent_at < $1)
		ORDER BY last_sent_at NULLS FIRST, id
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: due match alerts: %w", err)
	}
	defer rows.Close()
	var alerts []MatchAlert
	for rows.Next() {
		alert, err := scanMatchAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

// RecordAlertRun marks the alert handled for today. jobIDs are the jobs
// just emailed, remembered (the latest 400) so they aren't sent again.
func (r *MatchAlertsRepository) RecordAlertRun(ctx context.Context, id int64, jobIDs []string, emailed bool) error {
	sent := 0
	if emailed {
		sent = 1
	}
	if jobIDs == nil {
		jobIDs = []string{}
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE match_alerts
		SET last_sent_at = NOW(),
			emails_sent = emails_sent + $3,
			sent_job_ids = (($2::text[] || sent_job_ids))[1:400]
		WHERE id = $1`, id, jobIDs, sent)
	if err != nil {
		return fmt.Errorf("repository: record match alert run: %w", err)
	}
	return nil
}
