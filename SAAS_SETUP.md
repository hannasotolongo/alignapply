# CareerStore web SaaS: setup

The Go server serves both the API and the web app (`internal/web/static`,
embedded into the binary). One deploy, one origin, no CORS.

## What's included

| Area | What it does |
|---|---|
| Web auth | Email + password sign-up and login (`/api/v1/auth/signup`, `/login`, `/logout`). PBKDF2-SHA256 hashes, HttpOnly `SameSite=Lax` session cookie, 10 attempts per IP per 15 minutes. Bearer tokens still work for API clients. |
| Plans | No free plan. **Pro Monthly** $14.99 with a 3-day free trial (card required, one trial per account). **Job Search Sprint** $39 one-time for 90 days. **School License** $1,000/year: every user who confirms an email at exactly that domain gets Pro. Profile and résumé setup work without a plan; job matching, résumé recommendations and Gmail need one. |
| Billing | Stripe Checkout (subscribe), Stripe Billing Portal (change plan, update card, cancel), signed + idempotent webhooks. Deleting an account cancels its subscription immediately. |
| Enforcement | All checks are server-side. `/job-matches` now requires sign-in and is metered; `/resume-recommendation` and all Gmail endpoints require Pro (HTTP 402 `pro_required` otherwise). |
| Web app | Landing + pricing, sign up / sign in, 4-step onboarding (résumé → about you → preferences → ready), Find jobs with a résumé picker, job detail with full description, My jobs board (Saved / Applied / Interview / Offer / Closed) with timelines, résumé library (PDFs read in the browser with pdf.js; only text is stored), career profile with résumé prefill, Pro best-résumé recommendation, Pro Gmail sync, Plan & billing. |

## 1. Database migrations

Automatic. On startup the server applies any migration in `migrations/` that
isn't recorded in `schema_migrations`. A database set up by hand before this
existed is detected and recorded rather than re-run. A Postgres advisory lock
keeps parallel instances from migrating at the same time.

New service? Render → **New → Blueprint** with this repo uses `render.yaml`
to create the web service and database together.

## 2. Create the Stripe products

In the Stripe dashboard (start in **test mode**):

1. **Product catalog → Add product**, three prices:
   - "CareerStore Pro Monthly": **recurring** $14.99 every 1 month → `STRIPE_PRICE_PRO_MONTHLY` (the 3-day trial is added by CareerStore at checkout)
   - "Job Search Sprint": **one-time** $39 → `STRIPE_PRICE_SPRINT`
   - "CareerStore School License": **recurring** $1,000 every 1 year → `STRIPE_PRICE_SCHOOL`
2. **Settings → Billing → Customer portal**: turn on cancel, update payment
   method, and switching between the two prices.
3. **Developers → Webhooks → Add endpoint**
   - URL: `https://YOUR_DOMAIN/api/v1/billing/webhook`
   - Events: `checkout.session.completed`, `checkout.session.async_payment_succeeded`,
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
| `STRIPE_PRICE_PRO_MONTHLY` | at least one | recurring monthly `price_…` |
| `STRIPE_PRICE_SPRINT` | at least one | one-time `price_…` |
| `STRIPE_PRICE_SCHOOL` | at least one | recurring yearly `price_…` |
| `PRO_MONTHLY_DISPLAY_PRICE` / `SPRINT_DISPLAY_PRICE` / `SCHOOL_DISPLAY_PRICE` | no | defaults `$14.99` / `$39` / `$1,000` (display only; Stripe charges the price ID) |
| `RESEND_API_KEY` + `EMAIL_FROM` | for email | Resend API key, and a sender like `CareerStore <hello@yourdomain.com>` on a domain verified in Resend. Enables email confirmation, password reset, and school licenses |
| `SUPPORT_EMAIL` | yes | shown on Terms, Privacy, Refunds and Contact pages |
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

## Gmail sync (Pro, optional)

Gmail sync is hidden in the web app until Google OAuth is configured.

1. Google Cloud Console → create an OAuth client (type **Web application**).
2. Authorized redirect URI: `https://YOUR_DOMAIN/api/v1/email/gmail/callback`
3. Set `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and
   `GOOGLE_REDIRECT_URL` (the URI above) on the server.
4. `gmail.readonly` is a **restricted** Google scope. Before the public can
   use it, Google requires app verification and an annual third-party
   security assessment. Until then it works only for up to 100 test users
   you add on the OAuth consent screen.

After connecting, Google sends the user back to **My jobs**.
