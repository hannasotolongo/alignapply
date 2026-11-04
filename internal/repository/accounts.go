package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAuthTokenInvalid      = errors.New("repository: token is invalid or expired")
	ErrOrganizationNotFound  = errors.New("repository: organization not found")
	ErrDomainAlreadyLicensed = errors.New("repository: domain already has a license")
)

// ---------------------------------------------------------------------------
// Email verification and password reset tokens
// ---------------------------------------------------------------------------

type AccountsRepository struct {
	pool interface {
		Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}
}

func NewAccountsRepository(repo *UserCareerRepository) *AccountsRepository {
	return &AccountsRepository{pool: repo.pool}
}

func (r *AccountsRepository) CreateAuthToken(
	ctx context.Context,
	tokenHash string,
	userID string,
	purpose string,
	email string,
	ttl time.Duration,
) error {
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO auth_tokens (token_hash, user_id, purpose, email, expires_at)
		 VALUES ($1, $2, $3, $4, NOW() + make_interval(secs => $5))`,
		tokenHash, userID, purpose, strings.TrimSpace(email), ttl.Seconds(),
	)
	if err != nil {
		return fmt.Errorf("repository: create auth token: %w", err)
	}
	return nil
}

func (r *AccountsRepository) CountRecentTokens(
	ctx context.Context,
	userID string,
	purpose string,
	since time.Time,
) (int, error) {
	var count int
	err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM auth_tokens WHERE user_id = $1 AND purpose = $2 AND created_at >= $3`,
		userID, purpose, since,
	).Scan(&count)
	return count, err
}

// ConsumeAuthToken marks a token used and returns its user and email. A
// token works once, only for its purpose, and only before it expires.
func (r *AccountsRepository) ConsumeAuthToken(
	ctx context.Context,
	tokenHash string,
	purpose string,
) (userID string, email string, err error) {
	err = r.pool.QueryRow(
		ctx,
		`UPDATE auth_tokens SET used_at = NOW()
		  WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > NOW()
		  RETURNING user_id::text, email`,
		tokenHash, purpose,
	).Scan(&userID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrAuthTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("repository: consume auth token: %w", err)
	}
	return userID, email, nil
}

// MarkEmailVerified verifies the user's email only if it is still the
// address the token was sent to.
func (r *AccountsRepository) MarkEmailVerified(ctx context.Context, userID, email string) (bool, error) {
	tag, err := r.pool.Exec(
		ctx,
		`UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
		  WHERE id = $1 AND LOWER(email) = LOWER($2) AND deleted_at IS NULL`,
		userID, email,
	)
	if err != nil {
		return false, fmt.Errorf("repository: mark email verified: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *AccountsRepository) EmailVerified(ctx context.Context, userID string) (bool, error) {
	var verified bool
	err := r.pool.QueryRow(
		ctx,
		`SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`,
		userID,
	).Scan(&verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return verified, err
}

// SetPassword replaces the password and invalidates any other outstanding
// reset links for the user.
func (r *AccountsRepository) SetPassword(ctx context.Context, userID, passwordHash string) error {
	if _, err := r.pool.Exec(
		ctx,
		`UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		userID, passwordHash,
	); err != nil {
		return fmt.Errorf("repository: set password: %w", err)
	}
	_, err := r.pool.Exec(
		ctx,
		`UPDATE auth_tokens SET used_at = NOW()
		  WHERE user_id = $1 AND purpose = 'reset_password' AND used_at IS NULL`,
		userID,
	)
	return err
}

// ---------------------------------------------------------------------------
// School / program licenses
// ---------------------------------------------------------------------------

type Organization struct {
	ID                   string
	Name                 string
	EmailDomain          string
	OwnerUserID          string
	StripeCustomerID     string
	StripeSubscriptionID string
	Status               string
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
}

func (o Organization) Active() bool {
	return SubscriptionStatusActive(o.Status)
}

const organizationColumns = `
	id::text, name, email_domain, COALESCE(owner_user_id::text, ''),
	COALESCE(stripe_customer_id, ''), COALESCE(stripe_subscription_id, ''),
	status, current_period_end, cancel_at_period_end
`

func scanOrganization(row pgx.Row) (Organization, error) {
	var org Organization
	err := row.Scan(
		&org.ID, &org.Name, &org.EmailDomain, &org.OwnerUserID,
		&org.StripeCustomerID, &org.StripeSubscriptionID,
		&org.Status, &org.CurrentPeriodEnd, &org.CancelAtPeriodEnd,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrganizationNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("repository: scan organization: %w", err)
	}
	return org, nil
}

// ActiveOrganizationForDomain finds an active license for exactly this email
// domain. Subdomains are not covered: a license for ufl.edu does not apply
// to health.ufl.edu addresses.
func (r *AccountsRepository) ActiveOrganizationForDomain(ctx context.Context, domain string) (Organization, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	return scanOrganization(r.pool.QueryRow(
		ctx,
		`SELECT `+organizationColumns+` FROM organizations
		  WHERE status IN ('active', 'trialing', 'past_due')
		    AND email_domain = $1
		  LIMIT 1`,
		domain,
	))
}

func (r *AccountsRepository) OrganizationByOwner(ctx context.Context, userID string) (Organization, error) {
	return scanOrganization(r.pool.QueryRow(
		ctx,
		`SELECT `+organizationColumns+` FROM organizations WHERE owner_user_id = $1 ORDER BY created_at DESC LIMIT 1`,
		userID,
	))
}

// UpsertOrganizationForOwner creates the organization for a domain, or
// returns it if this owner already started one. A domain owned by someone
// else returns ErrDomainAlreadyLicensed.
func (r *AccountsRepository) UpsertOrganizationForOwner(ctx context.Context, name, domain, ownerUserID string) (Organization, error) {
	org, err := scanOrganization(r.pool.QueryRow(
		ctx,
		`INSERT INTO organizations (name, email_domain, owner_user_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (email_domain) DO UPDATE
		   SET name = CASE WHEN organizations.owner_user_id = EXCLUDED.owner_user_id
		                   THEN EXCLUDED.name ELSE organizations.name END,
		       updated_at = NOW()
		 RETURNING `+organizationColumns,
		strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(domain)), ownerUserID,
	))
	if err != nil {
		return Organization{}, err
	}
	if org.OwnerUserID != ownerUserID {
		return org, ErrDomainAlreadyLicensed
	}
	return org, nil
}

func (r *AccountsRepository) AttachOrganizationCustomer(ctx context.Context, orgID, customerID string) error {
	_, err := r.pool.Exec(
		ctx,
		`UPDATE organizations SET stripe_customer_id = COALESCE(stripe_customer_id, $2), updated_at = NOW() WHERE id = $1`,
		orgID, customerID,
	)
	return err
}

// ApplyOrganizationSubscription stores Stripe's latest state for a school
// license, keyed by the organization's Stripe customer.
func (r *AccountsRepository) ApplyOrganizationSubscription(ctx context.Context, update StripeSubscriptionUpdate) error {
	tag, err := r.pool.Exec(
		ctx,
		`UPDATE organizations SET
			stripe_subscription_id = NULLIF($2, ''),
			status = $3,
			current_period_end = $4,
			cancel_at_period_end = $5,
			updated_at = NOW()
		  WHERE stripe_customer_id = $1`,
		update.StripeCustomerID, update.StripeSubscriptionID, update.Status,
		update.CurrentPeriodEnd, update.CancelAtPeriodEnd,
	)
	if err != nil {
		return fmt.Errorf("repository: apply organization subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOrganizationNotFound
	}
	return nil
}
