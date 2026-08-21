-- Record why an import fell back to keyword extraction so the failure is visible
-- in the Inbox instead of looking like an email with no reservations in it.
alter table public.reservation_imports
  add column if not exists extraction_error text not null default '';
