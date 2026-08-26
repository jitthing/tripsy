-- Add lease tracking and progress visibility to reservation imports so a
-- stalled "processing" row can be recovered and users can see what stage a
-- job is in. Mirrors the calendar_sync_jobs lease pattern (locked_at/attempts).
alter table public.reservation_imports
  add column if not exists locked_at timestamptz,
  add column if not exists attempts integer not null default 0,
  add column if not exists stage text not null default '';

-- Rows already stranded in 'processing' before this migration have a NULL
-- locked_at, which the reclaim query never matches. Backdate them so the next
-- worker tick immediately requeues (or fails) them instead of leaving them
-- stuck forever.
update public.reservation_imports
  set locked_at = now() - interval '10 minutes'
  where status = 'processing' and locked_at is null;

create index if not exists reservation_imports_processing_idx
  on public.reservation_imports (status, locked_at)
  where status = 'processing';
