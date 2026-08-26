-- Add lease tracking and progress visibility to reservation imports so a
-- stalled "processing" row can be recovered and users can see what stage a
-- job is in. Mirrors the calendar_sync_jobs lease pattern (locked_at/attempts).
alter table public.reservation_imports
  add column if not exists locked_at timestamptz,
  add column if not exists attempts integer not null default 0,
  add column if not exists stage text not null default '';

create index if not exists reservation_imports_processing_idx
  on public.reservation_imports (status, locked_at)
  where status = 'processing';
