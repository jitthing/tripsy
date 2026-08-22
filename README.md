# Waypoint

Waypoint is a private, mobile-first travel organiser for a friend group. The frontend is React + Vite, the API is Go, and Supabase provides Google authentication, PostgreSQL, and private document storage.

## Run locally

1. Create a Supabase project and run the SQL files in [supabase/migrations](/Users/jittair/Documents/ChatGPT/exchange/supabase/migrations/202608100001_waypoint.sql) in filename order. The second migration adds the route comparison board without changing existing trips.
2. In Supabase Auth, enable Google and set its Google Cloud callback to `https://<project-ref>.supabase.co/auth/v1/callback`. Add `http://localhost:5173` to Supabase Auth redirect URLs.
3. Copy `.env.example` to `.env.local` and set the public Supabase URL, anon key, and local API URL.
4. Copy `backend/.env.example` to `backend/.env` and provide the direct Supabase Postgres connection URL and Supabase URL. Do not expose `DATABASE_URL` to the browser.
5. Start the API with `cd backend && set -a && source .env && set +a && go run ./cmd/api`.
6. In another terminal, run `npm install && npm run dev`.

Use Node.js 22 or later for development and deployment.

## Deploy

1. Deploy the API using [render.yaml](/Users/jittair/Documents/ChatGPT/exchange/render.yaml), or deploy `backend/Dockerfile` to any container host. Set `DATABASE_URL`, `SUPABASE_URL`, and `CORS_ORIGINS=https://<your-frontend-domain>`.
   The API serves `/health` and `/v1/*`. If a proxy mounts it under a prefix and forwards the path unchanged — for example `https://<domain>/api/v1/trips` — set `API_BASE_PATH=/api` so the prefix is stripped before routing; without it the router has no matching route and returns a plain-text `404 page not found`. Requests on the bare path keep working either way, so the health check needs no change.
2. Deploy this repository root to Vercel. Set `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`, and `VITE_API_URL=https://<your-api-domain>` at build time. [vercel.json](/Users/jittair/Documents/ChatGPT/exchange/vercel.json) serves the SPA for all client routes.
   The app uses real paths (`/trip/<id>/plans`, `/inbox`, `/search`), so the host must return `index.html` for any unmatched path. After deploying, load a deep link such as `https://<your-frontend-domain>/inbox` directly in a fresh tab; a 404 there means the SPA fallback is not configured.
3. Add the production frontend origin to Supabase Auth redirect URLs and update `CORS_ORIGINS` with that exact origin.
4. Have each friend sign in once before an owner adds them by email. The current v1 deliberately avoids sending invitation emails.

## Reservation Imports and Calendar

- Configure a Resend receiving webhook for `POST https://<api-domain>/webhooks/resend/email-received` and the `email.received` event. The webhook endpoint validates the Svix signature before it accepts a job.
- Set `RESEND_INBOUND_DOMAIN` to the receiving domain. The recommended mode uses one central `RESEND_INBOUND_ADDRESS` and `RESEND_INBOUND_OWNER_ID`; forwarded emails appear in the owner's Inbox for review. Legacy per-trip `imports-<token>@domain` addresses remain supported.
- Set `SUPABASE_SERVICE_ROLE_KEY` only on the API. It stores raw email source and attachments in the private `trip-imports` bucket; never expose it to the browser.
- When the extractor cannot run — no `OPENROUTER_*` variables, an API error, or an email with no readable text — the import still completes with a low-confidence keyword draft. The reason is logged at error level and stored on the import, and the Inbox shows an "AI extraction didn't run" warning above the draft. A silent fallback is a bug, not a normal outcome.
- PDF text extraction is available through Poppler in the API container. The image-OCR runtime is installed for future scanned-document extraction; unsupported or low-confidence source material remains a review draft.
- Create a separate Google OAuth web client. Add `https://<api-domain>/calendar/callback` as its authorized redirect URI, then set the five `GOOGLE_*` server variables. Waypoint creates and syncs a dedicated calendar, not the user's existing personal calendars.

## Security model

- Google OAuth is performed by Supabase Auth; the React app receives only the public anon key and a short-lived user JWT.
- The Go API validates each Bearer token against the Supabase JWKS and checks membership before accessing trip data.
- `trip-documents` is a private bucket. Its RLS policies allow only members of the UUID-named trip folder to upload, list, and retrieve files.
- Files are restricted to PDF, JPEG, PNG, and WebP, with a 10 MB maximum. Files are retrieved with a 60-second signed URL.
- Route comparisons are user-entered notes and saved links. Waypoint does not scrape transport providers or submit bookings.
- Never put the Supabase service role key, database URL, Google client secret, or OAuth secret in `VITE_*` variables.

## Verification

```bash
npm run build
(cd backend && go test ./... && go build ./...)
```
