package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CrawledJobsRepository stores Jane's crawler state: the employer job boards
// it reads and the jobs it has saved from them. It implements
// jobs.BoardStore.
type CrawledJobsRepository struct {
	pool *pgxpool.Pool
}

func NewCrawledJobsRepository(pool *pgxpool.Pool) (*CrawledJobsRepository, error) {
	if pool == nil {
		return nil, errors.New("repository: PostgreSQL pool is required")
	}
	return &CrawledJobsRepository{pool: pool}, nil
}

var _ jobs.BoardStore = (*CrawledJobsRepository)(nil)

// SeedBoards adds boards Jane doesn't know yet and refreshes company names;
// a board's crawl history and on/off state are kept.
func (r *CrawledJobsRepository) SeedBoards(ctx context.Context, boards []jobs.Board) error {
	if len(boards) == 0 {
		return nil
	}
	ats := make([]string, 0, len(boards))
	slugs := make([]string, 0, len(boards))
	companies := make([]string, 0, len(boards))
	for _, board := range boards {
		if board.ATS == "" || board.Slug == "" {
			continue
		}
		ats = append(ats, board.ATS)
		slugs = append(slugs, board.Slug)
		companies = append(companies, firstNonBlank(board.Company, board.Slug))
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO job_boards (ats, slug, company)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[])
		ON CONFLICT (ats, slug) DO UPDATE SET company = EXCLUDED.company`,
		ats, slugs, companies)
	if err != nil {
		return fmt.Errorf("repository: seed job boards: %w", err)
	}
	return nil
}

// DueBoards returns boards never read, or last read longer ago than
// olderThan, oldest first.
func (r *CrawledJobsRepository) DueBoards(ctx context.Context, limit int, olderThan time.Duration) ([]jobs.Board, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ats, slug, company FROM job_boards
		WHERE active AND (last_crawled_at IS NULL OR last_crawled_at < NOW() - make_interval(secs => $1)
			-- A board read only partly (big Workday boards save a batch per
			-- read) comes back after 15 minutes until all its jobs are saved.
			OR (last_status = 'partial' AND last_crawled_at < NOW() - INTERVAL '15 minutes'))
		ORDER BY last_crawled_at NULLS FIRST
		LIMIT $2`, olderThan.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("repository: due job boards: %w", err)
	}
	defer rows.Close()
	var boards []jobs.Board
	for rows.Next() {
		var board jobs.Board
		if err := rows.Scan(&board.ATS, &board.Slug, &board.Company); err != nil {
			return nil, err
		}
		boards = append(boards, board)
	}
	return boards, rows.Err()
}

func (r *CrawledJobsRepository) KnownJobIDs(ctx context.Context, board jobs.Board) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM crawled_jobs WHERE ats = $1 AND board_slug = $2`, board.ATS, board.Slug)
	if err != nil {
		return nil, fmt.Errorf("repository: known crawled jobs: %w", err)
	}
	defer rows.Close()
	known := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

const upsertCrawledJob = `
	INSERT INTO crawled_jobs (
		id, ats, board_slug, company, title, title_search, location, country_code,
		work_arrangement, employment_type, description, apply_url, posted_at,
		salary_min, salary_max, salary_currency, salary_period, active, last_seen_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, TRUE, NOW())
	ON CONFLICT (id) DO UPDATE SET
		company = EXCLUDED.company, title = EXCLUDED.title, title_search = EXCLUDED.title_search,
		location = EXCLUDED.location, country_code = EXCLUDED.country_code,
		work_arrangement = EXCLUDED.work_arrangement, employment_type = EXCLUDED.employment_type,
		description = EXCLUDED.description, apply_url = EXCLUDED.apply_url,
		posted_at = COALESCE(EXCLUDED.posted_at, crawled_jobs.posted_at),
		salary_min = EXCLUDED.salary_min, salary_max = EXCLUDED.salary_max,
		salary_currency = EXCLUDED.salary_currency, salary_period = EXCLUDED.salary_period,
		active = TRUE, last_seen_at = NOW()`

func (r *CrawledJobsRepository) SaveBoardJobs(ctx context.Context, board jobs.Board, saved []jobs.Job, seenIDs []string, complete bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: save crawled jobs: %w", err)
	}
	defer tx.Rollback(ctx)

	if len(saved) > 0 {
		batch := &pgx.Batch{}
		for _, job := range saved {
			batch.Queue(upsertCrawledJob,
				job.ID, board.ATS, board.Slug, firstNonBlank(job.Company, board.Company), job.Title,
				strings.ToLower(job.Title), job.Location, job.CountryCode, job.WorkArrangement,
				job.EmploymentType, job.Description, job.ApplyURL, job.PostedAt,
				job.SalaryMin, job.SalaryMax, job.SalaryCurrency, job.SalaryPeriod)
		}
		results := tx.SendBatch(ctx, batch)
		for range saved {
			if _, err := results.Exec(); err != nil {
				results.Close()
				return fmt.Errorf("repository: save crawled job: %w", err)
			}
		}
		if err := results.Close(); err != nil {
			return err
		}
	}

	if len(seenIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE crawled_jobs SET active = TRUE, last_seen_at = NOW() WHERE id = ANY($1)`, seenIDs); err != nil {
			return fmt.Errorf("repository: touch crawled jobs: %w", err)
		}
	}
	if complete {
		if _, err := tx.Exec(ctx, `
			UPDATE crawled_jobs SET active = FALSE
			WHERE ats = $1 AND board_slug = $2 AND active AND NOT (id = ANY($3))`,
			board.ATS, board.Slug, seenIDs); err != nil {
			return fmt.Errorf("repository: close crawled jobs: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// RecordCrawl notes a board read. After six failures in a row the board is
// switched off so a dead board doesn't keep being retried.
func (r *CrawledJobsRepository) RecordCrawl(ctx context.Context, board jobs.Board, status string, jobCount int, failed bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE job_boards SET
			last_crawled_at = NOW(),
			last_status = $3,
			job_count = CASE WHEN $5 THEN job_count ELSE $4 END,
			consecutive_failures = CASE WHEN $5 THEN consecutive_failures + 1 ELSE 0 END,
			active = CASE WHEN $5 AND consecutive_failures + 1 >= 6 THEN FALSE ELSE active END
		WHERE ats = $1 AND slug = $2`,
		board.ATS, board.Slug, status, jobCount, failed)
	if err != nil {
		return fmt.Errorf("repository: record crawl: %w", err)
	}
	return nil
}

// SearchJobs returns open US jobs whose title matches the regular
// expression, newest first.
func (r *CrawledJobsRepository) SearchJobs(ctx context.Context, titlePattern, nearPattern string, limit int) ([]jobs.Job, error) {
	// Jobs in the searched state come first ($2 = '' puts none first).
	rows, err := r.pool.Query(ctx, `
		SELECT id, ats, company, title, location, country_code, work_arrangement, employment_type,
			description, apply_url, posted_at, salary_min, salary_max, salary_currency, salary_period
		FROM crawled_jobs
		WHERE active AND description <> '' AND title_search ~ $1
		ORDER BY ($2 <> '' AND location ~* $2) DESC, posted_at DESC NULLS LAST, last_seen_at DESC
		LIMIT $3`, titlePattern, nearPattern, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: search crawled jobs: %w", err)
	}
	defer rows.Close()
	var found []jobs.Job
	for rows.Next() {
		var job jobs.Job
		var posted *time.Time
		if err := rows.Scan(&job.ID, &job.Source, &job.Company, &job.Title, &job.Location, &job.CountryCode,
			&job.WorkArrangement, &job.EmploymentType, &job.Description, &job.ApplyURL, &posted,
			&job.SalaryMin, &job.SalaryMax, &job.SalaryCurrency, &job.SalaryPeriod); err != nil {
			return nil, err
		}
		job.PostedAt = posted
		job.SourceURL = job.ApplyURL
		job.IsActive = true
		found = append(found, job)
	}
	return found, rows.Err()
}

// PruneClosed deletes jobs that have been closed for a while.
func (r *CrawledJobsRepository) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM crawled_jobs WHERE NOT active AND last_seen_at < NOW() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return fmt.Errorf("repository: prune crawled jobs: %w", err)
	}
	return nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
