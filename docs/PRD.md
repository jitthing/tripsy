# Waypoint PRD

## Product summary

Waypoint is a private travel organiser for a small group of friends who take frequent international trips. It replaces the fragmented pre-trip workflow of booking-email searches, document screenshots, shared notes, and chat reminders with one shared trip space.

## Problem

Frequent student travellers repeatedly need to answer the same operational questions: what is booked, when do we need to leave, who has the confirmation, and are our required documents ready? Existing itinerary apps solve part of this but do not give a friend group a small, shared, privacy-conscious place to coordinate readiness.

## Users

Primary: international exchange students and their travel friends, organising several short trips per term.

Secondary: the person in a friend group who normally coordinates bookings and reminders.

## Goals

- Let a signed-in user create and share a trip with friends.
- Give every member a chronological itinerary with bookings, activities, and transport.
- Keep sensitive documents in a private travel wallet and make retrieval quick.
- Surface incomplete pre-trip tasks clearly.
- Be safe for a small production deployment: Google sign-in, row-level access control, private storage, audit-friendly metadata, and no service keys in the browser.

## Non-goals for v1

- Automatic email import, booking parsing, or inbox access.
- Visa eligibility advice, live flight status, map routing, expense splitting, or chat.
- Public trip links or anonymous access.
- Native iOS/Android apps; the responsive web app is installable as a future PWA enhancement.

## User stories

| ID | Story | Acceptance criteria |
| --- | --- | --- |
| US-01 | As a traveller, I can sign in with Google so I do not need another password. | Google OAuth returns to the app; my profile exists; unauthenticated routes show sign-in. |
| US-02 | As a trip owner, I can create a trip with a destination and dates. | The new trip appears in my trip list and opens as my active trip. |
| US-03 | As an owner, I can invite a friend who has an account by email. | The friend sees the trip after refresh; duplicate membership is rejected. |
| US-04 | As a member, I can see a shared timeline of flights, stays, transport, and activities. | Plans are ordered by start time and display title, type, time, and location. |
| US-05 | As a member, I can add, edit, and remove my own itinerary plans. | Changes persist and are visible to other members after reload. |
| US-06 | As a traveller, I can mark pre-trip items as complete. | Completion persists and the readiness count updates. |
| US-07 | As a member, I can upload a booking confirmation or travel document. | A PDF, image, or text document is stored in the private `trip-documents` bucket and metadata is shown in the wallet. |
| US-08 | As a member, I can retrieve a document safely. | The app requests a short-lived signed URL only for a trip I can access. |
| US-09 | As a traveller, I can use the app on a phone without a desktop-specific workflow. | Main navigation and all core actions work at 390px viewport width. |
| US-10 | As a traveller, I can log and compare transport routes I researched myself. | I can save direct-flight, flight-plus-train, train-only, bus, or custom options with price, duration, transfers, link, notes, and shortlist status. |

## Functional requirements

- Google OAuth is provided by Supabase Auth. The SPA receives a Supabase JWT and passes it as a Bearer token to the Go API.
- The Go API verifies the JWT against Supabase's JWKS before every protected request.
- Database access is scoped by authenticated user membership in the Go API; Supabase RLS also protects direct database/storage access.
- The `trip-documents` bucket is private. Browser uploads use the user session and a `tripId/documentId` object key; the Go API records document metadata after upload.
- Owner-only actions: update/delete a trip, invite or remove members. Members can create and modify plans, checklist items, and document metadata.
- Route options are a comparison log, not a booking search tool. A trip member may save options they researched; only the author can edit or remove their own option.
- File restrictions: PDF, JPEG, PNG, and WebP only; configure the bucket to a 10 MB maximum in Supabase Dashboard.

## Success metrics

- At least 80% of invited friends sign in before a trip.
- Every trip has at least one itinerary item and 75% of expected checklist tasks completed before departure.
- A traveller retrieves a document in under 10 seconds without searching their inbox.

## Technical architecture

```text
React + Vite SPA
  | Supabase Auth (Google OAuth) and private Storage upload/signed download
  | Bearer Supabase JWT
Go API (chi + pgx)
  | validates JWT against Supabase JWKS
  | PostgreSQL connection
Supabase Postgres + RLS + private Storage
```

## Deployment

- Frontend: any static host such as Vercel, Netlify, or Cloudflare Pages.
- API: a container platform such as Fly.io, Render, Railway, or Google Cloud Run.
- Database/auth/storage: Supabase managed project.
- Required secrets: `DATABASE_URL`, `SUPABASE_URL`, `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`, Google OAuth client ID/secret configured only in Supabase, and `VITE_API_URL`.
- Set Google OAuth authorized redirect URI to `https://<project-ref>.supabase.co/auth/v1/callback`; add each deployed frontend URL under Supabase Auth redirect URLs.

## Launch checklist

- Apply the SQL migration in Supabase.
- Create a private `trip-documents` bucket with the 10 MB restriction.
- Enable Google provider in Supabase Auth and add its callback URI in Google Cloud Console.
- Configure production origins in `CORS_ORIGINS` and frontend URL in Supabase Auth.
- Deploy the API and frontend, then test a two-account shared-trip and document-download flow.
