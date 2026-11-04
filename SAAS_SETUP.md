# AlignApply web SaaS: setup

The Go server serves both the API and the web app (`internal/web/static`,
embedded into the binary). One deploy, one origin, no CORS.

## What's included

| Area | What it does |
|---|---|
| Web auth | Email + password sign-up and login (`/api/v1/auth/signup`, `/login`, `/logout`). PBKDF2-SHA256 hashes, HttpOnly `SameSite=Lax` session cookie, 10 attempts per IP per 15 minutes. Bearer tokens still work for API clients. |
| Plans | **Free**: 3 job-match searches per rolling week, career profile, application tracker. **Pro**: unlimited searches (30/hour fair-use ceiling), resume recommendation, Gmail sync. |
| Billing | Stripe Checkout (subscribe), Stripe Billing Portal (change plan, update card, cancel), signed + idempotent webhooks. Deleting an account cancels its subscription immediately. |
| Enforcement | All checks are server-side. `/job-matches` now requires sign-in and is metered; `/resume-recommendation` and all Gmail endpoints require Pro (HTTP 402 `pro_required` otherwise). |
| Web app | Landing + pricing, sign up / sign in, Find jobs, Applications board with timeline, Career profile editor, Plan & billing. |

## 1. Database migrations

Automatic. On startup the server applies any migration in `migrations/` that
isn't recorded in `schema_migrations`. A database set up by hand before this
existed is detected and recorded rather than re-run. A Postgres advisory lock
keeps parallel instances from migrating at the same time.

New service? Render → **New → Blueprint** with this repo uses `render.yaml`
to create the web service and database together.

## 2. Create the Stripe products

In the Stripe dashboard (start in **test mode**):

1. **Product catalog → Add product** "AlignApply Pro" with two recurring prices:
   - $2.99 every 1 month → copy its `price_…` ID
   - $14.99 every 6 months → copy its `price_…` ID
2. **Settings → Billing → Customer portal**: turn on cancel, update payment
   method, and switching between the two prices.
3. **Developers → Webhooks → Add endpoint**
   - URL: `https://YOUR_DOMAIN/api/v1/billing/webhook`
   - Events: `checkout.session.completed`,
     `customer.subscription.created`, `customer.subscription.updated`,
     `customer.subscription.deleted`, `customer.subscription.paused`,
     `customer.subscription.resumed`
   - Copy the signing secret (`whsec_…`).

## 3. Environment variables

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | |
| `SESSION_SECRET` | yes | 32+ random characters |
| `APP_BASE_URL` | yes in prod | e.g. `https://alignapply.com`. Used for Stripe redirects; `https://` turns on `Secure` cookies |
| `STRIPE_SECRET_KEY` | for billing | `sk_test_…` / `sk_live_…`. Unset = billing off, everyone on Free |
| `STRIPE_WEBHOOK_SECRET` | with Stripe | `whsec_…` |
| `STRIPE_PRICE_PRO_MONTHLY` | at least one | `price_…` |
| `STRIPE_PRICE_PRO_SIX_MONTH` | at least one | `price_…` |
| `PRO_MONTHLY_DISPLAY_PRICE` | no | default `$2.99` (display only; Stripe charges the price ID) |
| `PRO_SIX_MONTH_DISPLAY_PRICE` | no | default `$14.99` |
| `TRUST_PROXY_HEADERS` | on Render | `true` so login rate limiting uses the real client IP |
| `OPENAI_API_KEY` | prod | matching/verification (falls back to local Ollama) |
| `APPLE_CLIENT_ID` | no | now optional; only needed for Sign in with Apple |

## 4. Test billing locally

```bash
stripe listen --forward-to localhost:8080/api/v1/billing/webhook
```

Use the `whsec_…` it prints as `STRIPE_WEBHOOK_SECRET`, start the server with
test keys, subscribe from **Plan & billing**, and pay with card
`4242 4242 4242 4242`.

## Tests

```bash
go test ./...
```

`internal/api/saas_integration_test.go` runs the whole flow against local
PostgreSQL and a fake Stripe server: sign-up, cookie session, free quota,
Pro gate, checkout, webhook signature and replay, cancellation, job tracking,
and account deletion.
